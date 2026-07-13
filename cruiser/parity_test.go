package cruiser_test

import (
	"bytes"
	"cmp"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/butaosuinu/godep-cruiser/config"
	"github.com/butaosuinu/godep-cruiser/cruiser"
)

const parityFixtureRoot = "../testdata/parity/fanout-snapshot"

type parityOracle struct {
	Source paritySource  `json:"source"`
	Checks []parityCheck `json:"checks"`
}

type paritySource struct {
	Repository string `json:"repository"`
	Commit     string `json:"commit"`
	Blob       string `json:"blob"`
	Path       string `json:"path"`
}

type parityCheck struct {
	Name         string              `json:"name"`
	Findings     []parityFinding     `json:"findings"`
	ExpectedGaps []parityExpectedGap `json:"expectedGaps,omitempty"`
}

type parityFinding struct {
	Kind     string `json:"kind"`
	Severity string `json:"severity,omitempty"`
	Subject  string `json:"subject,omitempty"`
	From     string `json:"from,omitempty"`
	Line     int    `json:"line,omitempty"`
	To       string `json:"to,omitempty"`
}

type parityExpectedGap struct {
	Kind     string `json:"kind"`
	Severity string `json:"severity,omitempty"`
	Subject  string `json:"subject,omitempty"`
	From     string `json:"from,omitempty"`
	Line     int    `json:"line,omitempty"`
	To       string `json:"to,omitempty"`
	Reason   string `json:"reason"`
}

type parityRunner struct {
	name string
	run  func(*testing.T, string, parityCheck) []parityFinding
}

type parityUnexpectedGap struct {
	check   string
	kind    string
	finding parityFinding
	reason  string
}

func TestFanoutArchitectureParity(t *testing.T) {
	t.Parallel()

	fixtureRoot := filepath.Clean(parityFixtureRoot)
	oracle := loadParityOracle(t, filepath.Join(fixtureRoot, "oracle.golden.json"))
	runners := []parityRunner{
		{name: "TestAllPackagesClassified", run: runAllPackagesClassified},
		{name: "TestInternalTreeShape", run: runInternalTreeShape},
		{name: "TestExplicitLayerMapIsCurrent", run: runExplicitLayerMapIsCurrent},
		{name: "TestLayerImportDirection", run: runLayerImportDirection},
		{
			name: "TestCorePurity",
			run:  configuredParityRunner("core-purity.json"),
		},
		{
			name: "TestToolsStdlibOnly",
			run:  configuredParityRunner("tools-stdlib-only.json"),
		},
		{
			name: "TestPackageMainOnlyInCmd",
			run:  configuredParityRunner("package-main-only-in-cmd.json"),
		},
		{name: "TestScanSanity", run: runScanSanity},
	}

	matchedChecks := 0
	expectedGapCount := 0
	unexpectedGaps := make([]parityUnexpectedGap, 0)
	for index, runner := range runners {
		check := oracle.Checks[index]
		if runner.name != check.Name {
			t.Fatalf("runner[%d].name = %q, want oracle check %q", index, runner.name, check.Name)
		}
		var actual []parityFinding
		if ok := t.Run(runner.name, func(t *testing.T) {
			actual = canonicalParityFindings(runner.run(t, fixtureRoot, check))
		}); !ok {
			return
		}

		unexpected := compareParityFindings(check, actual)
		unexpectedGaps = append(unexpectedGaps, unexpected...)
		if len(unexpected) == 0 && len(check.ExpectedGaps) == 0 {
			matchedChecks++
		}
		for _, gap := range check.ExpectedGaps {
			if !slices.Contains(actual, gap.finding()) {
				expectedGapCount++
			}
		}
	}

	for _, gap := range unexpectedGaps {
		t.Errorf(
			"%s: %s finding %+v%s",
			gap.check,
			gap.kind,
			gap.finding,
			formatParityReason(gap.reason),
		)
	}
	if len(unexpectedGaps) != 0 {
		t.Fatalf(
			"matches=%d expected_gaps=%d unexpected_gaps=%d",
			matchedChecks,
			expectedGapCount,
			len(unexpectedGaps),
		)
	}
	if matchedChecks != 7 || expectedGapCount != 1 {
		t.Fatalf(
			"matches=%d expected_gaps=%d unexpected_gaps=0, want matches=7 expected_gaps=1 unexpected_gaps=0",
			matchedChecks,
			expectedGapCount,
		)
	}

	t.Logf("matches=%d expected_gaps=%d unexpected_gaps=0", matchedChecks, expectedGapCount)
}

func configuredParityRunner(configName string) func(*testing.T, string, parityCheck) []parityFinding {
	return func(t *testing.T, fixtureRoot string, _ parityCheck) []parityFinding {
		t.Helper()

		return runConfiguredParity(t, fixtureRoot, configName)
	}
}

func runAllPackagesClassified(t *testing.T, fixtureRoot string, _ parityCheck) []parityFinding {
	t.Helper()

	result := validateParityConfig(t, fixtureRoot, "all-packages-classified.json", nil)
	if len(result.Known) != 0 || len(result.Stale) != 0 {
		t.Fatalf("classification result has known=%d stale=%d, want both zero", len(result.Known), len(result.Stale))
	}

	findings := make([]parityFinding, 0, len(result.Violations))
	for _, violation := range result.Violations {
		if violation.Kind != cruiser.ViolationKindNotAllowed || violation.Rule != cruiser.NotInAllowedRuleName {
			t.Fatalf("classification violation = %+v, want an allowed-miss violation", violation)
		}

		subjects := make([]string, 0, 2)
		sourcePackage := path.Dir(violation.From.Path)
		if !isClassifiedFanoutPackage(sourcePackage) {
			subjects = append(subjects, sourcePackage)
		}
		if violation.To != nil &&
			violation.To.Type == config.DependencyTypeLocal &&
			!isClassifiedFanoutPackage(violation.To.Path) {
			subjects = append(subjects, violation.To.Path)
		}
		if len(subjects) == 0 {
			t.Fatalf("allowed miss %+v does not identify an unclassified local package", violation)
		}
		for _, subject := range subjects {
			findings = append(findings, parityFinding{Kind: "unclassified-package", Subject: subject})
		}
	}

	return canonicalParityFindings(findings)
}

func runInternalTreeShape(t *testing.T, fixtureRoot string, check parityCheck) []parityFinding {
	t.Helper()

	if len(check.ExpectedGaps) != 1 {
		t.Fatalf("expected gap count = %d, want 1", len(check.ExpectedGaps))
	}
	gap := check.ExpectedGaps[0]
	if gap.Kind != "directory-shape" || path.Dir(gap.Subject) != "internal" {
		t.Fatalf("directory-shape gap = %+v, want a direct child of internal", gap)
	}

	directory := filepath.Join(fixtureRoot, filepath.FromSlash(gap.Subject))
	info, err := os.Stat(directory)
	if err != nil {
		t.Fatalf("stat expected-gap directory %q: %v", directory, err)
	}
	if !info.IsDir() {
		t.Fatalf("expected-gap path %q is not a directory", directory)
	}
	nonGoFiles := 0
	err = filepath.WalkDir(directory, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if strings.HasSuffix(entry.Name(), ".go") {
			return fmt.Errorf("expected non-Go-only directory contains %s", filename)
		}
		nonGoFiles++

		return nil
	})
	if err != nil {
		t.Fatalf("inspect expected-gap directory %q: %v", directory, err)
	}
	if nonGoFiles == 0 {
		t.Fatalf("expected-gap directory %q contains no fixture file", directory)
	}

	entries, err := os.ReadDir(filepath.Join(fixtureRoot, "internal"))
	if err != nil {
		t.Fatalf("read fixture internal tree: %v", err)
	}
	for _, entry := range entries {
		if slices.Contains([]string{"app", "arch", "core", "infra", "ui"}, entry.Name()) ||
			strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		subject := path.Join("internal", entry.Name())
		if subject != gap.Subject {
			t.Fatalf("unrecorded TestInternalTreeShape oracle finding %q", subject)
		}
	}

	return nil
}

func runExplicitLayerMapIsCurrent(t *testing.T, fixtureRoot string, _ parityCheck) []parityFinding {
	t.Helper()

	validateNonEmptyScanRoot(t, fixtureRoot, filepath.Join("internal", "arch"))

	return nil
}

func runLayerImportDirection(t *testing.T, fixtureRoot string, _ parityCheck) []parityFinding {
	t.Helper()

	baseline, err := cruiser.LoadBaselineFile(filepath.Join(fixtureRoot, "baseline.json"))
	if err != nil {
		t.Fatalf("cruiser.LoadBaselineFile() error = %v", err)
	}
	result := validateParityConfig(t, fixtureRoot, "layer-import-direction.json", &baseline)
	modulePath := readParityModulePath(t, filepath.Join(fixtureRoot, "go.mod"))

	findings := make([]parityFinding, 0, len(result.Violations)+len(result.Known)+len(result.Stale))
	for _, violation := range result.Violations {
		findings = append(findings, projectParityViolation(t, violation, "forbidden"))
	}
	for _, violation := range result.Known {
		findings = append(findings, projectParityViolation(t, violation, "known-baseline"))
	}
	for _, stale := range result.Stale {
		finding := parityFinding{
			Kind:     "stale-baseline",
			Severity: string(config.SeverityError),
			From:     stale.Entry.From,
		}
		if stale.Entry.To != nil {
			finding.To = normalizeParityImport(modulePath, *stale.Entry.To)
		}
		findings = append(findings, finding)
	}

	return canonicalParityFindings(findings)
}

func runConfiguredParity(t *testing.T, fixtureRoot, configName string) []parityFinding {
	t.Helper()

	result := validateParityConfig(t, fixtureRoot, configName, nil)
	if len(result.Known) != 0 || len(result.Stale) != 0 {
		t.Fatalf("configured result has known=%d stale=%d, want both zero", len(result.Known), len(result.Stale))
	}
	findings := make([]parityFinding, 0, len(result.Violations))
	for _, violation := range result.Violations {
		findings = append(findings, projectParityViolation(t, violation, "forbidden"))
	}

	return canonicalParityFindings(findings)
}

func runScanSanity(t *testing.T, fixtureRoot string, _ parityCheck) []parityFinding {
	t.Helper()

	for _, scanRoot := range []string{"internal", "cmd", "tools"} {
		validateNonEmptyScanRoot(t, fixtureRoot, scanRoot)
	}
	modulePath := readParityModulePath(t, filepath.Join(fixtureRoot, "go.mod"))
	if modulePath == "" || strings.ContainsAny(modulePath, " \t\"") {
		t.Fatalf("fixture module path = %q, want a plausible path", modulePath)
	}

	return nil
}

func validateParityConfig(
	t *testing.T,
	fixtureRoot string,
	configName string,
	baseline *cruiser.Baseline,
) cruiser.Result {
	t.Helper()

	configuration, err := config.LoadFile(filepath.Join(fixtureRoot, "configs", configName))
	if err != nil {
		t.Fatalf("config.LoadFile(%q) error = %v", configName, err)
	}
	result, err := cruiser.Validate(configuration, cruiser.Options{
		ScanRoot:  fixtureRoot,
		GoModPath: filepath.Join(fixtureRoot, "go.mod"),
		Baseline:  baseline,
	})
	if err != nil {
		t.Fatalf("cruiser.Validate(%q) error = %v", configName, err)
	}

	return result
}

func validateNonEmptyScanRoot(t *testing.T, fixtureRoot, relativeRoot string) {
	t.Helper()

	result, err := cruiser.Validate(&config.Config{}, cruiser.Options{
		ScanRoot:  filepath.Join(fixtureRoot, relativeRoot),
		GoModPath: filepath.Join(fixtureRoot, "go.mod"),
	})
	if err != nil {
		t.Fatalf("cruiser.Validate(scan root %q) error = %v", relativeRoot, err)
	}
	if len(result.Violations) != 0 || len(result.Known) != 0 || len(result.Stale) != 0 {
		t.Fatalf("empty-config result for %q = %+v, want no findings", relativeRoot, result)
	}
}

func projectParityViolation(t *testing.T, violation cruiser.Violation, kind string) parityFinding {
	t.Helper()

	if violation.Kind != cruiser.ViolationKindForbidden {
		t.Fatalf("%s violation kind = %q, want %q", kind, violation.Kind, cruiser.ViolationKindForbidden)
	}
	finding := parityFinding{
		Kind:     kind,
		Severity: string(violation.Severity),
		From:     violation.From.Path,
		Line:     violation.From.Line,
	}
	if violation.To != nil {
		finding.To = violation.To.Path
	}

	return finding
}

func isClassifiedFanoutPackage(packagePath string) bool {
	for _, prefix := range []string{
		"internal/core/",
		"internal/app/",
		"internal/infra/",
		"internal/ui/",
		"cmd/",
		"tools/",
	} {
		if strings.HasPrefix(packagePath, prefix) {
			return true
		}
	}

	return packagePath == "internal/arch" || strings.HasPrefix(packagePath, "internal/arch/")
}

func loadParityOracle(t *testing.T, filename string) parityOracle {
	t.Helper()

	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v", filename, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var oracle parityOracle
	if err := decoder.Decode(&oracle); err != nil {
		t.Fatalf("decode parity oracle %q: %v", filename, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		t.Fatalf("decode trailing parity oracle %q: %v", filename, err)
	}
	if err := validateParityOracle(oracle); err != nil {
		t.Fatalf("validate parity oracle %q: %v", filename, err)
	}

	return oracle
}

func validateParityOracle(oracle parityOracle) error {
	wantSource := paritySource{
		Repository: "https://github.com/butaosuinu/fanout",
		Commit:     "b20d79497896adc99b09387b0c9f262ab5730375",
		Blob:       "fe25b893e97fdf967001047b96d852b4f817c738",
		Path:       "internal/arch/arch_test.go",
	}
	if oracle.Source != wantSource {
		return fmt.Errorf("source = %+v, want %+v", oracle.Source, wantSource)
	}
	if !validParityObjectID(oracle.Source.Commit) || !validParityObjectID(oracle.Source.Blob) {
		return fmt.Errorf("source commit/blob must be 40-character Git object IDs")
	}

	wantNames := []string{
		"TestAllPackagesClassified",
		"TestInternalTreeShape",
		"TestExplicitLayerMapIsCurrent",
		"TestLayerImportDirection",
		"TestCorePurity",
		"TestToolsStdlibOnly",
		"TestPackageMainOnlyInCmd",
		"TestScanSanity",
	}
	if len(oracle.Checks) != len(wantNames) {
		return fmt.Errorf("check count = %d, want %d", len(oracle.Checks), len(wantNames))
	}

	expectedGapCount := 0
	for index, check := range oracle.Checks {
		if check.Name != wantNames[index] {
			return fmt.Errorf("checks[%d].name = %q, want %q", index, check.Name, wantNames[index])
		}
		if check.Findings == nil {
			return fmt.Errorf("checks[%d].findings must be an array", index)
		}
		if err := validateSortedParityFindings(check.Findings); err != nil {
			return fmt.Errorf("check %q findings: %w", check.Name, err)
		}

		seen := make(map[parityFinding]struct{}, len(check.Findings)+len(check.ExpectedGaps))
		for _, finding := range check.Findings {
			if err := validateParityFinding(finding); err != nil {
				return fmt.Errorf("check %q: %w", check.Name, err)
			}
			seen[finding] = struct{}{}
		}
		gapFindings := make([]parityFinding, 0, len(check.ExpectedGaps))
		for _, gap := range check.ExpectedGaps {
			finding := gap.finding()
			if gap.Kind != "directory-shape" || gap.Reason == "" {
				return fmt.Errorf("check %q expected gap = %+v, want directory-shape with reason", check.Name, gap)
			}
			if err := validateParityFinding(finding); err != nil {
				return fmt.Errorf("check %q expected gap: %w", check.Name, err)
			}
			if _, duplicate := seen[finding]; duplicate {
				return fmt.Errorf("check %q duplicates logical finding %+v", check.Name, finding)
			}
			seen[finding] = struct{}{}
			gapFindings = append(gapFindings, finding)
			expectedGapCount++
		}
		if err := validateSortedParityFindings(gapFindings); err != nil {
			return fmt.Errorf("check %q expected gaps: %w", check.Name, err)
		}
	}
	if expectedGapCount != 1 || len(oracle.Checks[1].ExpectedGaps) != 1 {
		return fmt.Errorf("expected gap count = %d, want one on TestInternalTreeShape", expectedGapCount)
	}

	return nil
}

func validateParityFinding(finding parityFinding) error {
	switch finding.Kind {
	case "forbidden", "known-baseline":
		if finding.Severity != string(config.SeverityError) ||
			finding.Subject != "" ||
			!validParityGoPath(finding.From) ||
			finding.Line < 1 {
			return fmt.Errorf("invalid %s finding %+v", finding.Kind, finding)
		}
	case "stale-baseline":
		if finding.Severity != string(config.SeverityError) ||
			finding.Subject != "" ||
			!validParityGoPath(finding.From) ||
			finding.Line != 0 {
			return fmt.Errorf("invalid stale-baseline finding %+v", finding)
		}
	case "unclassified-package", "directory-shape":
		if finding.Severity != "" ||
			!fs.ValidPath(finding.Subject) ||
			finding.Subject == "." ||
			finding.From != "" ||
			finding.Line != 0 ||
			finding.To != "" {
			return fmt.Errorf("invalid %s finding %+v", finding.Kind, finding)
		}
	default:
		return fmt.Errorf("unknown finding kind %q", finding.Kind)
	}

	return nil
}

func validateSortedParityFindings(findings []parityFinding) error {
	for index := range findings {
		if index == 0 {
			continue
		}
		comparison := compareParityFinding(findings[index-1], findings[index])
		if comparison == 0 {
			return fmt.Errorf("duplicate logical finding %+v", findings[index])
		}
		if comparison > 0 {
			return fmt.Errorf("findings are not sorted: %+v appears after %+v", findings[index], findings[index-1])
		}
	}

	return nil
}

func compareParityFindings(check parityCheck, actual []parityFinding) []parityUnexpectedGap {
	expected := make(map[parityFinding]struct{}, len(check.Findings))
	for _, finding := range check.Findings {
		expected[finding] = struct{}{}
	}
	expectedGaps := make(map[parityFinding]string, len(check.ExpectedGaps))
	for _, gap := range check.ExpectedGaps {
		expectedGaps[gap.finding()] = gap.Reason
	}
	actualSet := make(map[parityFinding]struct{}, len(actual))
	for _, finding := range actual {
		actualSet[finding] = struct{}{}
	}

	unexpected := make([]parityUnexpectedGap, 0)
	for _, finding := range check.Findings {
		if _, ok := actualSet[finding]; !ok {
			unexpected = append(unexpected, parityUnexpectedGap{
				check:   check.Name,
				kind:    "missing",
				finding: finding,
			})
		}
	}
	for _, finding := range actual {
		if _, ok := expected[finding]; ok {
			continue
		}
		if reason, wasGap := expectedGaps[finding]; wasGap {
			unexpected = append(unexpected, parityUnexpectedGap{
				check:   check.Name,
				kind:    "stale expected-gap",
				finding: finding,
				reason:  reason,
			})
			continue
		}
		unexpected = append(unexpected, parityUnexpectedGap{
			check:   check.Name,
			kind:    "extra",
			finding: finding,
		})
	}

	return unexpected
}

func canonicalParityFindings(findings []parityFinding) []parityFinding {
	unique := make(map[parityFinding]struct{}, len(findings))
	for _, finding := range findings {
		unique[finding] = struct{}{}
	}
	canonical := make([]parityFinding, 0, len(unique))
	for finding := range unique {
		canonical = append(canonical, finding)
	}
	slices.SortFunc(canonical, compareParityFinding)

	return canonical
}

func compareParityFinding(left, right parityFinding) int {
	if byKind := cmp.Compare(left.Kind, right.Kind); byKind != 0 {
		return byKind
	}
	if bySeverity := cmp.Compare(left.Severity, right.Severity); bySeverity != 0 {
		return bySeverity
	}
	if bySubject := cmp.Compare(left.Subject, right.Subject); bySubject != 0 {
		return bySubject
	}
	if byFrom := cmp.Compare(left.From, right.From); byFrom != 0 {
		return byFrom
	}
	if byLine := cmp.Compare(left.Line, right.Line); byLine != 0 {
		return byLine
	}

	return cmp.Compare(left.To, right.To)
}

func (gap parityExpectedGap) finding() parityFinding {
	return parityFinding{
		Kind:     gap.Kind,
		Severity: gap.Severity,
		Subject:  gap.Subject,
		From:     gap.From,
		Line:     gap.Line,
		To:       gap.To,
	}
}

func validParityObjectID(value string) bool {
	decoded, err := hex.DecodeString(value)

	return err == nil && len(decoded) == 20
}

func validParityGoPath(value string) bool {
	return fs.ValidPath(value) && value != "." && path.Ext(value) == ".go"
}

func readParityModulePath(t *testing.T, filename string) string {
	t.Helper()

	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v", filename, err)
	}
	for line := range strings.Lines(string(data)) {
		line, _, _ = strings.Cut(line, "//")
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "module" {
			return fields[1]
		}
	}
	t.Fatalf("go.mod %q has no module directive", filename)

	return ""
}

func normalizeParityImport(modulePath, importPath string) string {
	if importPath == modulePath {
		return "."
	}
	if relative, ok := strings.CutPrefix(importPath, modulePath+"/"); ok {
		return relative
	}

	return importPath
}

func formatParityReason(reason string) string {
	if reason == "" {
		return ""
	}

	return ": " + reason
}

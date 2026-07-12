// Package testcorpus loads the repository's engine-independent violation corpus.
package testcorpus

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

const goldenFilename = "violations.golden.json"

// Location identifies a module-relative source location.
type Location struct {
	Path string `json:"path"`
	Line int    `json:"line"`
}

// Dependency identifies an import edge's comparison target. Path is the
// normalized resolver path, or the raw source path when resolution has no path.
type Dependency struct {
	Path           string `json:"path"`
	DependencyType string `json:"dependencyType"`
}

// ExpectedViolation is the stable projection compared by future engine tests.
// To is absent for source-only violations such as orphan and package-name rules.
type ExpectedViolation struct {
	Rule     string      `json:"rule"`
	Severity string      `json:"severity"`
	From     Location    `json:"from"`
	To       *Dependency `json:"to,omitempty"`
}

// PositiveControl identifies source facts that must remain present but must not
// appear in the engine's violation output.
type PositiveControl struct {
	Rule        string      `json:"rule"`
	From        Location    `json:"from"`
	To          *Dependency `json:"to,omitempty"`
	PackageName string      `json:"packageName,omitempty"`
}

// Case describes one standalone fixture module and its expected violations.
type Case struct {
	ID               string              `json:"-"`
	Name             string              `json:"name"`
	ModuleDir        string              `json:"-"`
	PositiveControls []PositiveControl   `json:"positiveControls,omitempty"`
	Violations       []ExpectedViolation `json:"violations"`
}

// Load discovers and validates all fixture modules directly below root.
func Load(root string) ([]Case, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read corpus root: %w", err)
	}

	cases := make([]Case, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || strings.HasPrefix(entry.Name(), "_") {
			continue
		}

		moduleDir := filepath.Join(root, entry.Name())
		if err := requireFile(filepath.Join(moduleDir, "go.mod")); err != nil {
			return nil, fmt.Errorf("case %q: %w", entry.Name(), err)
		}

		fixture, err := decodeGolden(filepath.Join(moduleDir, goldenFilename))
		if err != nil {
			return nil, fmt.Errorf("case %q: %w", entry.Name(), err)
		}
		fixture.ID = entry.Name()
		fixture.ModuleDir = moduleDir
		if err := validateCase(fixture); err != nil {
			return nil, fmt.Errorf("case %q: %w", entry.Name(), err)
		}
		cases = append(cases, fixture)
	}

	if len(cases) == 0 {
		return nil, errors.New("corpus contains no fixture modules")
	}

	slices.SortFunc(cases, func(a, b Case) int { return strings.Compare(a.ID, b.ID) })
	names := make(map[string]struct{}, len(cases))
	for _, fixture := range cases {
		if _, ok := names[fixture.Name]; ok {
			return nil, fmt.Errorf("case name %q is duplicated", fixture.Name)
		}
		names[fixture.Name] = struct{}{}
	}

	return cases, nil
}

func requireFile(filename string) error {
	info, err := os.Stat(filename)
	if err != nil {
		return fmt.Errorf("stat %s: %w", filepath.Base(filename), err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", filepath.Base(filename))
	}
	return nil
}

func decodeGolden(filename string) (Case, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return Case{}, fmt.Errorf("read %s: %w", goldenFilename, err)
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var fixture Case
	if err := decoder.Decode(&fixture); err != nil {
		return Case{}, fmt.Errorf("decode %s: %w", goldenFilename, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Case{}, fmt.Errorf("decode %s: trailing JSON value", goldenFilename)
	}
	return fixture, nil
}

func validateCase(fixture Case) error {
	if !validCaseName(fixture.Name) {
		return errors.New("name must follow \"<rule family>: <expected behavior>\"")
	}
	if len(fixture.Violations) == 0 {
		return errors.New("violations must not be empty")
	}
	if err := validatePositiveControls(fixture); err != nil {
		return err
	}

	seen := make(map[string]struct{}, len(fixture.Violations))
	previousKey := ""
	for i, violation := range fixture.Violations {
		if err := validateViolation(fixture.ModuleDir, violation); err != nil {
			return fmt.Errorf("violations[%d]: %w", i, err)
		}
		key := violationKey(violation)
		if _, ok := seen[key]; ok {
			return fmt.Errorf("violations[%d] duplicates %q", i, key)
		}
		if i > 0 && key < previousKey {
			return fmt.Errorf("violations must be sorted; %q appears after %q", key, previousKey)
		}
		seen[key] = struct{}{}
		previousKey = key
	}
	return nil
}

func validatePositiveControls(fixture Case) error {
	seen := make(map[string]struct{}, len(fixture.PositiveControls))
	previousKey := ""
	for i, control := range fixture.PositiveControls {
		if err := validatePositiveControl(fixture.ModuleDir, control); err != nil {
			return fmt.Errorf("positiveControls[%d]: %w", i, err)
		}
		key := positiveControlKey(control)
		if _, ok := seen[key]; ok {
			return fmt.Errorf("positiveControls[%d] duplicates %q", i, key)
		}
		if i > 0 && key < previousKey {
			return fmt.Errorf("positiveControls must be sorted; %q appears after %q", key, previousKey)
		}
		seen[key] = struct{}{}
		previousKey = key
	}
	return nil
}

func validCaseName(name string) bool {
	family, behavior, ok := strings.Cut(name, ": ")
	return ok && family != "" && behavior != "" && !strings.Contains(behavior, ": ")
}

func validateViolation(moduleDir string, violation ExpectedViolation) error {
	if violation.Rule == "" {
		return errors.New("rule must not be empty")
	}
	if !validSeverity(violation.Severity) {
		return fmt.Errorf("severity %q is not error, warn, info, or ignore", violation.Severity)
	}
	if err := validateLocation(moduleDir, violation.From); err != nil {
		return err
	}
	if violation.To == nil {
		return nil
	}
	return validateDependency(*violation.To)
}

func validatePositiveControl(moduleDir string, control PositiveControl) error {
	if control.Rule == "" {
		return errors.New("rule must not be empty")
	}
	if err := validateLocation(moduleDir, control.From); err != nil {
		return err
	}
	hasDependency := control.To != nil
	hasPackageName := control.PackageName != ""
	if hasDependency == hasPackageName {
		return errors.New("exactly one of to or packageName must be set")
	}
	if hasDependency {
		return validateDependency(*control.To)
	}
	if !token.IsIdentifier(control.PackageName) {
		return fmt.Errorf("packageName %q is not a Go identifier", control.PackageName)
	}
	return nil
}

func validateLocation(moduleDir string, location Location) error {
	if !validGoPath(location.Path) {
		return fmt.Errorf("from.path %q is not a module-relative .go path", location.Path)
	}
	if location.Line < 1 {
		return errors.New("from.line must be positive")
	}
	if err := requireFile(filepath.Join(moduleDir, filepath.FromSlash(location.Path))); err != nil {
		return fmt.Errorf("from.path: %w", err)
	}
	return nil
}

func validateDependency(dependency Dependency) error {
	if dependency.Path == "" {
		return errors.New("to.path must not be empty")
	}
	if !validDependencyType(dependency.DependencyType) {
		return fmt.Errorf("to.dependencyType %q is not stdlib, local, module, or unresolved", dependency.DependencyType)
	}
	if dependency.DependencyType == "local" && dependency.Path != "." && !validRelativePath(dependency.Path) {
		return fmt.Errorf("local to.path %q is not module-relative", dependency.Path)
	}
	return nil
}

func validGoPath(filename string) bool {
	return validRelativePath(filename) && path.Ext(filename) == ".go"
}

func validRelativePath(filename string) bool {
	return fs.ValidPath(filename) && filename != "." && path.Clean(filename) == filename
}

func validSeverity(severity string) bool {
	switch severity {
	case "error", "warn", "info", "ignore":
		return true
	default:
		return false
	}
}

func validDependencyType(dependencyType string) bool {
	switch dependencyType {
	case "stdlib", "local", "module", "unresolved":
		return true
	default:
		return false
	}
}

func violationKey(violation ExpectedViolation) string {
	toPath := ""
	dependencyType := ""
	if violation.To != nil {
		toPath = violation.To.Path
		dependencyType = violation.To.DependencyType
	}
	return strings.Join([]string{
		violation.Rule,
		violation.Severity,
		violation.From.Path,
		fmt.Sprintf("%09d", violation.From.Line),
		toPath,
		dependencyType,
	}, "\x00")
}

func positiveControlKey(control PositiveControl) string {
	toPath := ""
	dependencyType := ""
	if control.To != nil {
		toPath = control.To.Path
		dependencyType = control.To.DependencyType
	}
	return strings.Join([]string{
		control.Rule,
		control.From.Path,
		fmt.Sprintf("%09d", control.From.Line),
		toPath,
		dependencyType,
		control.PackageName,
	}, "\x00")
}

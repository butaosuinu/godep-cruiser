package cruiser_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/butaosuinu/godep-cruiser/config"
	"github.com/butaosuinu/godep-cruiser/cruiser"
)

func TestValidateComposesScanRulesAndBaseline(t *testing.T) {
	t.Parallel()

	root := createModule(t)
	configuration := &config.Config{Forbidden: []config.ForbiddenRule{{
		Name:     "no-os",
		Severity: config.SeverityError,
		From:     config.From{Path: []string{`^app\.go$`}},
		To: config.To{
			Path:            []string{`^os$`},
			DependencyTypes: []config.DependencyType{config.DependencyTypeStdlib},
		},
	}}}
	to := "os"

	tests := []struct {
		name           string
		baseline       *cruiser.Baseline
		wantViolations int
		wantKnown      int
		wantStale      int
		wantErrors     int
	}{
		{
			name:           "without baseline reports the current violation",
			wantViolations: 1,
			wantErrors:     1,
		},
		{
			name: "exact baseline suppresses the current violation",
			baseline: &cruiser.Baseline{Entries: []cruiser.BaselineEntry{{
				Rule: "no-os",
				From: "app.go",
				To:   &to,
			}}},
			wantKnown: 1,
		},
		{
			name: "unmatched baseline is stale and current violation remains",
			baseline: &cruiser.Baseline{Entries: []cruiser.BaselineEntry{{
				Rule: "removed-rule",
				From: "old.go",
			}}},
			wantViolations: 1,
			wantStale:      1,
			wantErrors:     2,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result, err := cruiser.Validate(configuration, cruiser.Options{
				ScanRoot: root,
				Baseline: test.baseline,
			})
			if err != nil {
				t.Fatalf("cruiser.Validate() error = %v", err)
			}
			if len(result.Violations) != test.wantViolations ||
				len(result.Known) != test.wantKnown ||
				len(result.Stale) != test.wantStale {
				t.Errorf(
					"result counts = (%d violations, %d known, %d stale), want (%d, %d, %d)",
					len(result.Violations),
					len(result.Known),
					len(result.Stale),
					test.wantViolations,
					test.wantKnown,
					test.wantStale,
				)
			}
			if got := result.ErrorCount(); got != test.wantErrors {
				t.Errorf("Result.ErrorCount() = %d, want %d", got, test.wantErrors)
			}
		})
	}
}

func TestValidateReportsRequiredDependencyThroughPublicTypes(t *testing.T) {
	t.Parallel()

	configuration := &config.Config{Required: []config.RequiredRule{{
		Name:     "app-requires-fmt",
		Comment:  "import fmt",
		Severity: config.SeverityError,
		From:     config.From{Path: []string{`^app\.go$`}},
		To: config.To{
			Path:            []string{`^fmt$`},
			DependencyTypes: []config.DependencyType{config.DependencyTypeStdlib},
		},
	}}}

	result, err := cruiser.Validate(configuration, cruiser.Options{ScanRoot: createModule(t)})
	if err != nil {
		t.Fatalf("cruiser.Validate() error = %v", err)
	}
	if len(result.Violations) != 1 {
		t.Fatalf("Violations = %#v, want one required violation", result.Violations)
	}
	violation := result.Violations[0]
	if violation.Rule != "app-requires-fmt" ||
		violation.Comment != "import fmt" ||
		violation.Severity != config.SeverityError ||
		violation.Kind != cruiser.ViolationKindRequired ||
		violation.From.Path != "app.go" || violation.From.Line != 1 ||
		violation.From.PackageName != "project" || violation.To != nil {
		t.Errorf("required violation = %#v", violation)
	}
}

func TestValidateSubrootPreservesUserFacingPaths(t *testing.T) {
	t.Parallel()

	moduleRoot := t.TempDir()
	writeTestFile(t, filepath.Join(moduleRoot, "go.mod"), "module example.com/project\n\ngo 1.25.0\n")
	scanRoot := filepath.Join(moduleRoot, "internal")
	if err := os.MkdirAll(filepath.Join(scanRoot, "app"), 0o750); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}
	writeTestFile(t, filepath.Join(scanRoot, "app", "app.go"), "package app\n\nimport _ \"os\"\n")
	to := "os"
	configuration := &config.Config{Forbidden: []config.ForbiddenRule{{
		Name:     "no-os",
		Severity: config.SeverityError,
		From:     config.From{Path: []string{`^app/app\.go$`}},
		To:       config.To{Path: []string{`^os$`}},
	}}}
	baseline := &cruiser.Baseline{Entries: []cruiser.BaselineEntry{{
		Rule: "no-os",
		From: "app/app.go",
		To:   &to,
	}}}

	result, err := cruiser.Validate(configuration, cruiser.Options{
		ScanRoot:  scanRoot,
		GoModPath: filepath.Join(moduleRoot, "go.mod"),
		Baseline:  baseline,
	})
	if err != nil {
		t.Fatalf("cruiser.Validate() error = %v", err)
	}
	if len(result.Violations) != 0 || len(result.Known) != 1 || len(result.Stale) != 0 {
		t.Fatalf("result = %+v, want one baseline-known violation", result)
	}
	if result.Known[0].From.Path != "app/app.go" {
		t.Errorf("known violation from path = %q, want scan-root-relative app/app.go", result.Known[0].From.Path)
	}
}

func TestValidateRejectsInvalidProgrammaticConfiguration(t *testing.T) {
	t.Parallel()

	configuration := &config.Config{Forbidden: []config.ForbiddenRule{
		{Name: "duplicate", From: config.From{}, To: config.To{}},
		{Name: "duplicate", From: config.From{}, To: config.To{}},
	}}

	_, err := cruiser.Validate(configuration, cruiser.Options{ScanRoot: createModule(t)})
	if err == nil || !strings.Contains(err.Error(), "duplicate rule name") {
		t.Fatalf("cruiser.Validate() error = %v, want duplicate rule validation error", err)
	}
}

func TestValidateRejectsEmptyProgrammaticMatcherSlices(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		configuration config.Config
		wantPath      string
	}{
		{
			name: "forbidden from path",
			configuration: config.Config{Forbidden: []config.ForbiddenRule{{
				Name: "rule", From: config.From{Path: []string{}}, To: config.To{},
			}}},
			wantPath: "$.forbidden[0].from.path",
		},
		{
			name: "forbidden from pathNot",
			configuration: config.Config{Forbidden: []config.ForbiddenRule{{
				Name: "rule", From: config.From{PathNot: []string{}}, To: config.To{},
			}}},
			wantPath: "$.forbidden[0].from.pathNot",
		},
		{
			name: "forbidden from packageName",
			configuration: config.Config{Forbidden: []config.ForbiddenRule{{
				Name: "rule", From: config.From{PackageName: []string{}}, To: config.To{},
			}}},
			wantPath: "$.forbidden[0].from.packageName",
		},
		{
			name: "forbidden to path",
			configuration: config.Config{Forbidden: []config.ForbiddenRule{{
				Name: "rule", From: config.From{}, To: config.To{Path: []string{}},
			}}},
			wantPath: "$.forbidden[0].to.path",
		},
		{
			name: "forbidden to pathNot",
			configuration: config.Config{Forbidden: []config.ForbiddenRule{{
				Name: "rule", From: config.From{}, To: config.To{PathNot: []string{}},
			}}},
			wantPath: "$.forbidden[0].to.pathNot",
		},
		{
			name: "forbidden to reachableFilePathNot",
			configuration: config.Config{Forbidden: []config.ForbiddenRule{{
				Name: "rule", From: config.From{}, To: config.To{ReachableFilePathNot: []string{}},
			}}},
			wantPath: "$.forbidden[0].to.reachableFilePathNot",
		},
		{
			name: "forbidden to dependencyTypes",
			configuration: config.Config{Forbidden: []config.ForbiddenRule{{
				Name: "rule", From: config.From{}, To: config.To{DependencyTypes: []config.DependencyType{}},
			}}},
			wantPath: "$.forbidden[0].to.dependencyTypes",
		},
		{
			name: "allowed to dependencyTypesNot",
			configuration: config.Config{Allowed: []config.AllowedRule{{
				Name: "rule", From: config.From{}, To: config.To{DependencyTypesNot: []config.DependencyType{}},
			}}},
			wantPath: "$.allowed[0].to.dependencyTypesNot",
		},
		{
			name: "required from path",
			configuration: config.Config{Required: []config.RequiredRule{{
				Name: "rule", From: config.From{Path: []string{}},
				To: config.To{Path: []string{`^fmt$`}},
			}}},
			wantPath: "$.required[0].from.path",
		},
		{
			name: "required to dependencyTypes",
			configuration: config.Config{Required: []config.RequiredRule{{
				Name: "rule", From: config.From{},
				To: config.To{DependencyTypes: []config.DependencyType{}},
			}}},
			wantPath: "$.required[0].to.dependencyTypes",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := cruiser.Validate(&test.configuration, cruiser.Options{})
			if err == nil || !strings.Contains(err.Error(), test.wantPath) {
				t.Fatalf("cruiser.Validate() error = %v, want path %q", err, test.wantPath)
			}
		})
	}
}

func TestValidateRejectsInvalidProgrammaticBaseline(t *testing.T) {
	t.Parallel()

	invalid := &cruiser.Baseline{Entries: []cruiser.BaselineEntry{{
		Rule: "",
		From: "app.go",
	}}}
	_, err := cruiser.Validate(&config.Config{}, cruiser.Options{
		ScanRoot: createModule(t),
		Baseline: invalid,
	})
	if err == nil || !strings.Contains(err.Error(), "validate baseline") {
		t.Fatalf("cruiser.Validate() error = %v, want baseline validation error", err)
	}
}

func TestBaselinePublicRoundTrip(t *testing.T) {
	t.Parallel()

	violations := []cruiser.Violation{
		{
			Rule:     "source",
			Severity: config.SeverityWarn,
			Kind:     cruiser.ViolationKindForbidden,
			From:     cruiser.Source{Path: "z.go", Line: 1},
		},
		{
			Rule:     "edge",
			Severity: config.SeverityError,
			Kind:     cruiser.ViolationKindForbidden,
			From:     cruiser.Source{Path: "a.go", Line: 3},
			To: &cruiser.Dependency{
				Path:       "internal/target",
				ImportPath: "example.com/project/internal/target",
				Type:       config.DependencyTypeLocal,
			},
		},
	}

	generated := cruiser.GenerateBaseline(violations)
	var document bytes.Buffer
	if err := cruiser.WriteBaseline(&document, generated); err != nil {
		t.Fatalf("cruiser.WriteBaseline() error = %v", err)
	}
	loaded, err := cruiser.LoadBaseline(bytes.NewReader(document.Bytes()))
	if err != nil {
		t.Fatalf("cruiser.LoadBaseline() error = %v", err)
	}
	var rewritten bytes.Buffer
	if err := cruiser.WriteBaseline(&rewritten, loaded); err != nil {
		t.Fatalf("cruiser.WriteBaseline(round trip) error = %v", err)
	}
	if rewritten.String() != document.String() {
		t.Errorf("baseline round trip =\n%s\nwant\n%s", rewritten.String(), document.String())
	}
}

func TestValidateFailsWhenScanRootContainsNoGoFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/empty\n\ngo 1.25.0\n")

	_, err := cruiser.Validate(&config.Config{}, cruiser.Options{ScanRoot: root})
	if err == nil || !strings.Contains(err.Error(), "contains no Go files") {
		t.Fatalf("cruiser.Validate() error = %v, want empty scan failure", err)
	}
}

func TestLoadBaselineFile(t *testing.T) {
	t.Parallel()

	filename := filepath.Join(t.TempDir(), "baseline.json")
	writeTestFile(t, filename, `{"entries":[]}`)
	loaded, err := cruiser.LoadBaselineFile(filename)
	if err != nil {
		t.Fatalf("cruiser.LoadBaselineFile() error = %v", err)
	}
	if loaded.Entries == nil || len(loaded.Entries) != 0 {
		t.Errorf("loaded entries = %#v, want non-nil empty slice", loaded.Entries)
	}
}

func TestWriteReportRejectsUnknownType(t *testing.T) {
	t.Parallel()

	err := cruiser.WriteReport(&bytes.Buffer{}, cruiser.OutputType("yaml"), cruiser.Result{})
	if err == nil || !strings.Contains(err.Error(), "unknown output type") {
		t.Fatalf("cruiser.WriteReport() error = %v, want unknown output type", err)
	}
}

func TestWriteReportIncludesStaleBaselineEntries(t *testing.T) {
	t.Parallel()

	result := cruiser.Result{Stale: []cruiser.StaleError{{
		Entry: cruiser.BaselineEntry{Rule: "removed", From: "old.go"},
	}}}
	tests := []struct {
		name       string
		outputType cruiser.OutputType
		want       []string
		wantAbsent string
	}{
		{
			name:       "err",
			outputType: cruiser.OutputTypeErr,
			want:       []string{"[error]", "remove this entry from the baseline"},
		},
		{
			name:       "json",
			outputType: cruiser.OutputTypeJSON,
			want:       []string{`"staleBaselineEntries"`, `"error": 1`},
		},
		{
			name:       "mermaid",
			outputType: cruiser.OutputTypeMermaid,
			want:       []string{"stale0", "staleBaselineError"},
			wantAbsent: "No violations",
		},
		{
			name:       "dot",
			outputType: cruiser.OutputTypeDOT,
			want:       []string{"digraph violations", "stale0", "baseline entry is stale"},
			wantAbsent: "No violations",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var output bytes.Buffer
			if err := cruiser.WriteReport(&output, test.outputType, result); err != nil {
				t.Fatalf("cruiser.WriteReport() error = %v", err)
			}
			for _, want := range test.want {
				if !strings.Contains(output.String(), want) {
					t.Errorf("report does not contain %q:\n%s", want, output.String())
				}
			}
			if test.wantAbsent != "" && strings.Contains(output.String(), test.wantAbsent) {
				t.Errorf("report contains %q:\n%s", test.wantAbsent, output.String())
			}
		})
	}
}

func TestValidateNilConfiguration(t *testing.T) {
	t.Parallel()

	_, err := cruiser.Validate(nil, cruiser.Options{})
	if err == nil || !strings.Contains(err.Error(), "configuration is nil") {
		t.Fatalf("cruiser.Validate(nil) error = %v, want nil configuration error", err)
	}
}

func createModule(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/project\n\ngo 1.25.0\n")
	writeTestFile(t, filepath.Join(root, "app.go"), "package project\n\nimport _ \"os\"\n")

	return root
}

func writeTestFile(t *testing.T, filename, contents string) {
	t.Helper()

	if err := os.WriteFile(filename, []byte(contents), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", filename, err)
	}
}

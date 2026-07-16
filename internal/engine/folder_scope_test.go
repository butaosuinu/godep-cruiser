package engine

import (
	"reflect"
	"testing"

	"github.com/butaosuinu/godep-cruiser/config"
	"github.com/butaosuinu/godep-cruiser/internal/scanner"
)

func TestFolderScopeForbiddenRule(t *testing.T) {
	t.Parallel()

	one := 1
	localImport := func(target string, line int) scanner.Import {
		return scanner.Import{
			Path:         "example.com/project/" + target,
			ResolvedPath: target,
			Type:         scanner.DependencyTypeLocal,
			Line:         line,
		}
	}
	files := []scanner.File{
		{
			Path:        "root.go",
			PackagePath: ".",
			Package:     "project",
			PackageLine: 1,
			Imports:     []scanner.Import{localImport("internal/features/alpha", 3)},
		},
		{
			Path:        "internal/features/alpha/alpha.go",
			PackagePath: "internal/features/alpha",
			Package:     "alpha",
			PackageLine: 1,
			Imports: []scanner.Import{
				localImport("shared/alpha", 3),
				{Path: "fmt", ResolvedPath: "fmt", Type: scanner.DependencyTypeStdlib, Line: 4},
			},
		},
		{
			Path:        "internal/features/alpha/alpha_test.go",
			PackagePath: "internal/features/alpha",
			Package:     "alpha_test",
			PackageLine: 1,
			Imports:     []scanner.Import{localImport("shared/alpha", 3)},
		},
		{
			Path:        "internal/features/beta/beta.go",
			PackagePath: "internal/features/beta",
			Package:     "beta",
			PackageLine: 1,
			Imports:     []scanner.Import{localImport("shared/beta", 3)},
		},
		{Path: "shared/alpha/alpha.go", PackagePath: "shared/alpha", Package: "alpha", PackageLine: 1},
		{Path: "shared/beta/beta.go", PackagePath: "shared/beta", Package: "beta", PackageLine: 1},
		{
			Path:    "unknown.go",
			Package: "unknown",
			Imports: []scanner.Import{localImport("shared/beta", 3)},
		},
	}
	configuration := config.Config{Forbidden: []config.ForbiddenRule{
		{
			Name:     "folder-capture",
			Comment:  "keep feature dependencies paired",
			Severity: config.SeverityError,
			Scope:    config.ScopeFolder,
			From:     config.From{Path: []string{`^internal/features/([^/]+)$`}},
			To:       config.To{Path: []string{`^shared/$1$`}},
		},
		{
			Name:     "folder-root",
			Severity: config.SeverityWarn,
			Scope:    config.ScopeFolder,
			From:     config.From{Path: []string{`^\.$`}},
			To:       config.To{Path: []string{`^internal/features/alpha$`}},
		},
		{
			Name:     "folder-zero-fan-in",
			Severity: config.SeverityInfo,
			Scope:    config.ScopeFolder,
			From: config.From{
				Path:                       []string{`^internal/features/beta$`},
				NumberOfDependentsLessThan: &one,
			},
			To: config.To{Path: []string{`^shared/beta$`}},
		},
	}}

	got, err := Evaluate(&configuration, files)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	want := []struct {
		rule     string
		severity config.Severity
		from     string
		to       string
	}{
		{rule: "folder-capture", severity: config.SeverityError, from: "internal/features/alpha", to: "shared/alpha"},
		{rule: "folder-capture", severity: config.SeverityError, from: "internal/features/beta", to: "shared/beta"},
		{rule: "folder-root", severity: config.SeverityWarn, from: ".", to: "internal/features/alpha"},
		{rule: "folder-zero-fan-in", severity: config.SeverityInfo, from: "internal/features/beta", to: "shared/beta"},
	}
	if len(got) != len(want) {
		t.Fatalf("Evaluate() violations = %#v, want %d folder violations", got, len(want))
	}
	for index, violation := range got {
		if violation.Rule != want[index].rule ||
			violation.Severity != want[index].severity ||
			violation.Kind != ViolationKindForbidden ||
			violation.From.Path != want[index].from ||
			violation.From.Line != 0 ||
			violation.From.PackageName != "" ||
			violation.To == nil ||
			violation.To.Path != want[index].to ||
			violation.To.ImportPath != "" ||
			violation.To.Type != scanner.DependencyTypeLocal {
			t.Errorf("folder violation[%d] = %#v, want (%s -> %s)", index, violation, want[index].from, want[index].to)
		}
	}
	if got[0].Comment != "keep feature dependencies paired" {
		t.Errorf("folder violation comment = %q, want capture rule comment", got[0].Comment)
	}
}

func TestModuleScopeForbiddenParity(t *testing.T) {
	t.Parallel()

	files := []scanner.File{{
		Path:        "internal/app/app.go",
		PackagePath: "internal/app",
		Package:     "app",
		PackageLine: 1,
		Imports: []scanner.Import{{
			Path:         "example.com/project/internal/core",
			ResolvedPath: "internal/core",
			Type:         scanner.DependencyTypeLocal,
			Line:         7,
		}},
	}}
	rule := config.ForbiddenRule{
		Name:     "app-no-core",
		Severity: config.SeverityError,
		From:     config.From{Path: []string{`^internal/app/app\.go$`}},
		To:       config.To{Path: []string{`^internal/core$`}},
	}
	defaultScope, err := Evaluate(&config.Config{Forbidden: []config.ForbiddenRule{rule}}, files)
	if err != nil {
		t.Fatalf("Evaluate(default scope) error = %v", err)
	}
	rule.Scope = config.ScopeModule
	explicitModule, err := Evaluate(&config.Config{Forbidden: []config.ForbiddenRule{rule}}, files)
	if err != nil {
		t.Fatalf("Evaluate(module scope) error = %v", err)
	}
	if !reflect.DeepEqual(defaultScope, explicitModule) {
		t.Fatalf("default scope violations = %#v, explicit module = %#v", defaultScope, explicitModule)
	}
	if len(defaultScope) != 1 {
		t.Fatalf("Evaluate(default scope) violations = %#v, want one", defaultScope)
	}
	violation := defaultScope[0]
	if violation.From.Path != files[0].Path || violation.From.Line != 7 ||
		violation.From.PackageName != "app" || violation.To == nil ||
		violation.To.ImportPath != "example.com/project/internal/core" {
		t.Errorf("module violation = %#v, want existing file-edge coordinates", violation)
	}
}

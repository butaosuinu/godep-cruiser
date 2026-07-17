package engine

import (
	"math"
	"testing"

	"github.com/butaosuinu/godep-cruiser/config"
	"github.com/butaosuinu/godep-cruiser/internal/graph"
	"github.com/butaosuinu/godep-cruiser/internal/scanner"
)

func TestMoreUnstableForbiddenRule(t *testing.T) {
	t.Parallel()

	moreUnstable := true
	files := instabilityFiles()
	tests := []struct {
		name       string
		scope      config.Scope
		from       config.From
		wantFrom   string
		wantLine   int
		wantTo     string
		wantImport string
	}{
		{
			name: "module scope keeps the source import edge",
			from: config.From{
				Path:                       []string{`^internal/source/source\.go$`},
				NumberOfDependentsLessThan: intPointer(3),
			},
			wantFrom:   "internal/source/source.go",
			wantLine:   4,
			wantTo:     "internal/more",
			wantImport: "example.test/project/internal/more",
		},
		{
			name:     "folder scope keeps the distinct package edge",
			scope:    config.ScopeFolder,
			from:     config.From{Path: []string{`^internal/source$`}},
			wantFrom: "internal/source",
			wantTo:   "internal/more",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			configuration := config.Config{Forbidden: []config.ForbiddenRule{{
				Name:     "stable-dependencies",
				Severity: config.SeverityError,
				Scope:    test.scope,
				From:     test.from,
				To:       config.To{MoreUnstable: &moreUnstable},
			}}}
			got, err := Evaluate(&configuration, files)
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("Evaluate() violations = %#v, want one strictly more-unstable local edge", got)
			}
			violation := got[0]
			if violation.Kind != ViolationKindForbidden ||
				violation.From.Path != test.wantFrom ||
				violation.From.Line != test.wantLine ||
				violation.To == nil ||
				violation.To.Path != test.wantTo ||
				violation.To.ImportPath != test.wantImport ||
				violation.To.Type != scanner.DependencyTypeLocal {
				t.Errorf("moreUnstable violation = %#v, want %s:%d -> %s", violation, test.wantFrom, test.wantLine, test.wantTo)
			}
		})
	}
}

func TestInstability(t *testing.T) {
	t.Parallel()

	packageGraph := graph.Build(instabilityFiles())
	tests := []struct {
		name        string
		packagePath string
		want        float64
	}{
		{name: "isolated package uses zero instead of dividing by zero", packagePath: "internal/isolated", want: 0},
		{name: "source combines fan in and fan out", packagePath: "internal/source", want: 0.5},
		{name: "target with more outgoing edges is more unstable", packagePath: "internal/more", want: 2.0 / 3.0},
		{name: "unknown package also uses zero", packagePath: "internal/unknown", want: 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := instability(packageGraph, test.packagePath); math.Abs(got-test.want) > 1e-12 {
				t.Errorf("instability(%q) = %v, want %v", test.packagePath, got, test.want)
			}
		})
	}
	if isMoreUnstable(packageGraph, "internal/source", "internal/equal") {
		t.Error("equal instability matched, want strict inequality")
	}
	if isMoreUnstable(packageGraph, "internal/more", "internal/source") {
		t.Error("more-stable target matched")
	}
}

func instabilityFiles() []scanner.File {
	localImport := func(target string, line int) scanner.Import {
		return scanner.Import{
			Path:         "example.test/project/" + target,
			ResolvedPath: target,
			Type:         scanner.DependencyTypeLocal,
			Line:         line,
		}
	}

	return []scanner.File{
		{
			Path:        "internal/source/source.go",
			PackagePath: "internal/source",
			Package:     "source",
			PackageLine: 1,
			Imports: []scanner.Import{
				localImport("internal/more", 4),
				localImport("internal/equal", 5),
				{Path: "fmt", ResolvedPath: "fmt", Type: scanner.DependencyTypeStdlib, Line: 6},
				{Path: "example.test/external", ResolvedPath: "example.test/external", Type: scanner.DependencyTypeModule, Line: 7},
				{Path: "C", Type: scanner.DependencyTypeUnresolved, Line: 8},
			},
		},
		{
			Path:        "internal/client/one.go",
			PackagePath: "internal/client",
			Imports:     []scanner.Import{localImport("internal/source", 3)},
		},
		{
			Path:        "internal/other/two.go",
			PackagePath: "internal/other",
			Imports:     []scanner.Import{localImport("internal/source", 3)},
		},
		{
			Path:        "internal/more/more.go",
			PackagePath: "internal/more",
			Imports: []scanner.Import{
				localImport("internal/leaf/one", 3),
				localImport("internal/leaf/two", 4),
			},
		},
		{
			Path:        "internal/equal/equal.go",
			PackagePath: "internal/equal",
			Imports:     []scanner.Import{localImport("internal/leaf/three", 3)},
		},
		{Path: "internal/leaf/one/one.go", PackagePath: "internal/leaf/one"},
		{Path: "internal/leaf/two/two.go", PackagePath: "internal/leaf/two"},
		{Path: "internal/leaf/three/three.go", PackagePath: "internal/leaf/three"},
		{Path: "internal/isolated/isolated.go", PackagePath: "internal/isolated"},
	}
}

func intPointer(value int) *int {
	return &value
}

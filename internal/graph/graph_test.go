package graph_test

import (
	"slices"
	"testing"

	"github.com/butaosuinu/godep-cruiser/internal/graph"
	"github.com/butaosuinu/godep-cruiser/internal/scanner"
)

func TestClosures(t *testing.T) {
	t.Parallel()

	dependencyGraph := graph.Build(graphFiles())
	tests := []struct {
		name string
		got  []string
		want []string
	}{
		{
			name: "forward transitive closure includes seed",
			got:  dependencyGraph.ForwardClosure("internal/a"),
			want: []string{"internal/a", "internal/b", "internal/c", "internal/missing"},
		},
		{
			name: "reverse transitive closure includes seed",
			got:  dependencyGraph.ReverseClosure("internal/c"),
			want: []string{"cmd/app", "internal/a", "internal/b", "internal/c"},
		},
		{
			name: "multiple seeds are deduplicated",
			got:  dependencyGraph.ForwardClosure("internal/b", "internal/a", "internal/b"),
			want: []string{"internal/a", "internal/b", "internal/c", "internal/missing"},
		},
		{
			name: "unknown seeds are ignored",
			got:  dependencyGraph.ForwardClosure("internal/unknown"),
			want: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if !slices.Equal(test.got, test.want) {
				t.Errorf("closure = %q, want %q", test.got, test.want)
			}
		})
	}
}

func TestDirectViewsAndCounts(t *testing.T) {
	t.Parallel()

	dependencyGraph := graph.Build(graphFiles())
	tests := []struct {
		name           string
		packagePath    string
		wantDependents []string
		wantFanIn      int
		wantFanOut     int
		wantImported   bool
	}{
		{
			name:           "duplicate file imports collapse to package edges",
			packagePath:    "internal/a",
			wantDependents: []string{"cmd/app"},
			wantFanIn:      1,
			wantFanOut:     3,
			wantImported:   true,
		},
		{
			name:           "reverse index deduplicates importer packages",
			packagePath:    "internal/c",
			wantDependents: []string{"internal/a", "internal/b"},
			wantFanIn:      2,
			wantImported:   true,
		},
		{
			name:         "same directory import is only in orphan view",
			packagePath:  "internal/self",
			wantFanIn:    0,
			wantFanOut:   0,
			wantImported: true,
		},
		{
			name:        "unimported package has empty views",
			packagePath: "internal/lonely",
		},
		{
			name:           "observed unscanned local target is a leaf node",
			packagePath:    "internal/missing",
			wantDependents: []string{"internal/a"},
			wantFanIn:      1,
			wantImported:   true,
		},
		{
			name:        "empty identity does not alias module root",
			packagePath: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := dependencyGraph.Dependents(test.packagePath); !slices.Equal(got, test.wantDependents) {
				t.Errorf("Dependents(%q) = %q, want %q", test.packagePath, got, test.wantDependents)
			}
			if got := dependencyGraph.FanIn(test.packagePath); got != test.wantFanIn {
				t.Errorf("FanIn(%q) = %d, want %d", test.packagePath, got, test.wantFanIn)
			}
			if got := dependencyGraph.FanOut(test.packagePath); got != test.wantFanOut {
				t.Errorf("FanOut(%q) = %d, want %d", test.packagePath, got, test.wantFanOut)
			}
			if got := dependencyGraph.IsImported(test.packagePath); got != test.wantImported {
				t.Errorf("IsImported(%q) = %t, want %t", test.packagePath, got, test.wantImported)
			}
		})
	}
}

func TestBuildUsesModuleRelativePackageIdentity(t *testing.T) {
	t.Parallel()

	files := []scanner.File{
		{
			Path:        "app/app.go",
			PackagePath: "internal/app",
			Imports: []scanner.Import{{
				Path:         "example.com/project/internal/core",
				ResolvedPath: "internal/core",
				Type:         scanner.DependencyTypeLocal,
			}},
		},
		{Path: "core/core.go", PackagePath: "internal/core"},
	}
	dependencyGraph := graph.Build(files)
	if got, want := dependencyGraph.ForwardClosure("internal/app"), []string{"internal/app", "internal/core"}; !slices.Equal(got, want) {
		t.Errorf("ForwardClosure(module path) = %q, want %q", got, want)
	}
	if got, want := dependencyGraph.Dependents("internal/core"), []string{"internal/app"}; !slices.Equal(got, want) {
		t.Errorf("Dependents(module path) = %q, want %q", got, want)
	}
	if got := dependencyGraph.ForwardClosure("app"); len(got) != 0 {
		t.Errorf("ForwardClosure(scan-root path) = %q, want empty", got)
	}
}

func TestRootPackageAndEmptyGraph(t *testing.T) {
	t.Parallel()

	rootImport := scanner.Import{
		Path:         "example.com/project",
		ResolvedPath: ".",
		Type:         scanner.DependencyTypeLocal,
	}
	rootGraph := graph.Build([]scanner.File{{
		Path:        "main.go",
		PackagePath: ".",
		Imports:     []scanner.Import{rootImport},
	}})
	tests := []struct {
		name string
		got  []string
		want []string
	}{
		{name: "forward root closure", got: rootGraph.ForwardClosure("."), want: []string{"."}},
		{name: "reverse root closure", got: rootGraph.ReverseClosure("."), want: []string{"."}},
		{name: "empty seed", got: rootGraph.ForwardClosure(""), want: nil},
		{name: "zero value graph", got: graph.Build(nil).ForwardClosure("."), want: nil},
		{
			name: "empty file identity",
			got:  graph.Build([]scanner.File{{Path: "main.go"}}).ForwardClosure("."),
			want: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if !slices.Equal(test.got, test.want) {
				t.Errorf("closure = %q, want %q", test.got, test.want)
			}
		})
	}
	if rootGraph.FanIn(".") != 0 || rootGraph.FanOut(".") != 0 {
		t.Errorf("root self edge counts = (%d, %d), want (0, 0)", rootGraph.FanIn("."), rootGraph.FanOut("."))
	}
	if !rootGraph.IsImported(".") {
		t.Error("IsImported(root) = false, want true")
	}
}

func TestSelfEdgeDoesNotExpandClosures(t *testing.T) {
	t.Parallel()

	dependencyGraph := graph.Build(graphFiles())
	want := []string{"internal/self"}
	if got := dependencyGraph.ForwardClosure("internal/self"); !slices.Equal(got, want) {
		t.Errorf("ForwardClosure() = %q, want %q", got, want)
	}
	if got := dependencyGraph.ReverseClosure("internal/self"); !slices.Equal(got, want) {
		t.Errorf("ReverseClosure() = %q, want %q", got, want)
	}
}

func graphFiles() []scanner.File {
	localImport := func(target string) scanner.Import {
		return scanner.Import{
			Path:         "example.com/project/" + target,
			ResolvedPath: target,
			Type:         scanner.DependencyTypeLocal,
		}
	}

	return []scanner.File{
		{
			Path:        "cmd/app/main.go",
			PackagePath: "cmd/app",
			Imports: []scanner.Import{
				localImport("internal/a"),
			},
		},
		{
			Path:        "internal/a/one.go",
			PackagePath: "internal/a",
			Imports: []scanner.Import{
				localImport("internal/b"),
				localImport("internal/b"),
				localImport("internal/c"),
				{Path: "fmt", ResolvedPath: "fmt", Type: scanner.DependencyTypeStdlib},
				{Path: "C", Type: scanner.DependencyTypeUnresolved},
				localImport("internal/missing"),
			},
		},
		{
			Path:        "internal/a/two.go",
			PackagePath: "internal/a",
			Imports:     []scanner.Import{localImport("internal/c")},
		},
		{
			Path:        "internal/b/b.go",
			PackagePath: "internal/b",
			Imports:     []scanner.Import{localImport("internal/c")},
		},
		{Path: "internal/c/c.go", PackagePath: "internal/c"},
		{Path: "internal/lonely/lonely.go", PackagePath: "internal/lonely"},
		{Path: "internal/self/self.go", PackagePath: "internal/self"},
		{
			Path:        "internal/self/self_test.go",
			PackagePath: "internal/self",
			Package:     "self_test",
			Imports: []scanner.Import{
				localImport("internal/self"),
			},
		},
	}
}

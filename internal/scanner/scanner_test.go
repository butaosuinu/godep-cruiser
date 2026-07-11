package scanner

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestScanRecordsEveryGoFile(t *testing.T) {
	t.Parallel()

	root := filepath.Join("testdata", "module")
	resolver, err := NewResolverFromGoMod(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("NewResolverFromGoMod() error = %v", err)
	}

	files, err := Scan(root, resolver)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	wantPaths := []string{
		".hidden.go",
		"_generated.go",
		"dot.dir/keep.go",
		"feature_test.go",
		"imports.go",
		"nested/file.go",
		"os_linux.go",
		"os_windows.go",
		"tagged.go",
		"testdatax/keep.go",
		"under_score/keep.go",
		"vendorized/keep.go",
	}
	gotPaths := make([]string, 0, len(files))
	for _, file := range files {
		gotPaths = append(gotPaths, file.Path)
	}
	if !slices.Equal(gotPaths, wantPaths) {
		t.Fatalf("Scan() paths = %q, want %q", gotPaths, wantPaths)
	}

	tests := []struct {
		name        string
		path        string
		wantPackage string
		wantImports []Import
	}{
		{
			name:        "four dependency types retain import metadata",
			path:        "imports.go",
			wantPackage: "fixture",
			wantImports: []Import{
				{Path: "C", Type: DependencyTypeUnresolved, Line: 4},
				{
					Path:         "example.com/fixture/internal/local",
					ResolvedPath: "internal/local",
					Type:         DependencyTypeLocal,
					Line:         5,
				},
				{Path: "fmt", ResolvedPath: "fmt", Type: DependencyTypeStdlib, Line: 6},
				{
					Path:         "github.com/acme/third",
					ResolvedPath: "github.com/acme/third",
					Type:         DependencyTypeModule,
					Line:         7,
				},
			},
		},
		{
			name:        "external test package is retained",
			path:        "feature_test.go",
			wantPackage: "fixture_test",
			wantImports: []Import{
				{Path: "testing", ResolvedPath: "testing", Type: DependencyTypeStdlib, Line: 3},
			},
		},
		{
			name:        "linux suffix is retained",
			path:        "os_linux.go",
			wantPackage: "fixture",
			wantImports: []Import{
				{Path: "os", ResolvedPath: "os", Type: DependencyTypeStdlib, Line: 3},
			},
		},
		{
			name:        "windows suffix is retained",
			path:        "os_windows.go",
			wantPackage: "fixture",
			wantImports: []Import{
				{Path: "syscall", ResolvedPath: "syscall", Type: DependencyTypeStdlib, Line: 3},
			},
		},
		{
			name:        "build tagged file is retained",
			path:        "tagged.go",
			wantPackage: "fixture",
			wantImports: []Import{
				{Path: "runtime", ResolvedPath: "runtime", Type: DependencyTypeStdlib, Line: 5},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			file, ok := findFile(files, tt.path)
			if !ok {
				t.Fatalf("Scan() has no file %q", tt.path)
			}
			if file.Package != tt.wantPackage {
				t.Errorf("file %q package = %q, want %q", tt.path, file.Package, tt.wantPackage)
			}
			if !reflect.DeepEqual(file.Imports, tt.wantImports) {
				t.Errorf("file %q imports = %#v, want %#v", tt.path, file.Imports, tt.wantImports)
			}
		})
	}
}

func TestScanSkipsDirectories(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		directory string
	}{
		{name: "testdata", directory: "testdata"},
		{name: "vendor", directory: "vendor"},
		{name: "dot prefix", directory: ".ignored"},
		{name: "underscore prefix", directory: "_ignored"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			writeTestFile(t, filepath.Join(root, "visible.go"), "package visible\n")
			writeTestFile(t, filepath.Join(root, "nested", tt.directory, "poison.go"), "package\n")

			resolver := mustResolver(t, "example.com/fixture")
			files, err := Scan(root, resolver)
			if err != nil {
				t.Fatalf("Scan() error = %v", err)
			}
			if len(files) != 1 || files[0].Path != "visible.go" {
				t.Errorf("Scan() files = %#v, want only visible.go", files)
			}
		})
	}
}

func TestScanHonorsExplicitRootBeforeSkipRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
	}{
		{name: "testdata"},
		{name: "vendor"},
		{name: ".explicit"},
		{name: "_explicit"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := filepath.Join(t.TempDir(), tt.name)
			writeTestFile(t, filepath.Join(root, "source.go"), "package source\n")

			files, err := Scan(root, mustResolver(t, "example.com/fixture"))
			if err != nil {
				t.Fatalf("Scan() error = %v", err)
			}
			if len(files) != 1 || files[0].Path != "source.go" {
				t.Errorf("Scan() files = %#v, want source.go", files)
			}
		})
	}
}

func TestScanFollowsExplicitSymlinkRoot(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	target := filepath.Join(workspace, "target")
	writeTestFile(t, filepath.Join(target, "source.go"), "package source\n")

	root := filepath.Join(workspace, "root")
	if err := os.Symlink("target", root); err != nil {
		t.Skipf("create directory symlink: %v", err)
	}

	files, err := Scan(root, mustResolver(t, "example.com/fixture"))
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(files) != 1 || files[0].Path != "source.go" {
		t.Errorf("Scan() files = %#v, want source.go", files)
	}
}

func TestScanFailsWhenNoGoFilesAreParsed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(*testing.T, string)
	}{
		{name: "empty root"},
		{
			name: "non-Go files only",
			setup: func(t *testing.T, root string) {
				t.Helper()

				writeTestFile(t, filepath.Join(root, "README.md"), "no Go source\n")
			},
		},
		{
			name: "Go files only in skipped directories",
			setup: func(t *testing.T, root string) {
				t.Helper()

				writeTestFile(t, filepath.Join(root, "testdata", "ignored.go"), "package ignored\n")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			if tt.setup != nil {
				tt.setup(t, root)
			}

			_, err := Scan(root, mustResolver(t, "example.com/fixture"))
			if !errors.Is(err, ErrNoGoFiles) {
				t.Fatalf("Scan() error = %v, want errors.Is(error, ErrNoGoFiles)", err)
			}
			if !strings.Contains(err.Error(), root) {
				t.Errorf("Scan() error = %q, want it to contain root %q", err, root)
			}
		})
	}
}

func TestScanUsesImportsOnly(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "invalid_body.go"), "package fixture\n\nimport \"fmt\"\n\nfunc broken( {\n")

	files, err := Scan(root, mustResolver(t, "example.com/fixture"))
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	wantImports := []Import{
		{Path: "fmt", ResolvedPath: "fmt", Type: DependencyTypeStdlib, Line: 3},
	}
	if len(files) != 1 || !reflect.DeepEqual(files[0].Imports, wantImports) {
		t.Errorf("Scan() files = %#v, want one file with imports %#v", files, wantImports)
	}
}

func TestScanReportsImportParseErrorsEvenWhenBuildTagged(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "tagged.go")
	writeTestFile(t, path, "//go:build never\n\npackage fixture\n\nimport (\n")

	_, err := Scan(root, mustResolver(t, "example.com/fixture"))
	if err == nil {
		t.Fatal("Scan() error = nil, want a parse error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("Scan() error = %q, want it to contain %q", err, path)
	}
}

func TestScanRejectsInvalidRoots(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		root func(*testing.T) string
	}{
		{
			name: "missing root",
			root: func(t *testing.T) string {
				t.Helper()

				return filepath.Join(t.TempDir(), "missing")
			},
		},
		{
			name: "root is a file",
			root: func(t *testing.T) string {
				t.Helper()

				path := filepath.Join(t.TempDir(), "source.go")
				writeTestFile(t, path, "package source\n")
				return path
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := Scan(tt.root(t), mustResolver(t, "example.com/fixture"))
			if err == nil {
				t.Fatal("Scan() error = nil, want an invalid-root error")
			}
			if errors.Is(err, ErrNoGoFiles) {
				t.Errorf("Scan() error = %v, do not want ErrNoGoFiles", err)
			}
		})
	}
}

func findFile(files []File, path string) (File, bool) {
	for _, file := range files {
		if file.Path == path {
			return file, true
		}
	}

	return File{}, false
}

func mustResolver(t *testing.T, modulePath string) Resolver {
	t.Helper()

	resolver, err := NewResolver(modulePath)
	if err != nil {
		t.Fatalf("NewResolver(%q) error = %v", modulePath, err)
	}

	return resolver
}

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("create parent directory for %q: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

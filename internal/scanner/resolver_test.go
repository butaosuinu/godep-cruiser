package scanner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolverResolve(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		modulePath string
		importPath string
		want       Resolution
	}{
		{
			name:       "standard library package",
			modulePath: "example.com/acme/app",
			importPath: "fmt",
			want:       Resolution{Path: "fmt", Type: DependencyTypeStdlib},
		},
		{
			name:       "standard library subpackage",
			modulePath: "example.com/acme/app",
			importPath: "net/http",
			want:       Resolution{Path: "net/http", Type: DependencyTypeStdlib},
		},
		{
			name:       "module root is local dot",
			modulePath: "example.com/acme/app",
			importPath: "example.com/acme/app",
			want:       Resolution{Path: ".", Type: DependencyTypeLocal},
		},
		{
			name:       "module subpackage is local and relative",
			modulePath: "example.com/acme/app",
			importPath: "example.com/acme/app/internal/report",
			want:       Resolution{Path: "internal/report", Type: DependencyTypeLocal},
		},
		{
			name:       "module prefix collision is third party",
			modulePath: "example.com/acme/app",
			importPath: "example.com/acme/application/report",
			want: Resolution{
				Path: "example.com/acme/application/report",
				Type: DependencyTypeModule,
			},
		},
		{
			name:       "third party module",
			modulePath: "example.com/acme/app",
			importPath: "github.com/lib/pq",
			want:       Resolution{Path: "github.com/lib/pq", Type: DependencyTypeModule},
		},
		{
			name:       "dotless module wins before stdlib heuristic",
			modulePath: "acme/project",
			importPath: "acme/project/report",
			want:       Resolution{Path: "report", Type: DependencyTypeLocal},
		},
		{
			name:       "dotless unknown import follows stdlib heuristic",
			modulePath: "example.com/acme/app",
			importPath: "corp/internal",
			want:       Resolution{Path: "corp/internal", Type: DependencyTypeStdlib},
		},
		{
			name:       "cgo pseudo-import is unresolved",
			modulePath: "example.com/acme/app",
			importPath: "C",
			want:       Resolution{Type: DependencyTypeUnresolved},
		},
		{
			name:       "empty import is unresolved",
			modulePath: "example.com/acme/app",
			importPath: "",
			want:       Resolution{Type: DependencyTypeUnresolved},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resolver, err := NewResolver(tt.modulePath)
			if err != nil {
				t.Fatalf("NewResolver(%q) error = %v", tt.modulePath, err)
			}

			if got := resolver.Resolve(tt.importPath); got != tt.want {
				t.Errorf("Resolve(%q) = %#v, want %#v", tt.importPath, got, tt.want)
			}
		})
	}
}

func TestNewResolver(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		modulePath string
		wantError  string
	}{
		{name: "empty path", modulePath: "", wantError: "empty"},
		{name: "leading whitespace", modulePath: " example.com/app", wantError: "whitespace"},
		{name: "trailing whitespace", modulePath: "example.com/app\t", wantError: "whitespace"},
		{name: "trailing slash", modulePath: "example.com/app/", wantError: "trailing slash"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := NewResolver(tt.modulePath)
			if err == nil {
				t.Fatalf("NewResolver(%q) error = nil, want it to contain %q", tt.modulePath, tt.wantError)
			}
			if !strings.Contains(err.Error(), tt.wantError) {
				t.Errorf("NewResolver(%q) error = %q, want it to contain %q", tt.modulePath, err, tt.wantError)
			}
		})
	}
}

func TestNewResolverFromGoMod(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		contents  string
		wantPath  string
		wantError string
	}{
		{
			name:     "plain directive",
			contents: "module example.com/acme/app\n\ngo 1.25.0\n",
			wantPath: "example.com/acme/app",
		},
		{
			name:     "tabbed quoted directive with comment and CRLF",
			contents: "// module ignored.example/fake\r\nmodule\t\"example.com/acme/app\" // root module\r\n",
			wantPath: "example.com/acme/app",
		},
		{
			name:     "unspaced line comment",
			contents: "module example.com/acme/app//root module\n",
			wantPath: "example.com/acme/app",
		},
		{
			name:     "utf8 byte order mark",
			contents: "\ufeffmodule example.com/acme/app\n",
			wantPath: "example.com/acme/app",
		},
		{
			name:      "missing directive",
			contents:  "go 1.25.0\n",
			wantError: "module directive not found",
		},
		{
			name:      "directive without path",
			contents:  "module\n",
			wantError: "has no path",
		},
		{
			name:      "empty quoted path",
			contents:  "module \"\"\n",
			wantError: "module path is empty",
		},
		{
			name:      "invalid quoted path",
			contents:  "module \"example.com/acme/app\n",
			wantError: "invalid quoted module path",
		},
		{
			name:      "duplicate directive",
			contents:  "module example.com/one\nmodule example.com/two\n",
			wantError: "multiple module directives",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			goModPath := filepath.Join(t.TempDir(), "go.mod")
			if err := os.WriteFile(goModPath, []byte(tt.contents), 0o600); err != nil {
				t.Fatalf("write go.mod: %v", err)
			}

			resolver, err := NewResolverFromGoMod(goModPath)
			if tt.wantError != "" {
				if err == nil {
					t.Fatalf("NewResolverFromGoMod() error = nil, want it to contain %q", tt.wantError)
				}
				if !strings.Contains(err.Error(), tt.wantError) {
					t.Errorf("NewResolverFromGoMod() error = %q, want it to contain %q", err, tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewResolverFromGoMod() error = %v", err)
			}
			if got := resolver.ModulePath(); got != tt.wantPath {
				t.Errorf("ModulePath() = %q, want %q", got, tt.wantPath)
			}
		})
	}
}

func TestNewResolverFromGoModReportsMissingFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "missing", "go.mod")
	_, err := NewResolverFromGoMod(path)
	if err == nil {
		t.Fatal("NewResolverFromGoMod() error = nil, want a missing-file error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("NewResolverFromGoMod() error = %q, want it to contain %q", err, path)
	}
}

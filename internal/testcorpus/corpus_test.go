package testcorpus

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/butaosuinu/godep-cruiser/internal/scanner"
)

func TestViolationCorpus(t *testing.T) {
	t.Parallel()

	cases, err := Load(filepath.Join("..", "..", "testdata", "corpus"))
	if err != nil {
		t.Fatalf("Load() = %v, want nil", err)
	}

	wantIDs := []string{
		"forbidden-import-target",
		"layer-direction",
		"orphan-file",
		"package-main-placement",
		"stdlib-denylist-exception",
		"stdlib-only-tree",
		"third-party-in-core",
		"unclassified-dependency",
	}
	gotIDs := make([]string, 0, len(cases))
	for _, fixture := range cases {
		gotIDs = append(gotIDs, fixture.ID)
	}
	if !slices.Equal(gotIDs, wantIDs) {
		t.Fatalf("Load() IDs = %q, want %q", gotIDs, wantIDs)
	}

	for _, fixture := range cases {
		t.Run(fixture.Name, func(t *testing.T) {
			t.Parallel()

			resolver, err := scanner.NewResolverFromGoMod(filepath.Join(fixture.ModuleDir, "go.mod"))
			if err != nil {
				t.Fatalf("NewResolverFromGoMod() = %v, want nil", err)
			}
			files := parseFixtureModule(t, fixture.ModuleDir, resolver)
			if len(files) == 0 {
				t.Fatal("parsed 0 Go files, want at least 1")
			}
			assertGoldenLocations(t, fixture, files)
			assertPositiveControlLocations(t, fixture, files)
			vetFixtureModule(t, fixture.ModuleDir)
		})
	}
}

type parsedImport struct {
	path           string
	dependencyType string
	line           int
}

type parsedFile struct {
	packageName string
	packageLine int
	imports     []parsedImport
}

func parseFixtureModule(t *testing.T, moduleDir string, resolver scanner.Resolver) map[string]parsedFile {
	t.Helper()

	fset := token.NewFileSet()
	files := make(map[string]parsedFile)
	err := filepath.WalkDir(moduleDir, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && filename != moduleDir && skippedDirectory(entry.Name()) {
			return filepath.SkipDir
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" {
			return nil
		}

		parsed, err := parser.ParseFile(fset, filename, nil, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("parse %s: %w", filename, err)
		}
		rel, err := filepath.Rel(moduleDir, filename)
		if err != nil {
			return fmt.Errorf("make %s relative: %w", filename, err)
		}
		file := parsedFile{
			packageName: parsed.Name.Name,
			packageLine: fset.Position(parsed.Name.Pos()).Line,
		}
		for _, spec := range parsed.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return fmt.Errorf("unquote import in %s: %w", filename, err)
			}
			targetPath, dependencyType := projectImportForGolden(resolver, importPath)
			file.imports = append(file.imports, parsedImport{
				path:           targetPath,
				dependencyType: dependencyType,
				line:           fset.Position(spec.Path.Pos()).Line,
			})
		}
		files[filepath.ToSlash(rel)] = file
		return nil
	})
	if err != nil {
		t.Fatalf("parse fixture module: %v", err)
	}
	return files
}

func skippedDirectory(name string) bool {
	return name == "testdata" || name == "vendor" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

func projectImportForGolden(resolver scanner.Resolver, importPath string) (string, string) {
	resolution := resolver.Resolve(importPath)
	targetPath := resolution.Path
	if targetPath == "" {
		targetPath = importPath
	}
	return targetPath, string(resolution.Type)
}

func assertGoldenLocations(t *testing.T, fixture Case, files map[string]parsedFile) {
	t.Helper()

	for _, violation := range fixture.Violations {
		if err := validateGoldenLocation(violation, files); err != nil {
			t.Error(err)
		}
	}
}

func validateGoldenLocation(violation ExpectedViolation, files map[string]parsedFile) error {
	file, ok := files[violation.From.Path]
	if !ok {
		return fmt.Errorf("golden from.path %q was not parsed", violation.From.Path)
	}
	if violation.To == nil {
		if violation.From.Line != file.packageLine {
			return fmt.Errorf("source-only golden %s line = %d, want package line %d", violation.Rule, violation.From.Line, file.packageLine)
		}
		switch violation.Rule {
		case "package-main-placement":
			if file.packageName != "main" {
				return fmt.Errorf("source-only golden %s package = %q, want %q", violation.Rule, file.packageName, "main")
			}
		case "no-orphans":
			if err := validateOrphanLocation(violation.From.Path, file, files); err != nil {
				return err
			}
		}
		return nil
	}

	found := slices.ContainsFunc(file.imports, func(found parsedImport) bool {
		return found.path == violation.To.Path &&
			found.dependencyType == violation.To.DependencyType &&
			found.line == violation.From.Line
	})
	if !found {
		return fmt.Errorf("golden %s target %q (%s) at line %d does not match an import in %s",
			violation.Rule,
			violation.To.Path,
			violation.To.DependencyType,
			violation.From.Line,
			violation.From.Path,
		)
	}
	return nil
}

func validateOrphanLocation(sourcePath string, file parsedFile, files map[string]parsedFile) error {
	if len(file.imports) != 0 {
		return fmt.Errorf("source-only golden no-orphans file %s has %d outgoing imports", sourcePath, len(file.imports))
	}

	packagePath := path.Dir(sourcePath)
	for importerPath, importer := range files {
		incoming := slices.ContainsFunc(importer.imports, func(found parsedImport) bool {
			return found.dependencyType == string(scanner.DependencyTypeLocal) && found.path == packagePath
		})
		if incoming {
			return fmt.Errorf("source-only golden no-orphans file %s has an incoming import from %s", sourcePath, importerPath)
		}
	}
	return nil
}

func assertPositiveControlLocations(t *testing.T, fixture Case, files map[string]parsedFile) {
	t.Helper()

	for _, control := range fixture.PositiveControls {
		if err := validatePositiveControlLocation(control, files); err != nil {
			t.Error(err)
		}
	}
}

func validatePositiveControlLocation(control PositiveControl, files map[string]parsedFile) error {
	file, ok := files[control.From.Path]
	if !ok {
		return fmt.Errorf("positive control from.path %q was not parsed", control.From.Path)
	}
	if control.To == nil {
		if control.From.Line != file.packageLine {
			return fmt.Errorf("positive control %s line = %d, want package line %d", control.Rule, control.From.Line, file.packageLine)
		}
		if file.packageName != control.PackageName {
			return fmt.Errorf("positive control %s package = %q, want %q", control.Rule, file.packageName, control.PackageName)
		}
		return nil
	}

	found := slices.ContainsFunc(file.imports, func(found parsedImport) bool {
		return found.path == control.To.Path &&
			found.dependencyType == control.To.DependencyType &&
			found.line == control.From.Line
	})
	if !found {
		return fmt.Errorf("positive control %s target %q (%s) at line %d does not match an import in %s",
			control.Rule,
			control.To.Path,
			control.To.DependencyType,
			control.From.Line,
			control.From.Path,
		)
	}
	return nil
}

func TestProjectImportForGolden(t *testing.T) {
	t.Parallel()

	resolver, err := scanner.NewResolver("example.test/fixture")
	if err != nil {
		t.Fatalf("NewResolver() = %v, want nil", err)
	}
	tests := []struct {
		name               string
		importPath         string
		wantPath           string
		wantDependencyType string
	}{
		{
			name:               "cgo pseudo-import is unresolved",
			importPath:         "C",
			wantPath:           "C",
			wantDependencyType: "unresolved",
		},
		{
			name:               "module root is local",
			importPath:         "example.test/fixture",
			wantPath:           ".",
			wantDependencyType: "local",
		},
		{
			name:               "module child is relative",
			importPath:         "example.test/fixture/internal/core",
			wantPath:           "internal/core",
			wantDependencyType: "local",
		},
		{
			name:               "standard library import",
			importPath:         "net/http",
			wantPath:           "net/http",
			wantDependencyType: "stdlib",
		},
		{
			name:               "third-party module import",
			importPath:         "github.com/acme/dependency",
			wantPath:           "github.com/acme/dependency",
			wantDependencyType: "module",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			gotPath, gotDependencyType := projectImportForGolden(resolver, test.importPath)
			if gotPath != test.wantPath || gotDependencyType != test.wantDependencyType {
				t.Errorf("projectImportForGolden() = (%q, %q), want (%q, %q)",
					gotPath,
					gotDependencyType,
					test.wantPath,
					test.wantDependencyType,
				)
			}
		})
	}
}

func TestValidateGoldenLocation(t *testing.T) {
	t.Parallel()

	packageMain := ExpectedViolation{
		Rule:     "package-main-placement",
		Severity: "error",
		From:     Location{Path: "internal/worker/main.go", Line: 1},
	}
	orphan := ExpectedViolation{
		Rule:     "no-orphans",
		Severity: "error",
		From:     Location{Path: "internal/lonely/lonely.go", Line: 1},
	}
	tests := []struct {
		name      string
		violation ExpectedViolation
		files     map[string]parsedFile
		wantError string
	}{
		{
			name:      "package main matches package placement golden",
			violation: packageMain,
			files: map[string]parsedFile{
				"internal/worker/main.go": {packageName: "main", packageLine: 1},
			},
		},
		{
			name:      "non-main package rejects package placement golden",
			violation: packageMain,
			files: map[string]parsedFile{
				"internal/worker/main.go": {packageName: "worker", packageLine: 1},
			},
			wantError: `package = "worker", want "main"`,
		},
		{
			name:      "disconnected file matches orphan golden",
			violation: orphan,
			files: map[string]parsedFile{
				"internal/lonely/lonely.go": {packageName: "lonely", packageLine: 1},
			},
		},
		{
			name:      "outgoing import rejects orphan golden",
			violation: orphan,
			files: map[string]parsedFile{
				"internal/lonely/lonely.go": {
					packageName: "lonely",
					packageLine: 1,
					imports: []parsedImport{
						{path: "fmt", dependencyType: "stdlib", line: 3},
					},
				},
			},
			wantError: "outgoing imports",
		},
		{
			name:      "incoming import rejects orphan golden",
			violation: orphan,
			files: map[string]parsedFile{
				"cmd/app/main.go": {
					packageName: "main",
					packageLine: 1,
					imports: []parsedImport{
						{path: "internal/lonely", dependencyType: "local", line: 3},
					},
				},
				"internal/lonely/lonely.go": {packageName: "lonely", packageLine: 1},
			},
			wantError: "incoming import",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := validateGoldenLocation(test.violation, test.files)
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("validateGoldenLocation() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("validateGoldenLocation() = %v, want error containing %q", err, test.wantError)
			}
		})
	}
}

func TestValidatePositiveControlLocation(t *testing.T) {
	t.Parallel()

	importControl := PositiveControl{
		Rule: "layer-direction",
		From: Location{Path: "internal/core/core.go", Line: 3},
		To:   &Dependency{Path: "internal/model", DependencyType: "local"},
	}
	packageControl := PositiveControl{
		Rule:        "package-main-placement",
		From:        Location{Path: "cmd/app/main.go", Line: 1},
		PackageName: "main",
	}
	tests := []struct {
		name      string
		control   PositiveControl
		files     map[string]parsedFile
		wantError string
	}{
		{
			name:    "import control matches",
			control: importControl,
			files: map[string]parsedFile{
				"internal/core/core.go": {
					packageName: "core",
					packageLine: 1,
					imports: []parsedImport{
						{path: "internal/model", dependencyType: "local", line: 3},
					},
				},
			},
		},
		{
			name:    "missing import rejects control",
			control: importControl,
			files: map[string]parsedFile{
				"internal/core/core.go": {packageName: "core", packageLine: 1},
			},
			wantError: "does not match an import",
		},
		{
			name:    "package control matches",
			control: packageControl,
			files: map[string]parsedFile{
				"cmd/app/main.go": {packageName: "main", packageLine: 1},
			},
		},
		{
			name:    "different package rejects control",
			control: packageControl,
			files: map[string]parsedFile{
				"cmd/app/main.go": {packageName: "app", packageLine: 1},
			},
			wantError: `package = "app", want "main"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := validatePositiveControlLocation(test.control, test.files)
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("validatePositiveControlLocation() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("validatePositiveControlLocation() = %v, want error containing %q", err, test.wantError)
			}
		})
	}
}

func TestValidatePositiveControl(t *testing.T) {
	t.Parallel()

	moduleDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(moduleDir, "source.go"), []byte("package fixture\n"), 0o600); err != nil {
		t.Fatalf("write source.go: %v", err)
	}
	location := Location{Path: "source.go", Line: 1}
	tests := []struct {
		name      string
		control   PositiveControl
		wantError string
	}{
		{
			name: "import control",
			control: PositiveControl{
				Rule: "rule",
				From: location,
				To:   &Dependency{Path: "fmt", DependencyType: "stdlib"},
			},
		},
		{
			name: "package control",
			control: PositiveControl{
				Rule:        "rule",
				From:        location,
				PackageName: "fixture",
			},
		},
		{
			name:      "missing control shape",
			control:   PositiveControl{Rule: "rule", From: location},
			wantError: "exactly one",
		},
		{
			name: "ambiguous control shape",
			control: PositiveControl{
				Rule:        "rule",
				From:        location,
				To:          &Dependency{Path: "fmt", DependencyType: "stdlib"},
				PackageName: "fixture",
			},
			wantError: "exactly one",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := validatePositiveControl(moduleDir, test.control)
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("validatePositiveControl() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("validatePositiveControl() = %v, want error containing %q", err, test.wantError)
			}
		})
	}
}

func vetFixtureModule(t *testing.T, moduleDir string) {
	t.Helper()

	command := exec.CommandContext(t.Context(), "go", "vet", "./...")
	command.Dir = moduleDir
	command.Env = append(os.Environ(), "GOWORK=off")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go vet ./...: %v\n%s", err, output)
	}
}

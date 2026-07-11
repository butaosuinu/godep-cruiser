package testcorpus

import (
	"bufio"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
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

			modulePath := readFixtureModulePath(t, fixture.ModuleDir)
			files := parseFixtureModule(t, fixture.ModuleDir, modulePath)
			if len(files) == 0 {
				t.Fatal("parsed 0 Go files, want at least 1")
			}
			assertGoldenLocations(t, fixture, files)
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
	packageLine int
	imports     []parsedImport
}

func readFixtureModulePath(t *testing.T, moduleDir string) string {
	t.Helper()

	file, err := os.Open(filepath.Join(moduleDir, "go.mod"))
	if err != nil {
		t.Fatalf("open go.mod: %v", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Errorf("close go.mod: %v", err)
		}
	}()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if modulePath, ok := strings.CutPrefix(strings.TrimSpace(scanner.Text()), "module "); ok {
			if modulePath == "" {
				t.Fatal("go.mod has an empty module directive")
			}
			return modulePath
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan go.mod: %v", err)
	}
	t.Fatal("go.mod has no module directive")
	return ""
}

func parseFixtureModule(t *testing.T, moduleDir, modulePath string) map[string]parsedFile {
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
		file := parsedFile{packageLine: fset.Position(parsed.Name.Pos()).Line}
		for _, spec := range parsed.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return fmt.Errorf("unquote import in %s: %w", filename, err)
			}
			normalized, dependencyType := normalizeImport(modulePath, importPath)
			file.imports = append(file.imports, parsedImport{
				path:           normalized,
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

func normalizeImport(modulePath, importPath string) (string, string) {
	if importPath == modulePath {
		return ".", "local"
	}
	if local, ok := strings.CutPrefix(importPath, modulePath+"/"); ok {
		return local, "local"
	}
	first, _, _ := strings.Cut(importPath, "/")
	if !strings.Contains(first, ".") {
		return importPath, "stdlib"
	}
	return importPath, "module"
}

func assertGoldenLocations(t *testing.T, fixture Case, files map[string]parsedFile) {
	t.Helper()

	for _, violation := range fixture.Violations {
		file, ok := files[violation.From.Path]
		if !ok {
			t.Errorf("golden from.path %q was not parsed", violation.From.Path)
			continue
		}
		if violation.To == nil {
			if violation.From.Line != file.packageLine {
				t.Errorf("source-only golden %s line = %d, want package line %d", violation.Rule, violation.From.Line, file.packageLine)
			}
			continue
		}

		found := slices.ContainsFunc(file.imports, func(found parsedImport) bool {
			return found.path == violation.To.Path &&
				found.dependencyType == violation.To.DependencyType &&
				found.line == violation.From.Line
		})
		if !found {
			t.Errorf("golden %s target %q (%s) at line %d does not match an import in %s",
				violation.Rule,
				violation.To.Path,
				violation.To.DependencyType,
				violation.From.Line,
				violation.From.Path,
			)
		}
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

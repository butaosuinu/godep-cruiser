package testcorpus

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/butaosuinu/godep-cruiser/internal/graph"
	"github.com/butaosuinu/godep-cruiser/internal/scanner"
)

func TestViolationCorpus(t *testing.T) {
	t.Parallel()

	cases, err := Load(filepath.Join("..", "..", "testdata", "corpus"))
	if err != nil {
		t.Fatalf("Load() = %v, want nil", err)
	}

	wantIDs := []string{
		"baseline-expiry",
		"folder-scope",
		"forbidden-import-target",
		"layer-direction",
		"number-of-dependents",
		"orphan-file",
		"package-main-placement",
		"reachable-test-helper",
		"required-dependency",
		"stdlib-denylist-exception",
		"stdlib-only-tree",
		"third-party-in-core",
		"unclassified-dependency",
		"unreachable-dead-code",
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
			if err := validateSourceOnlyViolationCompleteness(fixture, files); err != nil {
				t.Error(err)
			}
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
	if violation.From.Line == 0 {
		return validatePackageEdgeLocation(violation.Rule, violation.From, violation.To, files)
	}

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
		if violation.Rule == "production-no-testutil" {
			return validateReachableGoldenLocation(violation, file, files)
		}
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

func validatePackageEdgeLocation(
	rule string,
	from Location,
	to *Dependency,
	files map[string]parsedFile,
) error {
	if to == nil {
		return fmt.Errorf("folder-scoped golden %s has no package target", rule)
	}
	if to.DependencyType != string(scanner.DependencyTypeLocal) {
		return fmt.Errorf(
			"folder-scoped golden %s target %q has dependency type %q, want local",
			rule,
			to.Path,
			to.DependencyType,
		)
	}

	foundPackage := false
	for _, sourcePath := range sortedFilePaths(files) {
		if path.Dir(sourcePath) != from.Path {
			continue
		}
		foundPackage = true
		if slices.ContainsFunc(files[sourcePath].imports, func(found parsedImport) bool {
			return found.path == to.Path && found.dependencyType == string(scanner.DependencyTypeLocal)
		}) {
			return nil
		}
	}
	if !foundPackage {
		return fmt.Errorf("folder-scoped golden %s source package %q was not parsed", rule, from.Path)
	}
	return fmt.Errorf(
		"folder-scoped golden %s target package %q is not a local dependency of %q",
		rule,
		to.Path,
		from.Path,
	)
}

func validateReachableGoldenLocation(
	violation ExpectedViolation,
	file parsedFile,
	files map[string]parsedFile,
) error {
	if violation.To.DependencyType != string(scanner.DependencyTypeLocal) {
		return fmt.Errorf(
			"reachable golden %s target %q has dependency type %q, want local",
			violation.Rule,
			violation.To.Path,
			violation.To.DependencyType,
		)
	}

	seeds := make([]string, 0)
	for _, dependency := range file.imports {
		if dependency.line == violation.From.Line &&
			dependency.dependencyType == string(scanner.DependencyTypeLocal) {
			seeds = append(seeds, dependency.path)
		}
	}
	if len(seeds) == 0 {
		return fmt.Errorf(
			"reachable golden %s source %s has no local import at line %d",
			violation.Rule,
			violation.From.Path,
			violation.From.Line,
		)
	}

	closure := fixtureDependencyGraph(files).ForwardClosure(seeds...)
	if !slices.Contains(closure, violation.To.Path) {
		return fmt.Errorf(
			"reachable golden %s target %q is not reachable from local imports at %s:%d",
			violation.Rule,
			violation.To.Path,
			violation.From.Path,
			violation.From.Line,
		)
	}
	return nil
}

func fixtureDependencyGraph(files map[string]parsedFile) graph.Graph {
	graphFiles := make([]scanner.File, 0, len(files))
	for _, sourcePath := range sortedFilePaths(files) {
		file := files[sourcePath]
		graphFile := scanner.File{
			Path:        sourcePath,
			PackagePath: path.Dir(sourcePath),
			Imports:     make([]scanner.Import, 0, len(file.imports)),
		}
		for _, dependency := range file.imports {
			graphFile.Imports = append(graphFile.Imports, scanner.Import{
				ResolvedPath: dependency.path,
				Type:         scanner.DependencyType(dependency.dependencyType),
			})
		}
		graphFiles = append(graphFiles, graphFile)
	}

	return graph.Build(graphFiles)
}

func validateOrphanLocation(sourcePath string, file parsedFile, files map[string]parsedFile) error {
	if isOrphanLocation(sourcePath, file, files) {
		return nil
	}
	if len(file.imports) != 0 {
		return fmt.Errorf("source-only golden no-orphans file %s has %d outgoing imports", sourcePath, len(file.imports))
	}
	return fmt.Errorf("source-only golden no-orphans file %s has an incoming import from %s", sourcePath, incomingImporter(sourcePath, files))
}

func isOrphanLocation(sourcePath string, file parsedFile, files map[string]parsedFile) bool {
	return len(file.imports) == 0 && incomingImporter(sourcePath, files) == ""
}

func incomingImporter(sourcePath string, files map[string]parsedFile) string {
	packagePath := path.Dir(sourcePath)
	for _, importerPath := range sortedFilePaths(files) {
		importer := files[importerPath]
		incoming := slices.ContainsFunc(importer.imports, func(found parsedImport) bool {
			return found.dependencyType == string(scanner.DependencyTypeLocal) && found.path == packagePath
		})
		if incoming {
			return importerPath
		}
	}
	return ""
}

func validateSourceOnlyViolationCompleteness(fixture Case, files map[string]parsedFile) error {
	var rule string
	var matches func(string, parsedFile, map[string]parsedFile) bool
	switch fixture.ID {
	case "orphan-file":
		rule = "no-orphans"
		matches = isOrphanLocation
	case "package-main-placement":
		rule = "package-main-placement"
		matches = isMisplacedPackageMain
	default:
		return nil
	}

	actual := collectSourceOnlyLocations(files, matches)
	want := goldenSourceOnlyLocations(fixture.Violations, rule)
	if slices.Equal(actual, want) {
		return nil
	}
	return fmt.Errorf(
		"fixture %q actual source-only %s locations = %q, want golden locations %q",
		fixture.ID,
		rule,
		formatLocations(actual),
		formatLocations(want),
	)
}

func isMisplacedPackageMain(sourcePath string, file parsedFile, _ map[string]parsedFile) bool {
	return file.packageName == "main" &&
		!strings.HasPrefix(sourcePath, "cmd/") &&
		!strings.HasPrefix(sourcePath, "tools/")
}

func collectSourceOnlyLocations(
	files map[string]parsedFile,
	matches func(string, parsedFile, map[string]parsedFile) bool,
) []Location {
	locations := make([]Location, 0)
	for _, sourcePath := range sortedFilePaths(files) {
		file := files[sourcePath]
		if matches(sourcePath, file, files) {
			locations = append(locations, Location{Path: sourcePath, Line: file.packageLine})
		}
	}
	return locations
}

func goldenSourceOnlyLocations(violations []ExpectedViolation, rule string) []Location {
	locations := make([]Location, 0)
	for _, violation := range violations {
		if violation.Rule == rule && violation.To == nil {
			locations = append(locations, violation.From)
		}
	}
	slices.SortFunc(locations, compareLocations)
	return locations
}

func compareLocations(a, b Location) int {
	if byPath := strings.Compare(a.Path, b.Path); byPath != 0 {
		return byPath
	}
	switch {
	case a.Line < b.Line:
		return -1
	case a.Line > b.Line:
		return 1
	default:
		return 0
	}
}

func sortedFilePaths(files map[string]parsedFile) []string {
	paths := make([]string, 0, len(files))
	for sourcePath := range files {
		paths = append(paths, sourcePath)
	}
	slices.Sort(paths)
	return paths
}

func formatLocations(locations []Location) []string {
	formatted := make([]string, len(locations))
	for i, location := range locations {
		formatted[i] = fmt.Sprintf("%s:%d", location.Path, location.Line)
	}
	return formatted
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
	if control.From.Line == 0 {
		return validatePackageEdgeLocation(control.Rule, control.From, control.To, files)
	}

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
	reachable := ExpectedViolation{
		Rule:     "production-no-testutil",
		Severity: "error",
		From:     Location{Path: "internal/app/unsafe.go", Line: 3},
		To: &Dependency{
			Path:           "internal/testutil",
			DependencyType: string(scanner.DependencyTypeLocal),
		},
	}
	reachableFiles := map[string]parsedFile{
		"internal/app/unsafe.go": {
			packageName: "app",
			packageLine: 1,
			imports: []parsedImport{{
				path:           "internal/service",
				dependencyType: string(scanner.DependencyTypeLocal),
				line:           3,
			}},
		},
		"internal/service/service.go": {
			packageName: "service",
			packageLine: 1,
			imports: []parsedImport{{
				path:           "internal/testutil",
				dependencyType: string(scanner.DependencyTypeLocal),
				line:           3,
			}},
		},
		"internal/testutil/testutil.go": {packageName: "testutil", packageLine: 1},
	}
	folderEdge := ExpectedViolation{
		Rule:     "app-no-blocked",
		Severity: "warn",
		From:     Location{Path: "internal/app", Line: 0},
		To: &Dependency{
			Path:           "internal/blocked",
			DependencyType: string(scanner.DependencyTypeLocal),
		},
	}
	folderFiles := map[string]parsedFile{
		"internal/app/first.go": {
			packageName: "app",
			packageLine: 1,
			imports: []parsedImport{{
				path:           "internal/blocked",
				dependencyType: string(scanner.DependencyTypeLocal),
				line:           3,
			}},
		},
		"internal/app/second.go": {
			packageName: "app",
			packageLine: 1,
			imports: []parsedImport{{
				path:           "internal/blocked",
				dependencyType: string(scanner.DependencyTypeLocal),
				line:           3,
			}},
		},
		"internal/blocked/blocked.go": {packageName: "blocked", packageLine: 1},
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
		{
			name:      "reachable edge accepts a transitive local target",
			violation: reachable,
			files:     reachableFiles,
		},
		{
			name: "reachable edge rejects a line without a local seed import",
			violation: ExpectedViolation{
				Rule:     reachable.Rule,
				Severity: reachable.Severity,
				From:     Location{Path: reachable.From.Path, Line: 4},
				To:       reachable.To,
			},
			files:     reachableFiles,
			wantError: "has no local import at line 4",
		},
		{
			name: "reachable edge rejects a target outside the seed closure",
			violation: ExpectedViolation{
				Rule:     reachable.Rule,
				Severity: reachable.Severity,
				From:     reachable.From,
				To: &Dependency{
					Path:           "internal/other",
					DependencyType: string(scanner.DependencyTypeLocal),
				},
			},
			files:     reachableFiles,
			wantError: "is not reachable",
		},
		{
			name: "ordinary edge still rejects a transitive-only target",
			violation: ExpectedViolation{
				Rule:     "ordinary-edge",
				Severity: reachable.Severity,
				From:     reachable.From,
				To:       reachable.To,
			},
			files:     reachableFiles,
			wantError: "does not match an import",
		},
		{
			name:      "folder edge accepts a deduplicated local package dependency",
			violation: folderEdge,
			files:     folderFiles,
		},
		{
			name: "folder edge rejects a missing package dependency",
			violation: ExpectedViolation{
				Rule:     folderEdge.Rule,
				Severity: folderEdge.Severity,
				From:     folderEdge.From,
				To: &Dependency{
					Path:           "internal/missing",
					DependencyType: string(scanner.DependencyTypeLocal),
				},
			},
			files:     folderFiles,
			wantError: "is not a local dependency",
		},
		{
			name:      "folder edge rejects a missing source package",
			violation: folderEdge,
			files: map[string]parsedFile{
				"internal/blocked/blocked.go": {packageName: "blocked", packageLine: 1},
			},
			wantError: "source package \"internal/app\" was not parsed",
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

func TestValidateSourceOnlyViolationCompleteness(t *testing.T) {
	t.Parallel()

	orphanFixture := Case{
		ID: "orphan-file",
		Violations: []ExpectedViolation{
			{
				Rule:     "no-orphans",
				Severity: "error",
				From:     Location{Path: "internal/lonely/lonely.go", Line: 1},
			},
		},
	}
	orphanFiles := map[string]parsedFile{
		"cmd/app/main.go": {
			packageName: "main",
			packageLine: 1,
			imports: []parsedImport{
				{path: "internal/connected", dependencyType: "local", line: 3},
			},
		},
		"internal/connected/connected.go": {packageName: "connected", packageLine: 1},
		"internal/lonely/lonely.go":       {packageName: "lonely", packageLine: 1},
	}
	packageMainFixture := Case{
		ID: "package-main-placement",
		Violations: []ExpectedViolation{
			{
				Rule:     "package-main-placement",
				Severity: "error",
				From:     Location{Path: "internal/worker/main.go", Line: 1},
			},
		},
	}
	packageMainFiles := map[string]parsedFile{
		"cmd/app/main.go":          {packageName: "main", packageLine: 1},
		"internal/library/file.go": {packageName: "library", packageLine: 1},
		"internal/worker/main.go":  {packageName: "main", packageLine: 1},
		"tools/generate/main.go":   {packageName: "main", packageLine: 1},
	}

	tests := []struct {
		name      string
		fixture   Case
		files     map[string]parsedFile
		wantError string
	}{
		{
			name:    "orphan actual set matches golden",
			fixture: orphanFixture,
			files:   orphanFiles,
		},
		{
			name:    "orphan missing from golden is rejected deterministically",
			fixture: orphanFixture,
			files: mergeParsedFiles(orphanFiles, map[string]parsedFile{
				"internal/also/also.go": {packageName: "also", packageLine: 1},
			}),
			wantError: `fixture "orphan-file" actual source-only no-orphans locations = ["internal/also/also.go:1" "internal/lonely/lonely.go:1"], want golden locations ["internal/lonely/lonely.go:1"]`,
		},
		{
			name:    "misplaced package main actual set matches golden",
			fixture: packageMainFixture,
			files:   packageMainFiles,
		},
		{
			name:    "misplaced package main missing from golden is rejected deterministically",
			fixture: packageMainFixture,
			files: mergeParsedFiles(packageMainFiles, map[string]parsedFile{
				"cmdtool/main.go": {packageName: "main", packageLine: 1},
			}),
			wantError: `fixture "package-main-placement" actual source-only package-main-placement locations = ["cmdtool/main.go:1" "internal/worker/main.go:1"], want golden locations ["internal/worker/main.go:1"]`,
		},
		{
			name:    "unrelated fixture has no completeness predicate",
			fixture: Case{ID: "layer-direction"},
			files:   map[string]parsedFile{"lonely.go": {packageName: "main", packageLine: 1}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := validateSourceOnlyViolationCompleteness(test.fixture, test.files)
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("validateSourceOnlyViolationCompleteness() = %v, want nil", err)
				}
				return
			}
			if err == nil || err.Error() != test.wantError {
				t.Fatalf("validateSourceOnlyViolationCompleteness() = %v, want %q", err, test.wantError)
			}
		})
	}
}

func mergeParsedFiles(base, extra map[string]parsedFile) map[string]parsedFile {
	merged := make(map[string]parsedFile, len(base)+len(extra))
	maps.Copy(merged, base)
	maps.Copy(merged, extra)
	return merged
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

func TestValidateViolationRuleShape(t *testing.T) {
	t.Parallel()

	moduleDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(moduleDir, "source.go"), []byte("package fixture\n"), 0o600); err != nil {
		t.Fatalf("write source.go: %v", err)
	}
	location := Location{Path: "source.go", Line: 1}
	edge := &Dependency{Path: "fmt", DependencyType: "stdlib"}
	folderLocation := Location{Path: ".", Line: 0}
	localPackage := &Dependency{Path: "internal/target", DependencyType: "local"}
	tests := []struct {
		name      string
		violation ExpectedViolation
		wantError string
	}{
		{
			name:      "edge rule keeps import target",
			violation: ExpectedViolation{Rule: "rule", Severity: "error", From: location, To: edge},
		},
		{
			name:      "edge rule rejects source-only shape",
			violation: ExpectedViolation{Rule: "rule", Severity: "error", From: location},
			wantError: `edge rule "rule" must set to`,
		},
		{
			name:      "folder edge keeps a local package target",
			violation: ExpectedViolation{Rule: "rule", Severity: "error", From: folderLocation, To: localPackage},
		},
		{
			name:      "folder edge rejects a non-local target",
			violation: ExpectedViolation{Rule: "rule", Severity: "error", From: folderLocation, To: edge},
			wantError: `folder-scoped edge rule "rule" must target a local package`,
		},
		{
			name:      "package main rule stays source-only",
			violation: ExpectedViolation{Rule: "package-main-placement", Severity: "error", From: location},
		},
		{
			name:      "package main rule rejects import target",
			violation: ExpectedViolation{Rule: "package-main-placement", Severity: "error", From: location, To: edge},
			wantError: `source-only rule "package-main-placement" must not set to`,
		},
		{
			name:      "dependent count rule stays source-only",
			violation: ExpectedViolation{Rule: "minimum-two-dependents", Severity: "error", From: location},
		},
		{
			name:      "dependent count rule rejects import target",
			violation: ExpectedViolation{Rule: "minimum-two-dependents", Severity: "error", From: location, To: edge},
			wantError: `source-only rule "minimum-two-dependents" must not set to`,
		},
		{
			name:      "dependent count upper-bound rule stays source-only",
			violation: ExpectedViolation{Rule: "maximum-two-dependents", Severity: "error", From: location},
		},
		{
			name:      "dependent count upper-bound rule rejects import target",
			violation: ExpectedViolation{Rule: "maximum-two-dependents", Severity: "error", From: location, To: edge},
			wantError: `source-only rule "maximum-two-dependents" must not set to`,
		},
		{
			name:      "orphan rule stays source-only",
			violation: ExpectedViolation{Rule: "no-orphans", Severity: "error", From: location},
		},
		{
			name:      "orphan rule rejects import target",
			violation: ExpectedViolation{Rule: "no-orphans", Severity: "error", From: location, To: edge},
			wantError: `source-only rule "no-orphans" must not set to`,
		},
		{
			name:      "required rule stays source-only",
			violation: ExpectedViolation{Rule: "handler-requires-logging", Severity: "error", From: location},
		},
		{
			name:      "required rule rejects import target",
			violation: ExpectedViolation{Rule: "handler-requires-logging", Severity: "error", From: location, To: edge},
			wantError: `source-only rule "handler-requires-logging" must not set to`,
		},
		{
			name:      "unreachable rule stays source-only",
			violation: ExpectedViolation{Rule: "entrypoint-reaches-production", Severity: "error", From: location},
		},
		{
			name:      "unreachable rule rejects import target",
			violation: ExpectedViolation{Rule: "entrypoint-reaches-production", Severity: "error", From: location, To: edge},
			wantError: `source-only rule "entrypoint-reaches-production" must not set to`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := validateViolation(moduleDir, test.violation)
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("validateViolation() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("validateViolation() = %v, want error containing %q", err, test.wantError)
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
	folderLocation := Location{Path: ".", Line: 0}
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
			name: "folder edge control",
			control: PositiveControl{
				Rule: "rule",
				From: folderLocation,
				To:   &Dependency{Path: "internal/target", DependencyType: "local"},
			},
		},
		{
			name: "package control",
			control: PositiveControl{
				Rule:        "package-main-placement",
				From:        location,
				PackageName: "fixture",
			},
		},
		{
			name: "orphan edge control",
			control: PositiveControl{
				Rule: "no-orphans",
				From: location,
				To:   &Dependency{Path: "internal/connected", DependencyType: "local"},
			},
		},
		{
			name: "edge rule rejects package control",
			control: PositiveControl{
				Rule:        "rule",
				From:        location,
				PackageName: "fixture",
			},
			wantError: `edge rule "rule" positive control must set to, not packageName`,
		},
		{
			name: "package rule rejects import control",
			control: PositiveControl{
				Rule: "package-main-placement",
				From: location,
				To:   &Dependency{Path: "fmt", DependencyType: "stdlib"},
			},
			wantError: "package-main-placement positive control must set packageName, not to",
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

func TestDecodeGoldenRejectsDuplicateKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		contents  string
		wantError string
	}{
		{
			name:      "top-level key",
			contents:  `{"name":"rule: behavior","violations":[],"violations":[]}`,
			wantError: `duplicate key "violations" at $`,
		},
		{
			name:      "nested dependency key",
			contents:  `{"name":"rule: behavior","violations":[{"rule":"rule","severity":"error","from":{"path":"source.go","line":1},"to":{"path":"fmt","path":"os","dependencyType":"stdlib"}}]}`,
			wantError: `duplicate key "path" at $.violations[0].to`,
		},
		{
			name:      "escaped equivalent key",
			contents:  `{"name":"rule: behavior","\u006eame":"rule: replaced","violations":[]}`,
			wantError: `duplicate key "name" at $`,
		},
		{
			name:     "same key in separate objects",
			contents: `{"name":"rule: behavior","positiveControls":[{"rule":"one","from":{"path":"one.go","line":1},"packageName":"one"},{"rule":"two","from":{"path":"two.go","line":1},"packageName":"two"}],"violations":[]}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			filename := filepath.Join(t.TempDir(), goldenFilename)
			if err := os.WriteFile(filename, []byte(test.contents), 0o600); err != nil {
				t.Fatalf("write %s: %v", goldenFilename, err)
			}
			_, err := decodeGolden(filename)
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("decodeGolden() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("decodeGolden() = %v, want error containing %q", err, test.wantError)
			}
		})
	}
}

func TestValidateCaseFactIdentities(t *testing.T) {
	t.Parallel()

	moduleDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(moduleDir, "source.go"), []byte("package fixture\n"), 0o600); err != nil {
		t.Fatalf("write source.go: %v", err)
	}
	location := Location{Path: "source.go", Line: 1}
	edge := &Dependency{Path: "fmt", DependencyType: "stdlib"}
	tests := []struct {
		name      string
		fixture   Case
		wantError string
	}{
		{
			name: "violation identity ignores severity",
			fixture: Case{
				Name:      "rule: duplicate severity",
				ModuleDir: moduleDir,
				Violations: []ExpectedViolation{
					{Rule: "rule", Severity: "error", From: location, To: edge},
					{Rule: "rule", Severity: "warn", From: location, To: edge},
				},
			},
			wantError: "duplicates identity",
		},
		{
			name: "edge cannot be positive and violated",
			fixture: Case{
				Name:             "rule: contradictory edge",
				ModuleDir:        moduleDir,
				PositiveControls: []PositiveControl{{Rule: "rule", From: location, To: edge}},
				Violations:       []ExpectedViolation{{Rule: "rule", Severity: "error", From: location, To: edge}},
			},
			wantError: "contradicts positiveControls",
		},
		{
			name: "source cannot be positive and violated",
			fixture: Case{
				Name:      "rule: contradictory source",
				ModuleDir: moduleDir,
				PositiveControls: []PositiveControl{
					{Rule: "package-main-placement", From: location, PackageName: "fixture"},
				},
				Violations: []ExpectedViolation{{Rule: "package-main-placement", Severity: "error", From: location}},
			},
			wantError: "contradicts positiveControls",
		},
		{
			name: "positive source identity ignores package name",
			fixture: Case{
				Name:      "rule: duplicate positive source",
				ModuleDir: moduleDir,
				PositiveControls: []PositiveControl{
					{Rule: "package-main-placement", From: location, PackageName: "fixture"},
					{Rule: "package-main-placement", From: location, PackageName: "other"},
				},
				Violations: []ExpectedViolation{{Rule: "other-rule", Severity: "error", From: location, To: edge}},
			},
			wantError: "duplicates identity",
		},
		{
			name: "different rules remain distinct",
			fixture: Case{
				Name:             "rule: distinct facts",
				ModuleDir:        moduleDir,
				PositiveControls: []PositiveControl{{Rule: "allowed-rule", From: location, To: edge}},
				Violations:       []ExpectedViolation{{Rule: "forbidden-rule", Severity: "error", From: location, To: edge}},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := validateCase(test.fixture)
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("validateCase() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("validateCase() = %v, want error containing %q", err, test.wantError)
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

// Package testcorpus loads the repository's engine-independent violation corpus.
package testcorpus

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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

// Dependency identifies the normalized target of an import edge.
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

// Case describes one standalone fixture module and its expected violations.
type Case struct {
	ID         string              `json:"-"`
	Name       string              `json:"name"`
	ModuleDir  string              `json:"-"`
	Violations []ExpectedViolation `json:"violations"`
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
	if !validGoPath(violation.From.Path) {
		return fmt.Errorf("from.path %q is not a module-relative .go path", violation.From.Path)
	}
	if violation.From.Line < 1 {
		return errors.New("from.line must be positive")
	}
	if err := requireFile(filepath.Join(moduleDir, filepath.FromSlash(violation.From.Path))); err != nil {
		return fmt.Errorf("from.path: %w", err)
	}
	if violation.To == nil {
		return nil
	}
	if violation.To.Path == "" {
		return errors.New("to.path must not be empty")
	}
	if !validDependencyType(violation.To.DependencyType) {
		return fmt.Errorf("to.dependencyType %q is not stdlib, local, module, or unresolved", violation.To.DependencyType)
	}
	if violation.To.DependencyType == "local" && violation.To.Path != "." && !validRelativePath(violation.To.Path) {
		return fmt.Errorf("local to.path %q is not module-relative", violation.To.Path)
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

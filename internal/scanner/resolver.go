package scanner

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Resolver classifies imports relative to one Go module.
type Resolver struct {
	modulePath string
	moduleRoot string
}

// NewResolver constructs a Resolver for modulePath without binding it to a
// filesystem module root. Scan treats its explicit root as the module root when
// populating File.PackagePath.
func NewResolver(modulePath string) (Resolver, error) {
	if modulePath == "" {
		return Resolver{}, fmt.Errorf("module path is empty")
	}
	if strings.TrimSpace(modulePath) != modulePath {
		return Resolver{}, fmt.Errorf("module path %q has surrounding whitespace", modulePath)
	}
	if strings.HasSuffix(modulePath, "/") {
		return Resolver{}, fmt.Errorf("module path %q has a trailing slash", modulePath)
	}

	return Resolver{modulePath: modulePath}, nil
}

// NewResolverFromGoMod reads the module directive in goModPath and constructs
// a Resolver bound to the module file's directory. go.work files and nested
// module discovery are not used.
func NewResolverFromGoMod(goModPath string) (Resolver, error) {
	file, err := os.Open(goModPath)
	if err != nil {
		return Resolver{}, fmt.Errorf("open go.mod %q: %w", goModPath, err)
	}

	modulePath, readErr := readModulePath(file)
	closeErr := file.Close()
	if readErr != nil {
		return Resolver{}, fmt.Errorf("read go.mod %q: %w", goModPath, readErr)
	}
	if closeErr != nil {
		return Resolver{}, fmt.Errorf("close go.mod %q: %w", goModPath, closeErr)
	}

	resolver, err := NewResolver(modulePath)
	if err != nil {
		return Resolver{}, fmt.Errorf("read go.mod %q: %w", goModPath, err)
	}
	moduleRoot, err := canonicalDirectory(filepath.Dir(goModPath))
	if err != nil {
		return Resolver{}, fmt.Errorf("resolve module root for go.mod %q: %w", goModPath, err)
	}
	resolver.moduleRoot = moduleRoot

	return resolver, nil
}

// ModulePath returns the module path used by the resolver.
func (r Resolver) ModulePath() string {
	return r.modulePath
}

// Resolve classifies and normalizes importPath. The cgo pseudo-import "C" is
// intentionally unresolved because it does not name a Go package.
func (r Resolver) Resolve(importPath string) Resolution {
	if importPath == "" || importPath == "C" {
		return Resolution{Type: DependencyTypeUnresolved}
	}

	if r.modulePath != "" {
		if importPath == r.modulePath {
			return Resolution{Path: ".", Type: DependencyTypeLocal}
		}

		modulePrefix := r.modulePath + "/"
		if relativePath, ok := strings.CutPrefix(importPath, modulePrefix); ok {
			return Resolution{
				Path: relativePath,
				Type: DependencyTypeLocal,
			}
		}
	}

	firstSegment, _, _ := strings.Cut(importPath, "/")
	if !strings.Contains(firstSegment, ".") {
		return Resolution{Path: importPath, Type: DependencyTypeStdlib}
	}

	return Resolution{Path: importPath, Type: DependencyTypeModule}
}

func canonicalDirectory(directory string) (string, error) {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return "", fmt.Errorf("make %q absolute: %w", directory, err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", directory, err)
	}

	return filepath.Clean(resolved), nil
}

func readModulePath(file *os.File) (string, error) {
	scanner := bufio.NewScanner(file)
	var modulePath string
	found := false

	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := scanner.Text()
		if lineNumber == 1 {
			line = strings.TrimPrefix(line, "\ufeff")
		}
		line, _, _ = strings.Cut(line, "//")

		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "module" {
			continue
		}
		if len(fields) < 2 {
			return "", fmt.Errorf("module directive on line %d has no path", lineNumber)
		}
		if found {
			return "", fmt.Errorf("multiple module directives (second on line %d)", lineNumber)
		}

		path, err := unquoteModulePath(fields[1])
		if err != nil {
			return "", fmt.Errorf("module directive on line %d: %w", lineNumber, err)
		}
		modulePath = path
		found = true
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan module file: %w", err)
	}
	if !found {
		return "", fmt.Errorf("module directive not found")
	}

	return modulePath, nil
}

func unquoteModulePath(value string) (string, error) {
	if !strings.HasPrefix(value, "\"") && !strings.HasPrefix(value, "`") {
		return value, nil
	}

	modulePath, err := strconv.Unquote(value)
	if err != nil {
		return "", fmt.Errorf("invalid quoted module path %q: %w", value, err)
	}

	return modulePath, nil
}

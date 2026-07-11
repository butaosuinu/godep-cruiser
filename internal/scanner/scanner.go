package scanner

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Scan parses every .go file below root and returns file-level imports in path
// order. Build constraints, _test suffixes, and platform suffixes are not
// evaluated. Skip-directory rules apply below root; the explicitly named root
// itself is always scanned.
func Scan(root string, resolver Resolver) ([]File, error) {
	if resolver.modulePath == "" {
		return nil, fmt.Errorf("scan root %q: resolver module path is empty", root)
	}

	root = filepath.Clean(root)
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat scan root %q: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("scan root %q is not a directory", root)
	}
	walkRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve scan root %q: %w", root, err)
	}

	files := make([]File, 0)
	err = filepath.WalkDir(walkRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != walkRoot && shouldSkipDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") {
			return nil
		}

		parsedFile, parseErr := parseFile(walkRoot, path, resolver)
		if parseErr != nil {
			return parseErr
		}
		files = append(files, parsedFile)

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan root %q: %w", root, err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNoGoFiles, root)
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})

	return files, nil
}

func parseFile(root, path string, resolver Resolver) (File, error) {
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, path, nil, parser.ImportsOnly)
	if err != nil {
		return File{}, fmt.Errorf("parse %q: %w", path, err)
	}

	relativePath, err := filepath.Rel(root, path)
	if err != nil {
		return File{}, fmt.Errorf("make %q relative to %q: %w", path, root, err)
	}

	imports := make([]Import, 0, len(parsed.Imports))
	for _, spec := range parsed.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return File{}, fmt.Errorf("unquote import in %q: %w", path, err)
		}

		resolution := resolver.Resolve(importPath)
		imports = append(imports, Import{
			Path:         importPath,
			ResolvedPath: resolution.Path,
			Type:         resolution.Type,
			Line:         fileSet.PositionFor(spec.Path.Pos(), false).Line,
		})
	}

	return File{
		Path:    filepath.ToSlash(relativePath),
		Package: parsed.Name.Name,
		Imports: imports,
	}, nil
}

func shouldSkipDirectory(name string) bool {
	return name == "testdata" || name == "vendor" ||
		strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

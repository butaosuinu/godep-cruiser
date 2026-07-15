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

// Scan parses every .go file below root and returns file-level imports in Path
// order. Path remains relative to root, while PackagePath is relative to the
// resolver's module root. Build constraints, _test suffixes, and platform
// suffixes are not evaluated. Skip-directory rules apply below root; the
// explicitly named root itself is always scanned.
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
	moduleRelativeRoot, err := moduleRelativeScanRoot(walkRoot, resolver)
	if err != nil {
		return nil, fmt.Errorf("scan root %q: %w", root, err)
	}

	files := make([]File, 0)
	err = filepath.WalkDir(walkRoot, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if filename != walkRoot && shouldSkipDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") {
			return nil
		}

		parsedFile, parseErr := parseFile(walkRoot, filename, moduleRelativeRoot, resolver)
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

func parseFile(root, filename, moduleRelativeRoot string, resolver Resolver) (File, error) {
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, filename, nil, parser.ImportsOnly)
	if err != nil {
		return File{}, fmt.Errorf("parse %q: %w", filename, err)
	}

	relativePath, err := filepath.Rel(root, filename)
	if err != nil {
		return File{}, fmt.Errorf("make %q relative to %q: %w", filename, root, err)
	}

	imports := make([]Import, 0, len(parsed.Imports))
	for _, spec := range parsed.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return File{}, fmt.Errorf("unquote import in %q: %w", filename, err)
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
		Path:        filepath.ToSlash(relativePath),
		PackagePath: filepath.ToSlash(filepath.Dir(filepath.Join(moduleRelativeRoot, relativePath))),
		Package:     parsed.Name.Name,
		PackageLine: fileSet.PositionFor(parsed.Name.Pos(), false).Line,
		Imports:     imports,
	}, nil
}

func moduleRelativeScanRoot(walkRoot string, resolver Resolver) (string, error) {
	if resolver.moduleRoot == "" {
		return ".", nil
	}
	absoluteWalkRoot, err := canonicalDirectory(walkRoot)
	if err != nil {
		return "", err
	}
	relativeRoot, err := filepath.Rel(resolver.moduleRoot, absoluteWalkRoot)
	if err != nil {
		return "", fmt.Errorf(
			"make resolved root %q relative to module root %q: %w",
			absoluteWalkRoot,
			resolver.moduleRoot,
			err,
		)
	}
	if relativeRoot == ".." || strings.HasPrefix(relativeRoot, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf(
			"resolved root %q is outside module root %q",
			absoluteWalkRoot,
			resolver.moduleRoot,
		)
	}

	return filepath.Clean(relativeRoot), nil
}

func shouldSkipDirectory(name string) bool {
	return name == "testdata" || name == "vendor" ||
		strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

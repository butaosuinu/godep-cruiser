package scanner

import "errors"

// ErrNoGoFiles reports a scan root from which no Go files were parsed.
var ErrNoGoFiles = errors.New("scan root contains no Go files")

// DependencyType is the mutually exclusive classification of an import.
type DependencyType string

const (
	// DependencyTypeStdlib identifies an import whose first path segment has no dot.
	DependencyTypeStdlib DependencyType = "stdlib"
	// DependencyTypeLocal identifies an import within the resolver's Go module.
	DependencyTypeLocal DependencyType = "local"
	// DependencyTypeModule identifies an import from another Go module.
	DependencyTypeModule DependencyType = "module"
	// DependencyTypeUnresolved identifies an import that cannot be resolved.
	DependencyTypeUnresolved DependencyType = "unresolved"
)

// Resolution describes how an import path is classified and normalized.
type Resolution struct {
	// Path is module-relative for local imports and unchanged for stdlib and
	// third-party module imports. It is empty for unresolved imports.
	Path string
	Type DependencyType
}

// Import records one import declaration in a Go source file.
type Import struct {
	// Path is the unquoted path exactly as declared by the source file.
	Path string
	// ResolvedPath follows the normalization documented by Resolution.Path.
	ResolvedPath string
	Type         DependencyType
	Line         int
}

// File records the dependency information parsed from one Go source file.
type File struct {
	// Path is slash-separated and relative to the explicit scan root.
	Path    string
	Package string
	Imports []Import
}

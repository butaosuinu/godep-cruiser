// Package scanner builds a file-level import graph from Go source files.
//
// Scans intentionally ignore build constraints and file-name selection rules:
// every .go file below an explicit scan root is parsed, including tests and
// platform-specific files. A Resolver represents exactly one Go module.
// Workspaces and automatic discovery of nested modules are deliberately out of
// scope; callers choose the go.mod file used to construct the Resolver.
package scanner

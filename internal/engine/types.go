package engine

import (
	"github.com/butaosuinu/godep-cruiser/config"
	"github.com/butaosuinu/godep-cruiser/internal/scanner"
)

// NotInAllowedRuleName is the reserved name for dependencies that match no
// allowed rule.
const NotInAllowedRuleName = "not-in-allowed"

// ViolationKind identifies why the engine reported a violation.
type ViolationKind string

const (
	// ViolationKindForbidden identifies a matching forbidden rule.
	ViolationKindForbidden ViolationKind = "forbidden"
	// ViolationKindNotAllowed identifies a dependency outside the allowed set.
	ViolationKindNotAllowed ViolationKind = NotInAllowedRuleName
	// ViolationKindRequired identifies a source missing a required dependency.
	ViolationKindRequired ViolationKind = "required"
	// ViolationKindReachable identifies a package transitively reachable from a
	// matching source file's local imports.
	ViolationKindReachable ViolationKind = "reachable"
	// ViolationKindUnreachable identifies a source whose package is not reachable
	// from the matching seed packages.
	ViolationKindUnreachable ViolationKind = "unreachable"
)

// Source identifies the importing source of a violation. Folder-scoped
// violations use a package path with line zero and an empty package name.
type Source struct {
	Path        string
	Line        int
	PackageName string
}

// Dependency identifies the imported target of an edge violation.
type Dependency struct {
	// Path is the normalized resolver path, or ImportPath when resolution has
	// no path.
	Path string
	// ImportPath is the path exactly as declared in the source file. It is empty
	// when the target is a synthesized package node, as with reachable and
	// folder-scoped violations.
	ImportPath string
	Type       scanner.DependencyType
}

// Violation describes one rule violation without discarding information used
// by reporters or baseline matching. To is nil for source-only violations.
type Violation struct {
	Rule     string
	Comment  string
	Severity config.Severity
	Kind     ViolationKind
	From     Source
	To       *Dependency
}

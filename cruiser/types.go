package cruiser

import (
	"github.com/butaosuinu/godep-cruiser/config"
	"github.com/butaosuinu/godep-cruiser/internal/baseline"
	"github.com/butaosuinu/godep-cruiser/internal/engine"
)

// NotInAllowedRuleName is the rule name used for dependencies that match no
// allowed rule.
const NotInAllowedRuleName = engine.NotInAllowedRuleName

// Options controls one validation run.
type Options struct {
	// ScanRoot is the directory whose Go files are inspected. An empty value
	// means the current directory.
	ScanRoot string
	// GoModPath names the single go.mod used to classify imports. An empty
	// value means "go.mod" below ScanRoot. go.work and nested-module discovery
	// are intentionally not performed. ScanRoot must be within the module
	// directory containing GoModPath.
	GoModPath string
	// Baseline suppresses exact matching current violations and makes unmatched
	// entries stale errors. A nil value disables baseline filtering.
	Baseline *Baseline
}

// ViolationKind identifies why a rule produced a violation.
type ViolationKind = engine.ViolationKind

// Supported violation kinds.
const (
	ViolationKindForbidden   ViolationKind = engine.ViolationKindForbidden
	ViolationKindNotAllowed  ViolationKind = engine.ViolationKindNotAllowed
	ViolationKindRequired    ViolationKind = engine.ViolationKindRequired
	ViolationKindReachable   ViolationKind = engine.ViolationKindReachable
	ViolationKindUnreachable ViolationKind = engine.ViolationKindUnreachable
)

// Source identifies the importing source of a violation. Folder-scoped
// violations use a package path with line zero and an empty package name.
type Source = engine.Source

// Dependency identifies the imported target of an edge violation. Path is
// module-relative for local dependencies and otherwise the resolved dependency
// path; it falls back to ImportPath when resolution has no path. ImportPath is
// the path exactly as declared by the Go source file and is empty when the
// target is a synthesized package node, as with reachable and folder-scoped
// violations.
type Dependency = engine.Dependency

// Violation describes one unsuppressed or baseline-known rule violation. To is
// nil for source-only rules such as orphan, package-name, dependent-count,
// required, and unreachable checks.
type Violation = engine.Violation

// StaleError reports a baseline entry that no longer matches a current
// violation. Stale entries are always errors regardless of rule severity.
type StaleError = baseline.StaleError

// Result contains the reportable, baseline-known, and stale outcomes of one
// validation run. Known violations are suppressed from reports and exit codes.
type Result struct {
	Violations []Violation
	Known      []Violation
	Stale      []StaleError
}

// ErrorCount returns the number of unsuppressed error-severity violations plus
// stale baseline entries.
func (result Result) ErrorCount() int {
	count := len(result.Stale)
	for _, violation := range result.Violations {
		if violation.Severity == config.SeverityError {
			count++
		}
	}

	return count
}

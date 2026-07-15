package cruiser

import (
	"fmt"

	"github.com/butaosuinu/godep-cruiser/config"
)

// NotInAllowedRuleName is the rule name used for dependencies that match no
// allowed rule.
const NotInAllowedRuleName = "not-in-allowed"

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
type ViolationKind string

// Supported violation kinds.
const (
	ViolationKindForbidden  ViolationKind = "forbidden"
	ViolationKindNotAllowed ViolationKind = NotInAllowedRuleName
	ViolationKindRequired   ViolationKind = "required"
)

// Source identifies the importing file and source position of a violation.
type Source struct {
	Path        string
	Line        int
	PackageName string
}

// Dependency identifies the imported target of an edge violation.
type Dependency struct {
	// Path is module-relative for local dependencies and otherwise the resolved
	// dependency path. It falls back to ImportPath when resolution has no path.
	Path string
	// ImportPath is the path exactly as declared by the Go source file.
	ImportPath string
	Type       config.DependencyType
}

// Violation describes one unsuppressed or baseline-known rule violation. To is
// nil for source-only rules such as orphan, package-name, and required checks.
type Violation struct {
	Rule     string
	Comment  string
	Severity config.Severity
	Kind     ViolationKind
	From     Source
	To       *Dependency
}

// StaleError reports a baseline entry that no longer matches a current
// violation. Stale entries are always errors regardless of rule severity.
type StaleError struct {
	Entry BaselineEntry
}

// Error describes the stale identity and how to resolve it.
func (err StaleError) Error() string {
	if err.Entry.To == nil {
		return fmt.Sprintf(
			"baseline entry is stale: rule %q, from %q; remove this entry from the baseline.",
			err.Entry.Rule,
			err.Entry.From,
		)
	}

	return fmt.Sprintf(
		"baseline entry is stale: rule %q, from %q, to %q; remove this entry from the baseline.",
		err.Entry.Rule,
		err.Entry.From,
		*err.Entry.To,
	)
}

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

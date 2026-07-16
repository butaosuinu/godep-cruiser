package baseline

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/butaosuinu/godep-cruiser/internal/engine"
)

// Baseline contains exact identities of known violations.
type Baseline struct {
	Entries []Entry `json:"entries"`
}

// Entry identifies one known violation. To is nil for source-only violations.
// Edge entries use the raw import path when present, and otherwise use the
// synthesized package path. Line, severity, dependency type, kind, and comment
// are not part of the identity.
type Entry struct {
	Rule string  `json:"rule"`
	From string  `json:"from"`
	To   *string `json:"to,omitempty"`
}

// Result partitions current violations from baseline entries that have gone
// stale. Violations contains current, unsuppressed violations, while Known
// contains current violations suppressed by the baseline.
type Result struct {
	Violations []engine.Violation
	Known      []engine.Violation
	Stale      []StaleError
}

// StaleError reports a baseline entry that no longer matches a current
// violation.
type StaleError struct {
	Entry Entry
}

// Error describes the exact stale identity and how to resolve it.
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

// Generate creates a stable, deduplicated baseline from current violations.
func Generate(violations []engine.Violation) Baseline {
	entries := make([]Entry, 0, len(violations))
	seen := make(map[identity]struct{}, len(violations))
	for _, violation := range violations {
		entry := entryFromViolation(violation)
		key := identityFromEntry(entry)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		entries = append(entries, entry)
	}

	sortEntries(entries)

	return Baseline{Entries: entries}
}

// Apply partitions current violations by the baseline and reports every
// baseline entry that has no current exact match. Current violation values are
// returned unchanged, including their configured severity.
func Apply(known Baseline, current []engine.Violation) Result {
	entriesByIdentity := make(map[identity]Entry, len(known.Entries))
	for _, entry := range known.Entries {
		entriesByIdentity[identityFromEntry(entry)] = entry
	}

	matched := make(map[identity]struct{}, len(entriesByIdentity))
	var result Result
	for _, violation := range current {
		key := identityFromViolation(violation)
		if _, exists := entriesByIdentity[key]; exists {
			result.Known = append(result.Known, violation)
			matched[key] = struct{}{}
			continue
		}
		result.Violations = append(result.Violations, violation)
	}

	entries := slices.Clone(known.Entries)
	sortEntries(entries)
	for _, entry := range entries {
		key := identityFromEntry(entry)
		if _, exists := matched[key]; exists {
			continue
		}
		result.Stale = append(result.Stale, StaleError{Entry: entry})
	}

	return result
}

type identity struct {
	rule  string
	from  string
	to    string
	hasTo bool
}

func entryFromViolation(violation engine.Violation) Entry {
	entry := Entry{
		Rule: violation.Rule,
		From: violation.From.Path,
	}
	if violation.To != nil {
		to := violation.To.ImportPath
		if to == "" {
			to = violation.To.Path
		}
		entry.To = &to
	}

	return entry
}

func identityFromViolation(violation engine.Violation) identity {
	return identityFromEntry(entryFromViolation(violation))
}

func identityFromEntry(entry Entry) identity {
	key := identity{
		rule: entry.Rule,
		from: entry.From,
	}
	if entry.To != nil {
		key.to = *entry.To
		key.hasTo = true
	}

	return key
}

func sortEntries(entries []Entry) {
	slices.SortStableFunc(entries, compareEntries)
}

func compareEntries(left, right Entry) int {
	if byRule := cmp.Compare(left.Rule, right.Rule); byRule != 0 {
		return byRule
	}
	if byFrom := cmp.Compare(left.From, right.From); byFrom != 0 {
		return byFrom
	}
	if left.To == nil && right.To != nil {
		return -1
	}
	if left.To != nil && right.To == nil {
		return 1
	}
	if left.To == nil {
		return 0
	}

	return cmp.Compare(*left.To, *right.To)
}

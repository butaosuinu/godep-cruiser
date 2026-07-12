package cruiser

import (
	"fmt"
	"io"
	"os"

	internalbaseline "github.com/butaosuinu/godep-cruiser/internal/baseline"
)

// Baseline contains exact identities of known violations.
type Baseline struct {
	Entries []BaselineEntry `json:"entries"`
}

// BaselineEntry identifies one known violation. To is nil for source-only
// violations. Resolved paths, lines, severity, and comments are not identities.
type BaselineEntry struct {
	Rule string  `json:"rule"`
	From string  `json:"from"`
	To   *string `json:"to,omitempty"`
}

// GenerateBaseline creates a stable, deduplicated baseline from current
// violations. Edge entries use the import path as written in source.
func GenerateBaseline(violations []Violation) Baseline {
	return fromInternalBaseline(internalbaseline.Generate(toEngineViolations(violations)))
}

// LoadBaseline reads one strict baseline JSON document.
func LoadBaseline(reader io.Reader) (Baseline, error) {
	loaded, err := internalbaseline.Load(reader)
	if err != nil {
		return Baseline{}, err
	}

	return fromInternalBaseline(loaded), nil
}

// LoadBaselineFile reads one strict baseline JSON document from filename.
func LoadBaselineFile(filename string) (Baseline, error) {
	file, err := os.Open(filename)
	if err != nil {
		return Baseline{}, fmt.Errorf("open baseline %q: %w", filename, err)
	}

	loaded, loadErr := LoadBaseline(file)
	closeErr := file.Close()
	if loadErr != nil {
		return Baseline{}, fmt.Errorf("load baseline %q: %w", filename, loadErr)
	}
	if closeErr != nil {
		return Baseline{}, fmt.Errorf("close baseline %q: %w", filename, closeErr)
	}

	return loaded, nil
}

// WriteBaseline emits a validated baseline as stable, indented JSON.
func WriteBaseline(writer io.Writer, known Baseline) error {
	return internalbaseline.Write(writer, toInternalBaseline(known))
}

func fromInternalBaseline(known internalbaseline.Baseline) Baseline {
	entries := make([]BaselineEntry, len(known.Entries))
	for index, entry := range known.Entries {
		entries[index] = fromInternalBaselineEntry(entry)
	}

	return Baseline{Entries: entries}
}

func toInternalBaseline(known Baseline) internalbaseline.Baseline {
	entries := make([]internalbaseline.Entry, len(known.Entries))
	for index, entry := range known.Entries {
		entries[index] = toInternalBaselineEntry(entry)
	}

	return internalbaseline.Baseline{Entries: entries}
}

func fromInternalStaleErrors(stale []internalbaseline.StaleError) []StaleError {
	converted := make([]StaleError, len(stale))
	for index, staleError := range stale {
		converted[index] = StaleError{Entry: fromInternalBaselineEntry(staleError.Entry)}
	}

	return converted
}

func toInternalStaleErrors(stale []StaleError) []internalbaseline.StaleError {
	converted := make([]internalbaseline.StaleError, len(stale))
	for index, staleError := range stale {
		converted[index] = internalbaseline.StaleError{Entry: toInternalBaselineEntry(staleError.Entry)}
	}

	return converted
}

func fromInternalBaselineEntry(entry internalbaseline.Entry) BaselineEntry {
	return BaselineEntry{
		Rule: entry.Rule,
		From: entry.From,
		To:   cloneStringPointer(entry.To),
	}
}

func toInternalBaselineEntry(entry BaselineEntry) internalbaseline.Entry {
	return internalbaseline.Entry{
		Rule: entry.Rule,
		From: entry.From,
		To:   cloneStringPointer(entry.To),
	}
}

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}

	clone := *value

	return &clone
}

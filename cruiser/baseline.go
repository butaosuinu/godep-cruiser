package cruiser

import (
	"fmt"
	"io"
	"os"

	"github.com/butaosuinu/godep-cruiser/internal/baseline"
)

// Baseline contains exact identities of known violations.
type Baseline = baseline.Baseline

// BaselineEntry identifies one known violation. To is nil for source-only
// violations. Ordinary edges use the raw import path; reachable edges use the
// target package path. Lines, severity, and comments are not identities.
type BaselineEntry = baseline.Entry

// GenerateBaseline creates a stable, deduplicated baseline from current
// violations. Ordinary edge entries use the import path as written in source;
// reachable edge entries use the target package path.
func GenerateBaseline(violations []Violation) Baseline {
	return baseline.Generate(violations)
}

// LoadBaseline reads one strict baseline JSON document.
func LoadBaseline(reader io.Reader) (Baseline, error) {
	loaded, err := baseline.Load(reader)
	if err != nil {
		return Baseline{}, err
	}

	return loaded, nil
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
	return baseline.Write(writer, known)
}

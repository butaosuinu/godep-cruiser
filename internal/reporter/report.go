package reporter

import (
	"github.com/butaosuinu/godep-cruiser/internal/baseline"
	"github.com/butaosuinu/godep-cruiser/internal/engine"
)

// Report contains every reportable outcome of one validation run. Baseline-
// known violations are omitted before constructing a Report.
type Report struct {
	Violations []engine.Violation
	Stale      []baseline.StaleError
}

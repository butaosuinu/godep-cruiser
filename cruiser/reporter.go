package cruiser

import (
	"fmt"
	"io"

	"github.com/butaosuinu/godep-cruiser/internal/reporter"
)

// OutputType selects a report representation.
type OutputType string

// Supported report representations.
const (
	OutputTypeErr     OutputType = "err"
	OutputTypeJSON    OutputType = "json"
	OutputTypeMermaid OutputType = "mermaid"
)

// WriteReport writes unsuppressed violations and stale baseline entries in the
// selected representation. Baseline-known violations remain suppressed.
func WriteReport(writer io.Writer, outputType OutputType, result Result) error {
	report := reporter.Report{
		Violations: toEngineViolations(result.Violations),
		Stale:      toInternalStaleErrors(result.Stale),
	}

	switch outputType {
	case OutputTypeErr:
		return reporter.WriteErrReport(writer, report)
	case OutputTypeJSON:
		return reporter.WriteJSONReport(writer, report)
	case OutputTypeMermaid:
		return reporter.WriteMermaidReport(writer, report)
	default:
		return fmt.Errorf("unknown output type %q", outputType)
	}
}

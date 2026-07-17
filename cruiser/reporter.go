package cruiser

import (
	"fmt"
	"io"

	"github.com/butaosuinu/godep-cruiser/internal/reporter"
)

// OutputType selects the err, JSON, Mermaid, GraphViz DOT, or HTML report
// representation.
type OutputType string

// Supported report representations.
const (
	OutputTypeErr     OutputType = "err"
	OutputTypeJSON    OutputType = "json"
	OutputTypeMermaid OutputType = "mermaid"
	OutputTypeDOT     OutputType = "dot"
	OutputTypeHTML    OutputType = "html"
)

// WriteReport writes unsuppressed violations and stale baseline entries in the
// selected representation. Baseline-known violations remain suppressed.
func WriteReport(writer io.Writer, outputType OutputType, result Result) error {
	report := reporter.Report{
		Violations: result.Violations,
		Stale:      result.Stale,
	}

	switch outputType {
	case OutputTypeErr:
		return reporter.WriteErrReport(writer, report)
	case OutputTypeJSON:
		return reporter.WriteJSONReport(writer, report)
	case OutputTypeMermaid:
		return reporter.WriteMermaidReport(writer, report)
	case OutputTypeDOT:
		return reporter.WriteDOTReport(writer, report)
	case OutputTypeHTML:
		return reporter.WriteHTMLReport(writer, report)
	default:
		return fmt.Errorf("unknown output type %q", outputType)
	}
}

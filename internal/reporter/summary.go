package reporter

import "github.com/butaosuinu/godep-cruiser/internal/engine"

// Summary counts reported outcomes by severity. Stale baseline entries count
// as errors.
type Summary struct {
	Total  int `json:"total"`
	Error  int `json:"error"`
	Warn   int `json:"warn"`
	Info   int `json:"info"`
	Ignore int `json:"ignore"`
}

// Summarize counts violations without changing their order.
func Summarize(violations []engine.Violation) Summary {
	return SummarizeReport(Report{Violations: violations})
}

// SummarizeReport counts violations by severity and counts each stale baseline
// entry as an error.
func SummarizeReport(report Report) Summary {
	summary := Summary{
		Total: len(report.Violations) + len(report.Stale),
		Error: len(report.Stale),
	}
	for _, violation := range report.Violations {
		switch string(violation.Severity) {
		case "error":
			summary.Error++
		case "warn":
			summary.Warn++
		case "info":
			summary.Info++
		case "ignore":
			summary.Ignore++
		}
	}

	return summary
}

// ErrorCount returns the number of error-severity violations.
func ErrorCount(violations []engine.Violation) int {
	return ReportErrorCount(Report{Violations: violations})
}

// ReportErrorCount returns the number of error-severity violations and stale
// baseline entries in report.
func ReportErrorCount(report Report) int {
	return SummarizeReport(report).Error
}

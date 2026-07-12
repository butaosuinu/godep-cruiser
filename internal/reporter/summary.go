package reporter

import "github.com/butaosuinu/godep-cruiser/internal/engine"

// Summary counts violations by severity.
type Summary struct {
	Total  int `json:"total"`
	Error  int `json:"error"`
	Warn   int `json:"warn"`
	Info   int `json:"info"`
	Ignore int `json:"ignore"`
}

// Summarize counts violations without changing their order.
func Summarize(violations []engine.Violation) Summary {
	summary := Summary{Total: len(violations)}
	for _, violation := range violations {
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
	return Summarize(violations).Error
}

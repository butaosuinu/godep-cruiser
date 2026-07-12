package reporter

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/butaosuinu/godep-cruiser/internal/engine"
)

// JSONSchemaVersion is the version of the JSON report schema emitted by
// WriteJSON.
const JSONSchemaVersion = 1

// JSONReport is the stable, machine-readable representation of a report.
type JSONReport struct {
	SchemaVersion        int                      `json:"schemaVersion"`
	Violations           []JSONViolation          `json:"violations"`
	StaleBaselineEntries []JSONStaleBaselineEntry `json:"staleBaselineEntries,omitempty"`
	Summary              Summary                  `json:"summary"`
}

// JSONStaleBaselineEntry is the stable JSON representation of a stale
// baseline entry.
type JSONStaleBaselineEntry struct {
	Rule    string  `json:"rule"`
	From    string  `json:"from"`
	To      *string `json:"to,omitempty"`
	Message string  `json:"message"`
}

// JSONViolation is the stable JSON representation of an engine violation.
type JSONViolation struct {
	Rule     string          `json:"rule"`
	Comment  string          `json:"comment"`
	Severity string          `json:"severity"`
	Kind     string          `json:"kind"`
	From     JSONSource      `json:"from"`
	To       *JSONDependency `json:"to"`
}

// JSONSource identifies the importing source location in a JSON report.
type JSONSource struct {
	Path        string `json:"path"`
	Line        int    `json:"line"`
	PackageName string `json:"packageName"`
}

// JSONDependency identifies an imported dependency in a JSON report.
type JSONDependency struct {
	Path           string `json:"path"`
	ImportPath     string `json:"importPath"`
	DependencyType string `json:"dependencyType"`
}

// WriteJSON writes violations and their summary as an indented JSON report.
func WriteJSON(writer io.Writer, violations []engine.Violation) error {
	return WriteJSONReport(writer, Report{Violations: violations})
}

// WriteJSONReport writes report as an indented JSON document.
func WriteJSONReport(writer io.Writer, input Report) error {
	report := JSONReport{
		SchemaVersion:        JSONSchemaVersion,
		Violations:           make([]JSONViolation, len(input.Violations)),
		StaleBaselineEntries: make([]JSONStaleBaselineEntry, len(input.Stale)),
		Summary:              SummarizeReport(input),
	}
	for index, violation := range input.Violations {
		report.Violations[index] = jsonViolation(violation)
	}
	for index, stale := range input.Stale {
		report.StaleBaselineEntries[index] = JSONStaleBaselineEntry{
			Rule:    stale.Entry.Rule,
			From:    stale.Entry.From,
			To:      stale.Entry.To,
			Message: stale.Error(),
		}
	}

	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("encode JSON report: %w", err)
	}

	return nil
}

func jsonViolation(violation engine.Violation) JSONViolation {
	projected := JSONViolation{
		Rule:     violation.Rule,
		Comment:  violation.Comment,
		Severity: string(violation.Severity),
		Kind:     string(violation.Kind),
		From: JSONSource{
			Path:        violation.From.Path,
			Line:        violation.From.Line,
			PackageName: violation.From.PackageName,
		},
	}
	if violation.To != nil {
		projected.To = &JSONDependency{
			Path:           violation.To.Path,
			ImportPath:     violation.To.ImportPath,
			DependencyType: string(violation.To.Type),
		}
	}

	return projected
}

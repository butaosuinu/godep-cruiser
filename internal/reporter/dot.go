package reporter

import (
	"fmt"
	"io"
	"strings"

	"github.com/butaosuinu/godep-cruiser/internal/engine"
)

// WriteDOT writes the violation-induced dependency subgraph as a GraphViz DOT
// directed graph. The engine exposes violations rather than the complete
// scanned graph, so every emitted edge is a violation.
func WriteDOT(writer io.Writer, violations []engine.Violation) error {
	return WriteDOTReport(writer, Report{Violations: violations})
}

// WriteDOTReport writes the violation-induced dependency subgraph and a
// standalone error node for every stale baseline entry.
func WriteDOTReport(writer io.Writer, report Report) error {
	nodes, edges := buildViolationGraph(report.Violations)

	var output strings.Builder
	output.WriteString("digraph violations {\n")
	output.WriteString("  rankdir=LR;\n")
	output.WriteString("  node [shape=box];\n")
	if len(nodes) == 0 && len(report.Stale) == 0 {
		output.WriteString("  no_violations [label=\"No violations\"];\n")
		output.WriteString("}\n")

		return writeDOTOutput(writer, output.String())
	}

	for _, node := range nodes {
		fmt.Fprintf(
			&output,
			"  %s [label=\"%s\"",
			node.id,
			escapeDOTQuotedString(violationGraphNodeLabel(node)),
		)
		if len(node.sourceViolationLabels) > 0 {
			output.WriteString(", color=\"#cf222e\", fillcolor=\"#ffebe9\", penwidth=2, style=\"filled\"")
		}
		output.WriteString("];\n")
	}
	for index, stale := range report.Stale {
		fmt.Fprintf(
			&output,
			"  stale%d [label=\"%s\", color=\"#cf222e\", fillcolor=\"#ffebe9\", penwidth=2, style=\"filled\"];\n",
			index,
			escapeDOTQuotedString("[error] "+stale.Error()),
		)
	}
	for _, edge := range edges {
		fmt.Fprintf(
			&output,
			"  %s -> %s [label=\"%s\", color=\"#cf222e\", penwidth=3];\n",
			edge.fromID,
			edge.toID,
			escapeDOTQuotedString(violationGraphEdgeLabel(edge)),
		)
	}
	output.WriteString("}\n")

	return writeDOTOutput(writer, output.String())
}

func escapeDOTQuotedString(value string) string {
	var escaped strings.Builder
	for _, character := range value {
		switch character {
		case '\\':
			escaped.WriteString(`\\`)
		case '"':
			escaped.WriteString(`\"`)
		case '\n':
			escaped.WriteString(`\n`)
		case '\r':
			escaped.WriteString(`\r`)
		default:
			escaped.WriteRune(character)
		}
	}

	return escaped.String()
}

func writeDOTOutput(writer io.Writer, output string) error {
	written, err := io.WriteString(writer, output)
	if err != nil {
		return fmt.Errorf("write DOT report: %w", err)
	}
	if written != len(output) {
		return fmt.Errorf("write DOT report: %w", io.ErrShortWrite)
	}

	return nil
}

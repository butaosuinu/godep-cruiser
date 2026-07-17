package reporter

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/butaosuinu/godep-cruiser/internal/engine"
)

// WriteMermaid writes the violation-induced dependency subgraph as a Mermaid
// flowchart. The engine exposes violations rather than the complete scanned
// graph, so every emitted edge is a violation and is highlighted.
func WriteMermaid(writer io.Writer, violations []engine.Violation) error {
	return WriteMermaidReport(writer, Report{Violations: violations})
}

// WriteMermaidReport writes the violation-induced dependency subgraph and a
// standalone error node for every stale baseline entry.
func WriteMermaidReport(writer io.Writer, report Report) error {
	nodes, edges := buildViolationGraph(report.Violations)

	var output strings.Builder
	output.WriteString("flowchart LR\n")
	if len(nodes) == 0 && len(report.Stale) == 0 {
		output.WriteString("  no_violations[\"No violations\"]\n")

		return writeMermaidOutput(writer, output.String())
	}

	for _, node := range nodes {
		fmt.Fprintf(
			&output,
			"  %s[\"%s\"]\n",
			node.id,
			escapeMermaidLabel(violationGraphNodeLabel(node)),
		)
	}
	for index, stale := range report.Stale {
		fmt.Fprintf(
			&output,
			"  stale%d[\"%s\"]\n",
			index,
			escapeMermaidLabel("[error] "+stale.Error()),
		)
	}
	for _, edge := range edges {
		fmt.Fprintf(
			&output,
			"  %s -->|\"%s\"| %s\n",
			edge.fromID,
			escapeMermaidLabel(violationGraphEdgeLabel(edge)),
			edge.toID,
		)
	}

	hasSourceViolations := false
	for _, node := range nodes {
		if len(node.sourceViolationLabels) > 0 {
			hasSourceViolations = true
			break
		}
	}
	if hasSourceViolations {
		output.WriteString("  classDef sourceViolation fill:#ffebe9,stroke:#cf222e,stroke-width:2px\n")
		for _, node := range nodes {
			if len(node.sourceViolationLabels) > 0 {
				fmt.Fprintf(&output, "  class %s sourceViolation\n", node.id)
			}
		}
	}
	if len(report.Stale) > 0 {
		output.WriteString("  classDef staleBaselineError fill:#ffebe9,stroke:#cf222e,stroke-width:2px\n")
		for index := range report.Stale {
			fmt.Fprintf(&output, "  class stale%d staleBaselineError\n", index)
		}
	}
	if len(edges) > 0 {
		output.WriteString("  linkStyle default stroke:#cf222e,stroke-width:3px\n")
	}

	return writeMermaidOutput(writer, output.String())
}

func escapeMermaidLabel(value string) string {
	var escaped strings.Builder
	for _, character := range value {
		switch character {
		case '"', '#', '&', '<', '>', '\\', '|', '[', ']', '\n', '\r':
			writeMermaidEntity(&escaped, character)
		default:
			if character < ' ' || character == '\u007f' {
				writeMermaidEntity(&escaped, character)
				continue
			}
			escaped.WriteRune(character)
		}
	}

	return escaped.String()
}

func writeMermaidEntity(builder *strings.Builder, character rune) {
	builder.WriteByte('#')
	builder.WriteString(strconv.Itoa(int(character)))
	builder.WriteByte(';')
}

func writeMermaidOutput(writer io.Writer, output string) error {
	written, err := io.WriteString(writer, output)
	if err != nil {
		return fmt.Errorf("write Mermaid report: %w", err)
	}
	if written != len(output) {
		return fmt.Errorf("write Mermaid report: %w", io.ErrShortWrite)
	}

	return nil
}

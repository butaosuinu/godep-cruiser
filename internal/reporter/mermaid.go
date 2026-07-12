package reporter

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/butaosuinu/godep-cruiser/internal/engine"
)

type mermaidNodeKind uint8

const (
	mermaidSourceNode mermaidNodeKind = iota
	mermaidDependencyNode
)

type mermaidNodeKey struct {
	kind           mermaidNodeKind
	path           string
	dependencyType string
}

type mermaidNode struct {
	id                    string
	label                 string
	sourceViolationLabels []string
}

type mermaidEdgeKey struct {
	from mermaidNodeKey
	to   mermaidNodeKey
	line int
}

type mermaidEdge struct {
	fromID          string
	toID            string
	line            int
	violationLabels []string
}

// WriteMermaid writes the violation-induced dependency subgraph as a Mermaid
// flowchart. The engine exposes violations rather than the complete scanned
// graph, so every emitted edge is a violation and is highlighted.
func WriteMermaid(writer io.Writer, violations []engine.Violation) error {
	nodes, edges := buildMermaidGraph(violations)

	var output strings.Builder
	output.WriteString("flowchart LR\n")
	if len(nodes) == 0 {
		output.WriteString("  no_violations[\"No violations\"]\n")

		return writeMermaidOutput(writer, output.String())
	}

	for _, node := range nodes {
		label := node.label
		if len(node.sourceViolationLabels) > 0 {
			label += " (" + strings.Join(node.sourceViolationLabels, "; ") + ")"
		}
		fmt.Fprintf(&output, "  %s[\"%s\"]\n", node.id, escapeMermaidLabel(label))
	}
	for _, edge := range edges {
		label := fmt.Sprintf("line %d: %s", edge.line, strings.Join(edge.violationLabels, "; "))
		fmt.Fprintf(
			&output,
			"  %s -->|\"%s\"| %s\n",
			edge.fromID,
			escapeMermaidLabel(label),
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
	if len(edges) > 0 {
		output.WriteString("  linkStyle default stroke:#cf222e,stroke-width:3px\n")
	}

	return writeMermaidOutput(writer, output.String())
}

func buildMermaidGraph(violations []engine.Violation) ([]mermaidNode, []mermaidEdge) {
	nodes := make([]mermaidNode, 0)
	nodeIndexes := make(map[mermaidNodeKey]int)
	edges := make([]mermaidEdge, 0)
	edgeIndexes := make(map[mermaidEdgeKey]int)

	ensureNode := func(key mermaidNodeKey, label string) int {
		if index, ok := nodeIndexes[key]; ok {
			return index
		}

		index := len(nodes)
		nodeIndexes[key] = index
		nodes = append(nodes, mermaidNode{
			id:    "n" + strconv.Itoa(index),
			label: label,
		})

		return index
	}

	for _, violation := range violations {
		fromKey := mermaidNodeKey{kind: mermaidSourceNode, path: violation.From.Path}
		fromIndex := ensureNode(fromKey, violation.From.Path)
		violationLabel := fmt.Sprintf("%s [%s]", violation.Rule, violation.Severity)
		if violation.To == nil {
			nodes[fromIndex].sourceViolationLabels = append(
				nodes[fromIndex].sourceViolationLabels,
				fmt.Sprintf("%s @ line %d", violationLabel, violation.From.Line),
			)
			continue
		}

		toKey := mermaidNodeKey{
			kind:           mermaidDependencyNode,
			path:           violation.To.Path,
			dependencyType: string(violation.To.Type),
		}
		toIndex := ensureNode(toKey, dependencyNodeLabel(violation.To))
		edgeKey := mermaidEdgeKey{from: fromKey, to: toKey, line: violation.From.Line}
		if edgeIndex, ok := edgeIndexes[edgeKey]; ok {
			edges[edgeIndex].violationLabels = append(edges[edgeIndex].violationLabels, violationLabel)
			continue
		}

		edgeIndexes[edgeKey] = len(edges)
		edges = append(edges, mermaidEdge{
			fromID:          nodes[fromIndex].id,
			toID:            nodes[toIndex].id,
			line:            violation.From.Line,
			violationLabels: []string{violationLabel},
		})
	}

	return nodes, edges
}

func dependencyNodeLabel(dependency *engine.Dependency) string {
	if dependency.Type == "" {
		return dependency.Path
	}

	return fmt.Sprintf("%s (%s)", dependency.Path, dependency.Type)
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

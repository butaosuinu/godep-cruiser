package reporter

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/butaosuinu/godep-cruiser/internal/engine"
)

type violationGraphNodeKind uint8

const (
	violationGraphSourceNode violationGraphNodeKind = iota
	violationGraphDependencyNode
)

type violationGraphNodeKey struct {
	kind           violationGraphNodeKind
	path           string
	dependencyType string
}

type violationGraphNode struct {
	id                    string
	label                 string
	sourceViolationLabels []string
}

type violationGraphEdgeKey struct {
	from violationGraphNodeKey
	to   violationGraphNodeKey
	line int
}

type violationGraphEdge struct {
	fromID          string
	toID            string
	line            int
	violationLabels []string
}

func buildViolationGraph(violations []engine.Violation) ([]violationGraphNode, []violationGraphEdge) {
	nodes := make([]violationGraphNode, 0)
	nodeIndexes := make(map[violationGraphNodeKey]int)
	edges := make([]violationGraphEdge, 0)
	edgeIndexes := make(map[violationGraphEdgeKey]int)

	ensureNode := func(key violationGraphNodeKey, label string) int {
		if index, ok := nodeIndexes[key]; ok {
			return index
		}

		index := len(nodes)
		nodeIndexes[key] = index
		nodes = append(nodes, violationGraphNode{
			id:    "n" + strconv.Itoa(index),
			label: label,
		})

		return index
	}

	for _, violation := range violations {
		fromKey := violationGraphNodeKey{kind: violationGraphSourceNode, path: violation.From.Path}
		fromIndex := ensureNode(fromKey, violation.From.Path)
		violationLabel := fmt.Sprintf("%s [%s]", violation.Rule, violation.Severity)
		if violation.To == nil {
			nodes[fromIndex].sourceViolationLabels = append(
				nodes[fromIndex].sourceViolationLabels,
				fmt.Sprintf("%s @ line %d", violationLabel, violation.From.Line),
			)
			continue
		}

		toKey := violationGraphNodeKey{
			kind:           violationGraphDependencyNode,
			path:           violation.To.Path,
			dependencyType: string(violation.To.Type),
		}
		toIndex := ensureNode(toKey, dependencyNodeLabel(violation.To))
		edgeKey := violationGraphEdgeKey{from: fromKey, to: toKey, line: violation.From.Line}
		if edgeIndex, ok := edgeIndexes[edgeKey]; ok {
			edges[edgeIndex].violationLabels = append(edges[edgeIndex].violationLabels, violationLabel)
			continue
		}

		edgeIndexes[edgeKey] = len(edges)
		edges = append(edges, violationGraphEdge{
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

func violationGraphNodeLabel(node violationGraphNode) string {
	if len(node.sourceViolationLabels) == 0 {
		return node.label
	}

	return node.label + " (" + strings.Join(node.sourceViolationLabels, "; ") + ")"
}

func violationGraphEdgeLabel(edge violationGraphEdge) string {
	return fmt.Sprintf("line %d: %s", edge.line, strings.Join(edge.violationLabels, "; "))
}

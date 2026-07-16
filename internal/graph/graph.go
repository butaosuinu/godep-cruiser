package graph

import (
	"path"
	"slices"

	"github.com/butaosuinu/godep-cruiser/internal/scanner"
)

// Graph is a package-level view of local imports. Package identities are
// module-relative directories. Edges between files in the same directory are
// excluded from dependency views, but retained by IsImported for orphan
// compatibility.
type Graph struct {
	forward  map[string]map[string]struct{}
	reverse  map[string]map[string]struct{}
	imported map[string]struct{}
}

// Build constructs a Graph from scanner files. Local imports are deduplicated
// at package granularity. Non-local imports do not form graph edges; observed
// local targets without scanned files are retained as leaf nodes. Files with
// an empty PackagePath are ignored instead of being treated as the root package.
func Build(files []scanner.File) Graph {
	graph := Graph{
		forward:  make(map[string]map[string]struct{}),
		reverse:  make(map[string]map[string]struct{}),
		imported: make(map[string]struct{}),
	}
	for _, file := range files {
		packagePath, ok := cleanPackagePath(file.PackagePath)
		if !ok {
			continue
		}
		graph.forward[packagePath] = make(map[string]struct{})
		graph.reverse[packagePath] = make(map[string]struct{})
	}

	for _, file := range files {
		from, ok := cleanPackagePath(file.PackagePath)
		if !ok {
			continue
		}
		for _, dependency := range file.Imports {
			if dependency.Type != scanner.DependencyTypeLocal || dependency.ResolvedPath == "" {
				continue
			}

			to := path.Clean(dependency.ResolvedPath)
			graph.imported[to] = struct{}{}
			if from == to {
				continue
			}
			if _, known := graph.forward[to]; !known {
				graph.forward[to] = make(map[string]struct{})
				graph.reverse[to] = make(map[string]struct{})
			}

			graph.forward[from][to] = struct{}{}
			graph.reverse[to][from] = struct{}{}
		}
	}

	return graph
}

// Dependents returns the distinct packages that directly import packagePath.
// Same-directory imports are excluded, which prevents external test imports
// and other self edges from becoming package-level dependents.
func (graph Graph) Dependents(packagePath string) []string {
	packagePath, ok := cleanPackagePath(packagePath)
	if !ok {
		return nil
	}

	return sortedKeys(graph.reverse[packagePath])
}

// Dependencies returns the distinct packages directly imported by packagePath.
// Same-directory imports and non-local imports are excluded.
func (graph Graph) Dependencies(packagePath string) []string {
	packagePath, ok := cleanPackagePath(packagePath)
	if !ok {
		return nil
	}

	return sortedKeys(graph.forward[packagePath])
}

// ForwardClosure returns the known seed packages and every package reachable
// from them by following local imports. Unknown seeds are ignored.
func (graph Graph) ForwardClosure(packagePaths ...string) []string {
	return graph.closure(graph.forward, packagePaths)
}

// ReverseClosure returns the known seed packages and every package that can
// reach them by following local imports in reverse. Unknown seeds are ignored.
func (graph Graph) ReverseClosure(packagePaths ...string) []string {
	return graph.closure(graph.reverse, packagePaths)
}

// FanIn returns the number of distinct direct package-level dependents.
func (graph Graph) FanIn(packagePath string) int {
	packagePath, ok := cleanPackagePath(packagePath)
	if !ok {
		return 0
	}

	return len(graph.reverse[packagePath])
}

// FanOut returns the number of distinct direct package-level dependencies.
func (graph Graph) FanOut(packagePath string) int {
	packagePath, ok := cleanPackagePath(packagePath)
	if !ok {
		return 0
	}

	return len(graph.forward[packagePath])
}

// IsImported reports whether any scanned file has a local import resolved to
// packagePath. Unlike Dependents and FanIn, same-directory imports count.
func (graph Graph) IsImported(packagePath string) bool {
	packagePath, ok := cleanPackagePath(packagePath)
	if !ok {
		return false
	}
	_, imported := graph.imported[packagePath]

	return imported
}

func (graph Graph) closure(
	edges map[string]map[string]struct{},
	packagePaths []string,
) []string {
	visited := make(map[string]struct{}, len(packagePaths))
	queue := make([]string, 0, len(packagePaths))
	for _, packagePath := range packagePaths {
		var ok bool
		packagePath, ok = cleanPackagePath(packagePath)
		if !ok {
			continue
		}
		if _, known := graph.forward[packagePath]; !known {
			continue
		}
		if _, seen := visited[packagePath]; seen {
			continue
		}
		visited[packagePath] = struct{}{}
		queue = append(queue, packagePath)
	}

	for index := 0; index < len(queue); index++ {
		current := queue[index]
		for _, next := range sortedKeys(edges[current]) {
			if _, seen := visited[next]; seen {
				continue
			}
			visited[next] = struct{}{}
			queue = append(queue, next)
		}
	}

	return sortedKeys(visited)
}

func cleanPackagePath(packagePath string) (string, bool) {
	if packagePath == "" {
		return "", false
	}

	return path.Clean(packagePath), true
}

func sortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	return keys
}

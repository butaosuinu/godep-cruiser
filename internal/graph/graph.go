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
	forward  map[string]packageEdges
	reverse  map[string]stringSet
	imported stringSet
}

type stringSet map[string]struct{}

// packageEdges maps each target package to the source files that form that
// package-level edge.
type packageEdges map[string]stringSet

// Build constructs a Graph from scanner files. Local imports are deduplicated
// at package granularity. Non-local imports do not form graph edges; observed
// local targets without scanned files are retained as leaf nodes. Files with
// an empty PackagePath are ignored instead of being treated as the root package.
func Build(files []scanner.File) Graph {
	graph := Graph{
		forward:  make(map[string]packageEdges),
		reverse:  make(map[string]stringSet),
		imported: make(stringSet),
	}
	for _, file := range files {
		packagePath, ok := cleanPackagePath(file.PackagePath)
		if !ok {
			continue
		}
		graph.forward[packagePath] = make(packageEdges)
		graph.reverse[packagePath] = make(stringSet)
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
				graph.forward[to] = make(packageEdges)
				graph.reverse[to] = make(stringSet)
			}

			provenance := graph.forward[from][to]
			if provenance == nil {
				provenance = make(stringSet)
				graph.forward[from][to] = provenance
			}
			provenance[file.Path] = struct{}{}
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
	return graph.ForwardClosureWithFilePredicate(nil, packagePaths...)
}

// ForwardClosureWithFilePredicate returns the known seed packages and every
// package reachable from them through local import edges. An edge is traversed
// when acceptFile is nil or at least one source file forming that edge is
// accepted. Unknown seeds are ignored.
func (graph Graph) ForwardClosureWithFilePredicate(
	acceptFile func(string) bool,
	packagePaths ...string,
) []string {
	return graph.closure(packagePaths, func(packagePath string) []string {
		dependencies := sortedKeys(graph.forward[packagePath])
		if acceptFile == nil {
			return dependencies
		}

		accepted := dependencies[:0]
		for _, dependency := range dependencies {
			if anyAccepted(graph.forward[packagePath][dependency], acceptFile) {
				accepted = append(accepted, dependency)
			}
		}

		return accepted
	})
}

// ReverseClosure returns the known seed packages and every package that can
// reach them by following local imports in reverse. Unknown seeds are ignored.
func (graph Graph) ReverseClosure(packagePaths ...string) []string {
	return graph.closure(packagePaths, func(packagePath string) []string {
		return sortedKeys(graph.reverse[packagePath])
	})
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
	packagePaths []string,
	nextPackages func(string) []string,
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
		for _, next := range nextPackages(current) {
			if _, seen := visited[next]; seen {
				continue
			}
			visited[next] = struct{}{}
			queue = append(queue, next)
		}
	}

	return sortedKeys(visited)
}

func anyAccepted(filePaths stringSet, acceptFile func(string) bool) bool {
	return slices.ContainsFunc(sortedKeys(filePaths), acceptFile)
}

func cleanPackagePath(packagePath string) (string, bool) {
	if packagePath == "" {
		return "", false
	}

	return path.Clean(packagePath), true
}

func sortedKeys[Value any](set map[string]Value) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	return keys
}

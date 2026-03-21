// Package cmd provides the CLI commands for bosun.
package cmd

import (
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// ComposeFileWithDeps represents a Docker Compose file with dependencies for YAML parsing.
type ComposeFileWithDeps struct {
	Services map[string]struct {
		DependsOn any `yaml:"depends_on"`
	} `yaml:"services"`
}

// extractDependencyGraph parses a compose file and returns a map of service -> dependencies.
// Returns an empty graph if the file cannot be read or parsed (callers treat this as "no deps").
func extractDependencyGraph(filename string) map[string][]string {
	graph := make(map[string][]string)

	data, err := os.ReadFile(filename)
	if err != nil {
		return graph
	}

	var compose ComposeFileWithDeps
	if err := yaml.Unmarshal(data, &compose); err != nil {
		return graph
	}

	for svc, svcCfg := range compose.Services {
		graph[svc] = []string{}

		if svcCfg.DependsOn == nil {
			continue
		}

		// depends_on can be either a list or a map
		switch deps := svcCfg.DependsOn.(type) {
		case []any:
			for _, d := range deps {
				if depName, ok := d.(string); ok {
					graph[svc] = append(graph[svc], depName)
				}
			}
		case map[string]any:
			for depName := range deps {
				graph[svc] = append(graph[svc], depName)
			}
		}
	}

	return graph
}

// detectCycles uses depth-first search with coloring to detect cycles in a dependency graph.
// Returns a list of cycle descriptions (e.g., "a -> b -> c -> a").
func detectCycles(graph map[string][]string) []string {
	// Node colors: white (0) = unvisited, gray (1) = in progress, black (2) = done
	const (
		white = 0
		gray  = 1
		black = 2
	)

	color := make(map[string]int)
	var cycles []string
	cycleSet := make(map[string]bool) // Deduplicate cycles

	// Use a local path slice instead of global parent map to avoid concurrent access issues
	var dfs func(node string, path []string)
	dfs = func(node string, path []string) {
		color[node] = gray
		// Copy to avoid slice aliasing: append may reuse the backing array,
		// causing sibling DFS branches to corrupt each other's paths.
		currentPath := make([]string, len(path)+1)
		copy(currentPath, path)
		currentPath[len(path)] = node

		for _, neighbor := range graph[node] {
			switch color[neighbor] {
			case gray:
				// Back edge found — construct cycle from current path
				cycle := buildCyclePathFromSlice(currentPath, neighbor)
				if !cycleSet[cycle] {
					cycleSet[cycle] = true
					cycles = append(cycles, cycle)
				}
			case white:
				dfs(neighbor, currentPath)
			}
		}

		color[node] = black
	}

	// Run DFS from each node
	for node := range graph {
		if color[node] == white {
			dfs(node, nil)
		}
	}

	return cycles
}

// buildCyclePathFromSlice constructs a cycle path string from the current DFS path.
func buildCyclePathFromSlice(path []string, cycleStart string) string {
	// Find where the cycle starts in the path
	startIdx := -1
	for i, node := range path {
		if node == cycleStart {
			startIdx = i
			break
		}
	}

	if startIdx == -1 {
		// cycleStart not in path, just show the back edge
		return path[len(path)-1] + " -> " + cycleStart
	}

	// Build cycle path from startIdx to end, then back to cycleStart.
	// Use explicit copy to avoid mutating the caller's path slice.
	segment := path[startIdx:]
	cyclePath := make([]string, len(segment)+1)
	copy(cyclePath, segment)
	cyclePath[len(segment)] = cycleStart
	return strings.Join(cyclePath, " -> ")
}

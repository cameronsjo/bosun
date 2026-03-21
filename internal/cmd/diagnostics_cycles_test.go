package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/config"
)

func TestDetectCycles(t *testing.T) {
	t.Run("no cycles", func(t *testing.T) {
		graph := map[string][]string{
			"a": {"b"},
			"b": {"c"},
			"c": {},
		}
		cycles := detectCycles(graph)
		assert.Empty(t, cycles)
	})

	t.Run("simple cycle", func(t *testing.T) {
		graph := map[string][]string{
			"a": {"b"},
			"b": {"a"},
		}
		cycles := detectCycles(graph)
		assert.Len(t, cycles, 1)
		// The cycle should contain both a and b
		assert.Contains(t, cycles[0], "a")
		assert.Contains(t, cycles[0], "b")
	})

	t.Run("self cycle", func(t *testing.T) {
		graph := map[string][]string{
			"a": {"a"},
		}
		cycles := detectCycles(graph)
		assert.Len(t, cycles, 1)
		assert.Contains(t, cycles[0], "a")
	})

	t.Run("larger cycle", func(t *testing.T) {
		graph := map[string][]string{
			"a": {"b"},
			"b": {"c"},
			"c": {"a"},
		}
		cycles := detectCycles(graph)
		assert.Len(t, cycles, 1)
		assert.Contains(t, cycles[0], "a")
		assert.Contains(t, cycles[0], "b")
		assert.Contains(t, cycles[0], "c")
	})

	t.Run("empty graph", func(t *testing.T) {
		graph := map[string][]string{}
		cycles := detectCycles(graph)
		assert.Empty(t, cycles)
	})

	t.Run("disconnected with one cycle", func(t *testing.T) {
		graph := map[string][]string{
			"a": {"b"},
			"b": {},
			"c": {"d"},
			"d": {"c"},
		}
		cycles := detectCycles(graph)
		assert.Len(t, cycles, 1)
		// Should find the c-d cycle
		assert.Contains(t, cycles[0], "c")
		assert.Contains(t, cycles[0], "d")
	})
}

func TestExtractDependencyGraph(t *testing.T) {
	t.Run("extract list format depends_on", func(t *testing.T) {
		tmpDir := t.TempDir()
		composeFile := filepath.Join(tmpDir, "compose.yml")

		content := `services:
  web:
    image: nginx
    depends_on:
      - db
      - redis
  db:
    image: postgres
  redis:
    image: redis
`
		require.NoError(t, os.WriteFile(composeFile, []byte(content), 0644))

		graph := extractDependencyGraph(composeFile)

		assert.Len(t, graph, 3)
		assert.ElementsMatch(t, []string{"db", "redis"}, graph["web"])
		assert.Empty(t, graph["db"])
		assert.Empty(t, graph["redis"])
	})

	t.Run("extract map format depends_on", func(t *testing.T) {
		tmpDir := t.TempDir()
		composeFile := filepath.Join(tmpDir, "compose.yml")

		content := `services:
  web:
    image: nginx
    depends_on:
      db:
        condition: service_healthy
      redis:
        condition: service_started
  db:
    image: postgres
  redis:
    image: redis
`
		require.NoError(t, os.WriteFile(composeFile, []byte(content), 0644))

		graph := extractDependencyGraph(composeFile)

		assert.Len(t, graph, 3)
		assert.Len(t, graph["web"], 2)
		assert.Contains(t, graph["web"], "db")
		assert.Contains(t, graph["web"], "redis")
	})

	t.Run("no depends_on", func(t *testing.T) {
		tmpDir := t.TempDir()
		composeFile := filepath.Join(tmpDir, "compose.yml")

		content := `services:
  web:
    image: nginx
  db:
    image: postgres
`
		require.NoError(t, os.WriteFile(composeFile, []byte(content), 0644))

		graph := extractDependencyGraph(composeFile)

		assert.Len(t, graph, 2)
		assert.Empty(t, graph["web"])
		assert.Empty(t, graph["db"])
	})

	t.Run("non-existent file", func(t *testing.T) {
		graph := extractDependencyGraph("/non/existent/file.yml")
		assert.Empty(t, graph)
	})
}

func TestBuildCyclePathFromSlice(t *testing.T) {
	t.Run("simple path", func(t *testing.T) {
		path := []string{"a", "b", "c"}
		result := buildCyclePathFromSlice(path, "a")
		// Should build path from a to c back to a
		assert.Contains(t, result, "->")
		assert.Contains(t, result, "a")
		assert.Equal(t, "a -> b -> c -> a", result)
	})
}

// TestCheckDependencyCycles_EdgeCases tests edge cases in cycle detection.
func TestCheckDependencyCycles_EdgeCases(t *testing.T) {
	testCases := []struct {
		name       string
		graph      map[string][]string
		wantCycles int
	}{
		{
			name:       "empty graph",
			graph:      map[string][]string{},
			wantCycles: 0,
		},
		{
			name: "single node no deps",
			graph: map[string][]string{
				"a": {},
			},
			wantCycles: 0,
		},
		{
			name: "diamond - no cycle",
			graph: map[string][]string{
				"a": {"b", "c"},
				"b": {"d"},
				"c": {"d"},
				"d": {},
			},
			wantCycles: 0,
		},
		{
			name: "long chain - no cycle",
			graph: map[string][]string{
				"a": {"b"},
				"b": {"c"},
				"c": {"d"},
				"d": {"e"},
				"e": {},
			},
			wantCycles: 0,
		},
		{
			name: "multiple independent cycles",
			graph: map[string][]string{
				"a": {"b"},
				"b": {"a"},
				"c": {"d"},
				"d": {"c"},
			},
			wantCycles: 2,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cycles := detectCycles(tc.graph)
			assert.Len(t, cycles, tc.wantCycles,
				"expected %d cycles, got %d: %v", tc.wantCycles, len(cycles), cycles)
		})
	}
}

// TestBuildCyclePathFromSlice_EdgeCases tests edge cases in cycle path building.
func TestBuildCyclePathFromSlice_EdgeCases(t *testing.T) {
	testCases := []struct {
		name       string
		path       []string
		cycleStart string
		expectPath string
	}{
		{
			name:       "self cycle",
			path:       []string{"a"},
			cycleStart: "a",
			expectPath: "a -> a",
		},
		{
			name:       "two node cycle",
			path:       []string{"a", "b"},
			cycleStart: "a",
			expectPath: "a -> b -> a",
		},
		{
			name:       "three node cycle",
			path:       []string{"a", "b", "c"},
			cycleStart: "a",
			expectPath: "a -> b -> c -> a",
		},
		{
			name:       "cycle start not at beginning",
			path:       []string{"x", "a", "b"},
			cycleStart: "a",
			expectPath: "a -> b -> a",
		},
		{
			name:       "cycle start not in path",
			path:       []string{"x", "y"},
			cycleStart: "z",
			expectPath: "y -> z",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := buildCyclePathFromSlice(tc.path, tc.cycleStart)
			assert.Equal(t, tc.expectPath, result)
		})
	}
}

// TestExtractDependencyGraph_EdgeCases tests edge cases in dependency graph extraction.
func TestExtractDependencyGraph_EdgeCases(t *testing.T) {
	testCases := []struct {
		name        string
		content     string
		expectGraph map[string][]string
	}{
		{
			name: "mixed depends_on formats in same file",
			content: `services:
  web:
    image: nginx
    depends_on:
      - db
  api:
    image: myapi
    depends_on:
      db:
        condition: service_healthy
  db:
    image: postgres
`,
			expectGraph: map[string][]string{
				"web": {"db"},
				"api": {"db"},
				"db":  {},
			},
		},
		{
			name: "empty depends_on list",
			content: `services:
  web:
    image: nginx
    depends_on: []
  db:
    image: postgres
`,
			expectGraph: map[string][]string{
				"web": {},
				"db":  {},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			composeFile := filepath.Join(tmpDir, "compose.yml")
			require.NoError(t, os.WriteFile(composeFile, []byte(tc.content), 0644))

			graph := extractDependencyGraph(composeFile)

			assert.Len(t, graph, len(tc.expectGraph))
			for svc, deps := range tc.expectGraph {
				assert.ElementsMatch(t, deps, graph[svc], "service %s deps mismatch", svc)
			}
		})
	}
}

func TestCheckDependencyCycles_WithFiles(t *testing.T) {
	t.Run("no compose files returns empty", func(t *testing.T) {
		tmpDir := t.TempDir()
		manifestDir := filepath.Join(tmpDir, "manifest")
		require.NoError(t, os.MkdirAll(filepath.Join(manifestDir, "output", "compose"), 0755))

		cfg := &config.Config{
			Root:        tmpDir,
			ManifestDir: manifestDir,
		}

		cycles := checkDependencyCycles(cfg)
		assert.Empty(t, cycles)
	})

	t.Run("detects cycle in compose file", func(t *testing.T) {
		tmpDir := t.TempDir()
		manifestDir := filepath.Join(tmpDir, "manifest")
		composeDir := filepath.Join(manifestDir, "output", "compose")
		require.NoError(t, os.MkdirAll(composeDir, 0755))

		content := `services:
  a:
    image: a:latest
    depends_on:
      - b
  b:
    image: b:latest
    depends_on:
      - a
`
		require.NoError(t, os.WriteFile(filepath.Join(composeDir, "cycle.yml"), []byte(content), 0644))

		cfg := &config.Config{
			Root:        tmpDir,
			ManifestDir: manifestDir,
		}

		cycles := checkDependencyCycles(cfg)
		assert.NotEmpty(t, cycles)
	})

	t.Run("no cycle in compose file", func(t *testing.T) {
		tmpDir := t.TempDir()
		manifestDir := filepath.Join(tmpDir, "manifest")
		composeDir := filepath.Join(manifestDir, "output", "compose")
		require.NoError(t, os.MkdirAll(composeDir, 0755))

		content := `services:
  web:
    image: nginx
    depends_on:
      - db
  db:
    image: postgres
`
		require.NoError(t, os.WriteFile(filepath.Join(composeDir, "nocycle.yml"), []byte(content), 0644))

		cfg := &config.Config{
			Root:        tmpDir,
			ManifestDir: manifestDir,
		}

		cycles := checkDependencyCycles(cfg)
		assert.Empty(t, cycles)
	})
}

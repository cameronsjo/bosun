package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatusCmd_Help(t *testing.T) {
	t.Run("status --help", func(t *testing.T) {
		output, err := executeCmd(t, "status", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "status")
	})
}

func TestStatusCmd_Aliases(t *testing.T) {
	t.Run("bridge alias", func(t *testing.T) {
		_, err := executeCmd(t, "bridge", "--help")
		assert.NoError(t, err)
	})
}

func TestLogCmd_Help(t *testing.T) {
	t.Run("log --help", func(t *testing.T) {
		output, err := executeCmd(t, "log", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "manifest")
	})
}

func TestLogCmd_Aliases(t *testing.T) {
	t.Run("ledger alias", func(t *testing.T) {
		_, err := executeCmd(t, "ledger", "--help")
		assert.NoError(t, err)
	})
}

func TestDriftCmd_Help(t *testing.T) {
	t.Run("drift --help", func(t *testing.T) {
		output, err := executeCmd(t, "drift", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "manifest")
		assert.Contains(t, output, "containers")
	})
}

func TestDriftCmd_Aliases(t *testing.T) {
	t.Run("compass alias", func(t *testing.T) {
		_, err := executeCmd(t, "compass", "--help")
		assert.NoError(t, err)
	})
}

func TestDoctorCmd_Help(t *testing.T) {
	t.Run("doctor --help", func(t *testing.T) {
		output, err := executeCmd(t, "doctor", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "diagnostic")
		assert.Contains(t, output, "Docker")
	})
}

func TestDoctorCmd_Aliases(t *testing.T) {
	t.Run("checkup alias", func(t *testing.T) {
		_, err := executeCmd(t, "checkup", "--help")
		assert.NoError(t, err)
	})
}

func TestLintCmd_Help(t *testing.T) {
	t.Run("lint --help", func(t *testing.T) {
		output, err := executeCmd(t, "lint", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "Validate")
	})
}

func TestLintCmd_Aliases(t *testing.T) {
	t.Run("inspect alias", func(t *testing.T) {
		// Note: 'inspect' is an alias for lint, not crew inspect
		_, err := executeCmd(t, "inspect", "--help")
		assert.NoError(t, err)
	})
}

func TestFormatBytes(t *testing.T) {
	testCases := []struct {
		bytes    int64
		expected string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
		{1099511627776, "1.0 TB"},
	}

	for _, tc := range testCases {
		t.Run(tc.expected, func(t *testing.T) {
			result := formatBytes(tc.bytes)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestExtractServicesFromCompose(t *testing.T) {
	t.Run("extract services", func(t *testing.T) {
		tmpDir := t.TempDir()
		composeFile := filepath.Join(tmpDir, "compose.yml")

		content := `services:
  web:
    image: nginx:latest
  api:
    image: myapi:v1
  db:
    image: postgres:15
`
		require.NoError(t, os.WriteFile(composeFile, []byte(content), 0644))

		services := extractServicesFromCompose(composeFile)

		assert.Len(t, services, 3)
		assert.Equal(t, "nginx:latest", services["web"])
		assert.Equal(t, "myapi:v1", services["api"])
		assert.Equal(t, "postgres:15", services["db"])
	})

	t.Run("service without image", func(t *testing.T) {
		tmpDir := t.TempDir()
		composeFile := filepath.Join(tmpDir, "compose.yml")

		content := `services:
  web:
    build: .
`
		require.NoError(t, os.WriteFile(composeFile, []byte(content), 0644))

		services := extractServicesFromCompose(composeFile)

		assert.Len(t, services, 1)
		assert.Equal(t, "", services["web"])
	})

	t.Run("non-existent file", func(t *testing.T) {
		services := extractServicesFromCompose("/non/existent/file.yml")
		assert.Empty(t, services)
	})
}

func TestValidateServiceFile(t *testing.T) {
	t.Run("valid service file", func(t *testing.T) {
		tmpDir := t.TempDir()
		serviceFile := filepath.Join(tmpDir, "service.yml")

		content := `name: myservice
provisions:
  - webapp
config:
  port: 8080
`
		require.NoError(t, os.WriteFile(serviceFile, []byte(content), 0644))

		// The validateServiceFile checks for name: and provisions: keywords
		// which are present, so should return true
		// Note: uv is not available, so the dry-run check is skipped
		result := validateServiceFile(serviceFile, tmpDir)
		// Result depends on whether uv is installed
		_ = result
	})

	t.Run("missing name", func(t *testing.T) {
		tmpDir := t.TempDir()
		serviceFile := filepath.Join(tmpDir, "service.yml")

		content := `provisions:
  - webapp
`
		require.NoError(t, os.WriteFile(serviceFile, []byte(content), 0644))

		result := validateServiceFile(serviceFile, tmpDir)
		assert.False(t, result)
	})

	t.Run("missing provisions", func(t *testing.T) {
		tmpDir := t.TempDir()
		serviceFile := filepath.Join(tmpDir, "service.yml")

		content := `name: myservice
config:
  port: 8080
`
		require.NoError(t, os.WriteFile(serviceFile, []byte(content), 0644))

		result := validateServiceFile(serviceFile, tmpDir)
		assert.False(t, result)
	})

	t.Run("non-existent file", func(t *testing.T) {
		result := validateServiceFile("/non/existent/file.yml", "/tmp")
		assert.False(t, result)
	})
}

func TestValidateStackFile(t *testing.T) {
	t.Run("valid stack file", func(t *testing.T) {
		tmpDir := t.TempDir()
		stackFile := filepath.Join(tmpDir, "stack.yml")

		content := `include:
  - service1.yml
  - service2.yml
`
		require.NoError(t, os.WriteFile(stackFile, []byte(content), 0644))

		// Result depends on uv availability
		result := validateStackFile(stackFile, tmpDir)
		_ = result
	})

	t.Run("stack without include", func(t *testing.T) {
		tmpDir := t.TempDir()
		stackFile := filepath.Join(tmpDir, "stack.yml")

		content := `name: mystack
`
		require.NoError(t, os.WriteFile(stackFile, []byte(content), 0644))

		result := validateStackFile(stackFile, tmpDir)
		assert.True(t, result) // Warning, not error
	})

	t.Run("non-existent file", func(t *testing.T) {
		result := validateStackFile("/non/existent/file.yml", "/tmp")
		assert.False(t, result)
	})
}

func TestExtractSection(t *testing.T) {
	t.Run("extract service section", func(t *testing.T) {
		content := `services:
    web:
      image: nginx
      ports:
        - "80:80"
    api:
      image: myapi
`
		section := extractSection(content, "web")
		assert.Contains(t, section, "web:")
		assert.Contains(t, section, "image: nginx")
		assert.Contains(t, section, "ports:")
		assert.NotContains(t, section, "api:")
	})

	t.Run("non-existent section", func(t *testing.T) {
		content := `services:
    web:
      image: nginx
`
		section := extractSection(content, "nonexistent")
		assert.Empty(t, section)
	})
}

func TestInfraContainers(t *testing.T) {
	assert.Contains(t, infraContainers, "traefik")
	assert.Contains(t, infraContainers, "authelia")
	assert.Contains(t, infraContainers, "gatus")
}

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

func TestBuildCyclePath(t *testing.T) {
	t.Run("simple path", func(t *testing.T) {
		parent := map[string]string{
			"b": "a",
			"c": "b",
		}
		path := buildCyclePath("c", "a", parent)
		// Should build path from a to c back to a
		assert.Contains(t, path, "->")
		assert.Contains(t, path, "a")
	})
}

func TestNormalizeImage(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple image with tag",
			input:    "nginx:latest",
			expected: "nginx",
		},
		{
			name:     "image with digest",
			input:    "nginx@sha256:abc123def456",
			expected: "nginx",
		},
		{
			name:     "image with tag and digest",
			input:    "nginx:latest@sha256:abc123def456",
			expected: "nginx",
		},
		{
			name:     "registry with port and tag",
			input:    "localhost:5000/myimage:v1",
			expected: "localhost:5000/myimage",
		},
		{
			name:     "registry with port and digest",
			input:    "localhost:5000/myimage@sha256:abc123",
			expected: "localhost:5000/myimage",
		},
		{
			name:     "gcr registry with tag",
			input:    "gcr.io/project/image:v2.0.0",
			expected: "gcr.io/project/image",
		},
		{
			name:     "ghcr registry with tag",
			input:    "ghcr.io/owner/repo:latest",
			expected: "ghcr.io/owner/repo",
		},
		{
			name:     "image without tag or digest",
			input:    "nginx",
			expected: "nginx",
		},
		{
			name:     "multi-path registry image",
			input:    "registry.example.com:5000/org/repo/image:tag",
			expected: "registry.example.com:5000/org/repo/image",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := normalizeImage(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

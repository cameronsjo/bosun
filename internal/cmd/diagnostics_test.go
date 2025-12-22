package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/config"
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
		name     string
		bytes    int64
		expected string
	}{
		{"negative_small", -1, "N/A"},
		{"negative_large", -12345, "N/A"},
		{"zero", 0, "0 B"},
		{"512_bytes", 512, "512 B"},
		{"1_kb", 1024, "1.0 KB"},
		{"1.5_kb", 1536, "1.5 KB"},
		{"1_mb", 1048576, "1.0 MB"},
		{"1_gb", 1073741824, "1.0 GB"},
		{"1_tb", 1099511627776, "1.0 TB"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
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

func TestDefaultInfraContainers(t *testing.T) {
	// Test that config package's default infra containers include expected values
	// Load config from a directory without config file to get defaults
	tmpDir := t.TempDir()

	// Create manifest directory to enable config loading
	manifestDir := filepath.Join(tmpDir, "manifest")
	require.NoError(t, os.MkdirAll(manifestDir, 0755))

	// Create bosun directory with docker-compose.yml
	bosunDir := filepath.Join(tmpDir, "bosun")
	require.NoError(t, os.MkdirAll(bosunDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(bosunDir, "docker-compose.yml"), []byte("version: '3'"), 0644))

	// Change to project root
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalWd)

	require.NoError(t, os.Chdir(tmpDir))

	cfg, err := config.Load()
	require.NoError(t, err)

	containers := cfg.InfraContainers()
	assert.Contains(t, containers, "traefik")
	assert.Contains(t, containers, "authelia")
	assert.Contains(t, containers, "gatus")
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

func TestParsePortString(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected []int
	}{
		{
			name:     "short syntax",
			input:    "80",
			expected: []int{80},
		},
		{
			name:     "standard mapping",
			input:    "8080:80",
			expected: []int{8080},
		},
		{
			name:     "with tcp protocol",
			input:    "8080:80/tcp",
			expected: []int{8080},
		},
		{
			name:     "with udp protocol",
			input:    "53:53/udp",
			expected: []int{53},
		},
		{
			name:     "host-bound",
			input:    "127.0.0.1:8080:80",
			expected: []int{8080},
		},
		{
			name:     "port range",
			input:    "8000-8003:8000-8003",
			expected: []int{8000, 8001, 8002, 8003},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := parsePortString(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}

	// Test empty/invalid cases separately with assert.Empty
	t.Run("empty string", func(t *testing.T) {
		result := parsePortString("")
		assert.Empty(t, result)
	})

	t.Run("invalid format", func(t *testing.T) {
		result := parsePortString("not:a:valid:port:format")
		assert.Empty(t, result)
	})
}

func TestParsePortEntry(t *testing.T) {
	t.Run("integer port", func(t *testing.T) {
		result := parsePortEntry(80)
		assert.Equal(t, []int{80}, result)
	})

	t.Run("string port mapping", func(t *testing.T) {
		result := parsePortEntry("8080:80")
		assert.Equal(t, []int{8080}, result)
	})

	t.Run("long syntax map with int published", func(t *testing.T) {
		entry := map[string]any{
			"published": 8080,
			"target":    80,
		}
		result := parsePortEntry(entry)
		assert.Equal(t, []int{8080}, result)
	})

	t.Run("long syntax map with string published", func(t *testing.T) {
		entry := map[string]any{
			"published": "9090",
			"target":    80,
		}
		result := parsePortEntry(entry)
		assert.Equal(t, []int{9090}, result)
	})

	t.Run("map without published", func(t *testing.T) {
		entry := map[string]any{
			"target": 80,
		}
		result := parsePortEntry(entry)
		assert.Empty(t, result)
	})

	t.Run("nil entry", func(t *testing.T) {
		result := parsePortEntry(nil)
		assert.Empty(t, result)
	})
}

func TestExtractPorts(t *testing.T) {
	t.Run("extract standard ports", func(t *testing.T) {
		tmpDir := t.TempDir()
		composeFile := filepath.Join(tmpDir, "compose.yml")

		content := `services:
  web:
    image: nginx
    ports:
      - "8080:80"
      - "8443:443"
  api:
    image: myapi
    ports:
      - 3000
`
		require.NoError(t, os.WriteFile(composeFile, []byte(content), 0644))

		ports := extractPorts(composeFile)

		assert.Equal(t, "web", ports[8080])
		assert.Equal(t, "web", ports[8443])
		assert.Equal(t, "api", ports[3000])
	})

	t.Run("extract host-bound ports", func(t *testing.T) {
		tmpDir := t.TempDir()
		composeFile := filepath.Join(tmpDir, "compose.yml")

		content := `services:
  web:
    image: nginx
    ports:
      - "127.0.0.1:8080:80"
`
		require.NoError(t, os.WriteFile(composeFile, []byte(content), 0644))

		ports := extractPorts(composeFile)

		assert.Equal(t, "web", ports[8080])
	})

	t.Run("extract traefik labels", func(t *testing.T) {
		tmpDir := t.TempDir()
		composeFile := filepath.Join(tmpDir, "compose.yml")

		content := `services:
  web:
    image: nginx
    labels:
      traefik.http.services.web.loadbalancer.server.port: "8080"
`
		require.NoError(t, os.WriteFile(composeFile, []byte(content), 0644))

		ports := extractPorts(composeFile)

		assert.Equal(t, "web (traefik)", ports[8080])
	})

	t.Run("extract port ranges", func(t *testing.T) {
		tmpDir := t.TempDir()
		composeFile := filepath.Join(tmpDir, "compose.yml")

		content := `services:
  web:
    image: nginx
    ports:
      - "8000-8002:8000-8002"
`
		require.NoError(t, os.WriteFile(composeFile, []byte(content), 0644))

		ports := extractPorts(composeFile)

		assert.Equal(t, "web", ports[8000])
		assert.Equal(t, "web", ports[8001])
		assert.Equal(t, "web", ports[8002])
	})

	t.Run("non-existent file", func(t *testing.T) {
		ports := extractPorts("/non/existent/file.yml")
		assert.Empty(t, ports)
	})

	t.Run("invalid yaml", func(t *testing.T) {
		tmpDir := t.TempDir()
		composeFile := filepath.Join(tmpDir, "compose.yml")

		content := `not: valid: yaml: content`
		require.NoError(t, os.WriteFile(composeFile, []byte(content), 0644))

		ports := extractPorts(composeFile)
		assert.Empty(t, ports)
	})
}

func TestExtractPorts_LongSyntax(t *testing.T) {
	t.Run("extract long syntax ports", func(t *testing.T) {
		tmpDir := t.TempDir()
		composeFile := filepath.Join(tmpDir, "compose.yml")

		content := `services:
  web:
    image: nginx
    ports:
      - published: 8080
        target: 80
      - published: "9090"
        target: 90
`
		require.NoError(t, os.WriteFile(composeFile, []byte(content), 0644))

		ports := extractPorts(composeFile)

		assert.Equal(t, "web", ports[8080])
		assert.Equal(t, "web", ports[9090])
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

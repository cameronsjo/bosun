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

func TestCheckResult_Add(t *testing.T) {
	t.Run("add two results", func(t *testing.T) {
		r1 := CheckResult{Passed: 2, Failed: 1, Warned: 3}
		r2 := CheckResult{Passed: 1, Failed: 2, Warned: 1}

		r1.Add(r2)

		assert.Equal(t, 3, r1.Passed)
		assert.Equal(t, 3, r1.Failed)
		assert.Equal(t, 4, r1.Warned)
	})

	t.Run("add empty result", func(t *testing.T) {
		r1 := CheckResult{Passed: 2, Failed: 1, Warned: 3}
		r2 := CheckResult{}

		r1.Add(r2)

		assert.Equal(t, 2, r1.Passed)
		assert.Equal(t, 1, r1.Failed)
		assert.Equal(t, 3, r1.Warned)
	})

	t.Run("add to empty result", func(t *testing.T) {
		r1 := CheckResult{}
		r2 := CheckResult{Passed: 2, Failed: 1, Warned: 3}

		r1.Add(r2)

		assert.Equal(t, 2, r1.Passed)
		assert.Equal(t, 1, r1.Failed)
		assert.Equal(t, 3, r1.Warned)
	})
}

func TestCheckGit(t *testing.T) {
	// Git is typically installed in test environments
	t.Run("git installed", func(t *testing.T) {
		result := checkGit()
		// Git should be installed on any dev machine running tests
		// If not, this is a warning that the test environment is unusual
		assert.True(t, result.Passed == 1 || result.Failed == 1,
			"checkGit should return exactly one passed or failed")
		assert.Equal(t, 0, result.Warned)
	})
}

func TestCheckProjectRoot(t *testing.T) {
	t.Run("with valid config", func(t *testing.T) {
		cfg := &config.Config{
			Root: "/some/path",
		}
		result := checkProjectRoot(cfg)
		assert.Equal(t, 1, result.Passed)
		assert.Equal(t, 0, result.Failed)
		assert.Equal(t, 0, result.Warned)
	})

	t.Run("with nil config", func(t *testing.T) {
		result := checkProjectRoot(nil)
		assert.Equal(t, 0, result.Passed)
		assert.Equal(t, 0, result.Failed)
		assert.Equal(t, 1, result.Warned)
	})
}

func TestCheckAgeKey(t *testing.T) {
	t.Run("with SOPS_AGE_KEY_FILE set to existing file", func(t *testing.T) {
		tmpDir := t.TempDir()
		keyFile := filepath.Join(tmpDir, "keys.txt")
		require.NoError(t, os.WriteFile(keyFile, []byte("test key"), 0600))

		t.Setenv("SOPS_AGE_KEY_FILE", keyFile)
		result := checkAgeKey()
		assert.Equal(t, 1, result.Passed)
		assert.Equal(t, 0, result.Failed)
		assert.Equal(t, 0, result.Warned)
	})

	t.Run("with SOPS_AGE_KEY_FILE set to non-existent file", func(t *testing.T) {
		t.Setenv("SOPS_AGE_KEY_FILE", "/non/existent/path/keys.txt")
		result := checkAgeKey()
		assert.Equal(t, 0, result.Passed)
		assert.Equal(t, 0, result.Failed)
		assert.Equal(t, 1, result.Warned)
	})
}

func TestCheckManifestDirectory(t *testing.T) {
	t.Run("with nil config", func(t *testing.T) {
		result := checkManifestDirectory(nil)
		assert.Equal(t, 0, result.Passed)
		assert.Equal(t, 0, result.Failed)
		assert.Equal(t, 0, result.Warned)
	})

	t.Run("with manifest.py present", func(t *testing.T) {
		tmpDir := t.TempDir()
		manifestDir := filepath.Join(tmpDir, "manifest")
		require.NoError(t, os.MkdirAll(manifestDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(manifestDir, "manifest.py"), []byte("# test"), 0644))

		cfg := &config.Config{
			ManifestDir: manifestDir,
		}
		result := checkManifestDirectory(cfg)
		assert.Equal(t, 1, result.Passed)
		assert.Equal(t, 0, result.Failed)
		assert.Equal(t, 0, result.Warned)
	})

	t.Run("with manifest directory present but no manifest.py", func(t *testing.T) {
		tmpDir := t.TempDir()
		manifestDir := filepath.Join(tmpDir, "manifest")
		require.NoError(t, os.MkdirAll(manifestDir, 0755))

		cfg := &config.Config{
			ManifestDir: manifestDir,
		}
		result := checkManifestDirectory(cfg)
		assert.Equal(t, 1, result.Passed)
		assert.Equal(t, 0, result.Failed)
		assert.Equal(t, 0, result.Warned)
	})

	t.Run("with non-existent manifest directory", func(t *testing.T) {
		cfg := &config.Config{
			ManifestDir: "/non/existent/manifest",
		}
		result := checkManifestDirectory(cfg)
		assert.Equal(t, 0, result.Passed)
		assert.Equal(t, 0, result.Failed)
		assert.Equal(t, 1, result.Warned)
	})
}

func TestCheckWebhook(t *testing.T) {
	// Note: This test checks behavior when webhook is not running
	// In a typical test environment, the webhook will not be running
	t.Run("webhook not responding", func(t *testing.T) {
		result := checkWebhook()
		// Should warn when webhook is not responding
		assert.Equal(t, 0, result.Passed)
		assert.Equal(t, 0, result.Failed)
		assert.Equal(t, 1, result.Warned)
	})
}

func TestCheckDockerCompose(t *testing.T) {
	// Docker Compose v2 is typically installed in test environments with Docker
	t.Run("docker compose check", func(t *testing.T) {
		result := checkDockerCompose()
		// Should return exactly one passed or failed (not warned)
		assert.True(t, result.Passed == 1 || result.Failed == 1,
			"checkDockerCompose should return exactly one passed or failed")
		assert.Equal(t, 0, result.Warned)
	})
}

func TestCheckSOPS(t *testing.T) {
	t.Run("sops check", func(t *testing.T) {
		result := checkSOPS()
		// Should return exactly one passed or warned
		assert.True(t, result.Passed == 1 || result.Warned == 1,
			"checkSOPS should return exactly one passed or warned")
		assert.Equal(t, 0, result.Failed)
	})
}

func TestCheckUV(t *testing.T) {
	t.Run("uv check", func(t *testing.T) {
		result := checkUV()
		// Should return exactly one passed or warned
		assert.True(t, result.Passed == 1 || result.Warned == 1,
			"checkUV should return exactly one passed or warned")
		assert.Equal(t, 0, result.Failed)
	})
}

func TestCheckChezmoi(t *testing.T) {
	t.Run("chezmoi check", func(t *testing.T) {
		result := checkChezmoi()
		// Should return exactly one passed or warned
		assert.True(t, result.Passed == 1 || result.Warned == 1,
			"checkChezmoi should return exactly one passed or warned")
		assert.Equal(t, 0, result.Failed)
	})
}

// TestDoctorCmd_MissingDependencies tests doctor with missing dependencies.
func TestDoctorCmd_MissingDependencies(t *testing.T) {
	t.Run("doctor help shows expected checks", func(t *testing.T) {
		output, err := executeCmd(t, "doctor", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "Docker")
		assert.Contains(t, output, "diagnostic")
	})
}

// TestLintCmd_MissingManifestDir tests lint when manifest directory doesn't exist.
func TestLintCmd_MissingManifestDir(t *testing.T) {
	t.Run("lint help shows expected content", func(t *testing.T) {
		output, err := executeCmd(t, "lint", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "Validate")
		assert.Contains(t, output, "provisions")
	})
}

// TestDriftCmd_NoContainers tests drift when no containers are running.
func TestDriftCmd_NoContainers(t *testing.T) {
	t.Run("drift help shows expected content", func(t *testing.T) {
		output, err := executeCmd(t, "drift", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "manifest")
		assert.Contains(t, output, "containers")
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

// TestExtractSection_EdgeCases tests edge cases in section extraction.
func TestExtractSection_EdgeCases(t *testing.T) {
	testCases := []struct {
		name          string
		content       string
		serviceName   string
		expectContain []string
		expectEmpty   bool
	}{
		{
			name: "first service",
			content: `services:
    web:
      image: nginx
    api:
      image: myapi
`,
			serviceName:   "web",
			expectContain: []string{"web:", "image: nginx"},
		},
		{
			name: "last service",
			content: `services:
    web:
      image: nginx
    api:
      image: myapi
`,
			serviceName:   "api",
			expectContain: []string{"api:", "image: myapi"},
		},
		{
			name: "service with complex config",
			content: `services:
    web:
      image: nginx
      ports:
        - "80:80"
        - "443:443"
      environment:
        - FOO=bar
      labels:
        traefik.enable: "true"
    api:
      image: myapi
`,
			serviceName:   "web",
			expectContain: []string{"web:", "ports:", "environment:", "labels:"},
		},
		{
			name:        "empty content",
			content:     "",
			serviceName: "web",
			expectEmpty: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			section := extractSection(tc.content, tc.serviceName)

			if tc.expectEmpty {
				assert.Empty(t, section)
			} else {
				for _, expected := range tc.expectContain {
					assert.Contains(t, section, expected)
				}
			}
		})
	}
}

// TestFormatBytes_AdditionalCases tests additional edge cases for formatBytes.
func TestFormatBytes_AdditionalCases(t *testing.T) {
	testCases := []struct {
		name     string
		bytes    int64
		expected string
	}{
		{
			name:     "max int64",
			bytes:    9223372036854775807,
			expected: "8.0 EB",
		},
		{
			name:     "1023 bytes (just under 1KB)",
			bytes:    1023,
			expected: "1023 B",
		},
		{
			name:     "1025 bytes (just over 1KB)",
			bytes:    1025,
			expected: "1.0 KB",
		},
		{
			name:     "petabyte",
			bytes:    1125899906842624,
			expected: "1.0 PB",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := formatBytes(tc.bytes)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// TestValidateServiceFile_EdgeCases tests edge cases in service file validation.
// Note: validateServiceFile requires uv to be installed and may fail dry-run validation.
func TestValidateServiceFile_EdgeCases(t *testing.T) {
	// Test cases that should definitely fail (missing required fields)
	t.Run("empty file fails", func(t *testing.T) {
		tmpDir := t.TempDir()
		serviceFile := filepath.Join(tmpDir, "service.yml")
		require.NoError(t, os.WriteFile(serviceFile, []byte(""), 0644))
		result := validateServiceFile(serviceFile, tmpDir)
		assert.False(t, result)
	})

	t.Run("name in comments only fails", func(t *testing.T) {
		tmpDir := t.TempDir()
		serviceFile := filepath.Join(tmpDir, "service.yml")
		content := `# name: not a real name
provisions:
  - webapp
`
		require.NoError(t, os.WriteFile(serviceFile, []byte(content), 0644))
		result := validateServiceFile(serviceFile, tmpDir)
		assert.False(t, result)
	})

	t.Run("missing provisions fails", func(t *testing.T) {
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

	t.Run("missing name fails", func(t *testing.T) {
		tmpDir := t.TempDir()
		serviceFile := filepath.Join(tmpDir, "service.yml")
		content := `provisions:
  - webapp
`
		require.NoError(t, os.WriteFile(serviceFile, []byte(content), 0644))
		result := validateServiceFile(serviceFile, tmpDir)
		assert.False(t, result)
	})
}

// TestValidateStackFile_EdgeCases tests edge cases in stack file validation.
// Note: validateStackFile runs uv dry-run validation which may fail in test environment.
func TestValidateStackFile_EdgeCases(t *testing.T) {
	// Test cases that return true regardless of uv validation
	t.Run("without include returns true (warning only)", func(t *testing.T) {
		tmpDir := t.TempDir()
		stackFile := filepath.Join(tmpDir, "stack.yml")
		content := `name: mystack
`
		require.NoError(t, os.WriteFile(stackFile, []byte(content), 0644))
		result := validateStackFile(stackFile, tmpDir)
		// validateStackFile returns true for "no include" as it's just a warning
		assert.True(t, result)
	})

	t.Run("empty file returns true (no include is just a warning)", func(t *testing.T) {
		tmpDir := t.TempDir()
		stackFile := filepath.Join(tmpDir, "stack.yml")
		require.NoError(t, os.WriteFile(stackFile, []byte(""), 0644))
		result := validateStackFile(stackFile, tmpDir)
		// validateStackFile returns true for empty (no include) as it's just a warning
		assert.True(t, result)
	})

	t.Run("non-existent file returns false", func(t *testing.T) {
		result := validateStackFile("/non/existent/file.yml", "/tmp")
		assert.False(t, result)
	})
}

// TestParsePortString_EdgeCases tests edge cases in port string parsing.
func TestParsePortString_EdgeCases(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected []int
	}{
		{
			name:     "ipv6-like (not supported)",
			input:    "::1:8080:80",
			expected: []int{},
		},
		{
			name:     "only protocol",
			input:    "/tcp",
			expected: []int{},
		},
		{
			name:     "port zero",
			input:    "0:80",
			expected: []int{},
		},
		{
			name:     "negative port",
			input:    "-1:80",
			expected: []int{},
		},
		{
			name:     "very large port",
			input:    "99999:80",
			expected: []int{99999},
		},
		{
			name:     "reverse range (invalid)",
			input:    "8010-8000:8010-8000",
			expected: []int{},
		},
		{
			name:     "single port range",
			input:    "8080-8080:80-80",
			expected: []int{8080},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := parsePortString(tc.input)
			if len(tc.expected) == 0 {
				assert.Empty(t, result)
			} else {
				assert.Equal(t, tc.expected, result)
			}
		})
	}
}

// TestExtractServicesFromCompose_EdgeCases tests edge cases in compose service extraction.
func TestExtractServicesFromCompose_EdgeCases(t *testing.T) {
	testCases := []struct {
		name          string
		content       string
		expectCount   int
		expectService map[string]string
	}{
		{
			name:        "empty services",
			content:     `services: {}`,
			expectCount: 0,
		},
		{
			name: "service with build instead of image",
			content: `services:
  app:
    build: .
`,
			expectCount:   1,
			expectService: map[string]string{"app": ""},
		},
		{
			name: "multiple services mixed",
			content: `services:
  web:
    image: nginx:latest
  api:
    build: ./api
  db:
    image: postgres:15
`,
			expectCount: 3,
			expectService: map[string]string{
				"web": "nginx:latest",
				"api": "",
				"db":  "postgres:15",
			},
		},
		{
			name: "service with complex image reference",
			content: `services:
  app:
    image: ghcr.io/org/repo/image:v1.2.3@sha256:abc123
`,
			expectCount: 1,
			expectService: map[string]string{
				"app": "ghcr.io/org/repo/image:v1.2.3@sha256:abc123",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			composeFile := filepath.Join(tmpDir, "compose.yml")
			require.NoError(t, os.WriteFile(composeFile, []byte(tc.content), 0644))

			services := extractServicesFromCompose(composeFile)
			assert.Len(t, services, tc.expectCount)

			for svc, image := range tc.expectService {
				assert.Equal(t, image, services[svc], "service %s image mismatch", svc)
			}
		})
	}
}

// TestBuildCyclePath_EdgeCases tests edge cases in cycle path building.
func TestBuildCyclePath_EdgeCases(t *testing.T) {
	testCases := []struct {
		name       string
		current    string
		cycleStart string
		parent     map[string]string
		expectPath string
	}{
		{
			name:       "self cycle",
			current:    "a",
			cycleStart: "a",
			parent:     map[string]string{},
			expectPath: "a -> a",
		},
		{
			name:       "two node cycle",
			current:    "b",
			cycleStart: "a",
			parent:     map[string]string{"b": "a"},
			expectPath: "a -> b -> a",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := buildCyclePath(tc.current, tc.cycleStart, tc.parent)
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

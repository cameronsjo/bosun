package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/config"
)

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

func TestCheckPortConflicts(t *testing.T) {
	t.Run("no compose files returns zero conflicts", func(t *testing.T) {
		tmpDir := t.TempDir()
		manifestDir := filepath.Join(tmpDir, "manifest")
		require.NoError(t, os.MkdirAll(filepath.Join(manifestDir, "output", "compose"), 0755))

		cfg := &config.Config{
			Root:        tmpDir,
			ManifestDir: manifestDir,
		}

		conflicts := checkPortConflicts(cfg)
		assert.Equal(t, 0, conflicts)
	})

	t.Run("no conflicts with unique ports", func(t *testing.T) {
		tmpDir := t.TempDir()
		manifestDir := filepath.Join(tmpDir, "manifest")
		composeDir := filepath.Join(manifestDir, "output", "compose")
		require.NoError(t, os.MkdirAll(composeDir, 0755))

		content := `services:
  web:
    image: nginx
    ports:
      - "8080:80"
  api:
    image: myapi
    ports:
      - "3000:3000"
`
		require.NoError(t, os.WriteFile(filepath.Join(composeDir, "stack.yml"), []byte(content), 0644))

		cfg := &config.Config{
			Root:        tmpDir,
			ManifestDir: manifestDir,
		}

		conflicts := checkPortConflicts(cfg)
		assert.Equal(t, 0, conflicts)
	})

	t.Run("detects conflicts across stacks", func(t *testing.T) {
		tmpDir := t.TempDir()
		manifestDir := filepath.Join(tmpDir, "manifest")
		composeDir := filepath.Join(manifestDir, "output", "compose")
		require.NoError(t, os.MkdirAll(composeDir, 0755))

		// Two stacks using the same port.
		stack1 := `services:
  web:
    image: nginx
    ports:
      - "8080:80"
`
		stack2 := `services:
  api:
    image: myapi
    ports:
      - "8080:3000"
`
		require.NoError(t, os.WriteFile(filepath.Join(composeDir, "stack1.yml"), []byte(stack1), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(composeDir, "stack2.yml"), []byte(stack2), 0644))

		cfg := &config.Config{
			Root:        tmpDir,
			ManifestDir: manifestDir,
		}

		conflicts := checkPortConflicts(cfg)
		assert.Greater(t, conflicts, 0, "should detect port conflict on 8080")
	})
}

func TestCheckPortConflictsFromStacks(t *testing.T) {
	t.Run("returns zero (no rendered files needed)", func(t *testing.T) {
		portMap := make(map[int]string)
		result := checkPortConflictsFromStacks(nil, portMap)
		assert.Equal(t, 0, result)
	})
}

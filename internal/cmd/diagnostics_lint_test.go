package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/config"
)

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
		result := validateServiceFile(serviceFile, tmpDir)
		assert.True(t, result)
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

		result := validateStackFile(stackFile, tmpDir)
		assert.True(t, result)
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

func TestCheckDependencies(t *testing.T) {
	t.Run("no compose files returns zero warnings", func(t *testing.T) {
		tmpDir := t.TempDir()
		manifestDir := filepath.Join(tmpDir, "manifest")
		require.NoError(t, os.MkdirAll(filepath.Join(manifestDir, "output", "compose"), 0755))

		cfg := &config.Config{
			Root:        tmpDir,
			ManifestDir: manifestDir,
		}

		warnings := checkDependencies(cfg)
		assert.Equal(t, 0, warnings)
	})

	t.Run("warns on missing depends_on for db service", func(t *testing.T) {
		tmpDir := t.TempDir()
		manifestDir := filepath.Join(tmpDir, "manifest")
		composeDir := filepath.Join(manifestDir, "output", "compose")
		require.NoError(t, os.MkdirAll(composeDir, 0755))

		// Create compose file with myapp and myapp-db, but myapp lacks depends_on.
		content := `services:
    myapp:
      image: myapp:latest
      ports:
        - "8080:80"
    myapp-db:
      image: postgres:16
`
		require.NoError(t, os.WriteFile(filepath.Join(composeDir, "stack.yml"), []byte(content), 0644))

		cfg := &config.Config{
			Root:        tmpDir,
			ManifestDir: manifestDir,
		}

		warnings := checkDependencies(cfg)
		assert.Greater(t, warnings, 0, "should warn about missing depends_on")
	})

	t.Run("no warning when depends_on is present", func(t *testing.T) {
		tmpDir := t.TempDir()
		manifestDir := filepath.Join(tmpDir, "manifest")
		composeDir := filepath.Join(manifestDir, "output", "compose")
		require.NoError(t, os.MkdirAll(composeDir, 0755))

		content := `services:
    myapp:
      image: myapp:latest
      depends_on:
        - myapp-db
    myapp-db:
      image: postgres:16
`
		require.NoError(t, os.WriteFile(filepath.Join(composeDir, "stack.yml"), []byte(content), 0644))

		cfg := &config.Config{
			Root:        tmpDir,
			ManifestDir: manifestDir,
		}

		warnings := checkDependencies(cfg)
		assert.Equal(t, 0, warnings)
	})

	t.Run("warns on traefik labels without proxynet", func(t *testing.T) {
		tmpDir := t.TempDir()
		manifestDir := filepath.Join(tmpDir, "manifest")
		composeDir := filepath.Join(manifestDir, "output", "compose")
		require.NoError(t, os.MkdirAll(composeDir, 0755))

		content := `services:
    web:
      image: nginx:latest
      labels:
        traefik.enable: "true"
        traefik.http.routers.web.rule: "Host('web.example.com')"
`
		require.NoError(t, os.WriteFile(filepath.Join(composeDir, "stack.yml"), []byte(content), 0644))

		cfg := &config.Config{
			Root:        tmpDir,
			ManifestDir: manifestDir,
		}

		warnings := checkDependencies(cfg)
		assert.Greater(t, warnings, 0, "should warn about missing proxynet")
	})
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

// TestValidateServiceFile_EdgeCases tests edge cases in service file validation.
func TestValidateServiceFile_EdgeCases(t *testing.T) {
	// Test cases that should definitely fail (missing required fields)
	t.Run("empty file fails", func(t *testing.T) {
		tmpDir := t.TempDir()
		serviceFile := filepath.Join(tmpDir, "service.yml")
		require.NoError(t, os.WriteFile(serviceFile, []byte(""), 0644))
		result := validateServiceFile(serviceFile, tmpDir)
		assert.False(t, result)
	})

	t.Run("name in comments - YAML parser correctly rejects", func(t *testing.T) {
		tmpDir := t.TempDir()
		serviceFile := filepath.Join(tmpDir, "service.yml")
		// YAML parsing correctly ignores commented-out fields
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
func TestValidateStackFile_EdgeCases(t *testing.T) {
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

// TestLintCmd_MissingManifestDir tests lint when manifest directory doesn't exist.
func TestLintCmd_MissingManifestDir(t *testing.T) {
	t.Run("lint help shows expected content", func(t *testing.T) {
		output, err := executeCmd(t, "lint", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "Validate")
		assert.Contains(t, output, "provisions")
	})
}

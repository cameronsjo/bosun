package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/manifest"
)

func TestMigrateCmd_Registration(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"migrate"})
	require.NoError(t, err)
	assert.Equal(t, "migrate", cmd.Name())
}

func TestMigrateCmd_Help(t *testing.T) {
	t.Run("migrate --help", func(t *testing.T) {
		output, err := executeCmd(t, "migrate", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "migrate")
		assert.Contains(t, output, "version")
		assert.Contains(t, output, "helm")
	})
}

func TestMigrateVersionCmd_Help(t *testing.T) {
	t.Run("migrate version --help", func(t *testing.T) {
		output, err := executeCmd(t, "migrate", "version", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "apiVersion")
		assert.Contains(t, output, "--write")
	})
}

func TestMigrateHelmCmd_Help(t *testing.T) {
	t.Run("migrate helm --help", func(t *testing.T) {
		output, err := executeCmd(t, "migrate", "helm", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "Helm-aligned")
		assert.Contains(t, output, "--force")
		assert.Contains(t, output, "charts/")
	})
}

func TestMigrateCmd_Subcommands(t *testing.T) {
	t.Run("version subcommand registered", func(t *testing.T) {
		cmd, _, err := rootCmd.Find([]string{"migrate", "version"})
		require.NoError(t, err)
		assert.Equal(t, "version", cmd.Name())
	})

	t.Run("helm subcommand registered", func(t *testing.T) {
		cmd, _, err := rootCmd.Find([]string{"migrate", "helm"})
		require.NoError(t, err)
		assert.Equal(t, "helm", cmd.Name())
	})
}

func TestConvertInterpolation(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "converts ${name} to Chart.Name",
			input:    "container_name: ${name}",
			expected: "container_name: {{ .Chart.Name }}",
		},
		{
			name:     "converts ${sidecar} to Values.sidecar",
			input:    "image: postgres:${sidecar}",
			expected: "image: postgres:{{ .Values.sidecar }}",
		},
		{
			name:     "converts ${var} to Values.var",
			input:    "port: ${port}",
			expected: "port: {{ .Values.port }}",
		},
		{
			name:     "converts multiple variables",
			input:    "image: ${image}\nport: ${port}",
			expected: "image: {{ .Values.image }}\nport: {{ .Values.port }}",
		},
		{
			name:     "preserves non-interpolation text",
			input:    "name: myapp\nimage: nginx:latest",
			expected: "name: myapp\nimage: nginx:latest",
		},
		{
			name:     "handles complex service names",
			input:    "${name}-db:\n  image: postgres:${version}",
			expected: "{{ .Chart.Name }}-db:\n  image: postgres:{{ .Values.version }}",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertInterpolation(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEnsureManifestHeader(t *testing.T) {
	t.Run("adds header to content without apiVersion", func(t *testing.T) {
		content := "compose:\n  services: {}"
		result := ensureManifestHeader(content, "Template")
		assert.Contains(t, result, "apiVersion: bosun.io/v1")
		assert.Contains(t, result, "kind: Template")
		assert.Contains(t, result, "compose:")
	})

	t.Run("preserves content with existing apiVersion", func(t *testing.T) {
		content := "apiVersion: bosun.io/v1\nkind: Provision\ncompose: {}"
		result := ensureManifestHeader(content, "Template")
		assert.Equal(t, content, result)
	})

	t.Run("empty content gets header", func(t *testing.T) {
		result := ensureManifestHeader("", "Stack")
		assert.Equal(t, "apiVersion: bosun.io/v1\nkind: Stack\n", result)
	})
}

func TestConvertToChart_Real(t *testing.T) {
	t.Run("basic service", func(t *testing.T) {
		svc := &manifest.ServiceManifest{
			Name:       "myapp",
			Provisions: []string{"webapp", "healthcheck"},
		}

		chart := convertToChart(svc)

		assert.Equal(t, manifest.APIVersionV1, chart["apiVersion"])
		assert.Equal(t, manifest.KindChart, chart["kind"])
		assert.Equal(t, "myapp", chart["name"])
		assert.Equal(t, []string{"webapp", "healthcheck"}, chart["templates"])
	})

	t.Run("with needs", func(t *testing.T) {
		svc := &manifest.ServiceManifest{
			Name:  "myapp",
			Needs: []string{"postgres", "redis"},
		}

		chart := convertToChart(svc)

		deps, ok := chart["dependencies"].([]map[string]any)
		require.True(t, ok)
		assert.Len(t, deps, 2)
		assert.Equal(t, "postgres", deps[0]["name"])
		assert.Equal(t, "redis", deps[1]["name"])
	})

	t.Run("with services including version", func(t *testing.T) {
		svc := &manifest.ServiceManifest{
			Name: "myapp",
			Services: map[string]map[string]any{
				"db": {
					"version": "16",
					"port":    5432,
				},
			},
		}

		chart := convertToChart(svc)

		deps, ok := chart["dependencies"].([]map[string]any)
		require.True(t, ok)
		assert.Len(t, deps, 1)
		assert.Equal(t, "db", deps[0]["name"])
		assert.Equal(t, "16", deps[0]["version"])

		values, ok := deps[0]["values"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, 5432, values["port"])
		_, hasVersion := values["version"]
		assert.False(t, hasVersion)
	})

	t.Run("with compose overrides", func(t *testing.T) {
		compose := map[string]any{"restart": "always"}
		svc := &manifest.ServiceManifest{
			Name:    "myapp",
			Compose: compose,
		}

		chart := convertToChart(svc)

		assert.Equal(t, compose, chart["compose"])
	})

	t.Run("empty provisions omitted", func(t *testing.T) {
		svc := &manifest.ServiceManifest{
			Name: "myapp",
		}

		chart := convertToChart(svc)

		_, hasTemplates := chart["templates"]
		assert.False(t, hasTemplates)
		_, hasDeps := chart["dependencies"]
		assert.False(t, hasDeps)
	})
}

func TestConvertToHelmStack(t *testing.T) {
	t.Run("with includes", func(t *testing.T) {
		legacy := &manifest.Stack{
			Include: []string{"webapp.yml", "api.yaml"},
		}

		stack := convertToHelmStack("mystack", legacy)

		assert.Equal(t, manifest.APIVersionV1, stack["apiVersion"])
		assert.Equal(t, manifest.KindStack, stack["kind"])
		assert.Equal(t, "mystack", stack["name"])

		charts, ok := stack["charts"].([]map[string]any)
		require.True(t, ok)
		assert.Len(t, charts, 2)
		assert.Equal(t, "webapp", charts[0]["name"])
		assert.Equal(t, "api", charts[1]["name"])
	})

	t.Run("with networks", func(t *testing.T) {
		networks := map[string]any{
			"proxynet": map[string]any{"external": true},
		}
		legacy := &manifest.Stack{
			Networks: networks,
		}

		stack := convertToHelmStack("mystack", legacy)

		assert.Equal(t, networks, stack["networks"])
	})

	t.Run("empty stack", func(t *testing.T) {
		legacy := &manifest.Stack{}

		stack := convertToHelmStack("empty", legacy)

		assert.Equal(t, "empty", stack["name"])
		_, hasCharts := stack["charts"]
		assert.False(t, hasCharts)
		_, hasNetworks := stack["networks"]
		assert.False(t, hasNetworks)
	})
}

func TestMigrateProvisions(t *testing.T) {
	t.Run("non-existent source returns nil", func(t *testing.T) {
		err := migrateProvisions("/nonexistent", "/tmp/out", true)
		assert.NoError(t, err)
	})

	t.Run("dry run prints without writing", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := t.TempDir()

		content := "container_name: ${name}\nport: ${port}\n"
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "webapp.yml"), []byte(content), 0644))

		err := migrateProvisions(srcDir, dstDir, true)
		require.NoError(t, err)

		// Dry run should NOT create the file
		_, err = os.Stat(filepath.Join(dstDir, "webapp.yaml"))
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("skips non-yaml files", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := t.TempDir()

		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "readme.txt"), []byte("hello"), 0644))

		err := migrateProvisions(srcDir, dstDir, true)
		assert.NoError(t, err)
	})

	t.Run("skips directories", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := t.TempDir()

		require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "subdir"), 0755))

		err := migrateProvisions(srcDir, dstDir, true)
		assert.NoError(t, err)
	})

	t.Run("writes files when not dry run", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := t.TempDir()

		content := "port: ${port}\n"
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "web.yml"), []byte(content), 0644))

		err := migrateProvisions(srcDir, dstDir, false)
		require.NoError(t, err)

		outPath := filepath.Join(dstDir, "web.yaml")
		assert.FileExists(t, outPath)

		data, err := os.ReadFile(outPath)
		require.NoError(t, err)
		assert.Contains(t, string(data), "{{ .Values.port }}")
		assert.Contains(t, string(data), "apiVersion: bosun.io/v1")
	})
}

func TestMigrateStacks(t *testing.T) {
	t.Run("non-existent source returns nil", func(t *testing.T) {
		err := migrateStacks("/nonexistent", "/tmp/out", true)
		assert.NoError(t, err)
	})

	t.Run("dry run prints without writing", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := t.TempDir()

		content := "include:\n  - webapp.yml\n  - api.yml\n"
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "core.yml"), []byte(content), 0644))

		err := migrateStacks(srcDir, dstDir, true)
		require.NoError(t, err)

		_, err = os.Stat(filepath.Join(dstDir, "core"))
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("writes stack when not dry run", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := t.TempDir()

		content := "include:\n  - webapp.yml\n"
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "core.yml"), []byte(content), 0644))

		err := migrateStacks(srcDir, dstDir, false)
		require.NoError(t, err)

		stackPath := filepath.Join(dstDir, "core", "Stack.yaml")
		assert.FileExists(t, stackPath)

		data, err := os.ReadFile(stackPath)
		require.NoError(t, err)
		assert.Contains(t, string(data), "apiVersion")
		assert.Contains(t, string(data), "kind")
		assert.Contains(t, string(data), "name: core")
	})

	t.Run("skips non-yaml files", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := t.TempDir()

		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "readme.md"), []byte("# Stacks"), 0644))

		err := migrateStacks(srcDir, dstDir, true)
		assert.NoError(t, err)
	})
}

func TestMigrateServices(t *testing.T) {
	t.Run("non-existent source returns nil", func(t *testing.T) {
		err := migrateServices("/nonexistent", "/tmp/out", true)
		assert.NoError(t, err)
	})

	t.Run("skips directories", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := t.TempDir()

		require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "subdir"), 0755))

		err := migrateServices(srcDir, dstDir, true)
		assert.NoError(t, err)
	})

	t.Run("skips non-yaml files", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := t.TempDir()

		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "readme.txt"), []byte("hello"), 0644))

		err := migrateServices(srcDir, dstDir, true)
		assert.NoError(t, err)
	})
}

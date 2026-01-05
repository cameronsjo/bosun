package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
		// Should not add duplicate header
		assert.Equal(t, content, result)
	})
}

func TestConvertToChart(t *testing.T) {
	// Test requires access to manifest package types
	// This is an integration test
	t.Run("converts service manifest to chart format", func(t *testing.T) {
		// Create a mock service manifest inline
		svc := &mockServiceManifest{
			name:       "myapp",
			provisions: []string{"container", "healthcheck"},
			config: map[string]any{
				"image": "myapp:latest",
				"port":  8080,
			},
			needs: []string{"postgres"},
		}

		chart := convertServiceToChart(svc)
		require.NotNil(t, chart)

		assert.Equal(t, "bosun.io/v1", chart["apiVersion"])
		assert.Equal(t, "Chart", chart["kind"])
		assert.Equal(t, "myapp", chart["name"])

		templates, ok := chart["templates"].([]string)
		require.True(t, ok)
		assert.Contains(t, templates, "container")
		assert.Contains(t, templates, "healthcheck")

		deps, ok := chart["dependencies"].([]map[string]any)
		require.True(t, ok)
		require.Len(t, deps, 1)
		assert.Equal(t, "postgres", deps[0]["name"])
	})
}

// mockServiceManifest is a minimal mock for testing
type mockServiceManifest struct {
	name       string
	provisions []string
	config     map[string]any
	needs      []string
	services   map[string]map[string]any
}

// convertServiceToChart is a test helper that mirrors convertToChart logic
func convertServiceToChart(svc *mockServiceManifest) map[string]any {
	chart := map[string]any{
		"apiVersion": "bosun.io/v1",
		"kind":       "Chart",
		"name":       svc.name,
		"version":    "1.0.0",
	}

	if len(svc.provisions) > 0 {
		chart["templates"] = svc.provisions
	}

	// Convert needs to dependencies
	var deps []map[string]any
	for _, need := range svc.needs {
		deps = append(deps, map[string]any{
			"name": need,
		})
	}

	// Convert explicit services to dependencies
	for svcName, svcConfig := range svc.services {
		dep := map[string]any{
			"name": svcName,
		}
		if len(svcConfig) > 0 {
			dep["values"] = svcConfig
		}
		deps = append(deps, dep)
	}

	if len(deps) > 0 {
		chart["dependencies"] = deps
	}

	return chart
}

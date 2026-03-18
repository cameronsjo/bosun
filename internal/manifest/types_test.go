package manifest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestTarget_AutoInitializesNilMap(t *testing.T) {
	// Task 5.7: Target() accessor auto-initializes nil map
	output := &RenderOutput{}
	assert.Nil(t, output.Targets)

	compose := output.Target(TargetCompose)
	require.NotNil(t, compose)
	require.NotNil(t, output.Targets)

	// Should be safe to assign into
	compose["name"] = "test"
	assert.Equal(t, "test", output.Targets[TargetCompose]["name"])
}

func TestTarget_ReturnsExistingMap(t *testing.T) {
	output := NewRenderOutput()
	output.Targets[TargetCompose]["services"] = map[string]any{"test": true}

	compose := output.Target(TargetCompose)
	services, ok := compose["services"].(map[string]any)
	require.True(t, ok)
	assert.True(t, services["test"].(bool))
}

func TestProvision_UnmarshalYAML_RoundTrip(t *testing.T) {
	// Task 5.5: custom YAML unmarshaling round-trips for Provision
	input := `apiVersion: bosun.io/v1
kind: Provision
compose:
    services:
        myapp:
            image: test:latest
traefik:
    http:
        routers:
            myapp: {}
gatus:
    endpoints:
        - name: myapp
includes:
    - base
`

	var p Provision
	require.NoError(t, yaml.Unmarshal([]byte(input), &p))

	assert.Equal(t, "bosun.io/v1", p.APIVersion)
	assert.Equal(t, "Provision", p.Kind)
	assert.Equal(t, []string{"base"}, p.Includes)

	// Targets should be populated
	require.NotNil(t, p.Targets[TargetCompose])
	require.NotNil(t, p.Targets[TargetTraefik])
	require.NotNil(t, p.Targets[TargetGatus])

	services, ok := p.Targets[TargetCompose]["services"].(map[string]any)
	require.True(t, ok)
	_, hasMyapp := services["myapp"]
	assert.True(t, hasMyapp)

	// Marshal back
	out, err := yaml.Marshal(p)
	require.NoError(t, err)

	// Re-unmarshal to verify round-trip
	var p2 Provision
	require.NoError(t, yaml.Unmarshal(out, &p2))
	assert.Equal(t, p.APIVersion, p2.APIVersion)
	assert.Equal(t, p.Kind, p2.Kind)
	assert.Equal(t, p.Includes, p2.Includes)
	assert.NotNil(t, p2.Targets[TargetCompose])
	assert.NotNil(t, p2.Targets[TargetTraefik])
	assert.NotNil(t, p2.Targets[TargetGatus])
}

func TestTemplate_UnmarshalYAML_RoundTrip(t *testing.T) {
	// Task 5.6: custom YAML unmarshaling round-trips for Template
	input := `apiVersion: bosun.io/v1
kind: Template
compose:
    services:
        "{{ .Chart.Name }}":
            image: "{{ .Values.image }}"
traefik:
    http:
        routers:
            "{{ .Chart.Name }}": {}
`

	var tmpl Template
	require.NoError(t, yaml.Unmarshal([]byte(input), &tmpl))

	assert.Equal(t, "bosun.io/v1", tmpl.APIVersion)
	assert.Equal(t, "Template", tmpl.Kind)
	require.NotNil(t, tmpl.Targets[TargetCompose])
	require.NotNil(t, tmpl.Targets[TargetTraefik])

	// Marshal back
	out, err := yaml.Marshal(tmpl)
	require.NoError(t, err)

	var tmpl2 Template
	require.NoError(t, yaml.Unmarshal(out, &tmpl2))
	assert.Equal(t, tmpl.APIVersion, tmpl2.APIVersion)
	assert.NotNil(t, tmpl2.Targets[TargetCompose])
	assert.NotNil(t, tmpl2.Targets[TargetTraefik])
}

func TestProvision_UnmarshalYAML_UnknownTargets(t *testing.T) {
	// Unregistered targets should be accepted at parse time
	input := `compose:
    services:
        test:
            image: test:latest
caddy:
    routes:
        - match: /api
`

	var p Provision
	require.NoError(t, yaml.Unmarshal([]byte(input), &p))

	assert.NotNil(t, p.Targets[TargetCompose])
	assert.NotNil(t, p.Targets["caddy"]) // Unregistered target preserved
}

func TestTargetNames_Sorted(t *testing.T) {
	names := TargetNames()
	require.Len(t, names, len(TargetRegistry))

	// Verify sorted order
	for i := 1; i < len(names); i++ {
		assert.True(t, names[i-1] < names[i], "TargetNames should be sorted: %s should come before %s", names[i-1], names[i])
	}
}

func TestWriteOutputs_SortedReproducibleOutput(t *testing.T) {
	// Task 5.8: WriteOutputs produces sorted, reproducible output
	tmpDir := t.TempDir()

	output := &RenderOutput{
		Targets: map[string]map[string]any{
			TargetGatus:   {"endpoints": []any{map[string]any{"name": "test"}}},
			TargetCompose: {"services": map[string]any{"app": map[string]any{"image": "test:latest"}}},
			TargetTraefik: {"http": map[string]any{"routers": map[string]any{}}},
		},
	}

	err := WriteOutputs(output, tmpDir, "test-stack")
	require.NoError(t, err)

	// All three output files should exist
	_, err = os.Stat(filepath.Join(tmpDir, "compose", "test-stack.yml.tmpl"))
	assert.NoError(t, err)
	_, err = os.Stat(filepath.Join(tmpDir, "traefik", "dynamic.yml"))
	assert.NoError(t, err)
	_, err = os.Stat(filepath.Join(tmpDir, "gatus", "endpoints.yml"))
	assert.NoError(t, err)
}

func TestWriteOutputs_SkipsUnregisteredTargets(t *testing.T) {
	// Task 5.10: WriteOutputs warns and skips unregistered target names
	tmpDir := t.TempDir()

	output := &RenderOutput{
		Targets: map[string]map[string]any{
			TargetCompose: {"services": map[string]any{}},
			"caddy":       {"routes": []any{"test"}},
		},
	}

	err := WriteOutputs(output, tmpDir, "test-stack")
	require.NoError(t, err)

	// Compose should exist
	_, err = os.Stat(filepath.Join(tmpDir, "compose", "test-stack.yml.tmpl"))
	assert.NoError(t, err)

	// Caddy directory should NOT exist (unregistered target skipped)
	_, err = os.Stat(filepath.Join(tmpDir, "caddy"))
	assert.True(t, os.IsNotExist(err))
}

func TestRenderToYAML_IncludesUnregisteredTargets(t *testing.T) {
	// Task 5.11: RenderToYAML includes unregistered targets in output
	output := &RenderOutput{
		Targets: map[string]map[string]any{
			TargetCompose: {"services": map[string]any{}},
			"caddy":       {"routes": []any{"test"}},
		},
	}

	yamlStr, err := RenderToYAML(output)
	require.NoError(t, err)

	assert.Contains(t, yamlStr, "compose:")
	assert.Contains(t, yamlStr, "caddy:")
}

func TestIntegration_FullRenderPipeline(t *testing.T) {
	// Task 5.9: integration test: full render pipeline with all three targets
	provisionsDir := filepath.Join("testdata", "provisions")

	manifest := &ServiceManifest{
		Name:       "integration-app",
		Provisions: []string{"webapp"},
		Config: map[string]any{
			"image":       "ghcr.io/example/integration:latest",
			"port":        "8080",
			"subdomain":   "integration",
			"domain":      "example.com",
			"group":       "Test",
			"icon":        "mdi-test",
			"description": "Integration test app",
		},
	}

	// Render
	output, err := RenderService(manifest, provisionsDir)
	require.NoError(t, err)

	// Verify all three targets populated
	assert.NotEmpty(t, output.Targets[TargetCompose], "compose target should have content")
	assert.NotEmpty(t, output.Targets[TargetTraefik], "traefik target should have content")
	assert.NotEmpty(t, output.Targets[TargetGatus], "gatus target should have content")

	// Write outputs
	tmpDir := t.TempDir()
	err = WriteOutputs(output, tmpDir, "integration-test")
	require.NoError(t, err)

	// Verify files exist with correct content
	composeContent, err := os.ReadFile(filepath.Join(tmpDir, "compose", "integration-test.yml.tmpl"))
	require.NoError(t, err)
	assert.Contains(t, string(composeContent), "integration-app")

	traefikContent, err := os.ReadFile(filepath.Join(tmpDir, "traefik", "dynamic.yml"))
	require.NoError(t, err)
	assert.Contains(t, string(traefikContent), "integration.example.com")

	gatusContent, err := os.ReadFile(filepath.Join(tmpDir, "gatus", "endpoints.yml"))
	require.NoError(t, err)
	assert.Contains(t, string(gatusContent), "integration-app")

	// RenderToYAML should also work
	yamlStr, err := RenderToYAML(output)
	require.NoError(t, err)
	assert.Contains(t, yamlStr, "compose:")
	assert.Contains(t, yamlStr, "traefik:")
	assert.Contains(t, yamlStr, "gatus:")
}

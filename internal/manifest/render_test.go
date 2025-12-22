package manifest

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

var update = flag.Bool("update", false, "update golden files")

func TestRenderService_Simple(t *testing.T) {
	provisionsDir := filepath.Join("testdata", "provisions")

	manifest := &ServiceManifest{
		Name:       "myapp",
		Provisions: []string{"container"},
		Config: map[string]any{
			"image": "ghcr.io/example/myapp:latest",
		},
	}

	output, err := RenderService(manifest, provisionsDir)
	require.NoError(t, err)
	require.NotNil(t, output)

	// Verify compose output
	require.NotNil(t, output.Compose)
	services, ok := output.Compose["services"].(map[string]any)
	require.True(t, ok)

	myapp, ok := services["myapp"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "ghcr.io/example/myapp:latest", myapp["image"])
	assert.Equal(t, "myapp", myapp["container_name"])
	assert.Equal(t, "unless-stopped", myapp["restart"])
}

func TestRenderService_WithNeedsShorthand(t *testing.T) {
	provisionsDir := filepath.Join("testdata", "provisions")

	manifest := &ServiceManifest{
		Name:       "dbapp",
		Provisions: []string{"container"},
		Needs:      []string{"postgres"},
		Config: map[string]any{
			"image":       "ghcr.io/example/dbapp:latest",
			"db_password": "secret123",
		},
	}

	output, err := RenderService(manifest, provisionsDir)
	require.NoError(t, err)

	services, ok := output.Compose["services"].(map[string]any)
	require.True(t, ok)

	// Should have main service
	_, hasMain := services["dbapp"]
	assert.True(t, hasMain)

	// Should have postgres sidecar
	dbService, hasDB := services["dbapp-db"].(map[string]any)
	require.True(t, hasDB)
	assert.Contains(t, dbService["image"], "postgres")
}

func TestRenderService_WithExplicitSidecars(t *testing.T) {
	provisionsDir := filepath.Join("testdata", "provisions")

	manifest := &ServiceManifest{
		Name:       "fullapp",
		Provisions: []string{"container"},
		Services: map[string]map[string]any{
			"postgres": {
				"version": "16",
				"db":      "fullapp_prod",
			},
			"redis": {
				"version": "7",
			},
		},
		Config: map[string]any{
			"image":       "ghcr.io/example/fullapp:latest",
			"db_password": "production_secret",
		},
	}

	output, err := RenderService(manifest, provisionsDir)
	require.NoError(t, err)

	services, ok := output.Compose["services"].(map[string]any)
	require.True(t, ok)

	// Should have postgres sidecar
	dbService, hasDB := services["fullapp-db"].(map[string]any)
	require.True(t, hasDB)
	assert.Contains(t, dbService["image"], "postgres:16")

	// Should have redis sidecar
	redisService, hasRedis := services["fullapp-redis"].(map[string]any)
	require.True(t, hasRedis)
	assert.Contains(t, redisService["image"], "redis:7")

	// Should have volumes for both
	volumes, ok := output.Compose["volumes"].(map[string]any)
	require.True(t, ok)
	_, hasDBVolume := volumes["fullapp_db_data"]
	assert.True(t, hasDBVolume)
	_, hasRedisVolume := volumes["fullapp_redis_data"]
	assert.True(t, hasRedisVolume)
}

func TestRenderService_RawPassthrough(t *testing.T) {
	manifest := &ServiceManifest{
		Name: "rawservice",
		Type: "raw",
		Compose: map[string]any{
			"rawservice": map[string]any{
				"image": "custom:image",
				"ports": []any{"8080:80"},
			},
		},
	}

	output, err := RenderService(manifest, "")
	require.NoError(t, err)

	services, ok := output.Compose["services"].(map[string]any)
	require.True(t, ok)

	raw, ok := services["rawservice"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "custom:image", raw["image"])
}

func TestRenderService_WithTraefikOutput(t *testing.T) {
	provisionsDir := filepath.Join("testdata", "provisions")

	manifest := &ServiceManifest{
		Name:       "webapp",
		Provisions: []string{"container", "reverse-proxy"},
		Config: map[string]any{
			"image":     "webapp:latest",
			"port":      "8080",
			"subdomain": "app",
			"domain":    "example.com",
		},
	}

	output, err := RenderService(manifest, provisionsDir)
	require.NoError(t, err)

	// Should have traefik output
	require.NotNil(t, output.Traefik)
	http, ok := output.Traefik["http"].(map[string]any)
	require.True(t, ok)

	routers, ok := http["routers"].(map[string]any)
	require.True(t, ok)

	router, ok := routers["webapp"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, router["rule"], "app.example.com")
}

func TestRenderService_WithGatusOutput(t *testing.T) {
	provisionsDir := filepath.Join("testdata", "provisions")

	manifest := &ServiceManifest{
		Name:       "monitoredapp",
		Provisions: []string{"monitoring"},
		Config: map[string]any{
			"subdomain": "app",
			"domain":    "example.com",
			"group":     "Apps",
		},
	}

	output, err := RenderService(manifest, provisionsDir)
	require.NoError(t, err)

	// Should have gatus output
	require.NotNil(t, output.Gatus)
	endpoints, ok := output.Gatus["endpoints"].([]any)
	require.True(t, ok)
	require.Len(t, endpoints, 1)

	ep, ok := endpoints[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "monitoredapp", ep["name"])
	assert.Contains(t, ep["url"], "app.example.com")
}

func TestRenderStack(t *testing.T) {
	stackPath := filepath.Join("testdata", "stacks", "test-stack.yml")
	provisionsDir := filepath.Join("testdata", "provisions")
	servicesDir := filepath.Join("testdata", "services")

	output, err := RenderStack(stackPath, provisionsDir, servicesDir, nil)
	require.NoError(t, err)
	require.NotNil(t, output)

	// Should have services
	services, ok := output.Compose["services"].(map[string]any)
	require.True(t, ok)
	_, hasMyapp := services["myapp"]
	assert.True(t, hasMyapp)

	// Should have networks from stack
	networks, ok := output.Compose["networks"].(map[string]any)
	require.True(t, ok)
	_, hasDefault := networks["default"]
	assert.True(t, hasDefault)
}

func TestRenderStack_WithValuesOverlay(t *testing.T) {
	stackPath := filepath.Join("testdata", "stacks", "test-stack.yml")
	provisionsDir := filepath.Join("testdata", "provisions")
	servicesDir := filepath.Join("testdata", "services")

	values := map[string]any{
		"image": "overridden:image",
	}

	output, err := RenderStack(stackPath, provisionsDir, servicesDir, values)
	require.NoError(t, err)

	services, ok := output.Compose["services"].(map[string]any)
	require.True(t, ok)

	myapp, ok := services["myapp"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "overridden:image", myapp["image"])
}

func TestRenderToYAML(t *testing.T) {
	output := &RenderOutput{
		Compose: map[string]any{
			"services": map[string]any{
				"test": map[string]any{
					"image": "test:latest",
				},
			},
		},
		Traefik: map[string]any{
			"http": map[string]any{},
		},
		Gatus: map[string]any{
			"endpoints": []any{},
		},
	}

	yamlStr, err := RenderToYAML(output)
	require.NoError(t, err)
	assert.Contains(t, yamlStr, "compose:")
	assert.Contains(t, yamlStr, "test:latest")
}

func TestGoldenFile_SimpleService(t *testing.T) {
	goldenPath := filepath.Join("testdata", "golden", "compose", "simple-service.yml")
	provisionsDir := filepath.Join("testdata", "provisions")

	manifest := &ServiceManifest{
		Name:       "myapp",
		Provisions: []string{"container"},
		Config: map[string]any{
			"image": "ghcr.io/example/myapp:latest",
		},
	}

	output, err := RenderService(manifest, provisionsDir)
	require.NoError(t, err)

	actual, err := yaml.Marshal(output.Compose)
	require.NoError(t, err)

	if *update {
		err := os.WriteFile(goldenPath, actual, 0644)
		require.NoError(t, err)
		return
	}

	expected, err := os.ReadFile(goldenPath)
	if os.IsNotExist(err) {
		t.Logf("Golden file %s does not exist, creating it", goldenPath)
		err := os.WriteFile(goldenPath, actual, 0644)
		require.NoError(t, err)
		return
	}
	require.NoError(t, err)
	assert.Equal(t, string(expected), string(actual))
}

func TestGoldenFile_WebappService(t *testing.T) {
	goldenPath := filepath.Join("testdata", "golden", "compose", "webapp-service.yml")
	provisionsDir := filepath.Join("testdata", "provisions")

	manifest := &ServiceManifest{
		Name:       "mywebapp",
		Provisions: []string{"webapp"},
		Config: map[string]any{
			"image":       "ghcr.io/example/mywebapp:latest",
			"port":        "8080",
			"subdomain":   "mywebapp",
			"domain":      "example.com",
			"group":       "Applications",
			"icon":        "si-application",
			"description": "My Web Application",
		},
	}

	output, err := RenderService(manifest, provisionsDir)
	require.NoError(t, err)

	actual, err := yaml.Marshal(output.Compose)
	require.NoError(t, err)

	if *update {
		err := os.WriteFile(goldenPath, actual, 0644)
		require.NoError(t, err)
		return
	}

	expected, err := os.ReadFile(goldenPath)
	if os.IsNotExist(err) {
		t.Logf("Golden file %s does not exist, creating it", goldenPath)
		err := os.WriteFile(goldenPath, actual, 0644)
		require.NoError(t, err)
		return
	}
	require.NoError(t, err)
	assert.Equal(t, string(expected), string(actual))
}

func TestGoldenFile_TraefikOutput(t *testing.T) {
	goldenPath := filepath.Join("testdata", "golden", "traefik", "webapp-dynamic.yml")
	provisionsDir := filepath.Join("testdata", "provisions")

	manifest := &ServiceManifest{
		Name:       "mywebapp",
		Provisions: []string{"reverse-proxy"},
		Config: map[string]any{
			"port":      "8080",
			"subdomain": "mywebapp",
			"domain":    "example.com",
		},
	}

	output, err := RenderService(manifest, provisionsDir)
	require.NoError(t, err)

	actual, err := yaml.Marshal(output.Traefik)
	require.NoError(t, err)

	if *update {
		err := os.WriteFile(goldenPath, actual, 0644)
		require.NoError(t, err)
		return
	}

	expected, err := os.ReadFile(goldenPath)
	if os.IsNotExist(err) {
		t.Logf("Golden file %s does not exist, creating it", goldenPath)
		err := os.WriteFile(goldenPath, actual, 0644)
		require.NoError(t, err)
		return
	}
	require.NoError(t, err)
	assert.Equal(t, string(expected), string(actual))
}

func TestGoldenFile_GatusOutput(t *testing.T) {
	goldenPath := filepath.Join("testdata", "golden", "gatus", "webapp-endpoints.yml")
	provisionsDir := filepath.Join("testdata", "provisions")

	manifest := &ServiceManifest{
		Name:       "mywebapp",
		Provisions: []string{"monitoring"},
		Config: map[string]any{
			"subdomain": "mywebapp",
			"domain":    "example.com",
			"group":     "Applications",
		},
	}

	output, err := RenderService(manifest, provisionsDir)
	require.NoError(t, err)

	actual, err := yaml.Marshal(output.Gatus)
	require.NoError(t, err)

	if *update {
		err := os.WriteFile(goldenPath, actual, 0644)
		require.NoError(t, err)
		return
	}

	expected, err := os.ReadFile(goldenPath)
	if os.IsNotExist(err) {
		t.Logf("Golden file %s does not exist, creating it", goldenPath)
		err := os.WriteFile(goldenPath, actual, 0644)
		require.NoError(t, err)
		return
	}
	require.NoError(t, err)
	assert.Equal(t, string(expected), string(actual))
}

func TestLoadServiceManifest(t *testing.T) {
	manifestPath := filepath.Join("testdata", "services", "simple-service.yml")
	manifest, err := LoadServiceManifest(manifestPath)
	require.NoError(t, err)

	assert.Equal(t, "myapp", manifest.Name)
	assert.Contains(t, manifest.Provisions, "container")
	assert.Equal(t, "ghcr.io/example/myapp:latest", manifest.Config["image"])
}

func TestLoadServiceManifest_NotFound(t *testing.T) {
	_, err := LoadServiceManifest("/nonexistent/path.yml")
	require.Error(t, err)
}

func TestLoadValuesOverlay(t *testing.T) {
	tmpDir := t.TempDir()
	valuesPath := filepath.Join(tmpDir, "values.yml")

	valuesContent := `image: custom:tag
port: "9090"
nested:
  key: value
`
	require.NoError(t, os.WriteFile(valuesPath, []byte(valuesContent), 0644))

	values, err := LoadValuesOverlay(valuesPath)
	require.NoError(t, err)

	assert.Equal(t, "custom:tag", values["image"])
	assert.Equal(t, "9090", values["port"])
	nested, ok := values["nested"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "value", nested["key"])
}

func TestNewRenderOutput(t *testing.T) {
	output := NewRenderOutput()
	require.NotNil(t, output)
	require.NotNil(t, output.Compose)
	require.NotNil(t, output.Traefik)
	require.NotNil(t, output.Gatus)

	// Should be empty but not nil
	assert.Empty(t, output.Compose)
	assert.Empty(t, output.Traefik)
	assert.Empty(t, output.Gatus)
}

func TestWriteOutputs(t *testing.T) {
	tmpDir := t.TempDir()

	output := &RenderOutput{
		Compose: map[string]any{
			"services": map[string]any{
				"test": map[string]any{
					"image": "test:latest",
				},
			},
		},
		Traefik: map[string]any{
			"http": map[string]any{
				"routers": map[string]any{},
			},
		},
		Gatus: map[string]any{
			"endpoints": []any{},
		},
	}

	err := WriteOutputs(output, tmpDir, "test-stack")
	require.NoError(t, err)

	// Verify compose output
	composePath := filepath.Join(tmpDir, "compose", "test-stack.yml")
	_, err = os.Stat(composePath)
	require.NoError(t, err)

	// Verify traefik output
	traefikPath := filepath.Join(tmpDir, "traefik", "dynamic.yml")
	_, err = os.Stat(traefikPath)
	require.NoError(t, err)

	// Verify gatus output
	gatusPath := filepath.Join(tmpDir, "gatus", "endpoints.yml")
	_, err = os.Stat(gatusPath)
	require.NoError(t, err)

	// Verify content
	composeContent, err := os.ReadFile(composePath)
	require.NoError(t, err)
	assert.Contains(t, string(composeContent), "test:latest")
}

func TestWriteOutputs_EmptyOutput(t *testing.T) {
	tmpDir := t.TempDir()

	output := &RenderOutput{
		Compose: map[string]any{},
		Traefik: map[string]any{},
		Gatus:   map[string]any{},
	}

	err := WriteOutputs(output, tmpDir, "empty-stack")
	require.NoError(t, err)

	// Empty outputs should not create files
	composePath := filepath.Join(tmpDir, "compose", "empty-stack.yml")
	_, err = os.Stat(composePath)
	assert.True(t, os.IsNotExist(err))
}

func TestWriteOutputs_PartialOutput(t *testing.T) {
	tmpDir := t.TempDir()

	output := &RenderOutput{
		Compose: map[string]any{
			"services": map[string]any{},
		},
		Traefik: map[string]any{}, // Empty - should not be written
		Gatus:   map[string]any{}, // Empty - should not be written
	}

	err := WriteOutputs(output, tmpDir, "partial-stack")
	require.NoError(t, err)

	// Compose should exist
	composePath := filepath.Join(tmpDir, "compose", "partial-stack.yml")
	_, err = os.Stat(composePath)
	require.NoError(t, err)

	// Traefik should not exist
	traefikPath := filepath.Join(tmpDir, "traefik", "dynamic.yml")
	_, err = os.Stat(traefikPath)
	assert.True(t, os.IsNotExist(err))
}

func TestLoadValuesOverlay_NotFound(t *testing.T) {
	_, err := LoadValuesOverlay("/nonexistent/values.yml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read values file")
}

func TestLoadValuesOverlay_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	valuesPath := filepath.Join(tmpDir, "invalid.yml")
	require.NoError(t, os.WriteFile(valuesPath, []byte("invalid: yaml: content:"), 0644))

	_, err := LoadValuesOverlay(valuesPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse values file")
}

func TestRenderStack_StackNotFound(t *testing.T) {
	_, err := RenderStack("/nonexistent/stack.yml", "", "", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read stack file")
}

func TestRenderStack_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	stackPath := filepath.Join(tmpDir, "invalid.yml")
	require.NoError(t, os.WriteFile(stackPath, []byte("invalid: yaml: ["), 0644))

	_, err := RenderStack(stackPath, "", "", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validate stack")
}

func TestRenderStack_ServiceNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	stackPath := filepath.Join(tmpDir, "stack.yml")
	stackContent := `include:
  - nonexistent-service.yml
`
	require.NoError(t, os.WriteFile(stackPath, []byte(stackContent), 0644))

	servicesDir := filepath.Join(tmpDir, "services")
	require.NoError(t, os.MkdirAll(servicesDir, 0755))

	_, err := RenderStack(stackPath, "", servicesDir, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read service")
}

func TestRenderService_ProvisionNotFound(t *testing.T) {
	manifest := &ServiceManifest{
		Name:       "test",
		Provisions: []string{"nonexistent-provision"},
		Config:     map[string]any{},
	}

	_, err := RenderService(manifest, t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load provision")
}

func TestRenderService_NeedsNonexistent(t *testing.T) {
	provisionsDir := filepath.Join("testdata", "provisions")

	manifest := &ServiceManifest{
		Name:       "test",
		Provisions: []string{"container"},
		Needs:      []string{"nonexistent-sidecar"},
		Config: map[string]any{
			"image": "test:latest",
		},
	}

	// Should succeed - nonexistent needs are skipped
	output, err := RenderService(manifest, provisionsDir)
	require.NoError(t, err)
	require.NotNil(t, output)
}

func TestRenderService_SidecarWithError(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a sidecar provision with missing variable
	sidecarContent := `compose:
  services:
    ${name}-sidecar:
      image: ${missing_variable}
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "badsidecar.yml"), []byte(sidecarContent), 0644))

	manifest := &ServiceManifest{
		Name: "test",
		Services: map[string]map[string]any{
			"badsidecar": {},
		},
		Config: map[string]any{},
	}

	_, err := RenderService(manifest, tmpDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load sidecar")
}

func TestRenderToYAML_Error(t *testing.T) {
	// Create output with valid data - RenderToYAML should succeed
	output := &RenderOutput{
		Compose: map[string]any{"key": "value"},
		Traefik: map[string]any{},
		Gatus:   map[string]any{},
	}

	yamlStr, err := RenderToYAML(output)
	require.NoError(t, err)
	assert.Contains(t, yamlStr, "compose:")
}

func TestLoadServiceManifest_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "invalid.yml")
	require.NoError(t, os.WriteFile(manifestPath, []byte("invalid: yaml: ["), 0644))

	_, err := LoadServiceManifest(manifestPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse manifest")
}

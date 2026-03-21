package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/config"
	"github.com/cameronsjo/bosun/internal/manifest"
)

// =============================================================================
// buildPortRegistry
// =============================================================================

func TestBuildPortRegistry_NoComposeDir(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Root:        tmpDir,
		ManifestDir: filepath.Join(tmpDir, "manifest"),
	}

	registry, err := buildPortRegistry(cfg)
	require.NoError(t, err)
	assert.Empty(t, registry.Entries())
}

func TestBuildPortRegistry_EmptyComposeDir(t *testing.T) {
	tmpDir := t.TempDir()
	manifestDir := filepath.Join(tmpDir, "manifest")
	composeDir := filepath.Join(manifestDir, "output", "compose")
	require.NoError(t, os.MkdirAll(composeDir, 0755))

	cfg := &config.Config{
		Root:        tmpDir,
		ManifestDir: manifestDir,
	}

	registry, err := buildPortRegistry(cfg)
	require.NoError(t, err)
	assert.Empty(t, registry.Entries())
}

func TestBuildPortRegistry_SingleStack(t *testing.T) {
	tmpDir := t.TempDir()
	manifestDir := filepath.Join(tmpDir, "manifest")
	composeDir := filepath.Join(manifestDir, "output", "compose")
	require.NoError(t, os.MkdirAll(composeDir, 0755))

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
	require.NoError(t, os.WriteFile(filepath.Join(composeDir, "apps.yml"), []byte(content), 0644))

	cfg := &config.Config{
		Root:        tmpDir,
		ManifestDir: manifestDir,
	}

	registry, err := buildPortRegistry(cfg)
	require.NoError(t, err)

	entries := registry.Entries()
	assert.Len(t, entries, 3)

	portNumbers := make([]int, 0, len(entries))
	for _, e := range entries {
		portNumbers = append(portNumbers, e.Port)
		assert.Equal(t, "apps", e.StackName)
	}
	assert.ElementsMatch(t, []int{3000, 8080, 8443}, portNumbers)
}

func TestBuildPortRegistry_MultipleStacks(t *testing.T) {
	tmpDir := t.TempDir()
	manifestDir := filepath.Join(tmpDir, "manifest")
	composeDir := filepath.Join(manifestDir, "output", "compose")
	require.NoError(t, os.MkdirAll(composeDir, 0755))

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
      - "3000:3000"
`
	require.NoError(t, os.WriteFile(filepath.Join(composeDir, "stack1.yml"), []byte(stack1), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(composeDir, "stack2.yml"), []byte(stack2), 0644))

	cfg := &config.Config{
		Root:        tmpDir,
		ManifestDir: manifestDir,
	}

	registry, err := buildPortRegistry(cfg)
	require.NoError(t, err)
	assert.Len(t, registry.Entries(), 2)
	assert.Empty(t, registry.Conflicts())
}

func TestBuildPortRegistry_ConflictDetected(t *testing.T) {
	tmpDir := t.TempDir()
	manifestDir := filepath.Join(tmpDir, "manifest")
	composeDir := filepath.Join(manifestDir, "output", "compose")
	require.NoError(t, os.MkdirAll(composeDir, 0755))

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

	registry, err := buildPortRegistry(cfg)
	require.NoError(t, err)
	assert.Len(t, registry.Conflicts(), 1)
	assert.Equal(t, 8080, registry.Conflicts()[0].Key.Port)
}

func TestBuildPortRegistry_LongSyntaxPorts(t *testing.T) {
	tmpDir := t.TempDir()
	manifestDir := filepath.Join(tmpDir, "manifest")
	composeDir := filepath.Join(manifestDir, "output", "compose")
	require.NoError(t, os.MkdirAll(composeDir, 0755))

	content := `services:
  web:
    image: nginx
    ports:
      - published: 8080
        target: 80
      - published: "9090"
        target: 90
`
	require.NoError(t, os.WriteFile(filepath.Join(composeDir, "stack.yml"), []byte(content), 0644))

	cfg := &config.Config{
		Root:        tmpDir,
		ManifestDir: manifestDir,
	}

	registry, err := buildPortRegistry(cfg)
	require.NoError(t, err)

	entries := registry.Entries()
	ports := make([]int, 0, len(entries))
	for _, e := range entries {
		ports = append(ports, e.Port)
	}
	assert.ElementsMatch(t, []int{8080, 9090}, ports)
}

func TestBuildPortRegistry_InvalidYAMLSkipped(t *testing.T) {
	tmpDir := t.TempDir()
	manifestDir := filepath.Join(tmpDir, "manifest")
	composeDir := filepath.Join(manifestDir, "output", "compose")
	require.NoError(t, os.MkdirAll(composeDir, 0755))

	valid := `services:
  web:
    image: nginx
    ports:
      - "8080:80"
`
	require.NoError(t, os.WriteFile(filepath.Join(composeDir, "valid.yml"), []byte(valid), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(composeDir, "invalid.yml"), []byte("not: valid: yaml: content"), 0644))

	cfg := &config.Config{
		Root:        tmpDir,
		ManifestDir: manifestDir,
	}

	// Invalid file emits a warning but does not return an error.
	registry, err := buildPortRegistry(cfg)
	require.NoError(t, err)
	assert.Len(t, registry.Entries(), 1)
}

func TestBuildPortRegistry_HostBoundPort(t *testing.T) {
	tmpDir := t.TempDir()
	manifestDir := filepath.Join(tmpDir, "manifest")
	composeDir := filepath.Join(manifestDir, "output", "compose")
	require.NoError(t, os.MkdirAll(composeDir, 0755))

	content := `services:
  web:
    image: nginx
    ports:
      - "127.0.0.1:8080:80"
`
	require.NoError(t, os.WriteFile(filepath.Join(composeDir, "stack.yml"), []byte(content), 0644))

	cfg := &config.Config{
		Root:        tmpDir,
		ManifestDir: manifestDir,
	}

	registry, err := buildPortRegistry(cfg)
	require.NoError(t, err)

	entries := registry.Entries()
	require.Len(t, entries, 1)
	assert.Equal(t, "127.0.0.1", entries[0].BindAddr)
}

func TestBuildPortRegistry_UDPPort(t *testing.T) {
	tmpDir := t.TempDir()
	manifestDir := filepath.Join(tmpDir, "manifest")
	composeDir := filepath.Join(manifestDir, "output", "compose")
	require.NoError(t, os.MkdirAll(composeDir, 0755))

	content := `services:
  dns:
    image: pihole
    ports:
      - "53:53/udp"
      - "53:53/tcp"
`
	require.NoError(t, os.WriteFile(filepath.Join(composeDir, "stack.yml"), []byte(content), 0644))

	cfg := &config.Config{
		Root:        tmpDir,
		ManifestDir: manifestDir,
	}

	registry, err := buildPortRegistry(cfg)
	require.NoError(t, err)

	// TCP and UDP on the same port are two distinct entries, no conflict.
	assert.Len(t, registry.Entries(), 2)
	assert.Empty(t, registry.Conflicts())
}

// =============================================================================
// addPortsFromCompose
// =============================================================================

func TestAddPortsFromCompose_PortRange(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "compose.yml")

	content := `services:
  proxy:
    image: haproxy
    ports:
      - "8000-8002:8000-8002"
`
	require.NoError(t, os.WriteFile(filePath, []byte(content), 0644))

	registry := manifest.NewPortRegistry()
	err := addPortsFromCompose(registry, filePath, "mystack")
	require.NoError(t, err)

	entries := registry.Entries()
	assert.Len(t, entries, 3)
	for _, e := range entries {
		assert.Equal(t, "mystack", e.StackName)
		assert.Equal(t, "proxy", e.ServiceName)
	}
}

func TestAddPortsFromCompose_NonexistentFile(t *testing.T) {
	registry := manifest.NewPortRegistry()
	err := addPortsFromCompose(registry, "/nonexistent/file.yml", "stack")
	assert.Error(t, err)
}

// =============================================================================
// isYAMLFile
// =============================================================================

func TestIsYAMLFile(t *testing.T) {
	assert.True(t, isYAMLFile("stack.yml"))
	assert.True(t, isYAMLFile("stack.yaml"))
	assert.True(t, isYAMLFile("STACK.YML"))
	assert.False(t, isYAMLFile("stack.json"))
	assert.False(t, isYAMLFile("stack.tmpl"))
	assert.False(t, isYAMLFile("stack"))
}

// =============================================================================
// runPortsFree - range parsing
// =============================================================================

func TestRunPortsFree_InvalidRange(t *testing.T) {
	registry := manifest.NewPortRegistry()
	err := runPortsFree(registry, "notarange")
	assert.ErrorContains(t, err, "invalid range")
}

func TestRunPortsFree_ReversedRange(t *testing.T) {
	registry := manifest.NewPortRegistry()
	err := runPortsFree(registry, "9000-8000")
	assert.ErrorContains(t, err, "must not exceed")
}

func TestRunPortsFree_InvalidStart(t *testing.T) {
	registry := manifest.NewPortRegistry()
	err := runPortsFree(registry, "abc-9000")
	assert.Error(t, err)
}

func TestRunPortsFree_ValidRange(t *testing.T) {
	registry := manifest.NewPortRegistry()
	registry.AddEntry(manifest.PortEntry{Port: 9001, Protocol: "tcp", ServiceName: "web", StackName: "s1"})

	err := runPortsFree(registry, "9000-9002")
	assert.NoError(t, err)
}

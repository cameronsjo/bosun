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

// newTestComposeDir creates a temp dir with manifest/output/compose/ and writes
// compose files from the provided map (filename → content).
func newTestComposeDir(t *testing.T, files map[string]string) *config.Config {
	t.Helper()
	tmpDir := t.TempDir()
	manifestDir := filepath.Join(tmpDir, "manifest")
	composeDir := filepath.Join(manifestDir, "output", "compose")
	require.NoError(t, os.MkdirAll(composeDir, 0755))

	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(composeDir, name), []byte(content), 0644))
	}

	return &config.Config{
		Root:        tmpDir,
		ManifestDir: manifestDir,
	}
}

func TestBuildPortRegistry_NoComposeDir(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Root:        tmpDir,
		ManifestDir: filepath.Join(tmpDir, "manifest"),
	}

	registry, loaded, err := buildPortRegistry(cfg)
	require.NoError(t, err)
	assert.False(t, loaded)
	assert.Empty(t, registry.Entries())
}

func TestBuildPortRegistry_EmptyComposeDir(t *testing.T) {
	cfg := newTestComposeDir(t, nil)

	registry, loaded, err := buildPortRegistry(cfg)
	require.NoError(t, err)
	assert.False(t, loaded)
	assert.Empty(t, registry.Entries())
}

func TestBuildPortRegistry(t *testing.T) {
	tests := []struct {
		name           string
		files          map[string]string
		wantEntries    int
		wantConflicts  int
		wantLoaded     bool
		checkPorts     []int
		checkStackName string
	}{
		{
			name: "single stack with multiple services",
			files: map[string]string{
				"apps.yml": "services:\n  web:\n    image: nginx\n    ports:\n      - \"8080:80\"\n      - \"8443:443\"\n  api:\n    image: myapi\n    ports:\n      - \"3000:3000\"\n",
			},
			wantEntries:    3,
			wantConflicts:  0,
			wantLoaded:     true,
			checkPorts:     []int{3000, 8080, 8443},
			checkStackName: "apps",
		},
		{
			name: "multiple stacks no conflicts",
			files: map[string]string{
				"stack1.yml": "services:\n  web:\n    image: nginx\n    ports:\n      - \"8080:80\"\n",
				"stack2.yml": "services:\n  api:\n    image: myapi\n    ports:\n      - \"3000:3000\"\n",
			},
			wantEntries:   2,
			wantConflicts: 0,
			wantLoaded:    true,
		},
		{
			name: "conflict detected across stacks",
			files: map[string]string{
				"stack1.yml": "services:\n  web:\n    image: nginx\n    ports:\n      - \"8080:80\"\n",
				"stack2.yml": "services:\n  api:\n    image: myapi\n    ports:\n      - \"8080:3000\"\n",
			},
			wantEntries:   2,
			wantConflicts: 1,
			wantLoaded:    true,
		},
		{
			name: "long syntax ports",
			files: map[string]string{
				"stack.yml": "services:\n  web:\n    image: nginx\n    ports:\n      - published: 8080\n        target: 80\n      - published: \"9090\"\n        target: 90\n",
			},
			wantEntries:   2,
			wantConflicts: 0,
			wantLoaded:    true,
			checkPorts:    []int{8080, 9090},
		},
		{
			name: "invalid YAML skipped gracefully",
			files: map[string]string{
				"valid.yml":   "services:\n  web:\n    image: nginx\n    ports:\n      - \"8080:80\"\n",
				"invalid.yml": "not: valid: yaml: content",
			},
			wantEntries:   1,
			wantConflicts: 0,
			wantLoaded:    true,
		},
		{
			name: "host bound port preserves bind address",
			files: map[string]string{
				"stack.yml": "services:\n  web:\n    image: nginx\n    ports:\n      - \"127.0.0.1:8080:80\"\n",
			},
			wantEntries:   1,
			wantConflicts: 0,
			wantLoaded:    true,
		},
		{
			name: "udp and tcp on same port are distinct",
			files: map[string]string{
				"stack.yml": "services:\n  dns:\n    image: pihole\n    ports:\n      - \"53:53/udp\"\n      - \"53:53/tcp\"\n",
			},
			wantEntries:   2,
			wantConflicts: 0,
			wantLoaded:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := newTestComposeDir(t, tc.files)

			registry, loaded, err := buildPortRegistry(cfg)
			require.NoError(t, err)
			assert.Equal(t, tc.wantLoaded, loaded)
			assert.Len(t, registry.Entries(), tc.wantEntries)
			assert.Len(t, registry.Conflicts(), tc.wantConflicts)

			if tc.checkPorts != nil {
				ports := make([]int, 0, len(registry.Entries()))
				for _, e := range registry.Entries() {
					ports = append(ports, e.Port)
				}
				assert.ElementsMatch(t, tc.checkPorts, ports)
			}
			if tc.checkStackName != "" {
				for _, e := range registry.Entries() {
					assert.Equal(t, tc.checkStackName, e.StackName)
				}
			}
		})
	}
}

func TestBuildPortRegistry_HostBoundPortBindAddr(t *testing.T) {
	cfg := newTestComposeDir(t, map[string]string{
		"stack.yml": "services:\n  web:\n    image: nginx\n    ports:\n      - \"127.0.0.1:8080:80\"\n",
	})

	registry, _, err := buildPortRegistry(cfg)
	require.NoError(t, err)

	entries := registry.Entries()
	require.Len(t, entries, 1)
	assert.Equal(t, "127.0.0.1", entries[0].BindAddr)
}

// =============================================================================
// addPortsFromCompose
// =============================================================================

func TestAddPortsFromCompose_PortRange(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "compose.yml")

	content := "services:\n  proxy:\n    image: haproxy\n    ports:\n      - \"8000-8002:8000-8002\"\n"
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
	tests := []struct {
		name string
		want bool
	}{
		{"stack.yml", true},
		{"stack.yaml", true},
		{"STACK.YML", true},
		{"stack.json", false},
		{"stack.tmpl", false},
		{"stack", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isYAMLFile(tc.name))
		})
	}
}

// =============================================================================
// runPortsFree - range parsing
// =============================================================================

func TestRunPortsFree(t *testing.T) {
	tests := []struct {
		name      string
		rangeStr  string
		wantErr   bool
		errSubstr string
	}{
		{"invalid format", "notarange", true, "invalid range"},
		{"reversed range", "9000-8000", true, "must not exceed"},
		{"invalid start", "abc-9000", true, "invalid range start"},
		{"valid range", "9000-9002", false, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			registry := manifest.NewPortRegistry()
			if tc.name == "valid range" {
				registry.AddEntry(manifest.PortEntry{Port: 9001, Protocol: "tcp", ServiceName: "web", StackName: "s1"})
			}

			err := runPortsFree(registry, tc.rangeStr)
			if tc.wantErr {
				assert.ErrorContains(t, err, tc.errSubstr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/manifest"
)

// TestAddPortsFromCompose_ListLabels pins the end-to-end path: a compose file
// whose service uses list-form labels must still register its ports. Before,
// the parse failed with "cannot unmarshal !!seq into map[string]string" and
// the stack's ports were dropped from conflict detection.
func TestAddPortsFromCompose_ListLabels(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "compose.yml")
	content := "services:\n  web:\n    image: nginx\n    labels:\n      - traefik.enable=true\n    ports:\n      - \"8080:80\"\n"
	require.NoError(t, os.WriteFile(filePath, []byte(content), 0o644))

	registry := manifest.NewPortRegistry()
	err := addPortsFromCompose(registry, filePath, "mystack")

	require.NoError(t, err)
	entries := registry.Entries()
	require.Len(t, entries, 1)
	assert.Equal(t, 8080, entries[0].Port)
	assert.Equal(t, "web", entries[0].ServiceName)
	assert.Equal(t, "mystack", entries[0].StackName)
}

package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUpgradeTraefikCmd_NoDynamicDirectory pins that fixes which cannot be
// applied are an error. With no dynamic config directory the middleware fixes
// are only printed, so the command must not say "Fixes applied!" and exit 0:
// a script that ran it would believe the middlewares are in place.
func TestUpgradeTraefikCmd_NoDynamicDirectory(t *testing.T) {
	resetUpgradeTraefikFlags(t)
	compose := filepath.Join(t.TempDir(), "compose.yml")
	require.NoError(t, os.WriteFile(compose, []byte("services:\n  traefik:\n    image: traefik:v3.2\n"), 0o644))

	_, err := executeCmd(t, "upgrade", "traefik", "--help=false", "--compose", compose, "--yes")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "--dynamic", "the error names the flag that supplies the directory")
	assert.Contains(t, err.Error(), "Security Headers")
	assert.Contains(t, err.Error(), "Compression")

	// The command-flag fixes do not depend on the dynamic directory and are
	// still applied.
	data, readErr := os.ReadFile(compose)
	require.NoError(t, readErr)
	assert.Contains(t, string(data), "exposedbydefault=false")
}

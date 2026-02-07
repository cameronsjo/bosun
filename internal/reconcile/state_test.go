package reconcile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadState_MissingFile(t *testing.T) {
	state := LoadState("/nonexistent/path/deploy-state.json")

	assert.Equal(t, currentSchemaVersion, state.SchemaVersion)
	assert.Empty(t, state.LastDeployedCommit)
	assert.Zero(t, state.AttemptCount)
}

func TestLoadState_CorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deploy-state.json")
	require.NoError(t, os.WriteFile(path, []byte("not valid json{{{"), 0644))

	state := LoadState(path)

	assert.Equal(t, currentSchemaVersion, state.SchemaVersion)
	assert.Empty(t, state.LastDeployedCommit)
}

func TestLoadState_ValidFile(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	original := &DeployState{
		SchemaVersion:       1,
		LastDeployedCommit:  "abc1234",
		DeployedAt:          now,
		Source:              "webhook:github",
		LastAttemptedCommit: "abc1234",
		AttemptCount:        1,
	}

	path := filepath.Join(t.TempDir(), "deploy-state.json")
	data, err := json.MarshalIndent(original, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0644))

	state := LoadState(path)

	assert.Equal(t, 1, state.SchemaVersion)
	assert.Equal(t, "abc1234", state.LastDeployedCommit)
	assert.Equal(t, now.UTC(), state.DeployedAt.UTC())
	assert.Equal(t, "webhook:github", state.Source)
	assert.Equal(t, 1, state.AttemptCount)
}

func TestLoadState_UnknownSchemaVersion(t *testing.T) {
	// Future schema versions should still load — unknown fields are ignored by JSON decoder.
	data := `{"schema_version": 99, "last_deployed_commit": "future123", "extra_field": true}`
	path := filepath.Join(t.TempDir(), "deploy-state.json")
	require.NoError(t, os.WriteFile(path, []byte(data), 0644))

	state := LoadState(path)

	assert.Equal(t, 99, state.SchemaVersion)
	assert.Equal(t, "future123", state.LastDeployedCommit)
}

func TestSaveState_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deploy-state.json")

	state := &DeployState{
		LastDeployedCommit:  "def5678",
		DeployedAt:          time.Now(),
		Source:              "cli",
		LastAttemptedCommit: "def5678",
		AttemptCount:        1,
	}

	err := SaveState(path, state)
	require.NoError(t, err)

	// Verify file exists and is valid JSON.
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var loaded DeployState
	require.NoError(t, json.Unmarshal(data, &loaded))
	assert.Equal(t, currentSchemaVersion, loaded.SchemaVersion)
	assert.Equal(t, "def5678", loaded.LastDeployedCommit)
	assert.Equal(t, "cli", loaded.Source)
}

func TestSaveState_Overwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deploy-state.json")

	// Write first state.
	state1 := &DeployState{LastDeployedCommit: "commit-1"}
	require.NoError(t, SaveState(path, state1))

	// Overwrite with second state.
	state2 := &DeployState{LastDeployedCommit: "commit-2"}
	require.NoError(t, SaveState(path, state2))

	loaded := LoadState(path)
	assert.Equal(t, "commit-2", loaded.LastDeployedCommit)
}

func TestSaveState_NoTempFileLeftOnSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deploy-state.json")

	require.NoError(t, SaveState(path, &DeployState{LastDeployedCommit: "abc"}))

	// Only the target file should exist, no leftover temp files.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1)
	assert.Equal(t, "deploy-state.json", entries[0].Name())
}

func TestSaveState_PermissionError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping permission test when running as root")
	}

	// Try to save to a read-only directory.
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0555))
	t.Cleanup(func() { _ = os.Chmod(dir, 0755) })

	path := filepath.Join(dir, "deploy-state.json")
	err := SaveState(path, &DeployState{LastDeployedCommit: "abc"})
	assert.Error(t, err)
}

func TestSaveState_SetsSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deploy-state.json")

	// Save with schema version 0 (unset) — should be overwritten to current.
	state := &DeployState{LastDeployedCommit: "abc"}
	require.NoError(t, SaveState(path, state))

	loaded := LoadState(path)
	assert.Equal(t, currentSchemaVersion, loaded.SchemaVersion)
}

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

func TestShouldAlert(t *testing.T) {
	tests := []struct {
		name              string
		attemptCount      int
		lastAlertedAttempt int
		expected          bool
	}{
		// Fixed thresholds: 1, 3, 10, 30
		{"first attempt always alerts", 1, 0, true},
		{"second attempt suppressed", 2, 1, false},
		{"third attempt alerts", 3, 1, true},
		{"fourth attempt suppressed", 4, 3, false},
		{"tenth attempt alerts", 10, 3, true},
		{"eleventh attempt suppressed", 11, 10, false},
		{"thirtieth attempt alerts", 30, 10, true},

		// Repeating every 30 after the last threshold
		{"60th attempt alerts (30 after last threshold)", 60, 30, true},
		{"61st attempt suppressed", 61, 60, false},
		{"90th attempt alerts", 90, 60, true},

		// Circuit breaker activation always alerts
		{"circuit breaker attempt alerts", MaxAttempts, 0, true},
		{"circuit breaker even if recently alerted", MaxAttempts, MaxAttempts - 1, true},

		// Edge cases
		{"attempt at or below last alerted is suppressed", 3, 3, false},
		{"attempt below last alerted is suppressed", 2, 3, false},
		{"zero attempt never alerts", 0, 0, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := ShouldAlert(tc.attemptCount, tc.lastAlertedAttempt)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestDriftAlertKey(t *testing.T) {
	tests := []struct {
		name     string
		item     DriftItem
		expected string
	}{
		{
			name:     "unhealthy service",
			item:     DriftItem{Service: "traefik", Type: DriftUnhealthy},
			expected: "traefik:unhealthy",
		},
		{
			name:     "missing service",
			item:     DriftItem{Service: "authelia", Type: DriftMissing},
			expected: "authelia:missing",
		},
		{
			name:     "image mismatch",
			item:     DriftItem{Service: "nginx", Type: DriftImageMismatch},
			expected: "nginx:image_mismatch",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, DriftAlertKey(tc.item))
		})
	}
}

func TestShouldAlertDrift(t *testing.T) {
	now := time.Now()
	cooldown := time.Hour

	tests := []struct {
		name             string
		currentItems     []DriftItem
		alertedItems     map[string]time.Time
		expectedAlerts   int
		expectedResolved int
	}{
		{
			name: "new item triggers alert",
			currentItems: []DriftItem{
				{Service: "traefik", Type: DriftUnhealthy},
			},
			alertedItems:     map[string]time.Time{},
			expectedAlerts:   1,
			expectedResolved: 0,
		},
		{
			name: "same item within cooldown is suppressed",
			currentItems: []DriftItem{
				{Service: "traefik", Type: DriftUnhealthy},
			},
			alertedItems: map[string]time.Time{
				"traefik:unhealthy": now.Add(-30 * time.Minute),
			},
			expectedAlerts:   0,
			expectedResolved: 0,
		},
		{
			name: "same item past cooldown re-alerts",
			currentItems: []DriftItem{
				{Service: "traefik", Type: DriftUnhealthy},
			},
			alertedItems: map[string]time.Time{
				"traefik:unhealthy": now.Add(-2 * time.Hour),
			},
			expectedAlerts:   1,
			expectedResolved: 0,
		},
		{
			name:         "removed item is resolved",
			currentItems: []DriftItem{},
			alertedItems: map[string]time.Time{
				"traefik:unhealthy": now.Add(-10 * time.Minute),
			},
			expectedAlerts:   0,
			expectedResolved: 1,
		},
		{
			name: "mix of new, suppressed, and resolved",
			currentItems: []DriftItem{
				{Service: "authelia", Type: DriftMissing},  // new
				{Service: "traefik", Type: DriftUnhealthy}, // within cooldown
			},
			alertedItems: map[string]time.Time{
				"traefik:unhealthy": now.Add(-30 * time.Minute), // suppressed
				"nginx:missing":     now.Add(-10 * time.Minute), // resolved
			},
			expectedAlerts:   1, // only authelia
			expectedResolved: 1, // only nginx
		},
		{
			name:             "empty inputs produce no results",
			currentItems:     []DriftItem{},
			alertedItems:     map[string]time.Time{},
			expectedAlerts:   0,
			expectedResolved: 0,
		},
		{
			name: "nil alertedItems treats all items as new",
			currentItems: []DriftItem{
				{Service: "traefik", Type: DriftUnhealthy},
				{Service: "authelia", Type: DriftMissing},
			},
			alertedItems:     nil,
			expectedAlerts:   2,
			expectedResolved: 0,
		},
		{
			name: "exact cooldown boundary re-alerts",
			currentItems: []DriftItem{
				{Service: "traefik", Type: DriftUnhealthy},
			},
			alertedItems: map[string]time.Time{
				"traefik:unhealthy": now.Add(-cooldown),
			},
			expectedAlerts:   1,
			expectedResolved: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			alertItems, resolvedKeys := ShouldAlertDrift(tc.currentItems, tc.alertedItems, cooldown)
			assert.Len(t, alertItems, tc.expectedAlerts)
			assert.Len(t, resolvedKeys, tc.expectedResolved)
		})
	}
}

func TestSaveState_RoundTrip(t *testing.T) {
	dir := evalSymlinks(t, t.TempDir())
	path := filepath.Join(dir, "deploy-state.json")

	now := time.Now().Truncate(time.Second)
	original := &DeployState{
		LastDeployedCommit:  "abc123",
		DeployedAt:          now,
		DeployCount:         5,
		Source:              "webhook:github",
		LastAttemptedCommit: "def456",
		AttemptCount:        2,
		LastAlertedAttempt:  1,
		DeclaredServices: []DeclaredService{
			{Name: "web", Image: "nginx:latest"},
			{Name: "api", Image: "golang:1.24"},
		},
		DriftCheckedAt: now,
		DriftItems: []DriftItem{
			{Service: "db", Type: DriftMissing, Declared: "postgres:16"},
		},
	}

	require.NoError(t, SaveState(path, original))
	loaded := LoadState(path)

	assert.Equal(t, currentSchemaVersion, loaded.SchemaVersion)
	assert.Equal(t, original.LastDeployedCommit, loaded.LastDeployedCommit)
	assert.Equal(t, original.DeployCount, loaded.DeployCount)
	assert.Equal(t, original.Source, loaded.Source)
	assert.Equal(t, original.AttemptCount, loaded.AttemptCount)
	assert.Equal(t, original.LastAlertedAttempt, loaded.LastAlertedAttempt)
	require.Len(t, loaded.DeclaredServices, 2)
	assert.Equal(t, "web", loaded.DeclaredServices[0].Name)
	require.Len(t, loaded.DriftItems, 1)
	assert.Equal(t, DriftMissing, loaded.DriftItems[0].Type)
}

func TestSaveState_DeployedFilesRoundTrip(t *testing.T) {
	dir := evalSymlinks(t, t.TempDir())
	path := filepath.Join(dir, "deploy-state.json")

	original := &DeployState{
		LastDeployedCommit: "abc123",
		DeployedFiles: []string{
			"authelia/configuration.yml",
			"authelia/users.yml",
			"compose/stack.yml",
		},
	}

	require.NoError(t, SaveState(path, original))
	loaded := LoadState(path)

	assert.Equal(t, original.DeployedFiles, loaded.DeployedFiles)
}

func TestLoadState_OldStateHasNoDeployedFiles(t *testing.T) {
	// A state file written before the managed-set manifest existed has no
	// "deployed_files" key. It must load with DeployedFiles nil/empty so the
	// first post-upgrade deploy prunes nothing and simply seeds the manifest.
	dir := evalSymlinks(t, t.TempDir())
	path := filepath.Join(dir, "deploy-state.json")

	legacy := `{"schema_version":1,"last_deployed_commit":"abc123","deploy_count":3}`
	require.NoError(t, os.WriteFile(path, []byte(legacy), 0644))

	loaded := LoadState(path)
	assert.Empty(t, loaded.DeployedFiles)
	assert.Equal(t, "abc123", loaded.LastDeployedCommit)
}

func TestLoadState_ReadError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping permission test when running as root")
	}

	// Create a directory (not a file) at the state path -> reading it fails with a
	// non-IsNotExist error, hitting the "other read error" branch.
	dir := evalSymlinks(t, t.TempDir())
	path := filepath.Join(dir, "deploy-state.json")
	require.NoError(t, os.MkdirAll(path, 0755))

	state := LoadState(path)

	assert.Equal(t, currentSchemaVersion, state.SchemaVersion)
	assert.Empty(t, state.LastDeployedCommit)
}

func TestFilterDebounced(t *testing.T) {
	now := time.Now()
	debounce := 5 * time.Minute

	t.Run("zero debounce passes all items through", func(t *testing.T) {
		items := []DriftItem{
			{Service: "traefik", Type: DriftUnhealthy},
			{Service: "authelia", Type: DriftMissing},
		}
		debounceMap := map[string]time.Time{}

		result := FilterDebounced(items, debounceMap, 0)

		assert.Len(t, result, 2)
		assert.Empty(t, debounceMap)
	})

	t.Run("new item added to debounce map and filtered out", func(t *testing.T) {
		items := []DriftItem{
			{Service: "traefik", Type: DriftUnhealthy},
		}
		debounceMap := map[string]time.Time{}

		result := FilterDebounced(items, debounceMap, debounce)

		assert.Empty(t, result, "new item should be filtered out")
		assert.Contains(t, debounceMap, "traefik:unhealthy")
	})

	t.Run("item within debounce window is filtered out", func(t *testing.T) {
		items := []DriftItem{
			{Service: "traefik", Type: DriftUnhealthy},
		}
		debounceMap := map[string]time.Time{
			"traefik:unhealthy": now.Add(-3 * time.Minute), // 3min ago, within 5min window
		}

		result := FilterDebounced(items, debounceMap, debounce)

		assert.Empty(t, result, "item within window should be filtered out")
		assert.Contains(t, debounceMap, "traefik:unhealthy")
	})

	t.Run("item past debounce window graduates", func(t *testing.T) {
		items := []DriftItem{
			{Service: "traefik", Type: DriftUnhealthy},
		}
		debounceMap := map[string]time.Time{
			"traefik:unhealthy": now.Add(-6 * time.Minute), // 6min ago, past 5min window
		}

		result := FilterDebounced(items, debounceMap, debounce)

		require.Len(t, result, 1)
		assert.Equal(t, "traefik", result[0].Service)
		assert.NotContains(t, debounceMap, "traefik:unhealthy", "graduated item removed from debounce map")
	})

	t.Run("resolved item removed from debounce map", func(t *testing.T) {
		items := []DriftItem{} // traefik resolved
		debounceMap := map[string]time.Time{
			"traefik:unhealthy": now.Add(-2 * time.Minute),
		}

		result := FilterDebounced(items, debounceMap, debounce)

		assert.Empty(t, result)
		assert.Empty(t, debounceMap, "resolved item should be removed from debounce map")
	})

	t.Run("mix of new, within window, graduated, and resolved", func(t *testing.T) {
		items := []DriftItem{
			{Service: "api", Type: DriftMissing},       // new
			{Service: "traefik", Type: DriftUnhealthy},  // within window
			{Service: "authelia", Type: DriftUnhealthy},  // past window
		}
		debounceMap := map[string]time.Time{
			"traefik:unhealthy":  now.Add(-2 * time.Minute), // within window
			"authelia:unhealthy": now.Add(-6 * time.Minute), // past window
			"nginx:missing":     now.Add(-1 * time.Minute), // resolved (not in items)
		}

		result := FilterDebounced(items, debounceMap, debounce)

		// Only authelia should graduate.
		require.Len(t, result, 1)
		assert.Equal(t, "authelia", result[0].Service)

		// api:missing added, traefik still in map, authelia and nginx removed.
		assert.Contains(t, debounceMap, "api:missing")
		assert.Contains(t, debounceMap, "traefik:unhealthy")
		assert.NotContains(t, debounceMap, "authelia:unhealthy")
		assert.NotContains(t, debounceMap, "nginx:missing")
	})

	t.Run("exact debounce boundary graduates", func(t *testing.T) {
		items := []DriftItem{
			{Service: "traefik", Type: DriftUnhealthy},
		}
		debounceMap := map[string]time.Time{
			"traefik:unhealthy": now.Add(-debounce), // exactly at boundary
		}

		result := FilterDebounced(items, debounceMap, debounce)

		require.Len(t, result, 1)
		assert.Equal(t, "traefik", result[0].Service)
	})
}

func TestFilterDebounced_Persistence(t *testing.T) {
	// Verify debounce state round-trips through state file persistence.
	dir := t.TempDir()
	path := filepath.Join(dir, "deploy-state.json")

	now := time.Now().Truncate(time.Second)
	state := &DeployState{
		LastDeployedCommit: "abc123",
		DriftDebounceItems: map[string]time.Time{
			"traefik:unhealthy": now.Add(-2 * time.Minute),
			"authelia:missing":  now.Add(-1 * time.Minute),
		},
	}

	require.NoError(t, SaveState(path, state))
	loaded := LoadState(path)

	require.Len(t, loaded.DriftDebounceItems, 2)
	assert.Equal(t,
		state.DriftDebounceItems["traefik:unhealthy"].UTC().Truncate(time.Second),
		loaded.DriftDebounceItems["traefik:unhealthy"].UTC().Truncate(time.Second),
	)
	assert.Equal(t,
		state.DriftDebounceItems["authelia:missing"].UTC().Truncate(time.Second),
		loaded.DriftDebounceItems["authelia:missing"].UTC().Truncate(time.Second),
	)
}

func TestNeedsRedeploy_RoundTrip(t *testing.T) {
	dir := evalSymlinks(t, t.TempDir())
	path := filepath.Join(dir, "deploy-state.json")

	state := &DeployState{
		LastDeployedCommit:  "abc123",
		LastAttemptedCommit: "def456",
		AttemptCount:        1,
		NeedsRedeploy:       true,
	}

	require.NoError(t, SaveState(path, state))
	loaded := LoadState(path)

	assert.True(t, loaded.NeedsRedeploy, "NeedsRedeploy should persist through save/load")
	assert.Equal(t, "abc123", loaded.LastDeployedCommit)
	assert.Equal(t, "def456", loaded.LastAttemptedCommit)
}

func TestNeedsRedeploy_OmittedWhenFalse(t *testing.T) {
	dir := evalSymlinks(t, t.TempDir())
	path := filepath.Join(dir, "deploy-state.json")

	state := &DeployState{
		LastDeployedCommit: "abc123",
		NeedsRedeploy:      false,
	}

	require.NoError(t, SaveState(path, state))

	// Verify the JSON does not contain needs_redeploy when false (omitempty).
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "needs_redeploy")
}

func TestNeedsRedeploy_BackwardsCompatible(t *testing.T) {
	// State files from before NeedsRedeploy was added should load with NeedsRedeploy=false.
	data := `{"schema_version": 2, "last_deployed_commit": "abc123"}`
	path := filepath.Join(t.TempDir(), "deploy-state.json")
	require.NoError(t, os.WriteFile(path, []byte(data), 0644))

	state := LoadState(path)

	assert.False(t, state.NeedsRedeploy, "old state files without NeedsRedeploy should default to false")
	assert.Equal(t, "abc123", state.LastDeployedCommit)
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

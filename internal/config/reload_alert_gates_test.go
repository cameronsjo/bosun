package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeProjectConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	bosunDir := filepath.Join(dir, ".bosun")
	require.NoError(t, os.MkdirAll(bosunDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(bosunDir, "config.yml"), []byte(body), 0o644))
	return dir
}

// TestLoadFromPopulatesAlertConfig is the regression test for #652.
//
// LoadFrom omitted alertConfig entirely, so GetAlertConfig() returned the zero
// value: on_success, on_failure and on_recovery all false. LoadReloadedConfig
// reads exactly that and hands it to the running reconciler, so the first
// reconcile after every daemon start silently overwrote the correct startup
// gates with false and disabled every deploy alert until the next restart.
//
// The symptom that exposed it was a log line reading `on_failure: false`
// seconds after a failure alert had been delivered -- which looked like the log
// misreporting the gate, and was in fact the gate being clobbered between the
// two events.
func TestLoadFromPopulatesAlertConfig(t *testing.T) {
	t.Run("no alerts block yields the documented defaults, not zero", func(t *testing.T) {
		dir := writeProjectConfig(t, "infrastructure:\n  containers:\n    - nginx\n")

		cfg, err := LoadFrom(dir)
		require.NoError(t, err)
		require.NotNil(t, cfg)

		alerts := cfg.GetAlertConfig()
		assert.True(t, alerts.OnFailure, "a reload must not disable failure alerts")
		assert.True(t, alerts.OnRecovery, "a reload must not disable retractions")
		assert.False(t, alerts.OnSuccess, "on_success's default is genuinely false")
	})

	t.Run("explicit values survive the reload path", func(t *testing.T) {
		dir := writeProjectConfig(t, "alerts:\n  on_success: true\n  on_failure: true\n  on_recovery: false\n")

		cfg, err := LoadFrom(dir)
		require.NoError(t, err)

		alerts := cfg.GetAlertConfig()
		assert.True(t, alerts.OnSuccess)
		assert.True(t, alerts.OnFailure)
		assert.False(t, alerts.OnRecovery)
	})
}

// TestLoadReloadedConfigCarriesAlertGates covers the consumer end: the DTO the
// reconciler actually applies. Asserting only on LoadFrom would leave the
// pointer plumbing untested.
func TestLoadReloadedConfigCarriesAlertGates(t *testing.T) {
	dir := writeProjectConfig(t, "infrastructure:\n  containers:\n    - nginx\n")

	reloaded, err := LoadReloadedConfig(dir)
	require.NoError(t, err)
	require.NotNil(t, reloaded)

	require.NotNil(t, reloaded.OnFailure)
	require.NotNil(t, reloaded.OnSuccess)
	require.NotNil(t, reloaded.OnRecovery)

	assert.True(t, *reloaded.OnFailure, "the reload DTO must not carry false into a running daemon")
	assert.True(t, *reloaded.OnRecovery)
	assert.False(t, *reloaded.OnSuccess)
}

// TestLoadAndLoadFromAgreeOnAlertGates pins the two loaders together. They
// diverged silently because nothing compared them, and the divergence only
// showed up as a production log line hours later.
func TestLoadAndLoadFromAgreeOnAlertGates(t *testing.T) {
	bodies := []string{
		"infrastructure:\n  containers:\n    - nginx\n",
		"alerts:\n  on_failure: false\n",
		"alerts:\n  on_success: true\n",
		"alerts:\n  on_recovery: false\n",
	}

	for _, body := range bodies {
		t.Run(body, func(t *testing.T) {
			dir := writeProjectConfig(t, body)

			fromLoadFrom, err := LoadFrom(dir)
			require.NoError(t, err)

			// Load() resolves the root by walking up from the working dir, so
			// compare against extractAlertConfig directly -- the same helper
			// Load uses -- rather than chdir-ing in a parallel-safe test.
			snapshot, err := loadConfigFileSnapshot(dir)
			require.NoError(t, err)
			want := extractAlertConfig(snapshot.config)

			assert.Equal(t, want, fromLoadFrom.GetAlertConfig(),
				"LoadFrom must extract the same alert config Load does")
		})
	}
}

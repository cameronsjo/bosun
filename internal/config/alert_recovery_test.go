package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func alertConfigFrom(t *testing.T, body string) AlertConfig {
	t.Helper()
	tmpDir := t.TempDir()
	bosunDir := filepath.Join(tmpDir, ".bosun")
	require.NoError(t, os.MkdirAll(bosunDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(bosunDir, "config.yml"), []byte(body), 0o644))

	cfg, err := loadConfigFile(tmpDir)
	require.NoError(t, err)
	return extractAlertConfig(cfg)
}

// TestOnRecoveryDefault pins the gate that makes a failure alert retractable.
//
// The subtle case is the third one. OnFailure's default is *coupled*: config.go
// only defaults it to true when neither on_success nor on_failure was set, so
// setting on_success alone silently leaves on_failure false. Mirroring that
// pattern for on_recovery -- the obvious thing to do -- would disable
// retractions for anyone who sets on_success, which is exactly the operator
// most likely to care about alert volume.
func TestOnRecoveryDefault(t *testing.T) {
	t.Run("unset defaults to true", func(t *testing.T) {
		cfg := alertConfigFrom(t, "alerts:\n  discord_webhook_url: https://example.invalid/hook\n")
		assert.True(t, cfg.OnRecovery)
		assert.True(t, cfg.OnFailure, "existing on_failure default is unchanged")
		assert.False(t, cfg.OnSuccess, "existing on_success default is unchanged")
	})

	t.Run("no alerts block at all defaults to true", func(t *testing.T) {
		cfg := alertConfigFrom(t, "infrastructure:\n  containers:\n    - nginx\n")
		assert.True(t, cfg.OnRecovery)
	})

	t.Run("explicit on_success does not suppress the recovery default", func(t *testing.T) {
		cfg := alertConfigFrom(t, "alerts:\n  on_success: true\n")
		assert.True(t, cfg.OnRecovery, "on_recovery is not coupled to on_success")
		assert.False(t, cfg.OnFailure, "on_failure's existing coupling is preserved unchanged")
	})

	t.Run("explicit false is honoured", func(t *testing.T) {
		cfg := alertConfigFrom(t, "alerts:\n  on_recovery: false\n")
		assert.False(t, cfg.OnRecovery)
	})

	t.Run("explicit true with on_failure false", func(t *testing.T) {
		cfg := alertConfigFrom(t, "alerts:\n  on_failure: false\n  on_recovery: true\n")
		assert.True(t, cfg.OnRecovery)
		assert.False(t, cfg.OnFailure)
	})
}

func TestAlertConfigFromEnvDefaultsRecovery(t *testing.T) {
	cfg := AlertConfigFromEnv()
	assert.True(t, cfg.OnRecovery, "the env-built config must carry the same default as the file-built one")
	assert.True(t, cfg.OnFailure)
}

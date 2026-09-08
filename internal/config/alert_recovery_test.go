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
	tests := []struct {
		name           string
		body           string
		wantOnRecovery bool
		wantOnFailure  bool
		wantOnSuccess  bool
		why            string
	}{
		{
			name:           "unset defaults to true",
			body:           "alerts:\n  discord_webhook_url: https://example.invalid/hook\n",
			wantOnRecovery: true, wantOnFailure: true, wantOnSuccess: false,
			why: "existing on_failure and on_success defaults are unchanged",
		},
		{
			name:           "no alerts block at all defaults to true",
			body:           "infrastructure:\n  containers:\n    - nginx\n",
			wantOnRecovery: true, wantOnFailure: true, wantOnSuccess: false,
		},
		{
			name:           "explicit on_success does not suppress the recovery default",
			body:           "alerts:\n  on_success: true\n",
			wantOnRecovery: true, wantOnFailure: false, wantOnSuccess: true,
			why: "on_recovery is not coupled to on_success; on_failure's existing coupling is preserved",
		},
		{
			name:           "explicit false is honoured",
			body:           "alerts:\n  on_recovery: false\n",
			wantOnRecovery: false, wantOnFailure: true, wantOnSuccess: false,
		},
		{
			name:           "explicit true with on_failure false",
			body:           "alerts:\n  on_failure: false\n  on_recovery: true\n",
			wantOnRecovery: true, wantOnFailure: false, wantOnSuccess: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := alertConfigFrom(t, tc.body)
			assert.Equal(t, tc.wantOnRecovery, cfg.OnRecovery, tc.why)
			assert.Equal(t, tc.wantOnFailure, cfg.OnFailure, tc.why)
			assert.Equal(t, tc.wantOnSuccess, cfg.OnSuccess, tc.why)
		})
	}
}

func TestAlertConfigFromEnvDefaultsRecovery(t *testing.T) {
	cfg := AlertConfigFromEnv()
	assert.True(t, cfg.OnRecovery, "the env-built config must carry the same default as the file-built one")
	assert.True(t, cfg.OnFailure)
}

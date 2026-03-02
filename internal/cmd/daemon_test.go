package cmd

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDaemonCmd_Registration(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"daemon"})
	require.NoError(t, err)
	assert.Equal(t, "daemon", cmd.Name())
}

func TestDaemonCmd_Help(t *testing.T) {
	t.Run("daemon --help shows description", func(t *testing.T) {
		output, err := executeCmd(t, "daemon", "--help")
		require.NoError(t, err)
		assert.Contains(t, output, "daemon")
		assert.Contains(t, output, "GitOps")
	})

	t.Run("daemon --help shows env vars", func(t *testing.T) {
		output, err := executeCmd(t, "daemon", "--help")
		require.NoError(t, err)
		assert.Contains(t, output, "REPO_URL")
		assert.Contains(t, output, "POLL_INTERVAL")
		assert.Contains(t, output, "DISCORD_WEBHOOK_URL")
	})

	t.Run("daemon --help shows endpoints", func(t *testing.T) {
		output, err := executeCmd(t, "daemon", "--help")
		require.NoError(t, err)
		assert.Contains(t, output, "/health")
		assert.Contains(t, output, "/ready")
		assert.Contains(t, output, "/webhook")
		assert.Contains(t, output, "/metrics")
	})

	t.Run("daemon --help shows webhook details", func(t *testing.T) {
		output, err := executeCmd(t, "daemon", "--help")
		require.NoError(t, err)
		assert.Contains(t, output, "HTTP server")
		assert.Contains(t, output, "webhooks")
		assert.Contains(t, output, "health")
		assert.Contains(t, output, "Polling")
		assert.Contains(t, output, "SIGTERM")
	})
}

func TestSecondsToDuration(t *testing.T) {
	tests := []struct {
		name     string
		seconds  int
		expected time.Duration
	}{
		{"zero", 0, 0},
		{"one second", 1, time.Second},
		{"one minute", 60, time.Minute},
		{"one hour", 3600, time.Hour},
		{"arbitrary", 90, 90 * time.Second},
		{"large value", 86400, 24 * time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := secondsToDuration(tt.seconds)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCreateDaemonAlertManager_NoProviders(t *testing.T) {
	// Clear all alert-related env vars to ensure no providers are configured.
	t.Setenv("DISCORD_WEBHOOK_URL", "")
	t.Setenv("SENDGRID_API_KEY", "")
	t.Setenv("TWILIO_ACCOUNT_SID", "")

	mgr := createDaemonAlertManager()
	// createDaemonAlertManager always returns a manager (never nil).
	require.NotNil(t, mgr)
	assert.False(t, mgr.HasProviders())
}

func TestCreateDaemonAlertManager_WithDiscord(t *testing.T) {
	t.Setenv("DISCORD_WEBHOOK_URL", "https://discord.com/api/webhooks/test/token")
	t.Setenv("SENDGRID_API_KEY", "")
	t.Setenv("TWILIO_ACCOUNT_SID", "")

	mgr := createDaemonAlertManager()
	require.NotNil(t, mgr)
	assert.True(t, mgr.HasProviders())
	assert.Contains(t, mgr.ProviderNames(), "discord")
}

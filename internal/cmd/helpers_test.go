package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilterEmptyStrings(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{"nil slice", nil, []string{}},
		{"empty slice", []string{}, []string{}},
		{"all empty", []string{"", "  ", ""}, []string{}},
		{"no empty", []string{"a", "b", "c"}, []string{"a", "b", "c"}},
		{"mixed", []string{"a", "", "b", "  ", "c"}, []string{"a", "b", "c"}},
		{"whitespace trimmed", []string{" a ", " b "}, []string{"a", "b"}},
		{"single empty", []string{""}, []string{}},
		{"single non-empty", []string{"x"}, []string{"x"}},
		{"tabs and newlines", []string{"\t", "\n", "  \t  "}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterEmptyStrings(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCreateAlertManager_NoProviders(t *testing.T) {
	// Clear all alert-related env vars.
	t.Setenv("DISCORD_WEBHOOK_URL", "")
	t.Setenv("SENDGRID_API_KEY", "")
	t.Setenv("TWILIO_ACCOUNT_SID", "")

	mgr := createAlertManager()
	// createAlertManager returns nil when no providers are configured.
	assert.Nil(t, mgr)
}

func TestCreateAlertManager_WithDiscord(t *testing.T) {
	t.Setenv("DISCORD_WEBHOOK_URL", "https://discord.com/api/webhooks/test/token")
	t.Setenv("SENDGRID_API_KEY", "")
	t.Setenv("TWILIO_ACCOUNT_SID", "")

	mgr := createAlertManager()
	require.NotNil(t, mgr)
	assert.True(t, mgr.HasProviders())
	assert.Contains(t, mgr.ProviderNames(), "discord")
}

func TestCreateAlertManager_WithSendGrid(t *testing.T) {
	t.Setenv("DISCORD_WEBHOOK_URL", "")
	t.Setenv("SENDGRID_API_KEY", "SG.test-key")
	t.Setenv("SENDGRID_FROM_EMAIL", "from@example.com")
	t.Setenv("SENDGRID_FROM_NAME", "Bosun")
	t.Setenv("SENDGRID_TO_EMAILS", "to@example.com")
	t.Setenv("TWILIO_ACCOUNT_SID", "")

	mgr := createAlertManager()
	require.NotNil(t, mgr)
	assert.True(t, mgr.HasProviders())
	assert.Contains(t, mgr.ProviderNames(), "sendgrid")
}

func TestCreateAlertManager_WithTwilio(t *testing.T) {
	t.Setenv("DISCORD_WEBHOOK_URL", "")
	t.Setenv("SENDGRID_API_KEY", "")
	t.Setenv("TWILIO_ACCOUNT_SID", "AC1234567890")
	t.Setenv("TWILIO_AUTH_TOKEN", "token123")
	t.Setenv("TWILIO_FROM_NUMBER", "+15551234567")
	t.Setenv("TWILIO_TO_NUMBERS", "+15559876543")

	mgr := createAlertManager()
	require.NotNil(t, mgr)
	assert.True(t, mgr.HasProviders())
	assert.Contains(t, mgr.ProviderNames(), "twilio")
}

func TestCreateAlertManager_MultipleProviders(t *testing.T) {
	t.Setenv("DISCORD_WEBHOOK_URL", "https://discord.com/api/webhooks/test/token")
	t.Setenv("SENDGRID_API_KEY", "SG.test-key")
	t.Setenv("SENDGRID_FROM_EMAIL", "from@example.com")
	t.Setenv("SENDGRID_TO_EMAILS", "to@example.com")
	t.Setenv("TWILIO_ACCOUNT_SID", "")

	mgr := createAlertManager()
	require.NotNil(t, mgr)
	assert.True(t, mgr.HasProviders())
	names := mgr.ProviderNames()
	assert.Contains(t, names, "discord")
	assert.Contains(t, names, "sendgrid")
}

func TestCapitalizeProviderName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"tailscale", "tailscale", "Tailscale"},
		{"cloudflare", "cloudflare", "Cloudflare"},
		{"generic", "wireguard", "Wireguard"},
		{"empty", "", ""},
		{"single char", "x", "X"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := capitalizeProviderName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSetNestedValue(t *testing.T) {
	t.Run("simple key", func(t *testing.T) {
		m := make(map[string]any)
		setNestedValue(m, "key", "value")
		assert.Equal(t, "value", m["key"])
	})

	t.Run("dotted key creates nested maps", func(t *testing.T) {
		m := make(map[string]any)
		setNestedValue(m, "db.host", "localhost")
		nested, ok := m["db"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "localhost", nested["host"])
	})

	t.Run("deeply nested key", func(t *testing.T) {
		m := make(map[string]any)
		setNestedValue(m, "a.b.c.d", "deep")
		a := m["a"].(map[string]any)
		b := a["b"].(map[string]any)
		c := b["c"].(map[string]any)
		assert.Equal(t, "deep", c["d"])
	})

	t.Run("overwrites non-map intermediate", func(t *testing.T) {
		m := map[string]any{"db": "string-value"}
		setNestedValue(m, "db.host", "localhost")
		nested, ok := m["db"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "localhost", nested["host"])
	})

	t.Run("preserves existing nested values", func(t *testing.T) {
		m := map[string]any{
			"db": map[string]any{"port": 5432},
		}
		setNestedValue(m, "db.host", "localhost")
		nested := m["db"].(map[string]any)
		assert.Equal(t, "localhost", nested["host"])
		assert.Equal(t, 5432, nested["port"])
	})
}

func TestConfigDomain(t *testing.T) {
	t.Run("nil config returns empty", func(t *testing.T) {
		result := configDomain(nil)
		assert.Equal(t, "", result)
	})
}

func TestGetBackupDir(t *testing.T) {
	t.Run("uses BACKUP_DIR env var when set", func(t *testing.T) {
		t.Setenv("BACKUP_DIR", "/custom/backups")
		result := getBackupDir()
		assert.Equal(t, "/custom/backups", result)
	})
}

func TestGetAppdataDir(t *testing.T) {
	t.Run("uses LOCAL_APPDATA env var when set", func(t *testing.T) {
		t.Setenv("LOCAL_APPDATA", "/custom/appdata")
		result := getAppdataDir()
		assert.Equal(t, "/custom/appdata", result)
	})
}

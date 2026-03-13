package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetEnvOrDefault(t *testing.T) {
	tests := []struct {
		name         string
		envKey       string
		envValue     string
		setEnv       bool
		defaultValue string
		want         string
	}{
		{
			name:         "env set overrides default",
			envKey:       "TEST_BOSUN_HELPER_1",
			envValue:     "from-env",
			setEnv:       true,
			defaultValue: "from-config",
			want:         "from-env",
		},
		{
			name:         "env not set returns default",
			envKey:       "TEST_BOSUN_HELPER_UNSET",
			setEnv:       false,
			defaultValue: "fallback-value",
			want:         "fallback-value",
		},
		{
			name:         "env set to empty returns default",
			envKey:       "TEST_BOSUN_HELPER_2",
			envValue:     "",
			setEnv:       true,
			defaultValue: "fallback-value",
			want:         "fallback-value",
		},
		{
			name:         "empty default with env set",
			envKey:       "TEST_BOSUN_HELPER_3",
			envValue:     "something",
			setEnv:       true,
			defaultValue: "",
			want:         "something",
		},
		{
			name:         "both empty",
			envKey:       "TEST_BOSUN_HELPER_4",
			envValue:     "",
			setEnv:       true,
			defaultValue: "",
			want:         "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv(tt.envKey, tt.envValue)
			}
			got := getEnvOrDefault(tt.envKey, tt.defaultValue)
			assert.Equal(t, tt.want, got)
		})
	}
}

// clearAlertEnvVars clears all alert-related environment variables (both
// BOSUN_-prefixed and legacy unprefixed) so tests start from a clean state.
func clearAlertEnvVars(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"BOSUN_DISCORD_WEBHOOK_URL", "DISCORD_WEBHOOK_URL",
		"BOSUN_SENDGRID_API_KEY", "SENDGRID_API_KEY",
		"BOSUN_SENDGRID_FROM_EMAIL", "SENDGRID_FROM_EMAIL",
		"BOSUN_SENDGRID_FROM_NAME", "SENDGRID_FROM_NAME",
		"BOSUN_TWILIO_ACCOUNT_SID", "TWILIO_ACCOUNT_SID",
		"BOSUN_TWILIO_AUTH_TOKEN", "TWILIO_AUTH_TOKEN",
		"BOSUN_TWILIO_FROM_NUMBER", "TWILIO_FROM_NUMBER",
	} {
		t.Setenv(key, "")
	}
}

// boolPtr returns a pointer to a bool value.
func boolPtr(b bool) *bool { return &b }

func TestExtractAlertConfig(t *testing.T) {
	t.Run("defaults OnFailure to true when neither flag set", func(t *testing.T) {
		clearAlertEnvVars(t)

		cfg := configFile{}
		alertCfg := extractAlertConfig(cfg)

		assert.True(t, alertCfg.OnFailure)
		assert.False(t, alertCfg.OnSuccess)
	})

	t.Run("respects OnSuccess when explicitly set", func(t *testing.T) {
		clearAlertEnvVars(t)

		cfg := configFile{
			Alerts: alertConfigRaw{OnSuccess: boolPtr(true)},
		}
		alertCfg := extractAlertConfig(cfg)

		assert.True(t, alertCfg.OnSuccess)
		// When OnSuccess is explicitly set, OnFailure stays at zero value (false).
		assert.False(t, alertCfg.OnFailure)
	})

	t.Run("explicit false for both flags is respected", func(t *testing.T) {
		clearAlertEnvVars(t)

		cfg := configFile{
			Alerts: alertConfigRaw{
				OnSuccess: boolPtr(false),
				OnFailure: boolPtr(false),
			},
		}
		alertCfg := extractAlertConfig(cfg)

		assert.False(t, alertCfg.OnSuccess)
		assert.False(t, alertCfg.OnFailure)
	})

	t.Run("explicit OnFailure true is preserved", func(t *testing.T) {
		clearAlertEnvVars(t)

		cfg := configFile{
			Alerts: alertConfigRaw{OnFailure: boolPtr(true)},
		}
		alertCfg := extractAlertConfig(cfg)

		assert.True(t, alertCfg.OnFailure)
		assert.False(t, alertCfg.OnSuccess)
	})

	t.Run("legacy env vars override config file values", func(t *testing.T) {
		clearAlertEnvVars(t)
		t.Setenv("DISCORD_WEBHOOK_URL", "https://discord.com/env")
		t.Setenv("SENDGRID_API_KEY", "SG.env-key")
		t.Setenv("SENDGRID_FROM_EMAIL", "env@example.com")
		t.Setenv("SENDGRID_FROM_NAME", "EnvBosun")
		t.Setenv("TWILIO_ACCOUNT_SID", "AC-env")
		t.Setenv("TWILIO_AUTH_TOKEN", "env-token")
		t.Setenv("TWILIO_FROM_NUMBER", "+15550000000")

		cfg := configFile{
			Alerts: alertConfigRaw{
				DiscordWebhookURL: "https://discord.com/config",
				SendGridAPIKey:    "SG.config-key",
				SendGridFromEmail: "config@example.com",
				SendGridFromName:  "ConfigBosun",
				TwilioAccountSID:  "AC-config",
				TwilioAuthToken:   "config-token",
				TwilioFromNumber:  "+15559999999",
			},
		}
		alertCfg := extractAlertConfig(cfg)

		assert.Equal(t, "https://discord.com/env", alertCfg.DiscordWebhookURL)
		assert.Equal(t, "SG.env-key", alertCfg.SendGridAPIKey)
		assert.Equal(t, "env@example.com", alertCfg.SendGridFromEmail)
		assert.Equal(t, "EnvBosun", alertCfg.SendGridFromName)
		assert.Equal(t, "AC-env", alertCfg.TwilioAccountSID)
		assert.Equal(t, "env-token", alertCfg.TwilioAuthToken)
		assert.Equal(t, "+15550000000", alertCfg.TwilioFromNumber)
	})

	t.Run("BOSUN_ prefix takes precedence over legacy env vars", func(t *testing.T) {
		clearAlertEnvVars(t)
		// Set both legacy and BOSUN_-prefixed — prefixed should win.
		t.Setenv("DISCORD_WEBHOOK_URL", "https://discord.com/legacy")
		t.Setenv("BOSUN_DISCORD_WEBHOOK_URL", "https://discord.com/bosun")
		t.Setenv("SENDGRID_API_KEY", "SG.legacy")
		t.Setenv("BOSUN_SENDGRID_API_KEY", "SG.bosun")
		t.Setenv("TWILIO_ACCOUNT_SID", "AC-legacy")
		t.Setenv("BOSUN_TWILIO_ACCOUNT_SID", "AC-bosun")

		cfg := configFile{
			Alerts: alertConfigRaw{
				DiscordWebhookURL: "https://discord.com/config",
				SendGridAPIKey:    "SG.config",
				TwilioAccountSID:  "AC-config",
				OnFailure:         boolPtr(true),
			},
		}
		alertCfg := extractAlertConfig(cfg)

		assert.Equal(t, "https://discord.com/bosun", alertCfg.DiscordWebhookURL)
		assert.Equal(t, "SG.bosun", alertCfg.SendGridAPIKey)
		assert.Equal(t, "AC-bosun", alertCfg.TwilioAccountSID)
	})

	t.Run("empty env vars fall back to config file values", func(t *testing.T) {
		clearAlertEnvVars(t)

		cfg := configFile{
			Alerts: alertConfigRaw{
				DiscordWebhookURL: "https://discord.com/config",
				SendGridAPIKey:    "SG.config-key",
				TwilioAccountSID:  "AC-config",
				OnFailure:         boolPtr(true),
			},
		}
		alertCfg := extractAlertConfig(cfg)

		assert.Equal(t, "https://discord.com/config", alertCfg.DiscordWebhookURL)
		assert.Equal(t, "SG.config-key", alertCfg.SendGridAPIKey)
		assert.Equal(t, "AC-config", alertCfg.TwilioAccountSID)
	})

	t.Run("preserves list fields untouched by env override", func(t *testing.T) {
		clearAlertEnvVars(t)

		cfg := configFile{
			Alerts: alertConfigRaw{
				SendGridToEmails: []string{"a@b.com", "c@d.com"},
				TwilioToNumbers:  []string{"+15551234567"},
				OnFailure:        boolPtr(true),
			},
		}
		alertCfg := extractAlertConfig(cfg)

		assert.Equal(t, []string{"a@b.com", "c@d.com"}, alertCfg.SendGridToEmails)
		assert.Equal(t, []string{"+15551234567"}, alertCfg.TwilioToNumbers)
	})
}

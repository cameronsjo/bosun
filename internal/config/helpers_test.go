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

func TestExtractAlertConfig(t *testing.T) {
	t.Run("defaults OnFailure to true when neither flag set", func(t *testing.T) {
		// Clear env vars that would interfere
		t.Setenv("DISCORD_WEBHOOK_URL", "")
		t.Setenv("SENDGRID_API_KEY", "")
		t.Setenv("SENDGRID_FROM_EMAIL", "")
		t.Setenv("SENDGRID_FROM_NAME", "")
		t.Setenv("TWILIO_ACCOUNT_SID", "")
		t.Setenv("TWILIO_AUTH_TOKEN", "")
		t.Setenv("TWILIO_FROM_NUMBER", "")

		cfg := configFile{}
		alertCfg := extractAlertConfig(cfg)

		assert.True(t, alertCfg.OnFailure)
		assert.False(t, alertCfg.OnSuccess)
	})

	t.Run("respects OnSuccess when explicitly set", func(t *testing.T) {
		t.Setenv("DISCORD_WEBHOOK_URL", "")
		t.Setenv("SENDGRID_API_KEY", "")
		t.Setenv("SENDGRID_FROM_EMAIL", "")
		t.Setenv("SENDGRID_FROM_NAME", "")
		t.Setenv("TWILIO_ACCOUNT_SID", "")
		t.Setenv("TWILIO_AUTH_TOKEN", "")
		t.Setenv("TWILIO_FROM_NUMBER", "")

		cfg := configFile{
			Alerts: AlertConfig{OnSuccess: true},
		}
		alertCfg := extractAlertConfig(cfg)

		assert.True(t, alertCfg.OnSuccess)
		// When OnSuccess is set, the default-OnFailure logic doesn't fire
		assert.False(t, alertCfg.OnFailure)
	})

	t.Run("env vars override config file values", func(t *testing.T) {
		t.Setenv("DISCORD_WEBHOOK_URL", "https://discord.com/env")
		t.Setenv("SENDGRID_API_KEY", "SG.env-key")
		t.Setenv("SENDGRID_FROM_EMAIL", "env@example.com")
		t.Setenv("SENDGRID_FROM_NAME", "EnvBosun")
		t.Setenv("TWILIO_ACCOUNT_SID", "AC-env")
		t.Setenv("TWILIO_AUTH_TOKEN", "env-token")
		t.Setenv("TWILIO_FROM_NUMBER", "+15550000000")

		cfg := configFile{
			Alerts: AlertConfig{
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

	t.Run("empty env vars fall back to config file values", func(t *testing.T) {
		t.Setenv("DISCORD_WEBHOOK_URL", "")
		t.Setenv("SENDGRID_API_KEY", "")
		t.Setenv("SENDGRID_FROM_EMAIL", "")
		t.Setenv("SENDGRID_FROM_NAME", "")
		t.Setenv("TWILIO_ACCOUNT_SID", "")
		t.Setenv("TWILIO_AUTH_TOKEN", "")
		t.Setenv("TWILIO_FROM_NUMBER", "")

		cfg := configFile{
			Alerts: AlertConfig{
				DiscordWebhookURL: "https://discord.com/config",
				SendGridAPIKey:    "SG.config-key",
				TwilioAccountSID:  "AC-config",
				OnFailure:         true,
			},
		}
		alertCfg := extractAlertConfig(cfg)

		assert.Equal(t, "https://discord.com/config", alertCfg.DiscordWebhookURL)
		assert.Equal(t, "SG.config-key", alertCfg.SendGridAPIKey)
		assert.Equal(t, "AC-config", alertCfg.TwilioAccountSID)
	})

	t.Run("preserves list fields untouched by env override", func(t *testing.T) {
		t.Setenv("DISCORD_WEBHOOK_URL", "")
		t.Setenv("SENDGRID_API_KEY", "")
		t.Setenv("SENDGRID_FROM_EMAIL", "")
		t.Setenv("SENDGRID_FROM_NAME", "")
		t.Setenv("TWILIO_ACCOUNT_SID", "")
		t.Setenv("TWILIO_AUTH_TOKEN", "")
		t.Setenv("TWILIO_FROM_NUMBER", "")

		cfg := configFile{
			Alerts: AlertConfig{
				SendGridToEmails: []string{"a@b.com", "c@d.com"},
				TwilioToNumbers:  []string{"+15551234567"},
				OnFailure:        true,
			},
		}
		alertCfg := extractAlertConfig(cfg)

		assert.Equal(t, []string{"a@b.com", "c@d.com"}, alertCfg.SendGridToEmails)
		assert.Equal(t, []string{"+15551234567"}, alertCfg.TwilioToNumbers)
	})
}

package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// clearAlertResolverEnv unsets every variable the alert resolver reads.
func clearAlertResolverEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"BOSUN_DISCORD_WEBHOOK_URL", "DISCORD_WEBHOOK_URL",
		"BOSUN_SLACK_WEBHOOK_URL", "SLACK_WEBHOOK_URL",
		"BOSUN_SENDGRID_API_KEY", "SENDGRID_API_KEY",
		"BOSUN_SENDGRID_FROM_EMAIL", "SENDGRID_FROM_EMAIL",
		"BOSUN_SENDGRID_FROM_NAME", "SENDGRID_FROM_NAME",
		"BOSUN_SENDGRID_TO_EMAILS", "SENDGRID_TO_EMAILS",
		"BOSUN_TWILIO_ACCOUNT_SID", "TWILIO_ACCOUNT_SID",
		"BOSUN_TWILIO_AUTH_TOKEN", "TWILIO_AUTH_TOKEN",
		"BOSUN_TWILIO_FROM_NUMBER", "TWILIO_FROM_NUMBER",
		"BOSUN_TWILIO_TO_NUMBERS", "TWILIO_TO_NUMBERS",
		"BOSUN_WEBHOOK_URL", "BOSUN_WEBHOOK_METHOD", "BOSUN_WEBHOOK_HEADERS",
	} {
		t.Setenv(key, "")
	}
}

// chdirAlertProject writes a project with the given bosun.yaml and switches into
// it, so config.Load reads that file.
func chdirAlertProject(t *testing.T, bosunYAML string) {
	t.Helper()

	projectDir := evalSymlinks(t, t.TempDir())
	require.NoError(t, os.Mkdir(filepath.Join(projectDir, "manifest"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "bosun.yaml"), []byte(bosunYAML), 0o644))

	origDir, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(projectDir))
}

// TestBuildAlertManagerRaw_RecipientSources pins that the manager the daemon and
// `bosun reconcile` share takes recipients from bosun.yaml as well as the
// environment. Before the fix it read recipients from the environment only, so a
// provider configured in bosun.yaml had credentials but no recipients and was
// dropped as unconfigured.
func TestBuildAlertManagerRaw_RecipientSources(t *testing.T) {
	const sendgridFile = "alerts:\n" +
		"  sendgrid_api_key: file-sendgrid-key\n" +
		"  sendgrid_from_email: from@example.com\n" +
		"  sendgrid_to_emails:\n    - yaml@example.com\n"
	const twilioFile = "alerts:\n" +
		"  twilio_account_sid: ACfile\n" +
		"  twilio_auth_token: file-twilio-token\n" +
		"  twilio_from_number: \"+10000000000\"\n" +
		"  twilio_to_numbers:\n    - \"+19999999999\"\n"

	tests := []struct {
		name         string
		bosunYAML    string
		env          map[string]string
		wantProvider string
		wantAbsent   string
	}{
		{
			name:         "sendgrid configured in the file",
			bosunYAML:    sendgridFile,
			wantProvider: "sendgrid",
		},
		{
			name:         "twilio configured in the file",
			bosunYAML:    twilioFile,
			wantProvider: "twilio",
		},
		{
			name: "sendgrid key from the environment with recipients from the file",
			bosunYAML: "alerts:\n" +
				"  sendgrid_from_email: from@example.com\n" +
				"  sendgrid_to_emails:\n    - yaml@example.com\n",
			env:          map[string]string{"BOSUN_SENDGRID_API_KEY": "env-sendgrid-key"},
			wantProvider: "sendgrid",
		},
		{
			name: "twilio credentials from the environment with recipients from the file",
			bosunYAML: "alerts:\n" +
				"  twilio_from_number: \"+10000000000\"\n" +
				"  twilio_to_numbers:\n    - \"+19999999999\"\n",
			env: map[string]string{
				"BOSUN_TWILIO_ACCOUNT_SID": "ACenv",
				"BOSUN_TWILIO_AUTH_TOKEN":  "env-twilio-token",
			},
			wantProvider: "twilio",
		},
		{
			// Control: the provider is configured with either list. Which list wins
			// is pinned in TestAlertConfig_RecipientEnvPrecedence.
			name:      "environment recipients alongside the file's still configure the provider",
			bosunYAML: sendgridFile,
			env: map[string]string{
				"SENDGRID_TO_EMAILS": "legacy@example.com",
			},
			wantProvider: "sendgrid",
		},
		{
			// Control: the name assertion is not vacuous. With the key and the
			// sender but no recipient anywhere, the provider is not configured.
			name: "sendgrid without recipients anywhere is not configured",
			bosunYAML: "alerts:\n" +
				"  sendgrid_api_key: file-sendgrid-key\n" +
				"  sendgrid_from_email: from@example.com\n",
			wantAbsent: "sendgrid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearAlertResolverEnv(t)
			for key, value := range tt.env {
				t.Setenv(key, value)
			}
			chdirAlertProject(t, tt.bosunYAML)

			mgr := buildAlertManagerRaw()

			if tt.wantProvider != "" {
				assert.Contains(t, mgr.ProviderNames(), tt.wantProvider)
			}
			if tt.wantAbsent != "" {
				assert.NotContains(t, mgr.ProviderNames(), tt.wantAbsent)
			}
		})
	}
}

// TestAlertConfigFromEnv_AlertCommand pins that `bosun alert test` resolves its
// environment-only fallback with the same names as the daemon. Before the fix it
// read only the unprefixed credential names and left sender and recipient
// fields empty, so a SendGrid or Twilio test could never get past its
// "not configured" checks.
func TestAlertConfigFromEnv_AlertCommand(t *testing.T) {
	t.Run("BOSUN names beat legacy names", func(t *testing.T) {
		got := alertConfigFromEnv(envGetter(map[string]string{
			"BOSUN_DISCORD_WEBHOOK_URL": "https://discord.invalid/bosun",
			"DISCORD_WEBHOOK_URL":       "https://discord.invalid/legacy",
			"BOSUN_SENDGRID_API_KEY":    "bosun-key",
			"SENDGRID_API_KEY":          "legacy-key",
			"BOSUN_TWILIO_AUTH_TOKEN":   "bosun-token",
			"TWILIO_AUTH_TOKEN":         "legacy-token",
		}))

		assert.Equal(t, "https://discord.invalid/bosun", got.DiscordWebhookURL)
		assert.Equal(t, "bosun-key", got.SendGridAPIKey)
		assert.Equal(t, "bosun-token", got.TwilioAuthToken)
	})

	t.Run("legacy names still work", func(t *testing.T) {
		got := alertConfigFromEnv(envGetter(map[string]string{
			"SLACK_WEBHOOK_URL":  "https://slack.invalid/legacy",
			"TWILIO_ACCOUNT_SID": "AClegacy",
			"TWILIO_AUTH_TOKEN":  "legacy-token",
		}))

		assert.Equal(t, "https://slack.invalid/legacy", got.SlackWebhookURL)
		assert.Equal(t, "AClegacy", got.TwilioAccountSID)
		assert.Equal(t, "legacy-token", got.TwilioAuthToken)
	})

	t.Run("sender and recipient fields are read", func(t *testing.T) {
		got := alertConfigFromEnv(envGetter(map[string]string{
			"BOSUN_SENDGRID_FROM_EMAIL": "from@example.com",
			"BOSUN_SENDGRID_FROM_NAME":  "Bosun",
			"BOSUN_SENDGRID_TO_EMAILS":  "a@example.com, b@example.com",
			"BOSUN_TWILIO_FROM_NUMBER":  "+10000000000",
			"TWILIO_TO_NUMBERS":         "+11111111111",
		}))

		assert.Equal(t, "from@example.com", got.SendGridFromEmail)
		assert.Equal(t, "Bosun", got.SendGridFromName)
		assert.Equal(t, []string{"a@example.com", "b@example.com"}, got.SendGridToEmails)
		assert.Equal(t, "+10000000000", got.TwilioFromNumber)
		assert.Equal(t, []string{"+11111111111"}, got.TwilioToNumbers)
	})

	t.Run("gates take the daemon defaults", func(t *testing.T) {
		got := alertConfigFromEnv(envGetter(nil))

		assert.True(t, got.OnFailure)
		assert.True(t, got.OnRecovery)
		assert.False(t, got.OnSuccess)
	})
}

// TestDisplayAlertStatusFromEnv_Output pins what `bosun alert status` prints when
// there is no project config. Before the fix it ignored the BOSUN names, showed
// no recipients, and reported "Alert on failure: no" because the empty config
// skips the default. It must also never print a full credential.
func TestDisplayAlertStatusFromEnv_Output(t *testing.T) {
	const (
		sendgridKey  = "SG.abcdefghijklmnopqrstuvwxyz0123456789SECRETTAIL"
		twilioSID    = "ACabcdefghijklmnopqrstuvwxyz0123456789"
		twilioToken  = "twilio-auth-token-0123456789abcdef"
		discordHook  = "https://discord.invalid/api/webhooks/1234567890/abcdefghijklmnopqrstuvwxyz"
		sendgridMail = "to@example.com"
	)
	clearAlertResolverEnv(t)
	t.Setenv("BOSUN_DISCORD_WEBHOOK_URL", discordHook)
	t.Setenv("BOSUN_SENDGRID_API_KEY", sendgridKey)
	t.Setenv("BOSUN_SENDGRID_FROM_EMAIL", "from@example.com")
	t.Setenv("BOSUN_SENDGRID_TO_EMAILS", sendgridMail)
	t.Setenv("BOSUN_TWILIO_ACCOUNT_SID", twilioSID)
	t.Setenv("BOSUN_TWILIO_AUTH_TOKEN", twilioToken)
	t.Setenv("BOSUN_TWILIO_FROM_NUMBER", "+10000000000")
	t.Setenv("BOSUN_TWILIO_TO_NUMBERS", "+19999999999")

	output := captureStdout(t, displayAlertStatusFromEnv)

	assert.Contains(t, output, "Alert on failure: yes")
	assert.Contains(t, output, "Alert on recovery: yes")
	assert.Contains(t, output, "To: [to@example.com]")
	assert.Contains(t, output, "To: [+19999999999]")
	for _, secret := range []string{sendgridKey, twilioSID, twilioToken, discordHook} {
		assert.NotContains(t, output, secret, "status output must not print a full credential")
	}
}

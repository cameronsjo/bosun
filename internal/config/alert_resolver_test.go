package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// clearAlertResolverEnv unsets every variable the alert resolver reads, so a
// test starts from the project file alone. getEnvOrDefault treats an empty
// value as unset.
func clearAlertResolverEnv(t *testing.T) {
	t.Helper()
	clearAlertEnvVars(t)
	for _, key := range []string{
		"BOSUN_SLACK_WEBHOOK_URL", "SLACK_WEBHOOK_URL",
		"BOSUN_SENDGRID_TO_EMAILS", "SENDGRID_TO_EMAILS",
		"BOSUN_TWILIO_TO_NUMBERS", "TWILIO_TO_NUMBERS",
		"BOSUN_WEBHOOK_URL", "BOSUN_WEBHOOK_METHOD", "BOSUN_WEBHOOK_HEADERS",
	} {
		t.Setenv(key, "")
	}
}

// TestAlertConfig_RecipientEnvPrecedence pins where recipients come from:
// BOSUN_* env, then the legacy env name, then bosun.yaml. A value that holds no
// address (blank, or only separators) counts as unset and falls through to the
// next source. Before the fix extractAlertConfig read recipients from the file
// only, so every env row below returned the file's list.
func TestAlertConfig_RecipientEnvPrecedence(t *testing.T) {
	const body = "alerts:\n" +
		"  sendgrid_to_emails:\n    - yaml@example.com\n" +
		"  twilio_to_numbers:\n    - \"+10000000000\"\n"

	yamlEmails := []string{"yaml@example.com"}
	yamlNumbers := []string{"+10000000000"}

	tests := []struct {
		name        string
		env         map[string]string
		wantEmails  []string
		wantNumbers []string
	}{
		{
			name:        "file only",
			wantEmails:  yamlEmails,
			wantNumbers: yamlNumbers,
		},
		{
			name: "legacy env beats the file",
			env: map[string]string{
				"SENDGRID_TO_EMAILS": "legacy@example.com",
				"TWILIO_TO_NUMBERS":  "+11111111111",
			},
			wantEmails:  []string{"legacy@example.com"},
			wantNumbers: []string{"+11111111111"},
		},
		{
			name: "BOSUN env beats legacy env and the file, and is trimmed",
			env: map[string]string{
				"BOSUN_SENDGRID_TO_EMAILS": "bosun-a@example.com, bosun-b@example.com ,",
				"SENDGRID_TO_EMAILS":       "legacy@example.com",
				"BOSUN_TWILIO_TO_NUMBERS":  "+12222222222 , +13333333333",
				"TWILIO_TO_NUMBERS":        "+11111111111",
			},
			wantEmails:  []string{"bosun-a@example.com", "bosun-b@example.com"},
			wantNumbers: []string{"+12222222222", "+13333333333"},
		},
		{
			name: "a separators-only BOSUN value falls through to the legacy value",
			env: map[string]string{
				"BOSUN_SENDGRID_TO_EMAILS": " , ",
				"SENDGRID_TO_EMAILS":       "legacy@example.com",
				"BOSUN_TWILIO_TO_NUMBERS":  " , ",
				"TWILIO_TO_NUMBERS":        "+11111111111",
			},
			wantEmails:  []string{"legacy@example.com"},
			wantNumbers: []string{"+11111111111"},
		},
		{
			name: "separators-only values in both names fall through to the file",
			env: map[string]string{
				"BOSUN_SENDGRID_TO_EMAILS": " , ",
				"SENDGRID_TO_EMAILS":       ",,",
				"BOSUN_TWILIO_TO_NUMBERS":  " , ",
				"TWILIO_TO_NUMBERS":        ",,",
			},
			wantEmails:  yamlEmails,
			wantNumbers: yamlNumbers,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearAlertResolverEnv(t)
			for key, value := range tt.env {
				t.Setenv(key, value)
			}

			got := alertConfigFrom(t, body)

			assert.Equal(t, tt.wantEmails, got.SendGridToEmails)
			assert.Equal(t, tt.wantNumbers, got.TwilioToNumbers)
		})
	}
}

// TestAlertConfig_CredentialFromEnvRecipientsFromFile pins the pairing the docs
// describe: a credential from the environment is used with the recipients named
// in bosun.yaml. Whoever can write bosun.yaml already chooses the destinations.
func TestAlertConfig_CredentialFromEnvRecipientsFromFile(t *testing.T) {
	const body = "alerts:\n" +
		"  sendgrid_from_email: from@example.com\n" +
		"  sendgrid_to_emails:\n    - yaml@example.com\n" +
		"  twilio_from_number: \"+10000000000\"\n" +
		"  twilio_to_numbers:\n    - \"+19999999999\"\n"

	clearAlertResolverEnv(t)
	t.Setenv("BOSUN_SENDGRID_API_KEY", "env-sendgrid-key")
	t.Setenv("BOSUN_TWILIO_ACCOUNT_SID", "ACenv")
	t.Setenv("BOSUN_TWILIO_AUTH_TOKEN", "env-twilio-token")

	got := alertConfigFrom(t, body)

	assert.Equal(t, "env-sendgrid-key", got.SendGridAPIKey)
	assert.Equal(t, []string{"yaml@example.com"}, got.SendGridToEmails)
	assert.Equal(t, "ACenv", got.TwilioAccountSID)
	assert.Equal(t, "env-twilio-token", got.TwilioAuthToken)
	assert.Equal(t, []string{"+19999999999"}, got.TwilioToNumbers)
}

// TestAlertConfig_WebhookHeadersPairWithURL pins that webhook headers come from
// the same source as the webhook URL, so a header from the environment (often
// an Authorization value) is never sent to a URL that only the file chose.
// Before the fix an environment header replaced the file's headers whatever
// the URL source was.
func TestAlertConfig_WebhookHeadersPairWithURL(t *testing.T) {
	const body = "alerts:\n" +
		"  webhook_url: https://file.example.com/hook\n" +
		"  webhook_headers:\n    X-Source: file\n"

	tests := []struct {
		name        string
		env         map[string]string
		wantURL     string
		wantHeaders map[string]string
	}{
		{
			name:        "file URL keeps file headers",
			wantURL:     "https://file.example.com/hook",
			wantHeaders: map[string]string{"X-Source": "file"},
		},
		{
			name:        "env headers are ignored when the URL comes from the file",
			env:         map[string]string{"BOSUN_WEBHOOK_HEADERS": `{"Authorization":"Bearer env-secret"}`},
			wantURL:     "https://file.example.com/hook",
			wantHeaders: map[string]string{"X-Source": "file"},
		},
		{
			name: "env URL takes env headers",
			env: map[string]string{
				"BOSUN_WEBHOOK_URL":     "https://env.example.com/hook",
				"BOSUN_WEBHOOK_HEADERS": `{"Authorization":"Bearer env-secret"}`,
			},
			wantURL:     "https://env.example.com/hook",
			wantHeaders: map[string]string{"Authorization": "Bearer env-secret"},
		},
		{
			name: "env URL without env headers does not inherit the file's headers",
			env: map[string]string{
				"BOSUN_WEBHOOK_URL": "https://env.example.com/hook",
			},
			wantURL:     "https://env.example.com/hook",
			wantHeaders: nil,
		},
		{
			// A guard for the new parse branch: headers are nil before the fix
			// as well, because nothing parsed them.
			name: "env URL with malformed env headers ends up with no headers",
			env: map[string]string{
				"BOSUN_WEBHOOK_URL":     "https://env.example.com/hook",
				"BOSUN_WEBHOOK_HEADERS": "not json",
			},
			wantURL:     "https://env.example.com/hook",
			wantHeaders: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearAlertResolverEnv(t)
			for key, value := range tt.env {
				t.Setenv(key, value)
			}

			got := alertConfigFrom(t, body)

			assert.Equal(t, tt.wantURL, got.WebhookURL)
			assert.Equal(t, tt.wantHeaders, got.WebhookHeaders)
		})
	}
}

// TestAlertConfigFromEnv_RecipientsAndWebhookHeaders pins the env-only builder:
// it reads the same names as the file-backed resolver. Before the fix it ignored
// the legacy recipient names and BOSUN_WEBHOOK_HEADERS.
func TestAlertConfigFromEnv_RecipientsAndWebhookHeaders(t *testing.T) {
	t.Run("legacy recipient names", func(t *testing.T) {
		clearAlertResolverEnv(t)
		t.Setenv("SENDGRID_TO_EMAILS", "legacy-a@example.com,legacy-b@example.com")
		t.Setenv("TWILIO_TO_NUMBERS", "+11111111111")

		got := AlertConfigFromEnv()

		assert.Equal(t, []string{"legacy-a@example.com", "legacy-b@example.com"}, got.SendGridToEmails)
		assert.Equal(t, []string{"+11111111111"}, got.TwilioToNumbers)
	})

	t.Run("BOSUN recipient names beat the legacy names", func(t *testing.T) {
		clearAlertResolverEnv(t)
		t.Setenv("BOSUN_SENDGRID_TO_EMAILS", "bosun@example.com")
		t.Setenv("SENDGRID_TO_EMAILS", "legacy@example.com")

		got := AlertConfigFromEnv()

		assert.Equal(t, []string{"bosun@example.com"}, got.SendGridToEmails)
	})

	t.Run("a separators-only BOSUN value falls through to the legacy name", func(t *testing.T) {
		clearAlertResolverEnv(t)
		t.Setenv("BOSUN_SENDGRID_TO_EMAILS", " , ")
		t.Setenv("SENDGRID_TO_EMAILS", "legacy@example.com")

		got := AlertConfigFromEnv()

		assert.Equal(t, []string{"legacy@example.com"}, got.SendGridToEmails)
	})

	t.Run("webhook headers with a webhook URL", func(t *testing.T) {
		clearAlertResolverEnv(t)
		t.Setenv("BOSUN_WEBHOOK_URL", "https://env.example.com/hook")
		t.Setenv("BOSUN_WEBHOOK_HEADERS", `{"Authorization":"Bearer env-secret","X-Team":"ops"}`)

		got := AlertConfigFromEnv()

		assert.Equal(t, "https://env.example.com/hook", got.WebhookURL)
		assert.Equal(t, map[string]string{"Authorization": "Bearer env-secret", "X-Team": "ops"}, got.WebhookHeaders)
	})

	t.Run("webhook headers without a webhook URL are not used", func(t *testing.T) {
		clearAlertResolverEnv(t)
		t.Setenv("BOSUN_WEBHOOK_HEADERS", `{"Authorization":"Bearer env-secret"}`)

		got := AlertConfigFromEnv()

		assert.Empty(t, got.WebhookURL)
		assert.Nil(t, got.WebhookHeaders)
	})

	t.Run("gates keep their defaults", func(t *testing.T) {
		clearAlertResolverEnv(t)

		got := AlertConfigFromEnv()

		assert.True(t, got.OnFailure)
		assert.True(t, got.OnRecovery)
		assert.False(t, got.OnSuccess)
	})
}

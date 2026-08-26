package cmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/config"
)

func TestAlertCmd_Help(t *testing.T) {
	t.Run("alert --help", func(t *testing.T) {
		output, err := executeCmd(t, "alert", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "Alert")
		assert.Contains(t, output, "status")
		assert.Contains(t, output, "test")
	})
}

func TestAlertCmd_Aliases(t *testing.T) {
	t.Run("horn alias", func(t *testing.T) {
		_, err := executeCmd(t, "horn", "--help")
		assert.NoError(t, err)
	})
}

func TestAlertStatusCmd_Help(t *testing.T) {
	t.Run("alert status --help", func(t *testing.T) {
		output, err := executeCmd(t, "alert", "status", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "providers")
	})
}

func TestAlertTestCmd_Help(t *testing.T) {
	t.Run("alert test --help", func(t *testing.T) {
		output, err := executeCmd(t, "alert", "test", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "provider")
		assert.Contains(t, output, "message")
		assert.Contains(t, output, "severity")
	})
}

func TestAlertTestCmd_Flags(t *testing.T) {
	t.Run("provider flag", func(t *testing.T) {
		output, err := executeCmd(t, "alert", "test", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "-p, --provider")
		assert.Contains(t, output, "discord")
	})

	t.Run("message flag", func(t *testing.T) {
		output, err := executeCmd(t, "alert", "test", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "-m, --message")
	})

	t.Run("severity flag", func(t *testing.T) {
		output, err := executeCmd(t, "alert", "test", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "-s, --severity")
	})
}

func TestAlertTestCmd_ReturnsErrorsThroughCobra(t *testing.T) {
	assert.NotNil(t, alertTestCmd.RunE)
	assert.Nil(t, alertTestCmd.Run)
}

func TestDisplayAlertStatus(t *testing.T) {
	t.Run("display with discord configured", func(t *testing.T) {
		cfg := config.AlertConfig{
			DiscordWebhookURL: "https://discord.invalid/api/webhooks/1234567890/abcdefghijklmnopqrstuvwxyz",
			OnFailure:         true,
		}

		// This function prints to stdout, so we just verify it doesn't panic
		displayAlertStatus(cfg)
	})

	t.Run("display with sendgrid configured", func(t *testing.T) {
		cfg := config.AlertConfig{
			SendGridAPIKey:    "SG.abcdefghijklmnopqrstuvwxyz.1234567890",
			SendGridFromEmail: "alerts@example.com",
			SendGridFromName:  "Bosun Alerts",
			SendGridToEmails:  []string{"admin@example.com"},
			OnFailure:         true,
		}

		displayAlertStatus(cfg)
	})

	t.Run("display with twilio configured", func(t *testing.T) {
		cfg := config.AlertConfig{
			TwilioAccountSID: "AC1234567890abcdefghijklmnopqrstuv",
			TwilioAuthToken:  "1234567890abcdefghijklmnopqrstuv",
			TwilioFromNumber: "+15551234567",
			TwilioToNumbers:  []string{"+15559876543"},
			OnFailure:        true,
		}

		displayAlertStatus(cfg)
	})

	t.Run("display with no providers", func(t *testing.T) {
		cfg := config.AlertConfig{
			OnFailure: true,
		}

		displayAlertStatus(cfg)
	})

	t.Run("display with all providers", func(t *testing.T) {
		cfg := config.AlertConfig{
			DiscordWebhookURL: "https://discord.invalid/api/webhooks/1234567890/abcdefghijklmnopqrstuvwxyz",
			SendGridAPIKey:    "SG.abcdefghijklmnopqrstuvwxyz.1234567890",
			SendGridFromEmail: "alerts@example.com",
			TwilioAccountSID:  "AC1234567890abcdefghijklmnopqrstuv",
			TwilioAuthToken:   "1234567890abcdefghijklmnopqrstuv",
			TwilioFromNumber:  "+15551234567",
			TwilioToNumbers:   []string{"+15559876543"},
			OnSuccess:         true,
			OnFailure:         true,
		}

		displayAlertStatus(cfg)
	})

	t.Run("short credentials are safely redacted", func(t *testing.T) {
		secrets := []string{"密🔐", "slack-secret", "sendgrid", "twilio"}
		cfg := config.AlertConfig{
			DiscordWebhookURL: secrets[0],
			SlackWebhookURL:   secrets[1],
			SendGridAPIKey:    secrets[2],
			TwilioAccountSID:  secrets[3],
			TwilioAuthToken:   "configured-auth-token",
		}

		output := captureStdout(t, func() { displayAlertStatus(cfg) })

		assert.Equal(t, 4, strings.Count(output, "[configured]"))
		for _, secret := range secrets {
			assert.NotContains(t, output, secret)
		}
	})
}

func TestRedactCredential(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		prefixLen int
		suffixLen int
		want      string
	}{
		{name: "short value", value: "secret", prefixLen: 8, suffixLen: 4, want: "[configured]"},
		{name: "exact visible length", value: "123456789012", prefixLen: 8, suffixLen: 4, want: "[configured]"},
		{name: "long value", value: "1234567890123", prefixLen: 8, suffixLen: 4, want: "12345678...0123"},
		{name: "unicode uses rune boundaries", value: "abcdefghij🔐klmn", prefixLen: 8, suffixLen: 4, want: "abcdefgh...klmn"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, redactCredential(tt.value, tt.prefixLen, tt.suffixLen))
		})
	}
}

type recordedAlertTestCall struct {
	provider string
	message  string
	severity string
}

func executeAlertTestCommand(t *testing.T, ctx context.Context, deps alertTestDependencies, args ...string) (string, string, error) {
	t.Helper()

	cmd := newAlertTestCommand(deps)
	cmd.SetContext(ctx)
	cmd.SetArgs(args)
	commandOutput := new(bytes.Buffer)
	cmd.SetOut(commandOutput)
	cmd.SetErr(commandOutput)

	var runErr error
	stdout := captureStdout(t, func() {
		runErr = cmd.Execute()
	})
	return stdout, commandOutput.String(), runErr
}

func recordingAlertTestSenders(calls *[]recordedAlertTestCall, failures map[string]error) alertTestSenders {
	record := func(provider string) alertTestSendFunc {
		return func(_ context.Context, _ config.AlertConfig, message, severity string) error {
			*calls = append(*calls, recordedAlertTestCall{provider: provider, message: message, severity: severity})
			return failures[provider]
		}
	}

	return alertTestSenders{
		discord:  record("discord"),
		slack:    record("slack"),
		sendgrid: record("sendgrid"),
		twilio:   record("twilio"),
	}
}

func fullyConfiguredAlertTestConfig() config.AlertConfig {
	return config.AlertConfig{
		DiscordWebhookURL: "discord-configured",
		SlackWebhookURL:   "slack-configured",
		SendGridAPIKey:    "sendgrid-configured",
		SendGridFromEmail: "sender@example.invalid",
		SendGridToEmails:  []string{"recipient@example.invalid"},
		TwilioAccountSID:  "twilio-configured",
		TwilioAuthToken:   "twilio-auth-configured",
		TwilioFromNumber:  "+15551234567",
		TwilioToNumbers:   []string{"+15557654321"},
	}
}

func TestExecuteAlertTests(t *testing.T) {
	t.Run("all providers succeed", func(t *testing.T) {
		var calls []recordedAlertTestCall
		senders := recordingAlertTestSenders(&calls, nil)

		output := captureStdout(t, func() {
			require.NoError(t, executeAlertTests(context.Background(), fullyConfiguredAlertTestConfig(), alertTestOptions{
				message:  "custom message",
				severity: "warning",
			}, senders))
		})

		assert.Equal(t, []recordedAlertTestCall{
			{provider: "discord", message: "custom message", severity: "warning"},
			{provider: "slack", message: "custom message", severity: "warning"},
			{provider: "sendgrid", message: "custom message", severity: "warning"},
			{provider: "twilio", message: "custom message", severity: "warning"},
		}, calls)
		assert.Contains(t, output, "Tested: 4, Passed: 4, Failed: 0")
	})

	t.Run("specific provider selection", func(t *testing.T) {
		var calls []recordedAlertTestCall
		senders := recordingAlertTestSenders(&calls, nil)

		output := captureStdout(t, func() {
			require.NoError(t, executeAlertTests(context.Background(), fullyConfiguredAlertTestConfig(), alertTestOptions{
				provider: "sendgrid",
				severity: "info",
			}, senders))
		})

		require.Len(t, calls, 1)
		assert.Equal(t, "sendgrid", calls[0].provider)
		assert.Equal(t, "This is a test alert from bosun", calls[0].message)
		assert.Contains(t, output, "Tested: 1, Passed: 1, Failed: 0")
	})

	t.Run("failures are summarized after all providers run", func(t *testing.T) {
		var calls []recordedAlertTestCall
		senders := recordingAlertTestSenders(&calls, map[string]error{"sendgrid": errors.New("rejected")})

		var runErr error
		output := captureStdout(t, func() {
			runErr = executeAlertTests(context.Background(), fullyConfiguredAlertTestConfig(), alertTestOptions{severity: "error"}, senders)
		})

		require.Error(t, runErr)
		assert.EqualError(t, runErr, "1 alert provider test(s) failed")
		assert.Len(t, calls, 4)
		assert.Contains(t, output, "Tested: 4, Passed: 3, Failed: 1")
	})

	t.Run("no configured providers returns an error", func(t *testing.T) {
		var calls []recordedAlertTestCall
		err := executeAlertTests(context.Background(), config.AlertConfig{}, alertTestOptions{}, recordingAlertTestSenders(&calls, nil))

		assert.EqualError(t, err, "no alert providers configured to test")
		assert.Empty(t, calls)
	})

	for _, tt := range []struct {
		provider string
		wantErr  string
	}{
		{provider: "discord", wantErr: "Discord not configured"},
		{provider: "slack", wantErr: "Slack not configured"},
		{provider: "sendgrid", wantErr: "SendGrid not configured"},
		{provider: "twilio", wantErr: "Twilio not configured"},
	} {
		t.Run(tt.provider+" must be configured when selected", func(t *testing.T) {
			var calls []recordedAlertTestCall
			err := executeAlertTests(context.Background(), config.AlertConfig{}, alertTestOptions{provider: tt.provider}, recordingAlertTestSenders(&calls, nil))

			assert.EqualError(t, err, tt.wantErr)
			assert.Empty(t, calls)
		})
	}
}

type alertTestContextKey struct{}

func TestAlertTestCommandExecution(t *testing.T) {
	t.Run("config flags and context reach the selected sender", func(t *testing.T) {
		wantConfig := fullyConfiguredAlertTestConfig()
		ctx := context.WithValue(context.Background(), alertTestContextKey{}, "request-context")
		getenvCalls := 0
		var gotConfig config.AlertConfig
		var gotMessage, gotSeverity, gotContext string
		deps := alertTestDependencies{
			loadConfig: func() (config.AlertConfig, error) { return wantConfig, nil },
			getenv: func(string) string {
				getenvCalls++
				return ""
			},
			senders: alertTestSenders{
				discord: func(ctx context.Context, cfg config.AlertConfig, message, severity string) error {
					gotConfig = cfg
					gotMessage = message
					gotSeverity = severity
					gotContext, _ = ctx.Value(alertTestContextKey{}).(string)
					return nil
				},
			},
		}

		stdout, commandOutput, err := executeAlertTestCommand(t, ctx, deps,
			"--provider", "discord", "--message", "from flags", "--severity", "warning")

		require.NoError(t, err)
		assert.Equal(t, wantConfig, gotConfig)
		assert.Equal(t, "from flags", gotMessage)
		assert.Equal(t, "warning", gotSeverity)
		assert.Equal(t, "request-context", gotContext)
		assert.Zero(t, getenvCalls)
		assert.Contains(t, stdout, "Tested: 1, Passed: 1, Failed: 0")
		assert.NotContains(t, commandOutput, "Usage:")
	})

	t.Run("config failure falls back to environment and defaults", func(t *testing.T) {
		env := map[string]string{
			"SLACK_WEBHOOK_URL": "https://slack.invalid/test",
		}
		var gotConfig config.AlertConfig
		var gotMessage, gotSeverity string
		deps := alertTestDependencies{
			loadConfig: func() (config.AlertConfig, error) {
				return config.AlertConfig{}, errors.New("no project config")
			},
			getenv: envGetter(env),
			senders: alertTestSenders{
				slack: func(_ context.Context, cfg config.AlertConfig, message, severity string) error {
					gotConfig = cfg
					gotMessage = message
					gotSeverity = severity
					return nil
				},
			},
		}

		stdout, commandOutput, err := executeAlertTestCommand(t, context.Background(), deps, "--provider", "slack")

		require.NoError(t, err)
		assert.Equal(t, env["SLACK_WEBHOOK_URL"], gotConfig.SlackWebhookURL)
		assert.Equal(t, "This is a test alert from bosun", gotMessage)
		assert.Equal(t, "info", gotSeverity)
		assert.Contains(t, stdout, "Tested: 1, Passed: 1, Failed: 0")
		assert.NotContains(t, commandOutput, "Usage:")
	})

	t.Run("runtime failure returns nonzero without usage", func(t *testing.T) {
		deps := alertTestDependencies{
			loadConfig: func() (config.AlertConfig, error) {
				return config.AlertConfig{DiscordWebhookURL: "https://discord.invalid/test"}, nil
			},
			getenv: envGetter(nil),
			senders: alertTestSenders{
				discord: func(context.Context, config.AlertConfig, string, string) error {
					return errors.New("delivery rejected")
				},
			},
		}

		stdout, commandOutput, err := executeAlertTestCommand(t, context.Background(), deps, "--provider", "discord")

		require.EqualError(t, err, "1 alert provider test(s) failed")
		assert.Contains(t, stdout, "Tested: 1, Passed: 0, Failed: 1")
		assert.NotContains(t, commandOutput, "Usage:")
	})

	t.Run("argument failure retains usage", func(t *testing.T) {
		cmd := newAlertTestCommand(alertTestDependencies{})
		cmd.SetArgs([]string{"--unknown-flag"})
		commandOutput := new(bytes.Buffer)
		cmd.SetOut(commandOutput)
		cmd.SetErr(commandOutput)

		err := cmd.Execute()

		require.Error(t, err)
		assert.False(t, cmd.SilenceUsage)
		assert.Contains(t, commandOutput.String(), "Usage:")
	})
}

func envGetter(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestDefaultAlertTestSenders(t *testing.T) {
	senders := defaultAlertTestSenders()
	tests := []struct {
		name string
		send alertTestSendFunc
		cfg  config.AlertConfig
		want string
	}{
		{name: "discord", send: senders.discord, want: "discord webhook URL not configured"},
		{name: "slack", send: senders.slack, want: "slack webhook URL not configured"},
		{name: "sendgrid", send: senders.sendgrid, cfg: config.AlertConfig{SendGridAPIKey: "configured"}, want: "sendgrid_from_email not configured"},
		{name: "twilio", send: senders.twilio, cfg: config.AlertConfig{TwilioAccountSID: "configured", TwilioAuthToken: "configured"}, want: "twilio_from_number not configured"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.send(context.Background(), tt.cfg, "message", "info")
			assert.EqualError(t, err, tt.want)
		})
	}
}

func TestTestDiscordAlert(t *testing.T) {
	t.Run("returns error if webhook not configured", func(t *testing.T) {
		err := testDiscordAlert(context.Background(), "", "test message", "info")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not configured")
	})
}

func TestTestSlackAlert(t *testing.T) {
	t.Run("returns error if webhook not configured", func(t *testing.T) {
		err := testSlackAlert(context.Background(), "", "test message", "info")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not configured")
	})
}

func TestTestSendGridAlert(t *testing.T) {
	t.Run("returns error if from_email not set", func(t *testing.T) {
		cfg := config.AlertConfig{
			SendGridAPIKey: "SG.test",
		}
		err := testSendGridAlert(context.Background(), cfg, "test message", "info")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "sendgrid_from_email")
	})

	t.Run("returns error if to_emails not set", func(t *testing.T) {
		cfg := config.AlertConfig{
			SendGridAPIKey:    "SG.test",
			SendGridFromEmail: "from@example.com",
		}
		err := testSendGridAlert(context.Background(), cfg, "test message", "info")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "sendgrid_to_emails")
	})
}

func TestTestTwilioAlert(t *testing.T) {
	t.Run("returns error if from_number not set", func(t *testing.T) {
		cfg := config.AlertConfig{
			TwilioAccountSID: "AC.test",
			TwilioAuthToken:  "token",
		}
		err := testTwilioAlert(context.Background(), cfg, "test message", "info")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "twilio_from_number")
	})

	t.Run("returns error if to_numbers not set", func(t *testing.T) {
		cfg := config.AlertConfig{
			TwilioAccountSID: "AC.test",
			TwilioAuthToken:  "token",
			TwilioFromNumber: "+15551234567",
		}
		err := testTwilioAlert(context.Background(), cfg, "test message", "info")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "twilio_to_numbers")
	})
}

func TestDisplayAlertStatusFromEnv(t *testing.T) {
	t.Run("reads discord from env", func(t *testing.T) {
		t.Setenv("DISCORD_WEBHOOK_URL", "https://discord.invalid/api/webhooks/1234567890/abcdefghijklmnopqrstuvwxyz")
		t.Setenv("SENDGRID_API_KEY", "")
		t.Setenv("TWILIO_ACCOUNT_SID", "")

		// displayAlertStatusFromEnv writes to stdout; verify no panic
		displayAlertStatusFromEnv()
	})

	t.Run("reads sendgrid from env", func(t *testing.T) {
		t.Setenv("DISCORD_WEBHOOK_URL", "")
		t.Setenv("SENDGRID_API_KEY", "SG.abcdefghijklmnopqrstuvwxyz.1234567890")
		t.Setenv("TWILIO_ACCOUNT_SID", "")

		displayAlertStatusFromEnv()
	})

	t.Run("reads twilio from env", func(t *testing.T) {
		t.Setenv("DISCORD_WEBHOOK_URL", "")
		t.Setenv("SENDGRID_API_KEY", "")
		t.Setenv("TWILIO_ACCOUNT_SID", "AC1234567890abcdefghijklmnopqrstuv")

		displayAlertStatusFromEnv()
	})

	t.Run("no providers configured", func(t *testing.T) {
		t.Setenv("DISCORD_WEBHOOK_URL", "")
		t.Setenv("SENDGRID_API_KEY", "")
		t.Setenv("TWILIO_ACCOUNT_SID", "")

		displayAlertStatusFromEnv()
	})
}

func TestParseSeverity(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"critical", "critical"},
		{"CRITICAL", "critical"},
		{"error", "error"},
		{"ERROR", "error"},
		{"warning", "warning"},
		{"WARNING", "warning"},
		{"info", "info"},
		{"INFO", "info"},
		{"unknown", "info"}, // defaults to info
		{"", "info"},        // defaults to info
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseSeverity(tt.input)
			assert.Equal(t, tt.expected, string(result))
		})
	}
}

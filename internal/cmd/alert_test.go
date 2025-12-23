package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"

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

func TestDisplayAlertStatus(t *testing.T) {
	t.Run("display with discord configured", func(t *testing.T) {
		cfg := config.AlertConfig{
			DiscordWebhookURL: "https://discord.com/api/webhooks/1234567890/abcdefghijklmnopqrstuvwxyz",
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
			DiscordWebhookURL: "https://discord.com/api/webhooks/1234567890/abcdefghijklmnopqrstuvwxyz",
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
}

func TestTestDiscordAlert(t *testing.T) {
	t.Run("returns error with invalid webhook", func(t *testing.T) {
		// Real API call with invalid URL returns error
		err := testDiscordAlert("https://discord.com/api/webhooks/test", "test message", "info")
		assert.Error(t, err)
	})

	t.Run("returns error if webhook not configured", func(t *testing.T) {
		err := testDiscordAlert("", "test message", "info")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not configured")
	})
}

func TestTestSendGridAlert(t *testing.T) {
	t.Run("returns error if from_email not set", func(t *testing.T) {
		cfg := config.AlertConfig{
			SendGridAPIKey: "SG.test",
		}
		err := testSendGridAlert(cfg, "test message", "info")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "sendgrid_from_email")
	})

	t.Run("returns error if to_emails not set", func(t *testing.T) {
		cfg := config.AlertConfig{
			SendGridAPIKey:    "SG.test",
			SendGridFromEmail: "from@example.com",
		}
		err := testSendGridAlert(cfg, "test message", "info")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "sendgrid_to_emails")
	})

	t.Run("returns error with invalid credentials", func(t *testing.T) {
		// Real API call with invalid credentials returns error
		cfg := config.AlertConfig{
			SendGridAPIKey:    "SG.test",
			SendGridFromEmail: "from@example.com",
			SendGridToEmails:  []string{"to@example.com"},
		}
		err := testSendGridAlert(cfg, "test message", "info")
		assert.Error(t, err)
	})
}

func TestTestTwilioAlert(t *testing.T) {
	t.Run("returns error if from_number not set", func(t *testing.T) {
		cfg := config.AlertConfig{
			TwilioAccountSID: "AC.test",
			TwilioAuthToken:  "token",
		}
		err := testTwilioAlert(cfg, "test message")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "twilio_from_number")
	})

	t.Run("returns error if to_numbers not set", func(t *testing.T) {
		cfg := config.AlertConfig{
			TwilioAccountSID: "AC.test",
			TwilioAuthToken:  "token",
			TwilioFromNumber: "+15551234567",
		}
		err := testTwilioAlert(cfg, "test message")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "twilio_to_numbers")
	})

	t.Run("returns error with invalid credentials", func(t *testing.T) {
		// Real API call with invalid credentials returns error
		cfg := config.AlertConfig{
			TwilioAccountSID: "AC.test",
			TwilioAuthToken:  "token",
			TwilioFromNumber: "+15551234567",
			TwilioToNumbers:  []string{"+15559876543"},
		}
		err := testTwilioAlert(cfg, "test message")
		assert.Error(t, err)
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

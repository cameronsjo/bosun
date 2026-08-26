package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cameronsjo/bosun/internal/alert"
	"github.com/cameronsjo/bosun/internal/config"
	"github.com/cameronsjo/bosun/internal/ui"
)

// alertCmd represents the alert command group.
var alertCmd = &cobra.Command{
	Use:     "alert",
	Aliases: []string{"horn"},
	Short:   "Alert configuration and testing commands",
	Long: `Alert commands for testing and managing notification providers.

Commands:
  status    Show which alert providers are configured
  test      Send test alert to all or specific providers`,
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

// alertStatusCmd shows configured alert providers.
var alertStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show which alert providers are configured",
	Long:  "Display the status of all alert providers and their configuration.",
	Run:   runAlertStatus,
}

// alertTestCmd sends a test alert.
var alertTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Send test alert to configured providers",
	Long:  "Send a test alert message to verify provider configuration.",
	RunE:  runAlertTest,
}

var (
	alertTestProvider string
	alertTestMessage  string
	alertTestSeverity string
)

func init() {
	// Add test command flags
	alertTestCmd.Flags().StringVarP(&alertTestProvider, "provider", "p", "", "Test specific provider (discord, slack, sendgrid, twilio)")
	alertTestCmd.Flags().StringVarP(&alertTestMessage, "message", "m", "", "Custom test message")
	alertTestCmd.Flags().StringVarP(&alertTestSeverity, "severity", "s", "info", "Test severity level (info, warning, error)")

	// Add subcommands to alert
	alertCmd.AddCommand(alertStatusCmd)
	alertCmd.AddCommand(alertTestCmd)

	// Add alert to root
	rootCmd.AddCommand(alertCmd)
}

func runAlertStatus(cmd *cobra.Command, args []string) {
	ui.Info("Checking alert configuration...")
	fmt.Println()

	cfg, err := config.Load()
	if err != nil {
		ui.Warning("Could not load project config: %v", err)
		ui.Info("Checking environment variables only...")
		fmt.Println()
		displayAlertStatusFromEnv()
		return
	}

	alertCfg := cfg.GetAlertConfig()
	displayAlertStatus(alertCfg)
}

func displayAlertStatusFromEnv() {
	// Check environment variables directly
	alertCfg := config.AlertConfig{}
	if v := os.Getenv("DISCORD_WEBHOOK_URL"); v != "" {
		alertCfg.DiscordWebhookURL = v
	}
	if v := os.Getenv("SLACK_WEBHOOK_URL"); v != "" {
		alertCfg.SlackWebhookURL = v
	}
	if v := os.Getenv("SENDGRID_API_KEY"); v != "" {
		alertCfg.SendGridAPIKey = v
	}
	if v := os.Getenv("TWILIO_ACCOUNT_SID"); v != "" {
		alertCfg.TwilioAccountSID = v
	}
	displayAlertStatus(alertCfg)
}

func redactCredential(value string, prefixLen, suffixLen int) string {
	valueRunes := []rune(value)
	if len(valueRunes) <= prefixLen+suffixLen {
		return "[configured]"
	}

	return string(valueRunes[:prefixLen]) + "..." + string(valueRunes[len(valueRunes)-suffixLen:])
}

func displayAlertStatus(alertCfg config.AlertConfig) {
	_, _ = ui.Blue.Println("--- Alert Providers ---")
	fmt.Println()

	hasProvider := false

	// Discord
	if alertCfg.DiscordWebhookURL != "" {
		ui.Success("Discord: configured")
		fmt.Printf("  Webhook URL: %s\n", redactCredential(alertCfg.DiscordWebhookURL, 30, 10))
		hasProvider = true
	} else {
		ui.Warning("Discord: not configured")
		fmt.Println("  Set DISCORD_WEBHOOK_URL or add discord_webhook_url to bosun.yaml")
	}
	fmt.Println()

	// Slack
	if alertCfg.SlackWebhookURL != "" {
		ui.Success("Slack: configured")
		fmt.Printf("  Webhook URL: %s\n", redactCredential(alertCfg.SlackWebhookURL, 30, 10))
		hasProvider = true
	} else {
		ui.Warning("Slack: not configured")
		fmt.Println("  Set SLACK_WEBHOOK_URL or add slack_webhook_url to bosun.yaml")
	}
	fmt.Println()

	// SendGrid
	if alertCfg.SendGridAPIKey != "" {
		ui.Success("SendGrid: configured")
		fmt.Printf("  API Key: %s\n", redactCredential(alertCfg.SendGridAPIKey, 8, 4))
		if alertCfg.SendGridFromEmail != "" {
			fmt.Printf("  From: %s", alertCfg.SendGridFromEmail)
			if alertCfg.SendGridFromName != "" {
				fmt.Printf(" (%s)", alertCfg.SendGridFromName)
			}
			fmt.Println()
		}
		if len(alertCfg.SendGridToEmails) > 0 {
			fmt.Printf("  To: %v\n", alertCfg.SendGridToEmails)
		}
		hasProvider = true
	} else {
		ui.Warning("SendGrid: not configured")
		fmt.Println("  Set SENDGRID_API_KEY or add sendgrid_api_key to bosun.yaml")
	}
	fmt.Println()

	// Twilio
	if alertCfg.TwilioAccountSID != "" && alertCfg.TwilioAuthToken != "" {
		ui.Success("Twilio: configured")
		fmt.Printf("  Account SID: %s\n", redactCredential(alertCfg.TwilioAccountSID, 8, 4))
		if alertCfg.TwilioFromNumber != "" {
			fmt.Printf("  From: %s\n", alertCfg.TwilioFromNumber)
		}
		if len(alertCfg.TwilioToNumbers) > 0 {
			fmt.Printf("  To: %v\n", alertCfg.TwilioToNumbers)
		}
		hasProvider = true
	} else {
		ui.Warning("Twilio: not configured")
		fmt.Println("  Set TWILIO_ACCOUNT_SID and TWILIO_AUTH_TOKEN or add to bosun.yaml")
	}
	fmt.Println()

	// Settings
	_, _ = ui.Blue.Println("--- Settings ---")
	fmt.Println()
	if alertCfg.OnSuccess {
		fmt.Println("  Alert on success: yes")
	} else {
		fmt.Println("  Alert on success: no")
	}
	if alertCfg.OnFailure {
		fmt.Println("  Alert on failure: yes")
	} else {
		fmt.Println("  Alert on failure: no")
	}
	fmt.Println()

	if !hasProvider {
		ui.Warning("No alert providers configured. Add configuration to bosun.yaml or set environment variables.")
	}
}

type alertTestSendFunc func(context.Context, config.AlertConfig, string, string) error

type alertTestSenders struct {
	discord  alertTestSendFunc
	slack    alertTestSendFunc
	sendgrid alertTestSendFunc
	twilio   alertTestSendFunc
}

type alertTestOptions struct {
	provider string
	message  string
	severity string
}

func defaultAlertTestSenders() alertTestSenders {
	return alertTestSenders{
		discord: func(ctx context.Context, cfg config.AlertConfig, message, severity string) error {
			return testDiscordAlert(ctx, cfg.DiscordWebhookURL, message, severity)
		},
		slack: func(ctx context.Context, cfg config.AlertConfig, message, severity string) error {
			return testSlackAlert(ctx, cfg.SlackWebhookURL, message, severity)
		},
		sendgrid: testSendGridAlert,
		twilio:   testTwilioAlert,
	}
}

func runAlertTest(cmd *cobra.Command, _ []string) error {
	ui.Info("Testing alert providers...")
	fmt.Println()

	cfg, err := config.Load()
	if err != nil {
		ui.Warning("Could not load project config: %v", err)
		ui.Info("Using environment variables only...")
		fmt.Println()
	}

	var alertCfg config.AlertConfig
	if cfg != nil {
		alertCfg = cfg.GetAlertConfig()
	} else {
		// Load from environment variables only
		if v := os.Getenv("DISCORD_WEBHOOK_URL"); v != "" {
			alertCfg.DiscordWebhookURL = v
		}
		if v := os.Getenv("SLACK_WEBHOOK_URL"); v != "" {
			alertCfg.SlackWebhookURL = v
		}
		if v := os.Getenv("SENDGRID_API_KEY"); v != "" {
			alertCfg.SendGridAPIKey = v
		}
		if v := os.Getenv("TWILIO_ACCOUNT_SID"); v != "" {
			alertCfg.TwilioAccountSID = v
		}
		if v := os.Getenv("TWILIO_AUTH_TOKEN"); v != "" {
			alertCfg.TwilioAuthToken = v
		}
	}

	return executeAlertTests(cmd.Context(), alertCfg, alertTestOptions{
		provider: alertTestProvider,
		message:  alertTestMessage,
		severity: alertTestSeverity,
	}, defaultAlertTestSenders())
}

func executeAlertTests(ctx context.Context, alertCfg config.AlertConfig, opts alertTestOptions, senders alertTestSenders) error {
	message := opts.message
	if message == "" {
		message = "This is a test alert from bosun"
	}

	providers := []struct {
		name       string
		label      string
		configured bool
		send       alertTestSendFunc
	}{
		{name: "discord", label: "Discord", configured: alertCfg.DiscordWebhookURL != "", send: senders.discord},
		{name: "slack", label: "Slack", configured: alertCfg.SlackWebhookURL != "", send: senders.slack},
		{name: "sendgrid", label: "SendGrid", configured: alertCfg.SendGridAPIKey != "", send: senders.sendgrid},
		{name: "twilio", label: "Twilio", configured: alertCfg.TwilioAccountSID != "" && alertCfg.TwilioAuthToken != "", send: senders.twilio},
	}

	tested, succeeded, failed := 0, 0, 0
	for _, provider := range providers {
		if opts.provider != "" && opts.provider != provider.name {
			continue
		}
		if !provider.configured {
			if opts.provider == provider.name {
				return fmt.Errorf("%s not configured", provider.label)
			}
			continue
		}

		tested++
		ui.Info("Testing %s...", provider.label)
		if err := provider.send(ctx, alertCfg, message, opts.severity); err != nil {
			ui.Error("%s test failed: %v", provider.label, err)
			failed++
		} else {
			ui.Success("%s test passed", provider.label)
			succeeded++
		}
		fmt.Println()
	}

	// Summary
	if tested == 0 {
		ui.Warning("No alert providers configured to test")
		return errors.New("no alert providers configured to test")
	}

	_, _ = ui.Blue.Println("--- Summary ---")
	fmt.Printf("  Tested: %d, Passed: %d, Failed: %d\n", tested, succeeded, failed)

	if failed > 0 {
		return fmt.Errorf("%d alert provider test(s) failed", failed)
	}

	return nil
}

// testSlackAlert sends a test message to Slack.
func testSlackAlert(ctx context.Context, webhookURL, message, severity string) error {
	if webhookURL == "" {
		return errors.New("slack webhook URL not configured")
	}

	provider := alert.NewSlackProvider(webhookURL)
	if !provider.IsConfigured() {
		return errors.New("slack webhook URL not configured")
	}

	testAlert := &alert.Alert{
		Title:    "Test Alert from Bosun",
		Message:  message,
		Severity: parseSeverity(severity),
		Source:   "alert-test",
		Metadata: map[string]string{
			"type": "test",
			"time": time.Now().Format(time.RFC3339),
		},
	}

	sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	return provider.Send(sendCtx, testAlert)
}

// testDiscordAlert sends a test message to Discord.
func testDiscordAlert(ctx context.Context, webhookURL, message, severity string) error {
	if webhookURL == "" {
		return errors.New("discord webhook URL not configured")
	}

	provider := alert.NewDiscordProvider(webhookURL)
	if !provider.IsConfigured() {
		return errors.New("discord webhook URL not configured")
	}

	testAlert := &alert.Alert{
		Title:    "Test Alert from Bosun",
		Message:  message,
		Severity: parseSeverity(severity),
		Source:   "alert-test",
		Metadata: map[string]string{
			"type": "test",
			"time": time.Now().Format(time.RFC3339),
		},
	}

	sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	return provider.Send(sendCtx, testAlert)
}

// testSendGridAlert sends a test email via SendGrid.
func testSendGridAlert(ctx context.Context, cfg config.AlertConfig, message, severity string) error {
	if cfg.SendGridFromEmail == "" {
		return fmt.Errorf("sendgrid_from_email not configured")
	}
	if len(cfg.SendGridToEmails) == 0 {
		return fmt.Errorf("sendgrid_to_emails not configured")
	}

	provider := alert.NewSendGrid(alert.SendGridConfig{
		APIKey:    cfg.SendGridAPIKey,
		FromEmail: cfg.SendGridFromEmail,
		FromName:  cfg.SendGridFromName,
		ToEmails:  cfg.SendGridToEmails,
	})

	if !provider.IsConfigured() {
		return fmt.Errorf("sendgrid not fully configured")
	}

	testAlert := &alert.Alert{
		Title:    "Test Alert from Bosun",
		Message:  message,
		Severity: parseSeverity(severity),
		Source:   "alert-test",
		Metadata: map[string]string{
			"type": "test",
			"time": time.Now().Format(time.RFC3339),
		},
	}

	sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	return provider.Send(sendCtx, testAlert)
}

// testTwilioAlert sends a test SMS via Twilio.
func testTwilioAlert(ctx context.Context, cfg config.AlertConfig, message, _ string) error {
	if cfg.TwilioFromNumber == "" {
		return fmt.Errorf("twilio_from_number not configured")
	}
	if len(cfg.TwilioToNumbers) == 0 {
		return fmt.Errorf("twilio_to_numbers not configured")
	}

	provider := alert.NewTwilio(alert.TwilioConfig{
		AccountSID: cfg.TwilioAccountSID,
		AuthToken:  cfg.TwilioAuthToken,
		FromNumber: cfg.TwilioFromNumber,
		ToNumbers:  cfg.TwilioToNumbers,
	})

	if !provider.IsConfigured() {
		return fmt.Errorf("twilio not fully configured")
	}

	// Note: Twilio only sends for error/critical severity to minimize costs
	testAlert := &alert.Alert{
		Title:    "Test Alert from Bosun",
		Message:  message,
		Severity: alert.SeverityError, // Force error severity to ensure SMS is sent
		Source:   "alert-test",
		Metadata: map[string]string{
			"type": "test",
			"time": time.Now().Format(time.RFC3339),
		},
	}

	sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	return provider.Send(sendCtx, testAlert)
}

// parseSeverity converts a string severity to alert.Severity.
func parseSeverity(s string) alert.Severity {
	switch strings.ToLower(s) {
	case "critical":
		return alert.SeverityCritical
	case "error":
		return alert.SeverityError
	case "warning":
		return alert.SeverityWarning
	default:
		return alert.SeverityInfo
	}
}

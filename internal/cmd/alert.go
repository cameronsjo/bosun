package cmd

import (
	"context"
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
		cmd.Help()
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
	Run:   runAlertTest,
}

var (
	alertTestProvider string
	alertTestMessage  string
	alertTestSeverity string
)

func init() {
	// Add test command flags
	alertTestCmd.Flags().StringVarP(&alertTestProvider, "provider", "p", "", "Test specific provider (discord, sendgrid, twilio)")
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
	if v := os.Getenv("SENDGRID_API_KEY"); v != "" {
		alertCfg.SendGridAPIKey = v
	}
	if v := os.Getenv("TWILIO_ACCOUNT_SID"); v != "" {
		alertCfg.TwilioAccountSID = v
	}
	displayAlertStatus(alertCfg)
}

func displayAlertStatus(alertCfg config.AlertConfig) {
	ui.Blue.Println("--- Alert Providers ---")
	fmt.Println()

	hasProvider := false

	// Discord
	if alertCfg.DiscordWebhookURL != "" {
		ui.Success("Discord: configured")
		fmt.Printf("  Webhook URL: %s...%s\n", alertCfg.DiscordWebhookURL[:30], alertCfg.DiscordWebhookURL[len(alertCfg.DiscordWebhookURL)-10:])
		hasProvider = true
	} else {
		ui.Warning("Discord: not configured")
		fmt.Println("  Set DISCORD_WEBHOOK_URL or add discord_webhook_url to bosun.yaml")
	}
	fmt.Println()

	// SendGrid
	if alertCfg.SendGridAPIKey != "" {
		ui.Success("SendGrid: configured")
		fmt.Printf("  API Key: %s...%s\n", alertCfg.SendGridAPIKey[:8], alertCfg.SendGridAPIKey[len(alertCfg.SendGridAPIKey)-4:])
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
		fmt.Printf("  Account SID: %s...%s\n", alertCfg.TwilioAccountSID[:8], alertCfg.TwilioAccountSID[len(alertCfg.TwilioAccountSID)-4:])
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
	ui.Blue.Println("--- Settings ---")
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

func runAlertTest(cmd *cobra.Command, args []string) {
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

	// Determine message
	message := alertTestMessage
	if message == "" {
		message = "This is a test alert from bosun"
	}

	// Track results
	tested := 0
	succeeded := 0
	failed := 0

	// Test specific provider or all
	if alertTestProvider == "" || alertTestProvider == "discord" {
		if alertCfg.DiscordWebhookURL != "" {
			tested++
			ui.Info("Testing Discord...")
			if err := testDiscordAlert(alertCfg.DiscordWebhookURL, message, alertTestSeverity); err != nil {
				ui.Error("Discord test failed: %v", err)
				failed++
			} else {
				ui.Success("Discord test passed")
				succeeded++
			}
			fmt.Println()
		} else if alertTestProvider == "discord" {
			ui.Error("Discord not configured")
			os.Exit(1)
		}
	}

	if alertTestProvider == "" || alertTestProvider == "sendgrid" {
		if alertCfg.SendGridAPIKey != "" {
			tested++
			ui.Info("Testing SendGrid...")
			if err := testSendGridAlert(alertCfg, message, alertTestSeverity); err != nil {
				ui.Error("SendGrid test failed: %v", err)
				failed++
			} else {
				ui.Success("SendGrid test passed")
				succeeded++
			}
			fmt.Println()
		} else if alertTestProvider == "sendgrid" {
			ui.Error("SendGrid not configured")
			os.Exit(1)
		}
	}

	if alertTestProvider == "" || alertTestProvider == "twilio" {
		if alertCfg.TwilioAccountSID != "" && alertCfg.TwilioAuthToken != "" {
			tested++
			ui.Info("Testing Twilio...")
			if err := testTwilioAlert(alertCfg, message); err != nil {
				ui.Error("Twilio test failed: %v", err)
				failed++
			} else {
				ui.Success("Twilio test passed")
				succeeded++
			}
			fmt.Println()
		} else if alertTestProvider == "twilio" {
			ui.Error("Twilio not configured")
			os.Exit(1)
		}
	}

	// Summary
	if tested == 0 {
		ui.Warning("No alert providers configured to test")
		os.Exit(1)
	}

	ui.Blue.Println("--- Summary ---")
	fmt.Printf("  Tested: %d, Passed: %d, Failed: %d\n", tested, succeeded, failed)

	if failed > 0 {
		os.Exit(1)
	}
}

// testDiscordAlert sends a test message to Discord.
func testDiscordAlert(webhookURL, message, severity string) error {
	provider := alert.NewDiscordProvider(webhookURL)
	if !provider.IsConfigured() {
		return fmt.Errorf("discord webhook URL not configured")
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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return provider.Send(ctx, testAlert)
}

// testSendGridAlert sends a test email via SendGrid.
func testSendGridAlert(cfg config.AlertConfig, message, severity string) error {
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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return provider.Send(ctx, testAlert)
}

// testTwilioAlert sends a test SMS via Twilio.
func testTwilioAlert(cfg config.AlertConfig, message string) error {
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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return provider.Send(ctx, testAlert)
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

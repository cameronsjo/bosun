package cmd

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cameronsjo/bosun/internal/alert"
	"github.com/cameronsjo/bosun/internal/daemon"
	"github.com/cameronsjo/bosun/internal/ui"
)

var (
	daemonPort         int
	daemonPollInterval int
	daemonDryRun       bool
)

// daemonCmd represents the daemon command.
var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Run the GitOps daemon",
	Long: `Run the GitOps daemon in foreground.

The daemon provides:
  - HTTP server for webhooks and health checks
  - Polling-based reconciliation at configurable intervals
  - Graceful shutdown on SIGTERM/SIGINT

Configuration via environment variables:
  REPO_URL / BOSUN_REPO_URL       Git repository URL (required)
  REPO_BRANCH / BOSUN_REPO_BRANCH Git branch to track (default: main)
  WEBHOOK_SECRET / GITHUB_WEBHOOK_SECRET  Webhook signature validation
  POLL_INTERVAL / BOSUN_POLL_INTERVAL     Poll interval in seconds (default: 3600)
  PORT / WEBHOOK_PORT              HTTP server port (default: 8080)
  DISCORD_WEBHOOK_URL              Discord notifications
  SENDGRID_API_KEY                 SendGrid email notifications
  TWILIO_ACCOUNT_SID               Twilio SMS notifications

Endpoints:
  /health        Health check (JSON status)
  /ready         Readiness check (200 OK or 503)
  /webhook       Generic webhook trigger
  /webhook/github GitHub push webhook
  /webhook/manual Manual trigger
  /metrics       Prometheus metrics`,
	Run: runDaemon,
}

func init() {
	daemonCmd.Flags().IntVarP(&daemonPort, "port", "p", 8080, "HTTP server port")
	daemonCmd.Flags().IntVarP(&daemonPollInterval, "poll-interval", "i", 3600, "Poll interval in seconds (0 disables)")
	daemonCmd.Flags().BoolVarP(&daemonDryRun, "dry-run", "n", false, "Dry run mode (no actual changes)")

	rootCmd.AddCommand(daemonCmd)
}

func runDaemon(cmd *cobra.Command, args []string) {
	// Load configuration from environment
	cfg := daemon.ConfigFromEnv()

	// Override with flags if set
	if cmd.Flags().Changed("port") {
		cfg.Port = daemonPort
	}
	if cmd.Flags().Changed("poll-interval") {
		cfg.PollInterval = secondsToDuration(daemonPollInterval)
	}
	if cmd.Flags().Changed("dry-run") || daemonDryRun {
		cfg.ReconcileConfig.DryRun = true
	}

	// Validate configuration
	if err := daemon.ValidateConfig(cfg); err != nil {
		ui.Fatal("Invalid configuration: %v", err)
	}

	// Set up alert manager
	cfg.AlertManager = createDaemonAlertManager()

	// Create and run daemon
	d, err := daemon.New(cfg)
	if err != nil {
		ui.Fatal("Failed to create daemon: %v", err)
	}

	ctx := context.Background()
	if err := d.Run(ctx); err != nil {
		ui.Fatal("Daemon failed: %v", err)
	}
}

// secondsToDuration converts seconds to time.Duration.
func secondsToDuration(seconds int) time.Duration {
	return time.Duration(seconds) * time.Second
}

// createDaemonAlertManager creates an alert manager for the daemon.
func createDaemonAlertManager() *alert.Manager {
	mgr := alert.NewManager()

	// Add Discord provider
	discord := alert.NewDiscordProvider(os.Getenv("DISCORD_WEBHOOK_URL"))
	mgr.AddProvider(discord)

	// Add SendGrid provider
	toEmails := filterEmptyStrings(strings.Split(os.Getenv("SENDGRID_TO_EMAILS"), ","))
	sendgrid := alert.NewSendGrid(alert.SendGridConfig{
		APIKey:    os.Getenv("SENDGRID_API_KEY"),
		FromEmail: os.Getenv("SENDGRID_FROM_EMAIL"),
		FromName:  os.Getenv("SENDGRID_FROM_NAME"),
		ToEmails:  toEmails,
	})
	mgr.AddProvider(sendgrid)

	// Add Twilio provider
	toNumbers := filterEmptyStrings(strings.Split(os.Getenv("TWILIO_TO_NUMBERS"), ","))
	twilio := alert.NewTwilio(alert.TwilioConfig{
		AccountSID: os.Getenv("TWILIO_ACCOUNT_SID"),
		AuthToken:  os.Getenv("TWILIO_AUTH_TOKEN"),
		FromNumber: os.Getenv("TWILIO_FROM_NUMBER"),
		ToNumbers:  toNumbers,
	})
	mgr.AddProvider(twilio)

	if mgr.HasProviders() {
		ui.Info("Alert providers: %v", mgr.ProviderNames())
	}

	return mgr
}

package cmd

import (
	"context"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/cameronsjo/bosun/internal/alert"
	"github.com/cameronsjo/bosun/internal/daemon"
	"github.com/cameronsjo/bosun/internal/telemetry"
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
  BOSUN_GIT_USERNAME / BOSUN_GIT_TOKEN  Paired private HTTPS Git credentials
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

	// Pass build-time version to daemon
	cfg.Version = version

	// Set up alert manager
	cfg.AlertManager = createDaemonAlertManager()

	// Initialize OpenTelemetry tracing (noop if BOSUN_OTEL_ENDPOINT is unset).
	ctx := context.Background()
	otelShutdown, err := telemetry.Init(ctx, "bosun", version, os.Getenv("BOSUN_OTEL_ENDPOINT"))
	if err != nil {
		ui.Warning("Failed to initialize OpenTelemetry: %v", err)
	} else {
		defer func() {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			_ = otelShutdown(shutdownCtx)
		}()
	}

	// Create and run daemon.
	d, daemonErr := daemon.New(cfg)
	if daemonErr != nil {
		ui.Fatal("Failed to create daemon: %v", daemonErr)
	}

	if runErr := d.Run(ctx); runErr != nil {
		ui.Fatal("Daemon failed: %v", runErr)
	}
}

// secondsToDuration converts seconds to time.Duration.
func secondsToDuration(seconds int) time.Duration {
	return time.Duration(seconds) * time.Second
}

// createDaemonAlertManager creates an alert manager for the daemon.
// Always returns a non-nil manager (callers check HasProviders() to decide
// whether to use it). Uses BOSUN_-first precedence for all alert env vars.
func createDaemonAlertManager() *alert.Manager {
	mgr := buildAlertManagerRaw() // defined in reconcile.go
	if mgr.HasProviders() {
		ui.Info("Alert providers: %v", mgr.ProviderNames())
	}
	return mgr
}

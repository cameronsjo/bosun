package cmd

import (
	"context"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/cameronsjo/bosun/internal/daemon"
	"github.com/cameronsjo/bosun/internal/ui"
)

var (
	triggerSocket  string
	triggerTCP     string
	triggerToken   string
	triggerSource  string
	triggerTimeout int
)

// triggerCmd represents the trigger command.
var triggerCmd = &cobra.Command{
	Use:   "trigger",
	Short: "Trigger a reconciliation via the daemon",
	Long: `Trigger a reconciliation run on the running bosun daemon.

This command connects to the daemon's Unix socket and requests
an immediate reconciliation. If a reconcile is already in progress,
the request is queued and will run after the current one completes.

Examples:
  bosun trigger                    # Trigger with default source "cli"
  bosun trigger -s "github-push"   # Trigger with custom source
  bosun trigger --socket /tmp/bosun.sock  # Use custom socket path`,
	Run: runTrigger,
}

func init() {
	triggerCmd.Flags().StringVar(&triggerSocket, "socket", "/var/run/bosun.sock", "Path to daemon socket")
	triggerCmd.Flags().StringVar(&triggerTCP, "tcp", "", "TCP address for remote daemon (e.g., host:9090)")
	triggerCmd.Flags().StringVar(&triggerToken, "token", "", "Bearer token for TCP auth (or BOSUN_BEARER_TOKEN)")
	triggerCmd.Flags().StringVarP(&triggerSource, "source", "s", "cli", "Source identifier for this trigger")
	triggerCmd.Flags().IntVarP(&triggerTimeout, "timeout", "t", 30, "Timeout in seconds")

	rootCmd.AddCommand(triggerCmd)
}

func runTrigger(cmd *cobra.Command, args []string) {
	var client *daemon.Client

	if triggerTCP != "" {
		// Use TCP with bearer token
		token := triggerToken
		if token == "" {
			token = os.Getenv("BOSUN_BEARER_TOKEN")
		}
		if token == "" {
			ui.Fatal("Bearer token required for TCP connection (--token or BOSUN_BEARER_TOKEN)")
		}
		client = daemon.NewTCPClient(triggerTCP, token)
	} else {
		// Use Unix socket
		client = daemon.NewClient(triggerSocket)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(triggerTimeout)*time.Second)
	defer cancel()

	// Check daemon connectivity first
	if err := client.Ping(ctx); err != nil {
		endpoint := triggerSocket
		if triggerTCP != "" {
			endpoint = triggerTCP
		}
		ui.Fatal("Cannot connect to daemon at %s: %v", endpoint, err)
	}

	// Trigger reconciliation
	resp, err := client.Trigger(ctx, triggerSource)
	if err != nil {
		ui.Fatal("Failed to trigger reconciliation: %v", err)
	}

	ui.Success("Reconciliation %s: %s", resp.Status, resp.Message)
}

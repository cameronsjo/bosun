package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/cameronsjo/bosun/internal/daemon"
	"github.com/cameronsjo/bosun/internal/ui"
)

var (
	statusSocket  string
	statusTimeout int
	statusJSON    bool
)

// statusCmd represents the status command (daemon status, not yacht status).
var daemonStatusCmd = &cobra.Command{
	Use:     "daemon-status",
	Aliases: []string{"ds"},
	Short:   "Show daemon status",
	Long: `Show the current status of the bosun daemon.

This command connects to the daemon's Unix socket and retrieves
status information including:
  - Current state (idle or reconciling)
  - Last reconciliation time and result
  - Daemon uptime

Examples:
  bosun daemon-status              # Show daemon status
  bosun ds                         # Short alias
  bosun daemon-status --json       # Output as JSON`,
	Run: runDaemonStatus,
}

func init() {
	daemonStatusCmd.Flags().StringVar(&statusSocket, "socket", resolveSocketPath(), "Path to daemon socket")
	daemonStatusCmd.Flags().IntVarP(&statusTimeout, "timeout", "t", 10, "Timeout in seconds")
	daemonStatusCmd.Flags().BoolVar(&statusJSON, "json", false, "Output as JSON")

	rootCmd.AddCommand(daemonStatusCmd)
}

func runDaemonStatus(cmd *cobra.Command, args []string) {
	client := daemon.NewClient(statusSocket)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(statusTimeout)*time.Second)
	defer cancel()

	// Get status
	status, err := client.Status(ctx)
	if err != nil {
		ui.Fatal("Failed to get daemon status: %v", err)
	}

	// Get health for additional info
	health, err := client.Health(ctx)
	if err != nil {
		ui.Warning("Could not get health info: %v", err)
	}

	if statusJSON {
		printStatusJSON(status, health)
		return
	}

	printStatusHuman(status, health)
}

func printStatusHuman(status *daemon.StatusResponse, health *daemon.HealthStatus) {
	ui.Header("=== Bosun Daemon Status ===")
	fmt.Println()

	// State
	stateColor := ui.Green
	stateIcon := "●"
	if status.State == "reconciling" {
		stateColor = ui.Yellow
		stateIcon = "◐"
	}
	_, _ = stateColor.Printf("  %s State: %s\n", stateIcon, status.State)

	// Uptime
	fmt.Printf("    Uptime: %s\n", status.Uptime)

	// Last reconcile
	if status.LastReconcile != nil {
		ago := time.Since(*status.LastReconcile).Round(time.Second)
		fmt.Printf("    Last Reconcile: %s ago\n", ago)
	} else {
		fmt.Printf("    Last Reconcile: never\n")
	}

	// Last error
	if status.LastError != "" {
		_, _ = ui.Red.Printf("  ✗ Last Error: %s\n", status.LastError)
	}

	// Health status
	if health != nil {
		fmt.Println()
		healthColor := ui.Green
		healthIcon := "✓"
		if health.Status == "degraded" {
			healthColor = ui.Yellow
			healthIcon = "⚠"
		}
		_, _ = healthColor.Printf("  %s Health: %s\n", healthIcon, health.Status)

		readyIcon := "✓"
		readyColor := ui.Green
		if !health.Ready {
			readyIcon = "✗"
			readyColor = ui.Red
		}
		_, _ = readyColor.Printf("  %s Ready: %v\n", readyIcon, health.Ready)
	}

	fmt.Println()
}

// statusJSONOutput is the JSON representation of daemon status for machine-readable output.
type statusJSONOutput struct {
	State         string  `json:"state"`
	Uptime        string  `json:"uptime"`
	LastReconcile *string `json:"last_reconcile"`
	LastError     *string `json:"last_error"`
	Health        *string `json:"health"`
	Ready         *bool   `json:"ready"`
}

func printStatusJSON(status *daemon.StatusResponse, health *daemon.HealthStatus) {
	out := statusJSONOutput{
		State:  status.State,
		Uptime: status.Uptime,
	}

	if status.LastReconcile != nil {
		t := status.LastReconcile.Format(time.RFC3339)
		out.LastReconcile = &t
	}

	if status.LastError != "" {
		out.LastError = &status.LastError
	}

	if health != nil {
		out.Health = &health.Status
		out.Ready = &health.Ready
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

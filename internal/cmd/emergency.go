// Package cmd provides the CLI commands for bosun.
package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cameronsjo/bosun/internal/config"
	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/snapshot"
	"github.com/cameronsjo/bosun/internal/ui"
)

var (
	maydayList     bool
	maydayRollback string
)

// maydayCmd handles emergency situations.
var maydayCmd = &cobra.Command{
	Use:     "mayday",
	Aliases: []string{"mutiny"},
	Short:   "Show recent errors across all crew",
	Long: `Emergency command to show recent errors from container logs.

By default, shows recent errors from all running containers.
Use --list to show available snapshots for rollback.
Use --rollback to restore a previous snapshot.`,
	Run: runMayday,
}

func runMayday(cmd *cobra.Command, args []string) {
	cfg, err := config.Load()
	if err != nil {
		// Config not required for basic error viewing
		cfg = nil
	}

	if maydayList {
		showSnapshots(cfg)
		return
	}

	if maydayRollback != "" {
		doRollback(cfg, maydayRollback)
		return
	}

	// Default: show recent errors
	showRecentErrors()
}

func showRecentErrors() {
	ui.Mayday("MAYDAY - Recent errors across all crew:")
	fmt.Println()

	ctx := context.Background()
	client, err := docker.NewClient()
	if err != nil {
		ui.Error("Docker not available: %v", err)
		return
	}
	defer client.Close()

	containers, err := client.ListContainers(ctx, true)
	if err != nil {
		ui.Error("Failed to list containers: %v", err)
		return
	}

	errorRegex := regexp.MustCompile(`(?i)(error|fatal|panic|exception)`)
	errorCount := 0
	maxErrors := 50

	for _, ctr := range containers {
		logs, err := client.GetContainerLogs(ctx, ctr.Name, 20)
		if err != nil {
			continue
		}

		lines := strings.Split(logs, "\n")
		for _, line := range lines {
			if errorCount >= maxErrors {
				break
			}
			// Clean up Docker log prefix (first 8 bytes are header)
			cleanLine := stripDockerLogPrefix(line)
			if errorRegex.MatchString(cleanLine) {
				ui.Red.Printf("[%s] %s\n", ctr.Name, cleanLine)
				errorCount++
			}
		}
	}

	if errorCount == 0 {
		ui.Green.Println("No recent errors found")
	}
}

func showSnapshots(cfg *config.Config) {
	if cfg == nil {
		ui.Yellow.Println("No snapshots found")
		fmt.Println("Snapshots are created automatically before each provision")
		return
	}

	snapshots, err := snapshot.List(cfg.ManifestDir)
	if err != nil {
		ui.Error("Failed to list snapshots: %v", err)
		return
	}

	if len(snapshots) == 0 {
		ui.Yellow.Println("No snapshots found")
		fmt.Println("Snapshots are created automatically before each provision")
		return
	}

	ui.Package("Available snapshots:")
	fmt.Println()

	for i, snap := range snapshots {
		if i >= 10 {
			remaining := len(snapshots) - 10
			fmt.Printf("  ... and %d more\n", remaining)
			break
		}

		ui.Green.Printf("  %s\n", snap.Name)
		fmt.Printf("    Created: %s\n", snap.Created.Format("2006-01-02 15:04:05"))
		fmt.Printf("    Files: %d\n", snap.FileCount)
		fmt.Println()
	}
}

func doRollback(cfg *config.Config, target string) {
	if cfg == nil {
		ui.Error("Project root not found")
		os.Exit(1)
	}

	snapshots, err := snapshot.List(cfg.ManifestDir)
	if err != nil {
		ui.Error("Failed to list snapshots: %v", err)
		os.Exit(1)
	}

	if len(snapshots) == 0 {
		ui.Error("No snapshots available")
		os.Exit(1)
	}

	// If target is empty or "interactive", prompt user
	if target == "" || target == "interactive" {
		target = promptForSnapshot(snapshots)
		if target == "" {
			fmt.Println("Aborted.")
			return
		}
	}

	// Verify target exists
	found := false
	for _, snap := range snapshots {
		if snap.Name == target {
			found = true
			break
		}
	}
	if !found {
		ui.Error("Snapshot not found: %s", target)
		os.Exit(1)
	}

	ui.Yellow.Printf("Rolling back to: %s\n", target)
	fmt.Println()

	if err := snapshot.Restore(cfg.ManifestDir, target); err != nil {
		ui.Error("Rollback failed: %v", err)
		os.Exit(1)
	}

	ui.Success("Rollback complete")
	fmt.Println()

	// Show restored files
	files, _ := snapshot.GetRestoredFiles(cfg.ManifestDir)
	if len(files) > 0 {
		fmt.Println("Restored files:")
		for _, f := range files {
			fmt.Printf("  - %s\n", f)
		}
		fmt.Println()
	}

	ui.Yellow.Println("Note: Run 'bosun yacht up' to apply restored configuration")
}

func promptForSnapshot(snapshots []snapshot.SnapshotInfo) string {
	ui.Blue.Println("Available snapshots:")
	fmt.Println()

	maxShow := 5
	if len(snapshots) < maxShow {
		maxShow = len(snapshots)
	}

	for i := 0; i < maxShow; i++ {
		snap := snapshots[i]
		fmt.Printf("  %d) %s (%s)\n", i+1, snap.Name, snap.Created.Format("2006-01-02 15:04:05"))
	}
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Select snapshot (1-5, or name): ")
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	if input == "" {
		return ""
	}

	// Check if input is a number
	if n, err := strconv.Atoi(input); err == nil && n >= 1 && n <= maxShow {
		return snapshots[n-1].Name
	}

	// Otherwise treat as snapshot name
	return input
}

func stripDockerLogPrefix(line string) string {
	// Docker multiplexed log format has 8-byte header
	// First byte: stream type (1=stdout, 2=stderr)
	// Bytes 2-4: reserved
	// Bytes 5-8: length (big-endian uint32)
	if len(line) >= 8 {
		// Check if this looks like a multiplexed header
		if line[0] == 1 || line[0] == 2 {
			return line[8:]
		}
	}
	return line
}

// overboardCmd forcefully removes a container.
var overboardCmd = &cobra.Command{
	Use:     "overboard [name]",
	Aliases: []string{"plank"},
	Short:   "Force remove a problematic container",
	Long:    "Forcefully remove a container by name. Use with caution!",
	Args:    cobra.ExactArgs(1),
	Run:     runOverboard,
}

func runOverboard(cmd *cobra.Command, args []string) {
	name := args[0]

	ctx := context.Background()
	client, err := docker.NewClient()
	if err != nil {
		ui.Error("Docker not available: %v", err)
		os.Exit(1)
	}
	defer client.Close()

	ui.Red.Printf("Man overboard! Removing %s...\n", name)

	if err := client.RemoveContainer(ctx, name); err != nil {
		ui.Error("Failed to remove container: %v", err)
		os.Exit(1)
	}

	ui.Success("Container %s removed", name)
}

func init() {
	maydayCmd.Flags().BoolVarP(&maydayList, "list", "l", false, "List available snapshots")
	maydayCmd.Flags().StringVarP(&maydayRollback, "rollback", "r", "", "Rollback to a snapshot (use 'interactive' for menu)")

	rootCmd.AddCommand(maydayCmd)
	rootCmd.AddCommand(overboardCmd)
}

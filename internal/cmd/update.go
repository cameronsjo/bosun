package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cameronsjo/bosun/internal/ui"
	"github.com/cameronsjo/bosun/internal/update"
)

var updateCmd = &cobra.Command{
	Use:     "update",
	Aliases: []string{"upgrade", "selfupdate"},
	Short:   "Update bosun to the latest version",
	Long: `Update bosun to the latest version from GitHub releases.

This command will:
1. Check for a newer version on GitHub
2. Download the appropriate binary for your platform
3. Replace the current binary with the new version

Examples:
  bosun update           # Update to latest version
  bosun update --check   # Check for updates without installing`,
	Run: runUpdate,
}

var (
	checkOnly bool
)

func init() {
	rootCmd.AddCommand(updateCmd)
	updateCmd.Flags().BoolVar(&checkOnly, "check", false, "Only check for updates, don't install")
}

func runUpdate(cmd *cobra.Command, args []string) {
	currentVersion := version
	platform := update.GetPlatformInfo()

	ui.Blue.Printf("Current version: %s (%s)\n", currentVersion, platform)

	if checkOnly {
		checkForUpdate(currentVersion)
		return
	}

	performUpdate(currentVersion)
}

func checkForUpdate(currentVersion string) {
	ui.Blue.Println("Checking for updates...")

	release, available, err := update.CheckForUpdate(currentVersion)
	if err != nil {
		ui.Error("Failed to check for updates: %v", err)
		return
	}

	if !available {
		ui.Success("You're running the latest version!")
		return
	}

	ui.Success("New version available: %s (released %s)", release.Version, release.PublishedAt)
	fmt.Println()
	ui.Blue.Println("To update, run: bosun update")
	fmt.Println()

	if release.Changelog != "" {
		ui.Yellow.Println("What's new:")
		// Print first few lines of changelog
		lines := strings.Split(release.Changelog, "\n")
		maxLines := 10
		if len(lines) < maxLines {
			maxLines = len(lines)
		}
		for i := 0; i < maxLines; i++ {
			fmt.Printf("  %s\n", lines[i])
		}
		if len(lines) > maxLines {
			fmt.Printf("  ... (%d more lines)\n", len(lines)-maxLines)
		}
	}
}

func performUpdate(currentVersion string) {
	ui.Blue.Println("Checking for updates...")

	release, err := update.Update(currentVersion)
	if err != nil {
		ui.Error("Update failed: %v", err)
		return
	}

	if release == nil {
		ui.Success("You're already running the latest version!")
		return
	}

	fmt.Println()
	ui.Success("Successfully updated to version %s!", release.Version)
	fmt.Println()

	if release.Changelog != "" {
		ui.Yellow.Println("What's new:")
		lines := strings.Split(release.Changelog, "\n")
		maxLines := 10
		if len(lines) < maxLines {
			maxLines = len(lines)
		}
		for i := 0; i < maxLines; i++ {
			fmt.Printf("  %s\n", lines[i])
		}
		if len(lines) > maxLines {
			fmt.Printf("  ... (%d more lines)\n", len(lines)-maxLines)
		}
	}

	fmt.Println()
	ui.Blue.Println("Restart bosun to use the new version.")
}

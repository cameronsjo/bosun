package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cameronsjo/bosun/internal/ui"
	"github.com/cameronsjo/bosun/internal/update"
)

var updateCmd = &cobra.Command{
	Use:     "update",
	Aliases: []string{"selfupdate"},
	Short:   "Update bosun to the latest version",
	Long: `Update bosun to the latest version from GitHub releases.

This command will:
1. Check for a newer version on GitHub
2. Download the appropriate binary for your platform
3. Replace the current binary with the new version

Examples:
  bosun update           # Update to latest version
  bosun update --check   # Check for updates without installing`,
	RunE: runUpdate,
}

var (
	checkOnly bool
)

type updateCommandDependencies struct {
	check   func(context.Context, string) (*update.Release, bool, error)
	install func(context.Context, string) (*update.Release, error)
}

func init() {
	rootCmd.AddCommand(updateCmd)
	updateCmd.Flags().BoolVar(&checkOnly, "check", false, "Only check for updates, don't install")
}

func runUpdate(cmd *cobra.Command, _ []string) error {
	return runUpdateWithDependencies(cmd, version, checkOnly, updateCommandDependencies{
		check:   update.CheckForUpdate,
		install: update.Update,
	})
}

func runUpdateWithDependencies(
	cmd *cobra.Command,
	currentVersion string,
	onlyCheck bool,
	deps updateCommandDependencies,
) (err error) {
	defer func() {
		if err != nil {
			cmd.SilenceUsage = true
		}
	}()

	return runUpdateCommand(cmd.Context(), currentVersion, onlyCheck, deps)
}

func runUpdateCommand(
	ctx context.Context,
	currentVersion string,
	onlyCheck bool,
	deps updateCommandDependencies,
) error {
	if ctx == nil {
		return update.ErrNilContext
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	platform := update.GetPlatformInfo()

	_, _ = ui.Blue.Printf("Current version: %s (%s)\n", currentVersion, platform)

	if onlyCheck {
		return checkForUpdate(ctx, currentVersion, deps.check)
	}

	return performUpdate(ctx, currentVersion, deps.install)
}

func checkForUpdate(
	ctx context.Context,
	currentVersion string,
	check func(context.Context, string) (*update.Release, bool, error),
) error {
	_, _ = ui.Blue.Println("Checking for updates...")

	release, available, err := check(ctx, currentVersion)
	if err != nil {
		return fmt.Errorf("check for updates: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("check for updates: %w", err)
	}

	if !available {
		ui.Success("You're running the latest version!")
		return nil
	}
	if release == nil {
		return fmt.Errorf("check for updates: %w", update.ErrMissingRelease)
	}

	ui.Success("New version available: %s (released %s)", release.Version, release.PublishedAt)
	fmt.Println()
	_, _ = ui.Blue.Println("To update, run: bosun update")
	fmt.Println()

	if release.Changelog != "" {
		_, _ = ui.Yellow.Println("What's new:")
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

	return nil
}

func performUpdate(
	ctx context.Context,
	currentVersion string,
	install func(context.Context, string) (*update.Release, error),
) error {
	_, _ = ui.Blue.Println("Checking for updates...")

	release, err := install(ctx, currentVersion)
	if err != nil {
		return fmt.Errorf("update bosun: %w", err)
	}

	if release == nil {
		ui.Success("You're already running the latest version!")
		return nil
	}

	fmt.Println()
	ui.Success("Successfully updated to version %s!", release.Version)
	fmt.Println()

	if release.Changelog != "" {
		_, _ = ui.Yellow.Println("What's new:")
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
	_, _ = ui.Blue.Println("Restart bosun to use the new version.")
	return nil
}

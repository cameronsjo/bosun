package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/cameronsjo/bosun/internal/manifest"
	"github.com/cameronsjo/bosun/internal/ui"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate manifests to add apiVersion and kind fields",
	Long: `Migrate manifests to the current schema version.

This command scans manifest directories and adds apiVersion/kind fields
to unversioned manifests. By default, it runs in dry-run mode showing
what would be changed without modifying files.

Examples:
  # Show which files need migration (dry-run)
  bosun migrate

  # Actually migrate files
  bosun migrate --write

  # Scan specific directories
  bosun migrate --provisions ./provisions --services ./services --stacks ./stacks`,
	Run: runMigrate,
}

var (
	migrateWrite     bool
	migrateProvDir   string
	migrateServDir   string
	migrateStacksDir string
)

func init() {
	rootCmd.AddCommand(migrateCmd)

	migrateCmd.Flags().BoolVarP(&migrateWrite, "write", "w", false, "Write changes to files (default is dry-run)")
	migrateCmd.Flags().StringVar(&migrateProvDir, "provisions", "provisions", "Provisions directory to scan")
	migrateCmd.Flags().StringVar(&migrateServDir, "services", "services", "Services directory to scan")
	migrateCmd.Flags().StringVar(&migrateStacksDir, "stacks", "stacks", "Stacks directory to scan")
}

func runMigrate(cmd *cobra.Command, args []string) {
	// Get absolute paths
	cwd, err := os.Getwd()
	if err != nil {
		ui.Red.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	provDir := filepath.Join(cwd, migrateProvDir)
	servDir := filepath.Join(cwd, migrateServDir)
	stacksDir := filepath.Join(cwd, migrateStacksDir)

	dirs := []string{provDir, servDir, stacksDir}

	if migrateWrite {
		ui.Yellow.Println("Migrating manifests...")
	} else {
		ui.Blue.Println("Scanning for unversioned manifests (dry-run mode)...")
		fmt.Println("Use --write to apply changes")
		fmt.Println()
	}

	opts := manifest.MigrateOptions{
		DryRun:  !migrateWrite,
		Verbose: true,
	}

	results, err := manifest.MigrateDirectory(dirs, opts)
	if err != nil {
		ui.Red.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	if len(results) == 0 {
		fmt.Println("No manifest files found in specified directories.")
		return
	}

	// Display results
	var migrated, skipped, errors int
	for _, r := range results {
		if r.Error != nil {
			errors++
			ui.Red.Printf("  ERROR: %s - %v\n", r.Path, r.Error)
		} else if r.Migrated {
			migrated++
			action := "would migrate"
			if migrateWrite {
				action = "migrated"
			}
			ui.Green.Printf("  %s: %s (kind: %s)\n", action, r.Path, r.Kind)
		} else if r.WasVersioned {
			skipped++
			fmt.Printf("  skipped: %s (already versioned)\n", r.Path)
		}
	}

	fmt.Println()

	// Summary
	if migrateWrite {
		ui.Green.Printf("Migrated: %d files\n", migrated)
	} else {
		ui.Blue.Printf("Would migrate: %d files\n", migrated)
	}
	fmt.Printf("Already versioned: %d files\n", skipped)
	if errors > 0 {
		ui.Red.Printf("Errors: %d files\n", errors)
	}

	if !migrateWrite && migrated > 0 {
		fmt.Println()
		ui.Yellow.Println("Run 'bosun migrate --write' to apply changes.")
	}
}

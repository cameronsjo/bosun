package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/cameronsjo/bosun/internal/daemon"
	"github.com/cameronsjo/bosun/internal/reconcile"
	"github.com/cameronsjo/bosun/internal/ui"
)

var (
	validateSocket  string
	validateTimeout int
	validateFull    bool
)

// validateCmd represents the validate command.
var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate configuration and connectivity",
	Long: `Validate bosun configuration without making changes.

This command performs validation checks:
  1. Environment variables and configuration
  2. Daemon connectivity (if running)
  3. Repository access and credentials
  4. Full dry-run reconciliation (with --full)

Use this before deploying to catch configuration issues early.

Examples:
  bosun validate              # Quick validation
  bosun validate --full       # Full dry-run validation
  bosun validate --socket /tmp/bosun.sock  # Check specific daemon`,
	Run: runValidate,
}

func init() {
	validateCmd.Flags().StringVar(&validateSocket, "socket", resolveSocketPath(), "Path to daemon socket")
	validateCmd.Flags().IntVarP(&validateTimeout, "timeout", "t", 30, "Timeout in seconds")
	validateCmd.Flags().BoolVar(&validateFull, "full", false, "Run full dry-run reconciliation")

	rootCmd.AddCommand(validateCmd)
}

func runValidate(cmd *cobra.Command, args []string) {
	ui.Header("=== Bosun Validation ===")
	fmt.Println()

	var errors, warnings int

	// 1. Check environment configuration
	_, _ = ui.Blue.Println("--- Environment Configuration ---")
	errors += validateEnvironment()
	fmt.Println()

	// 2. Check daemon connectivity (if socket exists)
	_, _ = ui.Blue.Println("--- Daemon Connectivity ---")
	warnings += validateDaemonConnection()
	fmt.Println()

	// 3. Check reconcile configuration
	_, _ = ui.Blue.Println("--- Reconcile Configuration ---")
	errors += validateReconcileConfig()
	fmt.Println()

	// 4. Full dry-run if requested
	if validateFull {
		_, _ = ui.Blue.Println("--- Full Dry-Run ---")
		if err := runFullDryRun(); err != nil {
			ui.Error("Dry-run failed: %v", err)
			errors++
		} else {
			_, _ = ui.Green.Println("  * Dry-run completed successfully")
		}
		fmt.Println()
	}

	// Summary
	_, _ = ui.Blue.Println("--- Summary ---")
	if errors > 0 {
		_, _ = ui.Red.Printf("  Errors: %d\n", errors)
	}
	if warnings > 0 {
		_, _ = ui.Yellow.Printf("  Warnings: %d\n", warnings)
	}

	if errors == 0 && warnings == 0 {
		_, _ = ui.Green.Println("  * All validations passed")
	}

	fmt.Println()

	if errors > 0 {
		_, _ = ui.Red.Println("Validation failed. Fix errors before deploying.")
		os.Exit(1)
	} else if warnings > 0 {
		_, _ = ui.Yellow.Println("Validation passed with warnings.")
	} else {
		_, _ = ui.Green.Println("Configuration is valid!")
	}
}

func validateEnvironment() int {
	errors := 0

	// Check for required environment variables.
	// BOSUN_REPO_URL takes precedence over legacy REPO_URL.
	repoURL := os.Getenv("BOSUN_REPO_URL")
	if repoURL == "" {
		repoURL = os.Getenv("REPO_URL")
	}

	if repoURL != "" {
		_, _ = ui.Green.Printf("  * REPO_URL: %s\n", repoURL)
	} else {
		_, _ = ui.Red.Println("  x BOSUN_REPO_URL (or legacy REPO_URL) not set")
		errors++
	}

	// Check optional but important variables.
	// BOSUN_REPO_BRANCH takes precedence over legacy REPO_BRANCH.
	branch := os.Getenv("BOSUN_REPO_BRANCH")
	if branch == "" {
		branch = os.Getenv("REPO_BRANCH")
	}
	if branch == "" {
		branch = "main (default)"
	}
	_, _ = ui.Green.Printf("  * Branch: %s\n", branch)

	// Check webhook secret
	webhookSecret := os.Getenv("WEBHOOK_SECRET")
	if webhookSecret == "" {
		webhookSecret = os.Getenv("GITHUB_WEBHOOK_SECRET")
	}
	if webhookSecret != "" {
		_, _ = ui.Green.Println("  * Webhook secret: configured")
	} else {
		_, _ = ui.Yellow.Println("  ! Webhook secret: not set (webhooks will be insecure)")
	}

	// Check deploy target
	target := os.Getenv("DEPLOY_TARGET")
	if target != "" {
		_, _ = ui.Green.Printf("  * Deploy target: %s\n", target)
	} else {
		_, _ = ui.Green.Println("  * Deploy target: local (no remote)")
	}

	return errors
}

func validateDaemonConnection() int {
	warnings := 0

	// Check if socket exists
	if _, err := os.Stat(validateSocket); os.IsNotExist(err) {
		_, _ = ui.Yellow.Printf("  ! Socket not found: %s\n", validateSocket)
		_, _ = ui.Yellow.Println("    (Daemon may not be running)")
		return 1
	}

	client := daemon.NewClient(validateSocket)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(validateTimeout)*time.Second)
	defer cancel()

	// Try to ping daemon
	if err := client.Ping(ctx); err != nil {
		_, _ = ui.Yellow.Printf("  ! Cannot connect to daemon: %v\n", err)
		return 1
	}

	_, _ = ui.Green.Println("  * Daemon is reachable")

	// Get health status
	health, err := client.Health(ctx)
	if err != nil {
		_, _ = ui.Yellow.Printf("  ! Cannot get health: %v\n", err)
		return 1
	}

	if health.Status == "healthy" {
		_, _ = ui.Green.Printf("  * Daemon health: %s\n", health.Status)
	} else {
		_, _ = ui.Yellow.Printf("  ! Daemon health: %s\n", health.Status)
		if health.LastError != "" {
			_, _ = ui.Yellow.Printf("    Last error: %s\n", health.LastError)
		}
		warnings++
	}

	if health.Ready {
		_, _ = ui.Green.Println("  * Daemon ready: yes")
	} else {
		_, _ = ui.Yellow.Println("  ! Daemon ready: no (still initializing?)")
		warnings++
	}

	return warnings
}

func validateReconcileConfig() int {
	errors := 0

	cfg := reconcile.DefaultConfig()

	// Load from environment. BOSUN_REPO_URL takes precedence over legacy REPO_URL.
	cfg.RepoURL = os.Getenv("BOSUN_REPO_URL")
	if cfg.RepoURL == "" {
		cfg.RepoURL = os.Getenv("REPO_URL")
	}

	if cfg.RepoURL == "" {
		_, _ = ui.Red.Println("  x Repository URL not configured")
		errors++
		return errors
	}

	// Validate URL format
	if !isValidGitURL(cfg.RepoURL) {
		_, _ = ui.Red.Printf("  x Invalid repository URL: %s\n", cfg.RepoURL)
		errors++
	} else {
		_, _ = ui.Green.Printf("  * Repository URL format: valid\n")
	}

	// Check directories
	if cfg.RepoDir != "" {
		if _, err := os.Stat(cfg.RepoDir); err == nil {
			_, _ = ui.Green.Printf("  * Repo directory exists: %s\n", cfg.RepoDir)
		} else {
			_, _ = ui.Yellow.Printf("  ! Repo directory will be created: %s\n", cfg.RepoDir)
		}
	}

	return errors
}

func runFullDryRun() error {
	cfg := reconcile.DefaultConfig()

	// Load from environment. BOSUN_REPO_URL takes precedence over legacy REPO_URL.
	cfg.RepoURL = os.Getenv("BOSUN_REPO_URL")
	if cfg.RepoURL == "" {
		cfg.RepoURL = os.Getenv("REPO_URL")
	}

	// BOSUN_REPO_BRANCH takes precedence over legacy REPO_BRANCH.
	if branch := os.Getenv("BOSUN_REPO_BRANCH"); branch != "" {
		cfg.RepoBranch = branch
	} else if branch := os.Getenv("REPO_BRANCH"); branch != "" {
		cfg.RepoBranch = branch
	}

	if target := os.Getenv("DEPLOY_TARGET"); target != "" {
		cfg.TargetHost = target
	}

	// Force dry-run
	cfg.DryRun = true

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	r := reconcile.NewReconciler(cfg)
	return r.Run(ctx)
}

// isValidGitURL checks if a string looks like a valid Git URL.
func isValidGitURL(url string) bool {
	// Accept common Git URL formats
	// - https://github.com/...
	// - git@github.com:...
	// - ssh://git@...
	// - file:///...

	if len(url) < 5 {
		return false
	}

	validPrefixes := []string{
		"https://",
		"http://",
		"git@",
		"ssh://",
		"git://",
		"file://",
	}

	for _, prefix := range validPrefixes {
		if len(url) >= len(prefix) && url[:len(prefix)] == prefix {
			return true
		}
	}

	return false
}

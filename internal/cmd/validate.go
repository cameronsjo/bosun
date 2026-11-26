package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/cameronsjo/bosun/internal/config"
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
	Long: `Validate bosun configuration and connectivity.

This command performs validation checks:
  1. Environment variables and configuration
  2. Daemon connectivity (if running)
  3. Repository access and credentials
  4. Full dry-run reconciliation (with --full)

Use this before deploying to catch configuration issues early.

The configuration is built the way "bosun reconcile" builds it, from the same
environment variables and bosun.yaml, and the checks "bosun reconcile" makes
before it starts (the age identity for the secrets files, target resolution,
and a well-formed BOSUN_TARGETS) are reported as errors.

The quick check changes nothing. --full is not read-only: one dry run per
target decrypts secrets with your age key and renders into the staging
directory, as "bosun reconcile --dry-run" does. It leaves the rendered tree
there at restricted permissions, may delete a leftover staging tree it cannot
secure, and on a staging directory shared with the daemon replaces the last
staging evidence. It runs no pre-deploy or post-sync hooks, sends no alerts,
and ignores FORCE.

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
	repoURL := config.BosunEnv("REPO_URL")

	if repoURL != "" {
		_, _ = ui.Green.Printf("  * REPO_URL: %s\n", reconcile.SanitizeGitURL(repoURL))
		if err := reconcile.ValidateGitAuthentication(repoURL); err != nil {
			_, _ = ui.Red.Printf("  x Git authentication: %v\n", reconcile.SanitizeGitError(err))
			errors++
		}
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

	// Check webhook secret, resolved exactly as the daemon resolves it (#794).
	webhookSecret, webhookSecretConflict := daemon.ResolveWebhookSecret(os.Getenv)
	switch {
	case webhookSecretConflict:
		_, _ = ui.Red.Println("  x Webhook secret: WEBHOOK_SECRET and GITHUB_WEBHOOK_SECRET are set to different values (webhook triggers will be rejected with 403)")
		errors++
	case webhookSecret != "":
		_, _ = ui.Green.Println("  * Webhook secret: configured")
	default:
		_, _ = ui.Yellow.Println("  ! Webhook secret: not set or the change-me placeholder (webhook triggers will be rejected with 403)")
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
		status, statusErr := client.Status(ctx)
		if statusErr != nil {
			_, _ = ui.Yellow.Printf("    Cannot get daemon status: %v\n", statusErr)
		} else if status.LastError != "" {
			_, _ = ui.Yellow.Printf("    Last error: %s\n", status.LastError)
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
	cfg.RepoURL = config.BosunEnv("REPO_URL")

	// Drift ignore rules are independent of repo config, so validate them
	// even if the repo URL check below fails or short-circuits.
	errors += validateDriftIgnoreConfig()

	if cfg.RepoURL == "" {
		_, _ = ui.Red.Println("  x Repository URL not configured")
		errors++
		return errors
	}
	if err := reconcile.ValidateGitAuthentication(cfg.RepoURL); err != nil {
		_, _ = ui.Red.Printf("  x Git authentication: %v\n", reconcile.SanitizeGitError(err))
		errors++
		return errors
	}

	// Validate URL format
	if !isValidGitURL(cfg.RepoURL) {
		_, _ = ui.Red.Printf("  x Invalid repository URL: %s\n", reconcile.SanitizeGitURL(cfg.RepoURL))
		errors++
	} else {
		_, _ = ui.Green.Printf("  * Repository URL format: valid\n")
	}

	// Everything else comes from the configuration `bosun reconcile` would run
	// with: the same variables and the same project config. A configuration
	// reconcile refuses to start on is reported here instead of at deploy time.
	built, err := buildValidateReconcileConfig()
	if err != nil {
		_, _ = ui.Red.Printf("  x Reconcile configuration: %v\n", err)
		errors++
		return errors
	}

	// The checks `bosun reconcile` makes before it starts: the Age identity for
	// the secrets files, and target resolution (reserved names, nested staging
	// paths and other collisions).
	if err := validateReconcileStartup(built); err != nil {
		_, _ = ui.Red.Printf("  x Reconcile startup check: %v\n", err)
		errors++
	}
	if _, err := built.ResolveTargets(); err != nil {
		_, _ = ui.Red.Printf("  x Targets: %v\n", err)
		errors++
	}

	// Check directories
	if built.RepoDir != "" {
		if _, err := os.Stat(built.RepoDir); err == nil {
			_, _ = ui.Green.Printf("  * Repo directory exists: %s\n", built.RepoDir)
		} else {
			_, _ = ui.Yellow.Printf("  ! Repo directory will be created: %s\n", built.RepoDir)
		}
	}

	return errors
}

// validateDriftIgnoreConfig validates drift_ignore rules loaded from the
// project config file or the BOSUN_DRIFT_IGNORE environment override.
// A total-suppression rule (service "*" and type "*") is reported as an error
// here -- unlike daemon startup, which only warns -- so `bosun validate` never
// passes silently on a rule that mutes all drift detection.
func validateDriftIgnoreConfig() int {
	rules := loadDriftIgnoreRules()
	if len(rules) == 0 {
		return 0
	}

	errors := 0
	warnings, err := reconcile.ValidateDriftIgnoreRules(rules)
	if err != nil {
		_, _ = ui.Red.Printf("  x drift_ignore: %v\n", err)
		errors++
	}
	for _, w := range warnings {
		_, _ = ui.Red.Printf("  x drift_ignore: %s\n", w)
		errors++
	}
	if errors == 0 {
		_, _ = ui.Green.Printf("  * drift_ignore: %d rule(s) valid\n", len(rules))
	}
	return errors
}

// buildValidateReconcileConfig is the reconciler configuration for
// `bosun validate`: the one `bosun reconcile` builds from the same environment
// and project config, with the command-line flags at their zero values.
//
// It then removes what a validation must not run or send. The reconcile
// pipeline runs pre-deploy hooks on a dry run, and the hot reload would pull a
// hook list out of the freshly cloned repository, so both are cleared. The
// caller also attaches no alerter. FORCE is ignored: a dry run that forces
// changes which skip and breaker decisions it previews.
func buildValidateReconcileConfig() (*reconcile.Config, error) {
	cfg, err := buildReconcileConfigWith(reconcileFlagInputs{})
	if err != nil {
		return nil, err
	}
	// `bosun reconcile` logs a warning and carries on with the implicit default
	// target when BOSUN_TARGETS does not parse. A validation that did the same
	// would report success after checking the wrong targets.
	if raw := os.Getenv("BOSUN_TARGETS"); raw != "" {
		if _, _, err := reconcile.ParseTargetsOverride(raw); err != nil {
			return nil, fmt.Errorf("BOSUN_TARGETS is not valid JSON: %w", err)
		}
	}
	cfg.DryRun = true
	cfg.Force = false
	clearValidateSideEffects(cfg)
	return cfg, nil
}

// clearValidateSideEffects removes the pre-deploy hooks and the config reloader
// from a config. ConfigForTarget re-applies a target's own pre-deploy hooks, so
// this runs on each per-target config as well as on the base.
func clearValidateSideEffects(cfg *reconcile.Config) {
	cfg.PreDeployHooks = reconcile.ConfigField[[]reconcile.PreDeployHook]{}
	cfg.ConfigReloader = nil
}

// validateRunner runs one dry-run reconciliation for a config. It takes no
// reconciler options, so a validation cannot attach an alerter (or anything
// else) to the reconciler it runs: the production runner builds one with none,
// and a test substitutes a runner that records the config it received.
type validateRunner func(ctx context.Context, cfg *reconcile.Config) error

// runValidateReconciler is the production validateRunner.
func runValidateReconciler(ctx context.Context, cfg *reconcile.Config) error {
	return reconcile.NewReconciler(cfg).Run(ctx)
}

// runFullDryRun runs the dry-run reconciliation `bosun validate --full` asks
// for, once per target, with the configuration `bosun reconcile` would use.
// Like `reconcile --dry-run` it decrypts secrets and renders into the staging
// directory; it runs no hooks and sends no alerts.
func runFullDryRun() error {
	return runFullDryRunWith(runValidateReconciler)
}

// runFullDryRunWith is runFullDryRun over an injected runner.
func runFullDryRunWith(run validateRunner) error {
	cfg, err := buildValidateReconcileConfig()
	if err != nil {
		return fmt.Errorf("build reconcile configuration: %w", err)
	}
	// The checks `bosun reconcile` makes before it touches Git: the Age identity
	// for the secrets files, then the Git authentication setup (sanitized).
	if err := validateReconcileStartup(cfg); err != nil {
		return fmt.Errorf("reconcile startup check: %w", err)
	}

	targets, err := cfg.ResolveTargets()
	if err != nil {
		return fmt.Errorf("resolve targets: %w", err)
	}

	// As reconcile does: secure any staging tree left by an older binary before
	// a target decrypts secrets into it.
	preflightCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := reconcile.PreflightStagingEvidence(preflightCtx, cfg, targets); err != nil {
		return fmt.Errorf("staging evidence preflight: %w", err)
	}

	var failures []error
	for _, target := range targets {
		targetCfg := cfg.ConfigForTarget(target)
		clearValidateSideEffects(targetCfg)
		targetCfg.DryRun = true
		targetCfg.Force = false

		if err := runFullDryRunTarget(targetCfg, run); err != nil {
			failures = append(failures, fmt.Errorf("target %s: %w", target.Name, err))
		}
	}
	return errors.Join(failures...)
}

// runFullDryRunTarget runs the full validation reconciliation for one config
// against a temporary state copy. Reconcile records simulated attempts and
// successes in its configured state file, so a CLI dry run must isolate those
// writes while preserving the state used for skip and breaker decisions.
func runFullDryRunTarget(cfg *reconcile.Config, run validateRunner) error {
	cfg.DryRun = true
	stateFile, cleanupState, err := prepareStateFileForCLIRun(cfg.StateFile, true)
	if err != nil {
		return fmt.Errorf("prepare state file for full dry run: %w", err)
	}
	defer cleanupState()
	cfg.StateFile = stateFile

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	return run(ctx, cfg)
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

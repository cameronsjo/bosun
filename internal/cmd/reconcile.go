package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/cameronsjo/bosun/internal/alert"
	"github.com/cameronsjo/bosun/internal/config"
	"github.com/cameronsjo/bosun/internal/log"
	"github.com/cameronsjo/bosun/internal/reconcile"
	"github.com/cameronsjo/bosun/internal/telemetry"
	"github.com/cameronsjo/bosun/internal/ui"
)

var (
	reconcileDryRun bool
	reconcileForce  bool
	reconcileLocal  bool
	reconcileRemote string
	reconcileTarget string
)

// reconcileCmd represents the reconcile command.
var reconcileCmd = &cobra.Command{
	Use:   "reconcile",
	Short: "Run GitOps reconciliation workflow",
	Long: `Reconcile runs the GitOps reconciliation workflow:

1. Acquire lock (prevent concurrent runs)
2. Clone/pull repository
3. Decrypt secrets with SOPS
4. Render templates with Chezmoi
5. Create backup of current configs
6. Deploy (native file copy or tar-over-SSH for remote)
7. Docker compose up
8. SIGHUP to agentgateway
9. Release lock

Configuration is loaded from environment variables:
  REPO_URL        - Git repository URL (required)
  REPO_BRANCH     - Git branch to track (default: main)
  BOSUN_GIT_USERNAME - Private HTTPS Git username (set with BOSUN_GIT_TOKEN)
  BOSUN_GIT_TOKEN    - Private HTTPS Git token (set with BOSUN_GIT_USERNAME)
  DEPLOY_TARGET   - Target host for remote deployment (e.g., root@192.168.1.8)
  SECRETS_FILES   - Comma-separated list of SOPS secret files relative to repo

Directories (defaults for container deployment):
  REPO_DIR        - Local repo directory (default: /app/repo)
  STAGING_DIR     - Staging directory (default: /app/staging)
  BACKUP_DIR      - Backup directory (default: /app/backups)
  LOG_DIR         - Log directory (default: /app/logs)
  LOCAL_APPDATA   - Local appdata path (default: /mnt/appdata)
  REMOTE_APPDATA  - Remote appdata path (default: /mnt/user/appdata)`,
	Run: runReconcile,
}

func init() {
	reconcileCmd.Flags().BoolVarP(&reconcileDryRun, "dry-run", "n", false, "Show what would be done without making changes")
	reconcileCmd.Flags().BoolVarP(&reconcileForce, "force", "f", false, "Force deployment even if no changes detected")
	reconcileCmd.Flags().BoolVarP(&reconcileLocal, "local", "l", false, "Force local deployment mode")
	reconcileCmd.Flags().StringVarP(&reconcileRemote, "remote", "r", "", "Target host for remote deployment (e.g., root@192.168.1.8)")
	reconcileCmd.Flags().StringVarP(&reconcileTarget, "target", "t", "", "Reconcile a single named target (from bosun.yaml targets: section)")

	rootCmd.AddCommand(reconcileCmd)
}

// templateIncludeDirForCLI resolves the template include allowlist root for the
// one-shot CLI, matching the daemon's precedence: the project-config value
// unless BOSUN_TEMPLATE_INCLUDE_DIR overrides it. An empty result lets the
// reconciler fall back to the <infraDir>/templates default.
func templateIncludeDirForCLI(projectConfigValue, envValue string) string {
	if envValue != "" {
		return envValue
	}
	return projectConfigValue
}

// ensureStateDirForCLI prepares the parent directory SaveState requires. The
// daemon does this during startup; the one-shot CLI must do the same for every
// resolved target because targets may override StateFile independently.
func ensureStateDirForCLI(stateFile string) error {
	if stateFile == "" {
		return nil
	}

	stateDir := filepath.Dir(stateFile)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return fmt.Errorf("create state directory %q: %w", stateDir, err)
	}

	// MkdirAll succeeds when the directory already exists, even if the CLI
	// cannot write there. Probe the same operation SaveState starts with so an
	// unusable existing directory fails before any deployment side effects.
	probe, err := os.CreateTemp(stateDir, ".bosun-state-probe-*")
	if err != nil {
		return fmt.Errorf("verify state directory %q is writable: %w", stateDir, err)
	}
	probePath := probe.Name()
	if err := probe.Close(); err != nil {
		// Preserve the close failure as the actionable error; removing an open
		// probe is best-effort cleanup and cannot make this directory usable.
		_ = os.Remove(probePath)
		return fmt.Errorf("close state directory probe in %q: %w", stateDir, err)
	}
	if err := os.Remove(probePath); err != nil {
		return fmt.Errorf("remove state directory probe %q: %w", probePath, err)
	}
	return nil
}

// prepareStateFileForCLIRun returns the state file a one-shot reconcile should
// use. Real runs use the configured file after verifying its parent. Dry runs
// use a temporary copy so skip and breaker decisions still see current state,
// while simulated attempt/success writes cannot mutate production state.
func prepareStateFileForCLIRun(stateFile string, dryRun bool) (string, func(), error) {
	return prepareStateFileForCLIRunWithSave(stateFile, dryRun, reconcile.SaveState)
}

// prepareStateFileForCLIRunWithSave exposes the scratch-state write as an
// explicit dependency for fault-injection tests without mutable package state.
func prepareStateFileForCLIRunWithSave(stateFile string, dryRun bool, saveState func(string, *reconcile.DeployState) error) (string, func(), error) {
	if !dryRun {
		if err := ensureStateDirForCLI(stateFile); err != nil {
			return "", func() {}, err
		}
		return stateFile, func() {}, nil
	}

	scratchDir, err := os.MkdirTemp("", "bosun-reconcile-dry-run-state-*")
	if err != nil {
		return "", func() {}, fmt.Errorf("create temporary dry-run state directory: %w", err)
	}
	cleanup := func() {
		if err := os.RemoveAll(scratchDir); err != nil {
			log.Warn().Err(err).Str(log.FieldPath, scratchDir).Msg("Failed to remove temporary dry-run state directory")
		}
	}
	scratchFile := filepath.Join(scratchDir, reconcile.DefaultStateFile)

	if stateFile != "" {
		if _, statErr := os.Stat(stateFile); statErr == nil {
			if err := saveState(scratchFile, reconcile.LoadState(stateFile)); err != nil {
				cleanup()
				return "", func() {}, fmt.Errorf("copy deploy state for dry run: %w", err)
			}
		} else if !os.IsNotExist(statErr) {
			// Match LoadState's fail-open behavior for an unreadable or malformed
			// source: the dry run proceeds from empty scratch state.
			log.Warn().Err(statErr).Str(log.FieldPath, stateFile).Msg("Could not inspect deploy state for dry run, using empty temporary state")
		}
	}

	return scratchFile, cleanup, nil
}

func runReconcile(cmd *cobra.Command, args []string) {
	// Build configuration from environment and flags.
	cfg := reconcile.DefaultConfig()

	// Required: repo URL. BOSUN_REPO_URL takes precedence over legacy REPO_URL.
	cfg.RepoURL = config.BosunEnv("REPO_URL")
	if cfg.RepoURL == "" {
		ui.Fatal("BOSUN_REPO_URL (or legacy REPO_URL) environment variable is required")
		return
	}
	if err := reconcile.ValidateGitAuthentication(cfg.RepoURL); err != nil {
		ui.Fatal("Invalid Git authentication configuration: %v", reconcile.SanitizeGitError(err))
		return
	}

	// Optional settings from environment.
	if branch := config.BosunEnv("REPO_BRANCH"); branch != "" {
		cfg.RepoBranch = branch
	}
	if repoDir := os.Getenv("REPO_DIR"); repoDir != "" {
		cfg.RepoDir = repoDir
	}
	if stagingDir := os.Getenv("STAGING_DIR"); stagingDir != "" {
		cfg.StagingDir = stagingDir
	}
	if backupDir := os.Getenv("BACKUP_DIR"); backupDir != "" {
		cfg.BackupDir = backupDir
	}
	if logDir := os.Getenv("LOG_DIR"); logDir != "" {
		cfg.LogDir = logDir
	}
	if localAppdata := os.Getenv("LOCAL_APPDATA"); localAppdata != "" {
		cfg.LocalAppdataPath = localAppdata
	}
	if remoteAppdata := os.Getenv("REMOTE_APPDATA"); remoteAppdata != "" {
		cfg.RemoteAppdataPath = remoteAppdata
	}

	// Secret files from environment.
	if secretsFiles := os.Getenv("SECRETS_FILES"); secretsFiles != "" {
		cfg.SecretsFiles = strings.Split(secretsFiles, ",")
		for i, f := range cfg.SecretsFiles {
			cfg.SecretsFiles[i] = strings.TrimSpace(f)
		}
	}

	// Target host from environment or flags.
	if target := os.Getenv("DEPLOY_TARGET"); target != "" {
		cfg.TargetHost = target
	}
	if reconcileRemote != "" {
		cfg.TargetHost = reconcileRemote
	}

	// Force local mode if --local flag is set.
	if reconcileLocal {
		cfg.DeployMode = "local"
	}

	// Dry run from environment or flags.
	if os.Getenv("DRY_RUN") == "true" {
		cfg.DryRun = true
	}
	if reconcileDryRun {
		cfg.DryRun = true
	}

	// Force from environment or flags.
	if os.Getenv("FORCE") == "true" {
		cfg.Force = true
	}
	if reconcileForce {
		cfg.Force = true
	}

	// State file from environment.
	if stateDir := os.Getenv("BOSUN_STATE_DIR"); stateDir != "" {
		cfg.StateFile = filepath.Join(stateDir, reconcile.DefaultStateFile)
	}

	// Load post-sync hooks, settle delay, and deploy paths from project config file.
	projectCfg, projectCfgErr := config.Load()
	if errors.Is(projectCfgErr, config.ErrInvalidPostSyncHooks) {
		ui.Fatal("Invalid configuration: %v", projectCfgErr)
		return
	}
	if projectCfgErr == nil {
		config.ApplyInitialHookConfig(projectCfg, cfg)
		cfg.DeployPaths.SetFromFile(projectCfg.DeployPaths())
		cfg.TemplateIncludeDir = projectCfg.TemplateIncludeDir()
	}

	// Wire config reloader so the reconciler can re-read bosun.yaml from the repo.
	cfg.ConfigReloader = config.LoadReloadedConfig

	// Environment variable override for post-sync hooks (JSON, same as daemon).
	if v := os.Getenv("BOSUN_POST_SYNC_HOOKS"); v != "" {
		var hooks []reconcile.PostSyncHook
		if err := json.Unmarshal([]byte(v), &hooks); err != nil {
			log.Warn().Err(err).Msg("Failed to parse BOSUN_POST_SYNC_HOOKS, ignoring")
		} else {
			cfg.PostSyncHooks.SetFromEnv(hooks)
		}
	}

	// Environment variable override for hook settle delay.
	if v := os.Getenv("BOSUN_HOOK_SETTLE_DELAY"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.HookSettleDelay.SetFromEnv(d)
		} else if d, err := time.ParseDuration(v + "s"); err == nil {
			cfg.HookSettleDelay.SetFromEnv(d)
		} else {
			log.Warn().Str("value", v).Msg("Failed to parse BOSUN_HOOK_SETTLE_DELAY, ignoring")
		}
	}

	// Environment variable override for deploy paths (JSON array).
	if v := os.Getenv("BOSUN_DEPLOY_PATHS"); v != "" {
		var paths []string
		if err := json.Unmarshal([]byte(v), &paths); err != nil {
			log.Warn().Err(err).Msg("Failed to parse BOSUN_DEPLOY_PATHS, ignoring")
		} else {
			cfg.DeployPaths.SetFromEnv(paths)
		}
	}

	// Template include allowlist root. Precedence matches the daemon: the
	// project-config value (assigned above) unless BOSUN_TEMPLATE_INCLUDE_DIR
	// overrides it.
	cfg.TemplateIncludeDir = templateIncludeDirForCLI(cfg.TemplateIncludeDir, os.Getenv("BOSUN_TEMPLATE_INCLUDE_DIR"))

	// Set source for state tracking.
	cfg.Source = "cli"

	// Initialize OpenTelemetry tracing (noop if BOSUN_OTEL_ENDPOINT is unset).
	initCtx := context.Background()
	otelShutdown, otelErr := telemetry.Init(initCtx, "bosun", version, os.Getenv("BOSUN_OTEL_ENDPOINT"))
	if otelErr != nil {
		ui.Warning("Failed to initialize OpenTelemetry: %v", otelErr)
	} else {
		defer func() {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			_ = otelShutdown(shutdownCtx)
		}()
	}

	// Create context with cancellation on SIGINT/SIGTERM.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Use sync.Once to ensure cancel() is only called once, preventing race condition
	// when multiple signals arrive or when defer also calls cancel.
	var cancelOnce sync.Once
	safeCancel := func() {
		cancelOnce.Do(func() {
			ui.Warning("Received shutdown signal, cancelling...")
			cancel()
		})
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		safeCancel()
	}()

	// Load targets and operational defaults from project config if available.
	if projectCfgErr == nil {
		if len(cfg.Targets) == 0 {
			if targets := projectCfg.Targets(); len(targets) > 0 {
				cfg.Targets = targets
			}
		}
		// Hydrate base config with project-level operational defaults so
		// ConfigForTarget can inherit them for named targets.
		if len(cfg.CriticalContainers.Value) == 0 && !cfg.CriticalContainers.FromEnv() {
			cfg.CriticalContainers.SetFromFile(projectCfg.CriticalContainers())
		}
		if len(cfg.DeploySyncPaths.Value) == 0 && !cfg.DeploySyncPaths.FromEnv() {
			cfg.DeploySyncPaths.SetFromFile(projectCfg.DeploySyncPaths())
		}
		if len(cfg.DeploySyncExclude.Value) == 0 && !cfg.DeploySyncExclude.FromEnv() {
			cfg.DeploySyncExclude.SetFromFile(projectCfg.DeploySyncExclude())
		}
	}

	// BOSUN_TARGETS env var overrides config file targets.
	if v := os.Getenv("BOSUN_TARGETS"); v != "" {
		applyTargetsOverrideForCLI(cfg, v)
	}

	// Set up alert manager.
	alerter := createAlertManager()

	opts := []reconcile.ReconcilerOption{}
	if alerter != nil {
		opts = append(opts, reconcile.WithAlerter(alerter))
	}

	// Resolve targets and optionally filter by --target flag.
	targets, targetsErr := cfg.ResolveTargets()
	if targetsErr != nil {
		// Fail loud (#391): a multi-target config carrying a reserved `default`
		// name aborts instead of silently dropping the target.
		ui.Fatal("Target resolution failed: %v", targetsErr)
	}
	if err := reconcile.PreflightStagingEvidence(ctx, cfg, targets); err != nil {
		ui.Fatal("Staging evidence preflight failed: %v", err)
	}
	if reconcileTarget != "" {
		var found bool
		for _, t := range targets {
			if strings.EqualFold(t.Name, reconcileTarget) {
				targets = []reconcile.Target{t}
				found = true
				break
			}
		}
		if !found {
			ui.Fatal("Target %q not found in configuration", reconcileTarget)
		}
	}

	// Run reconciliation for each target.
	var hadError bool
	for _, target := range targets {
		targetCfg := cfg.ConfigForTarget(target)
		stateFile, cleanupState, err := prepareStateFileForCLIRun(targetCfg.StateFile, targetCfg.DryRun)
		if err != nil {
			ui.Error("Target %s failed: %v", target.Name, err)
			hadError = true
			continue
		}
		targetCfg.StateFile = stateFile
		r := reconcile.NewReconciler(targetCfg, opts...)

		if !target.IsDefault() {
			ui.Info("Reconciling target: %s", target.Name)
		}

		err = r.Run(ctx)
		cleanupState()
		if err != nil {
			ui.Error("Target %s failed: %v", target.Name, err)
			hadError = true
			continue
		}
	}

	if hadError {
		ui.Fatal("Reconciliation completed with errors")
	}
}

func applyTargetsOverrideForCLI(cfg *reconcile.Config, value string) {
	targets, apply, err := reconcile.ParseTargetsOverride(value)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to parse BOSUN_TARGETS, ignoring")
		return
	}
	if !apply {
		return
	}

	// Validate security-sensitive fields on each target parsed from env.
	// Warn and clear (rather than skip) to mirror the YAML-load semantics in
	// extractTargets — a single bad field must not block the whole target.
	targets = reconcile.ValidateAndSanitizeTargets(targets, func(target, field string, err error) {
		log.Warn().Err(err).
			Str("target", target).
			Str(field, "").
			Msgf("BOSUN_TARGETS: invalid %s — ignoring and inheriting global value", field)
	})
	cfg.Targets = targets
	cfg.TargetsFromEnv = true
}

// createAlertManager creates an alert manager with configured providers.
// Returns nil when no providers are configured.
// BOSUN_-prefixed env vars take precedence over legacy unprefixed vars;
// project config (bosun.yaml) provides a further fallback via config.Load().
func createAlertManager() *alert.Manager {
	mgr := buildAlertManagerRaw()
	if !mgr.HasProviders() {
		return nil
	}
	ui.Info("Alert providers: %v", mgr.ProviderNames())
	return mgr
}

// buildAlertManagerRaw assembles the alert.Manager from config + env.
// Always returns a non-nil manager; callers check HasProviders() to decide
// whether any providers were actually configured.
func buildAlertManagerRaw() *alert.Manager {
	mgr := alert.NewManager()

	// Resolve credentials: config.Load() applies BOSUN_-first precedence for
	// all alert env vars (BOSUN_DISCORD_WEBHOOK_URL > DISCORD_WEBHOOK_URL, etc.).
	var alertCfg config.AlertConfig
	if projectCfg, err := config.Load(); err == nil {
		alertCfg = projectCfg.GetAlertConfig()
	} else {
		// No project config — fall back to raw env-var resolution.
		alertCfg = config.AlertConfigFromEnv()
	}

	// Add Discord provider.
	discord := alert.NewDiscordProvider(alertCfg.DiscordWebhookURL)
	mgr.AddProvider(discord)

	// Add Slack provider.
	slack := alert.NewSlackProvider(alertCfg.SlackWebhookURL)
	mgr.AddProvider(slack)

	// Add SendGrid provider.
	toEmails := filterEmptyStrings(strings.Split(os.Getenv("SENDGRID_TO_EMAILS"), ","))
	if v := os.Getenv("BOSUN_SENDGRID_TO_EMAILS"); v != "" {
		toEmails = filterEmptyStrings(strings.Split(v, ","))
	}
	sendgrid := alert.NewSendGrid(alert.SendGridConfig{
		APIKey:    alertCfg.SendGridAPIKey,
		FromEmail: alertCfg.SendGridFromEmail,
		FromName:  alertCfg.SendGridFromName,
		ToEmails:  toEmails,
	})
	mgr.AddProvider(sendgrid)

	// Add Twilio provider.
	toNumbers := filterEmptyStrings(strings.Split(os.Getenv("TWILIO_TO_NUMBERS"), ","))
	if v := os.Getenv("BOSUN_TWILIO_TO_NUMBERS"); v != "" {
		toNumbers = filterEmptyStrings(strings.Split(v, ","))
	}
	twilio := alert.NewTwilio(alert.TwilioConfig{
		AccountSID: alertCfg.TwilioAccountSID,
		AuthToken:  alertCfg.TwilioAuthToken,
		FromNumber: alertCfg.TwilioFromNumber,
		ToNumbers:  toNumbers,
	})
	mgr.AddProvider(twilio)

	// Add Webhook provider.
	webhook := alert.NewWebhookProvider(alert.WebhookConfig{
		URL:     alertCfg.WebhookURL,
		Headers: alertCfg.WebhookHeaders,
		Method:  alertCfg.WebhookMethod,
	})
	mgr.AddProvider(webhook)

	return mgr
}

// filterEmptyStrings removes empty strings from a slice.
func filterEmptyStrings(ss []string) []string {
	result := make([]string, 0, len(ss))
	for _, s := range ss {
		s = strings.TrimSpace(s)
		if s != "" {
			result = append(result, s)
		}
	}
	return result
}

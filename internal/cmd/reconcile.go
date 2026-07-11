package cmd

import (
	"context"
	"encoding/json"
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

func runReconcile(cmd *cobra.Command, args []string) {
	// Build configuration from environment and flags.
	cfg := reconcile.DefaultConfig()

	// Required: repo URL. BOSUN_REPO_URL takes precedence over legacy REPO_URL.
	cfg.RepoURL = config.BosunEnv("REPO_URL")
	if cfg.RepoURL == "" {
		ui.Fatal("BOSUN_REPO_URL (or legacy REPO_URL) environment variable is required")
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
	if projectCfg, err := config.Load(); err == nil {
		cfg.PostSyncHooks.SetFromFile(projectCfg.PostSyncHooks())
		cfg.HookSettleDelay.SetFromFile(projectCfg.HookSettleDelay())
		cfg.DeployPaths.SetFromFile(projectCfg.DeployPaths())
	}

	// Wire config reloader so the reconciler can re-read bosun.yaml from the repo.
	cfg.ConfigReloader = func(dir string) (*reconcile.ReloadedConfig, error) {
		projectCfg, err := config.LoadFrom(dir)
		if err != nil {
			return nil, err
		}
		hookSettleDelay := projectCfg.HookSettleDelay()
		return &reconcile.ReloadedConfig{
			PostSyncHooks:   projectCfg.PostSyncHooks(),
			HookSettleDelay: &hookSettleDelay,
			DeployPaths:     projectCfg.DeployPaths(),
			DriftIgnore:     projectCfg.DriftIgnore(),
		}, nil
	}

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
	if projectCfg, err := config.Load(); err == nil {
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
		var targets []reconcile.Target
		if err := json.Unmarshal([]byte(v), &targets); err != nil {
			log.Warn().Err(err).Msg("Failed to parse BOSUN_TARGETS, ignoring")
		} else {
			// Validate security-sensitive fields on each target parsed from env.
			// Warn and clear (rather than skip) to mirror the YAML-load semantics
			// in extractTargets — a single bad field must not block the whole target.
			targets = reconcile.ValidateAndSanitizeTargets(targets, func(target, field string, err error) {
				log.Warn().Err(err).
					Str("target", target).
					Str(field, "").
					Msgf("BOSUN_TARGETS: invalid %s — ignoring and inheriting global value", field)
			})
			cfg.Targets = targets
			cfg.TargetsFromEnv = true
		}
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
		r := reconcile.NewReconciler(targetCfg, opts...)

		if !target.IsDefault() {
			ui.Info("Reconciling target: %s", target.Name)
		}

		if err := r.Run(ctx); err != nil {
			ui.Error("Target %s failed: %v", target.Name, err)
			hadError = true
			continue
		}
	}

	if hadError {
		ui.Fatal("Reconciliation completed with errors")
	}
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

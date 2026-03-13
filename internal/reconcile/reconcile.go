package reconcile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/log"
	sentrypkg "github.com/cameronsjo/bosun/internal/sentry"
	"github.com/cameronsjo/bosun/internal/ui"
)

// ReloadedConfig holds the fields that can be reloaded from the repo's bosun.yaml.
type ReloadedConfig struct {
	PostSyncHooks      []PostSyncHook
	HookSettleDelay    time.Duration
	DeployPaths        []string
	CriticalContainers []string
	OnFailure          *bool
	OnSuccess          *bool
	RemoveOrphans      *bool
}

// ConfigReloaderFunc loads project config from a directory path.
// Returns nil ReloadedConfig if no config file is found (not an error).
type ConfigReloaderFunc func(dir string) (*ReloadedConfig, error)

// Config holds the reconciliation configuration.
type Config struct {
	// RepoURL is the git repository URL.
	RepoURL string
	// RepoBranch is the branch to track.
	RepoBranch string
	// RepoDir is the local directory for the cloned repository.
	RepoDir string
	// StagingDir is the directory for rendered templates.
	StagingDir string
	// BackupDir is the directory for configuration backups.
	BackupDir string
	// LogDir is the directory for log files.
	LogDir string
	// LockFile is the path to the reconciliation lock file.
	LockFile string

	// TargetHost is empty for local deployment, or "user@host" for remote.
	TargetHost string
	// LocalAppdataPath is the path to appdata when running locally.
	LocalAppdataPath string
	// RemoteAppdataPath is the path to appdata on the remote host.
	RemoteAppdataPath string

	// DryRun if true, only shows what would be done.
	DryRun bool
	// Force if true, runs deployment even if no changes detected.
	Force bool
	// Source identifies what triggered this reconciliation (e.g., "webhook:github", "poll", "cli").
	Source string

	// SecretsFiles is the list of SOPS-encrypted secret files to decrypt.
	SecretsFiles []string
	// InfraSubDir is the subdirectory within the repo containing infrastructure configs.
	// Use "." for repos where the root is the infrastructure (dedicated infra repos).
	// Use a path like "infrastructure" for repos where infra is nested (e.g., dotfiles).
	InfraSubDir string

	// BackupsToKeep is the number of backups to retain.
	BackupsToKeep int

	// ProjectName is the docker compose project name for consistent container namespacing.
	// All compose operations will use this name, ensuring --remove-orphans works correctly.
	ProjectName string

	// StateFile is the path to the deploy state file that tracks last successful deployment.
	StateFile string

	// HealthCheckTimeout is the maximum time to poll container health after
	// compose up. Zero disables health verification entirely. Default 60s.
	HealthCheckTimeout time.Duration

	// HealthCheckInterval is how often to poll container health during
	// post-deploy verification. Default 5s.
	HealthCheckInterval time.Duration

	// RestartBreakerEnabled controls whether the restart circuit breaker runs
	// during drift checks. Default true.
	RestartBreakerEnabled bool
	// RestartThreshold is the restart count delta that trips the breaker. Default 5.
	RestartThreshold int
	// RestartWindow is the time window for measuring restart velocity. Default 10m.
	RestartWindow time.Duration

	// PostSyncHooks defines container restart actions triggered by file changes.
	PostSyncHooks []PostSyncHook

	// HookSettleDelay is a global pause after deploy but before any post-sync hooks run.
	// Allows filesystem propagation on FUSE mounts (e.g., Unraid's shfs).
	HookSettleDelay time.Duration

	// ContentHashSync if true, compares file content hashes before writing.
	// Skips writes for unchanged files to avoid FUSE handle invalidation.
	ContentHashSync bool

	// RemoveOrphans if true, passes --remove-orphans to docker compose up.
	// Removes containers belonging to services deleted from the compose file.
	// Defaults to true.
	RemoveOrphans bool

	// PostSyncHooksFromEnv is true when BOSUN_POST_SYNC_HOOKS env var is set.
	// When true, repo config reload will not update PostSyncHooks.
	PostSyncHooksFromEnv bool

	// HookSettleDelayFromEnv is true when BOSUN_HOOK_SETTLE_DELAY env var is set.
	// When true, repo config reload will not update HookSettleDelay.
	HookSettleDelayFromEnv bool

	// DeployPaths is an allowlist of glob patterns for deploy-relevant paths.
	// When configured, commits that only touch files outside these patterns skip the pipeline.
	DeployPaths []string

	// DeployPathsFromEnv is true when BOSUN_DEPLOY_PATHS env var is set.
	// When true, repo config reload will not update DeployPaths.
	DeployPathsFromEnv bool

	// CriticalContainers is a list of container names that must be healthy after compose up.
	// When configured, the health gate runs after startup grace period before state save.
	// Empty list (default) skips the health gate entirely.
	CriticalContainers []string

	// CriticalContainersFromEnv is true when BOSUN_CRITICAL_CONTAINERS env var is set.
	// When true, repo config reload will not update CriticalContainers.
	CriticalContainersFromEnv bool

	// HealthGateTimeout is the maximum time to poll critical container health.
	// Default 60s. Configurable via BOSUN_HEALTH_GATE_TIMEOUT.
	HealthGateTimeout time.Duration

	// OnFailure gates failure alert dispatch. When false, no failure alerts are sent.
	// Defaults to true via DefaultConfig(). A bare Config{} leaves this false.
	OnFailure bool

	// OnSuccess gates success and recovery alert dispatch. When false, neither
	// success nor recovery alerts are sent. Defaults to false.
	OnSuccess bool

	// RemoveOrphansFromEnv is true when BOSUN_REMOVE_ORPHANS env var is set.
	// When true, repo config reload will not update RemoveOrphans.
	RemoveOrphansFromEnv bool

	// ComposeUpTimeout is the maximum time allowed for docker compose up.
	// Zero means use DefaultComposeUpTimeout (10 minutes).
	ComposeUpTimeout time.Duration

	// ConfigReloader loads project config from a directory path.
	// Set by daemon/CLI to break the config→reconcile import cycle.
	// When nil, config reload is skipped.
	ConfigReloader ConfigReloaderFunc
}

// DefaultLockFile is the default path for the reconciliation lock file.
const DefaultLockFile = "/var/run/bosun/reconcile.lock"

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		RepoBranch:        "main",
		RepoDir:           "/app/repo",
		StagingDir:        "/app/staging",
		BackupDir:         "/app/backups",
		LogDir:            "/app/logs",
		LockFile:          DefaultLockFile,
		StateFile:         filepath.Join(DefaultStateDir, DefaultStateFile),
		LocalAppdataPath:  "/mnt/appdata",
		RemoteAppdataPath: "/mnt/user/appdata",
		InfraSubDir:        ".",
		BackupsToKeep:      5,
		HealthCheckTimeout:  60 * time.Second,
		HealthCheckInterval:   5 * time.Second,
		RestartBreakerEnabled: true,
		RestartThreshold:      5,
		RestartWindow:         10 * time.Minute,
		OnFailure:             true,
		RemoveOrphans:         true,
		HealthGateTimeout:     60 * time.Second,
	}
}

// AlertSender sends alerts for reconciliation events.
type AlertSender interface {
	SendDeploySuccess(ctx context.Context, commit, target string) error
	SendDeployFailure(ctx context.Context, commit, target, reason string) error
	SendDeployRecovery(ctx context.Context, commit, target string, priorFailures int) error
	SendUnhealthyContainers(ctx context.Context, target string, containers []string) error
	SendRollbackSuccess(ctx context.Context, target, backupName string) error
	SendRollbackFailure(ctx context.Context, target, reason string) error
}

// DockerClientFunc returns a Docker client, or nil if unavailable.
type DockerClientFunc func() *docker.Client

// Reconciler orchestrates the GitOps reconciliation workflow.
type Reconciler struct {
	config           *Config
	git              GitOperations
	sops             SecretsDecryptor
	template         *TemplateOps
	deploy           *DeployOps
	alerter          AlertSender
	dockerClientFn   DockerClientFunc
	lockFile         string
	lockFd           *os.File
	lastBackupPath   string            // Path to the last backup for rollback support
	lastComposeFiles []string          // Compose files from last deploy (for health gate rollback)
	lastCommit       string            // Track commit for alerting
	declaredServices []DeclaredService // Extracted from rendered compose after templating
}

// NewReconciler creates a new Reconciler with the given configuration.
func NewReconciler(cfg *Config, opts ...ReconcilerOption) *Reconciler {
	lockFile := cfg.LockFile
	if lockFile == "" {
		lockFile = DefaultLockFile
	}

	r := &Reconciler{
		config:   cfg,
		git:      NewGitOps(cfg.RepoURL, cfg.RepoBranch, cfg.RepoDir),
		sops:     NewSOPSOps(),
		deploy:   &DeployOps{DryRun: cfg.DryRun, ProjectName: cfg.ProjectName, ContentHashSync: cfg.ContentHashSync, RemoveOrphans: cfg.RemoveOrphans, ComposeUpTimeout: cfg.ComposeUpTimeout},
		lockFile: lockFile,
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// ReconcilerOption is a functional option for configuring the Reconciler.
type ReconcilerOption func(*Reconciler)

// WithGitOperations sets the GitOperations implementation.
func WithGitOperations(git GitOperations) ReconcilerOption {
	return func(r *Reconciler) {
		r.git = git
	}
}

// WithSecretsDecryptor sets the SecretsDecryptor implementation.
func WithSecretsDecryptor(sops SecretsDecryptor) ReconcilerOption {
	return func(r *Reconciler) {
		r.sops = sops
	}
}

// WithDeployOps sets the DeployOps implementation.
func WithDeployOps(deploy *DeployOps) ReconcilerOption {
	return func(r *Reconciler) {
		r.deploy = deploy
	}
}

// WithLockFile sets the lock file path.
func WithLockFile(path string) ReconcilerOption {
	return func(r *Reconciler) {
		r.lockFile = path
	}
}

// WithAlerter sets the alert sender for notifications.
func WithAlerter(alerter AlertSender) ReconcilerOption {
	return func(r *Reconciler) {
		r.alerter = alerter
	}
}

// WithDockerClient sets the Docker client for post-deploy verification.
func WithDockerClient(client *docker.Client) ReconcilerOption {
	return func(r *Reconciler) {
		r.dockerClientFn = func() *docker.Client { return client }
	}
}

// WithDockerClientFunc sets a lazy Docker client provider for post-deploy verification.
func WithDockerClientFunc(fn DockerClientFunc) ReconcilerOption {
	return func(r *Reconciler) {
		r.dockerClientFn = fn
	}
}

// SetRunOptions sets per-run options (source and force) on the reconciler config.
// This is called by the daemon before each Run() to pass trigger context.
func (r *Reconciler) SetRunOptions(source string, force bool) {
	r.config.Source = source
	r.config.Force = force
}

// Run executes the full reconciliation workflow.
func (r *Reconciler) Run(ctx context.Context) error {
	startTime := time.Now()
	logger := log.ComponentCtx(ctx, log.ComponentReconcile)

	logger.Info().
		Str(log.FieldSource, r.config.Source).
		Bool("force", r.config.Force).
		Msg("Reconcile pipeline starting")

	// Reset per-run state to avoid stale data from a previous Run().
	r.declaredServices = nil
	r.lastComposeFiles = nil

	// Acquire lock to prevent concurrent runs.
	// Lock failures are transient (another reconciliation is running) and lack state context,
	// so they are logged as warnings without sending alerts.
	if err := r.acquireLock(); err != nil {
		logger.Warn().
			Err(err).
			Msg("Failed to acquire reconcile lock, another reconciliation may be in progress")
		return fmt.Errorf("failed to acquire lock (another reconciliation may be in progress): %w", err)
	}
	defer r.releaseLock()

	ui.Header("=== Starting reconciliation ===")

	// Step 1: Sync repository.
	spanCtx, finishSpan := sentrypkg.StartSpan(ctx, "reconcile.git_sync", "Git repository sync")
	changed, before, after, err := r.syncRepo(spanCtx)
	finishSpan(err)
	if err != nil {
		// Use the post-sync commit (may be empty on sync failure) to avoid reporting a stale SHA.
		r.lastCommit = after

		// Load state before alerting so throttle state is available.
		state := LoadState(r.config.StateFile)
		state.LastAttemptedCommit, state.AttemptCount = nextAttemptState(state.LastAttemptedCommit, after, state.AttemptCount)
		if saveErr := SaveState(r.config.StateFile, state); saveErr != nil {
			logger.Error().Err(saveErr).Str(log.FieldPath, r.config.StateFile).Msg("Failed to save attempt tracking state for git sync failure")
		}
		r.sendThrottledFailureAlert(ctx, state, fmt.Sprintf("failed to sync repository: %v", err))
		return fmt.Errorf("failed to sync repository: %w", err)
	}

	// Track commit for alerting.
	r.lastCommit = after

	// Step 2: Reload project config from repo (picks up hook changes).
	r.reloadProjectConfig()

	// Load persistent deploy state to determine if pipeline should run.
	state := LoadState(r.config.StateFile)

	// State-based skip logic: compare last *deployed* commit, not last *fetched* commit.
	if shouldSkipDeploy(state.LastDeployedCommit, after, r.config.Force) {
		ui.Info("=== Already deployed commit %s, skipping ===", after[:MinLen(after, 8)])
		return nil
	}

	// Path-aware skip: if deploy_paths is configured, check if any changed files
	// match the allowlist. If not, record the commit as deployed and skip.
	if len(r.config.DeployPaths) > 0 && changed && !r.config.Force {
		changedFiles, err := r.git.DiffFiles(ctx, before, after)
		if err != nil {
			logger.Warn().Err(err).Msg("Cannot diff for deploy_paths check, proceeding with full deploy")
		} else if !matchAnyPath(changedFiles, r.config.DeployPaths) {
			logger.Info().
				Strs("changed_files", changedFiles).
				Strs("deploy_paths", r.config.DeployPaths).
				Msg("No deploy-relevant files changed, skipping reconciliation")
			ui.Info("=== No deploy-relevant changes (%d files), skipping ===", len(changedFiles))

			// Record commit as deployed to avoid re-evaluation on next poll.
			state.LastDeployedCommit = after
			state.DeployedAt = time.Now()
			state.Source = r.config.Source
			if err := SaveState(r.config.StateFile, state); err != nil {
				logger.Error().Err(err).Msg("Failed to save state after path-aware skip")
			}
			return nil
		}
	}

	// Circuit breaker: stop retrying after MaxAttempts consecutive failures on the same commit.
	if shouldTriggerCircuitBreaker(state.LastAttemptedCommit, after, state.AttemptCount, MaxAttempts, r.config.Force) {
		logger.Error().
			Str("commit", after).
			Int("attempts", state.AttemptCount).
			Msg("Circuit breaker: too many consecutive failures on same commit, skipping (use --force to override)")
		ui.Error("Circuit breaker: %d consecutive failures on commit %s (use --force to retry)",
			state.AttemptCount, after[:MinLen(after, 8)])
		r.sendThrottledFailureAlert(ctx, state,
			fmt.Sprintf("circuit breaker: %d consecutive failures on commit %s", state.AttemptCount, after))
		return fmt.Errorf("circuit breaker: %d consecutive failures on commit %s", state.AttemptCount, after)
	}

	// Track this attempt before executing the pipeline.
	state.LastAttemptedCommit, state.AttemptCount = nextAttemptState(state.LastAttemptedCommit, after, state.AttemptCount)
	if err := SaveState(r.config.StateFile, state); err != nil {
		logger.Error().Err(err).Str(log.FieldPath, r.config.StateFile).Msg("Failed to save attempt tracking state")
	}

	if changed {
		ui.Success("Updated: %s -> %s", before, after)
	} else if r.config.Force {
		ui.Info("Force mode enabled, proceeding with deployment")
	} else {
		ui.Info("State mismatch detected (deployed=%s, current=%s), re-running pipeline",
			state.LastDeployedCommit[:MinLen(state.LastDeployedCommit, 8)], after[:MinLen(after, 8)])
	}

	// Step 2: Decrypt secrets.
	spanCtx, finishSpan = sentrypkg.StartSpan(ctx, "reconcile.decrypt", "SOPS secret decryption")
	secrets, err := r.decryptSecrets(spanCtx)
	finishSpan(err)
	if err != nil {
		r.sendThrottledFailureAlert(ctx, state, "failed to decrypt secrets")
		return fmt.Errorf("failed to decrypt secrets: %w", err)
	}

	// Step 3: Render templates.
	spanCtx, finishSpan = sentrypkg.StartSpan(ctx, "reconcile.template", "Template rendering")
	if err := r.renderTemplates(spanCtx, secrets); err != nil {
		finishSpan(err)
		r.sendThrottledFailureAlert(ctx, state, "failed to render templates")
		return fmt.Errorf("failed to render templates: %w", err)
	}
	finishSpan(nil)

	// Extract declared state from rendered compose files.
	stagingUnraid := filepath.Join(r.config.StagingDir, "unraid")
	declared, err := ExtractDeclaredState(stagingUnraid)
	if err != nil {
		logger.Warn().Err(err).Msg("Failed to extract declared state from rendered compose")
	} else {
		r.declaredServices = declared
		logger.Info().
			Int("declared_services", len(declared)).
			Msg("Extracted declared state from rendered compose")
	}

	// Step 4: Create backup (unless dry run).
	if !r.config.DryRun {
		spanCtx, finishSpan = sentrypkg.StartSpan(ctx, "reconcile.backup", "Configuration backup")
		if err := r.createBackup(spanCtx, secrets); err != nil {
			finishSpan(err)
			ui.Warning("Backup partially failed: %v", err)
		} else {
			finishSpan(nil)
		}
	}

	// Step 5: Deploy.
	spanCtx, finishSpan = sentrypkg.StartSpan(ctx, "reconcile.deploy", "Deployment")
	deployResult, err := r.doDeploy(spanCtx, secrets)
	if err != nil {
		finishSpan(err)
		r.sendThrottledFailureAlert(ctx, state, err.Error())
		return fmt.Errorf("deployment failed: %w", err)
	}
	finishSpan(nil)

	// Step 6: Cleanup staging directory after successful deployment.
	if err := r.cleanupStaging(); err != nil {
		ui.Warning("Failed to cleanup staging directory: %v", err)
	}

	// Step 7: Critical container health gate (if configured).
	if err := r.runHealthGate(ctx, state); err != nil {
		return fmt.Errorf("health gate failed: %w", err)
	}

	// Send recovery alert if this success follows previous failures.
	if state.AttemptCount > 1 {
		r.sendRecoveryAlert(ctx, state.AttemptCount-1)
	}

	// Capture previous commit before updating state (needed for post-sync hooks).
	previousCommit := state.LastDeployedCommit

	// Record successful deployment in state file.
	state.LastDeployedCommit = after
	state.DeployedAt = time.Now()
	state.DeployCount++
	state.Source = r.config.Source
	state.AttemptCount = 0
	state.LastAlertedAttempt = 0
	state.DeclaredServices = r.declaredServices
	if err := SaveState(r.config.StateFile, state); err != nil {
		logger.Error().
			Err(err).
			Str(log.FieldPath, r.config.StateFile).
			Msg("Failed to save deploy state after successful deployment")
	}

	// Execute post-sync hooks if any files changed and hooks are configured.
	if r.dockerClientFn != nil && !r.config.DryRun && len(r.config.PostSyncHooks) > 0 {
		r.executePostSyncHooks(ctx, previousCommit, after, deployResult)
	}

	// Post-deploy health verification: poll container health.
	if r.dockerClientFn != nil && !r.config.DryRun && len(r.declaredServices) > 0 {
		if client := r.dockerClientFn(); client != nil {
			if healthErr := r.verifyPostDeploy(ctx, state, client); healthErr != nil {
				// Health verification failed — treat as a deploy failure.
				r.sendThrottledFailureAlert(ctx, state, healthErr.Error())
				return fmt.Errorf("post-deploy health verification failed: %w", healthErr)
			}
		}
	}

	duration := time.Since(startTime)

	logger.Info().
		Int64(log.FieldDurationMS, duration.Milliseconds()).
		Str(log.FieldCommit, after).
		Msg("Reconcile pipeline completed")

	ui.Success("=== Reconciliation completed in %s ===", duration.Round(time.Second))

	// Send success alert.
	r.sendSuccessAlert(ctx)

	return nil
}

// MinLen returns min(len(s), n) for safe string slicing.
func MinLen(s string, n int) int {
	if len(s) < n {
		return len(s)
	}
	return n
}

// sendSuccessAlert sends a deployment success notification.
// Gated on config.OnSuccess: when false, no success alerts are sent.
func (r *Reconciler) sendSuccessAlert(ctx context.Context) {
	if r.alerter == nil {
		return
	}

	if !r.config.OnSuccess {
		return
	}

	target := r.config.TargetHost
	if target == "" {
		target = "local"
	}

	if err := r.alerter.SendDeploySuccess(ctx, r.lastCommit, target); err != nil {
		logger := log.ComponentCtx(ctx, log.ComponentReconcile)
		logger.Warn().
			Err(err).
			Str(log.FieldOperation, "alert_success").
			Str(log.FieldTarget, target).
			Msg("Failed to send success alert")
	}
}

// sendThrottledFailureAlert sends a failure alert if the throttle schedule allows it.
// Updates LastAlertedAttempt in the state and persists it.
// Gated on config.OnFailure: when false, no failure alerts are sent.
func (r *Reconciler) sendThrottledFailureAlert(ctx context.Context, state *DeployState, reason string) {
	if r.alerter == nil {
		return
	}

	if !r.config.OnFailure {
		return
	}

	if !ShouldAlert(state.AttemptCount, state.LastAlertedAttempt) {
		return
	}

	target := r.config.TargetHost
	if target == "" {
		target = "local"
	}

	if err := r.alerter.SendDeployFailure(ctx, r.lastCommit, target, reason); err != nil {
		logger := log.ComponentCtx(ctx, log.ComponentReconcile)
		logger.Warn().
			Err(err).
			Str(log.FieldOperation, "alert_failure").
			Str(log.FieldTarget, target).
			Msg("Failed to send failure alert")
		return
	}

	state.LastAlertedAttempt = state.AttemptCount
	if err := SaveState(r.config.StateFile, state); err != nil {
		log.Warn().Err(err).Msg("Failed to persist alert throttle state")
	}
}

// sendUnhealthyAlert sends a warning notification for unhealthy containers found post-deploy.
func (r *Reconciler) sendUnhealthyAlert(ctx context.Context, containers []string) {
	if r.alerter == nil {
		return
	}

	target := r.config.TargetHost
	if target == "" {
		target = "local"
	}

	if err := r.alerter.SendUnhealthyContainers(ctx, target, containers); err != nil {
		logger := log.ComponentCtx(ctx, log.ComponentReconcile)
		logger.Warn().
			Err(err).
			Str(log.FieldOperation, "alert_unhealthy").
			Str(log.FieldTarget, target).
			Int("container_count", len(containers)).
			Msg("Failed to send unhealthy containers alert")
	}
}

// sendRecoveryAlert sends a notification when deployment succeeds after failures.
// Gated on config.OnSuccess: recovery is a success-side alert.
func (r *Reconciler) sendRecoveryAlert(ctx context.Context, priorFailures int) {
	if r.alerter == nil {
		return
	}

	if !r.config.OnSuccess {
		return
	}

	target := r.config.TargetHost
	if target == "" {
		target = "local"
	}

	if err := r.alerter.SendDeployRecovery(ctx, r.lastCommit, target, priorFailures); err != nil {
		logger := log.ComponentCtx(ctx, log.ComponentReconcile)
		logger.Warn().
			Err(err).
			Str(log.FieldOperation, "alert_recovery").
			Str(log.FieldTarget, target).
			Int("prior_failures", priorFailures).
			Msg("Failed to send recovery alert")
	}
}

// verifyPostDeploy polls container health after deployment. Returns an error
// if health verification times out with unhealthy containers.
// When HealthCheckTimeout is zero, verification is disabled (returns nil).
func (r *Reconciler) verifyPostDeploy(ctx context.Context, state *DeployState, client *docker.Client) error {
	logger := log.ComponentCtx(ctx, log.ComponentReconcile)

	if r.config.HealthCheckTimeout <= 0 {
		logger.Debug().Msg("Post-deploy health verification disabled (timeout=0)")
		return nil
	}

	ui.Info("Verifying container health (timeout: %s, interval: %s)...",
		r.config.HealthCheckTimeout, r.config.HealthCheckInterval)

	result, err := pollContainerHealth(
		ctx, client, r.declaredServices, r.config.ProjectName,
		r.config.HealthCheckTimeout, r.config.HealthCheckInterval,
	)

	// Record health verdict in state file.
	now := time.Now()
	state.HealthVerifiedAt = now
	if result != nil {
		state.HealthVerificationPassed = result.Passed
	}

	if err != nil {
		logger.Error().
			Err(err).
			Strs("unhealthy", result.Unhealthy).
			Int("iterations", result.Iterations).
			Int64(log.FieldDurationMS, result.Duration.Milliseconds()).
			Msg("Post-deploy health verification failed")
		ui.Error("Health verification failed after %s: %s", result.Duration.Round(time.Second), err)

		// Also run a drift check to populate state.DriftItems for consistency.
		actual, collectErr := CollectActualState(ctx, client, r.config.ProjectName)
		if collectErr == nil {
			report := CompareDrift(r.declaredServices, actual)
			state.DriftCheckedAt = report.CheckedAt
			state.DriftItems = report.Items
		}

		if saveErr := SaveState(r.config.StateFile, state); saveErr != nil {
			logger.Warn().Err(saveErr).Msg("Failed to save state after health verification failure")
		}

		return err
	}

	logger.Info().
		Int("declared_services", len(r.declaredServices)).
		Int("iterations", result.Iterations).
		Int64(log.FieldDurationMS, result.Duration.Milliseconds()).
		Msg("Post-deploy health verification passed")
	ui.Success("Health verification passed: all %d declared services healthy (%s)",
		len(r.declaredServices), result.Duration.Round(time.Second))

	// Run drift check for state consistency.
	actual, collectErr := CollectActualState(ctx, client, r.config.ProjectName)
	if collectErr == nil {
		report := CompareDrift(r.declaredServices, actual)
		state.DriftCheckedAt = report.CheckedAt
		state.DriftItems = report.Items
	}

	if saveErr := SaveState(r.config.StateFile, state); saveErr != nil {
		logger.Warn().Err(saveErr).Msg("Failed to save state after health verification")
	}

	return nil
}

// executePostSyncHooks detects changed files and restarts matching containers via configured hooks.
// When deployResult is non-nil and has written files, those are used for matching instead of git diff.
// This ensures hooks only fire for files actually written to disk (content-hash sync).
func (r *Reconciler) executePostSyncHooks(ctx context.Context, previousCommit, currentCommit string, deployResult *DeployResult) {
	logger := log.ComponentCtx(ctx, log.ComponentReconcile)

	if previousCommit == "" {
		logger.Debug().Msg("No previous commit for post-sync hooks (first deploy), skipping")
		return
	}

	// Prefer written-files from content-hash sync over git diff.
	var changedFiles []string
	diffFailed := false
	if deployResult != nil && len(deployResult.WrittenFiles) > 0 {
		changedFiles = deployResult.WrittenFiles
		logger.Debug().Int("files", len(changedFiles)).Msg("Using written-files list for post-sync hooks")
	} else {
		var err error
		changedFiles, err = r.git.DiffFiles(ctx, previousCommit, currentCommit)
		if err != nil {
			// DiffFiles fails on shallow clones where the previous commit is no longer
			// reachable. Rather than silently skipping hooks, fall back to evaluating
			// all hooks unconditionally — a false positive restart is better than a
			// stale config on a FUSE mount. See GitHub #55.
			logger.Warn().Err(err).Msg("Failed to diff commits for post-sync hooks, will evaluate all hooks")
			diffFailed = true
		}
	}

	if len(changedFiles) == 0 && !diffFailed {
		return
	}

	// When diff fails (shallow clone), fire all hooks unconditionally.
	// A false-positive restart is safer than stale configs on FUSE mounts.
	var matched []PostSyncHook
	if diffFailed {
		matched = dedupeHooksByContainer(r.config.PostSyncHooks)
		logger.Info().Int("hooks", len(matched)).Msg("Diff unavailable, firing all configured hooks")
	} else {
		matched = EvaluatePostSyncHooks(changedFiles, r.config.PostSyncHooks)
	}
	if len(matched) == 0 {
		return
	}

	client := r.dockerClientFn()
	if client == nil {
		logger.Warn().Msg("Docker client unavailable for post-sync hooks")
		return
	}

	ui.Info("Executing %d post-sync hook(s)...", len(matched))
	if err := ExecutePostSyncHooks(ctx, client, matched, r.config.HookSettleDelay); err != nil {
		logger.Warn().Err(err).Msg("Some post-sync hooks failed")
		ui.Warning("Post-sync hook errors: %v", err)
	}
}

// reloadProjectConfig re-reads bosun.yaml from the repo working directory
// and updates PostSyncHooks and HookSettleDelay if the file has changed.
// Fields overridden by environment variables are not updated.
func (r *Reconciler) reloadProjectConfig() {
	if r.config.ConfigReloader == nil {
		return
	}

	logger := log.Component(log.ComponentReconcile) // No ctx available in this method.

	reloaded, err := r.config.ConfigReloader(r.config.RepoDir)
	if err != nil {
		logger.Warn().Err(err).Msg("Failed to reload project config from repo, keeping existing config")
		return
	}
	if reloaded == nil {
		return
	}

	// If no field has any value from the repo, there's nothing to reload.
	if len(reloaded.PostSyncHooks) == 0 && reloaded.HookSettleDelay == 0 && len(reloaded.DeployPaths) == 0 && len(reloaded.CriticalContainers) == 0 && reloaded.OnFailure == nil && reloaded.OnSuccess == nil && reloaded.RemoveOrphans == nil {
		return
	}

	changed := false

	if !r.config.PostSyncHooksFromEnv && len(reloaded.PostSyncHooks) > 0 {
		r.config.PostSyncHooks = reloaded.PostSyncHooks
		changed = true
	}

	if !r.config.HookSettleDelayFromEnv && reloaded.HookSettleDelay > 0 {
		r.config.HookSettleDelay = reloaded.HookSettleDelay
		changed = true
	}

	if !r.config.DeployPathsFromEnv && len(reloaded.DeployPaths) > 0 {
		r.config.DeployPaths = reloaded.DeployPaths
		changed = true
	}

	if !r.config.CriticalContainersFromEnv && len(reloaded.CriticalContainers) > 0 {
		r.config.CriticalContainers = reloaded.CriticalContainers
		changed = true
	}

	if reloaded.OnFailure != nil {
		r.config.OnFailure = *reloaded.OnFailure
		changed = true
	}

	if reloaded.OnSuccess != nil {
		r.config.OnSuccess = *reloaded.OnSuccess
		changed = true
	}

	if !r.config.RemoveOrphansFromEnv && reloaded.RemoveOrphans != nil {
		r.config.RemoveOrphans = *reloaded.RemoveOrphans
		r.deploy.RemoveOrphans = *reloaded.RemoveOrphans
		changed = true
	}

	if changed {
		logger.Info().
			Int("hooks", len(r.config.PostSyncHooks)).
			Dur("settle_delay", r.config.HookSettleDelay).
			Int("deploy_paths", len(r.config.DeployPaths)).
			Bool("on_failure", r.config.OnFailure).
			Bool("on_success", r.config.OnSuccess).
			Bool("remove_orphans", r.config.RemoveOrphans).
			Msg("Reloaded project config from repo")
	}
}

// cleanupStaging removes the staging directory after successful deployment.
func (r *Reconciler) cleanupStaging() error {
	if r.config.DryRun {
		return nil
	}

	if r.config.StagingDir == "" {
		return nil
	}

	if err := os.RemoveAll(r.config.StagingDir); err != nil {
		return fmt.Errorf("failed to remove staging directory: %w", err)
	}

	ui.Info("Cleaned up staging directory")
	return nil
}

// runHealthGate checks critical container health after compose up.
// Skipped when: DryRun, remote deploy (TargetHost set), no Docker client,
// or empty CriticalContainers list.
// On failure: triggers rollback and sends a throttled failure alert.
func (r *Reconciler) runHealthGate(ctx context.Context, state *DeployState) error {
	containers := r.config.CriticalContainers
	if len(containers) == 0 {
		return nil
	}

	logger := log.ComponentCtx(ctx, log.ComponentReconcile)

	if r.config.DryRun {
		logger.Debug().Msg("Health gate skipped. Reason: dry run")
		return nil
	}

	if r.config.TargetHost != "" {
		logger.Warn().
			Strs("containers", containers).
			Msg("Health gate skipped for remote deploy. Docker API is local-only")
		return nil
	}

	if r.dockerClientFn == nil {
		logger.Warn().Msg("Health gate skipped. Reason: no Docker client")
		return nil
	}

	client := r.dockerClientFn()
	if client == nil {
		logger.Warn().Msg("Health gate skipped. Reason: Docker client unavailable")
		return nil
	}

	timeout := r.config.HealthGateTimeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	err := CheckCriticalContainerHealth(ctx, client, containers, timeout)
	if err != nil {
		logger.Error().
			Err(err).
			Strs("containers", containers).
			Msg("Critical container health gate failed. Triggering rollback")

		// Trigger rollback via existing mechanism.
		if r.lastBackupPath != "" && len(r.lastComposeFiles) > 0 {
			rollbackErr := r.deploy.ComposeUpMultipleWithRollback(ctx, r.lastComposeFiles, r.lastBackupPath)
			if rollbackErr != nil {
				logger.Error().Err(rollbackErr).Msg("Rollback after health gate failure also failed")
			}
		}

		r.sendThrottledFailureAlert(ctx, state, err.Error())
		return err
	}

	return nil
}

// syncRepo syncs the git repository.
func (r *Reconciler) syncRepo(ctx context.Context) (bool, string, string, error) {
	start := time.Now()
	logger := log.ComponentCtx(ctx, log.ComponentGit)

	logger.Info().
		Str(log.FieldOperation, "sync").
		Msg("Syncing repository")

	ui.Info("Syncing repository...")

	changed, before, after, err := r.git.Sync(ctx)
	if err != nil {
		logger.Error().
			Err(err).
			Str(log.FieldOperation, "sync").
			Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
			Msg("Repository sync failed")
		return changed, before, after, err
	}

	logger.Info().
		Str(log.FieldOperation, "sync").
		Bool("changed", changed).
		Str("commit_before", before).
		Str("commit_after", after).
		Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
		Msg("Repository sync completed")

	return changed, before, after, nil
}

// decryptSecrets decrypts SOPS secret files.
func (r *Reconciler) decryptSecrets(ctx context.Context) (map[string]any, error) {
	start := time.Now()
	logger := log.ComponentCtx(ctx, log.ComponentSOPS)

	logger.Info().
		Str(log.FieldOperation, "decrypt").
		Int("file_count", len(r.config.SecretsFiles)).
		Msg("Decrypting secrets")

	ui.Info("Decrypting secrets...")

	if len(r.config.SecretsFiles) == 0 {
		return make(map[string]any), nil
	}

	// Build full paths to secret files.
	var files []string
	for _, f := range r.config.SecretsFiles {
		path := filepath.Join(r.config.RepoDir, f)
		if _, err := os.Stat(path); err != nil {
			logger.Error().
				Str(log.FieldPath, path).
				Msg("Secrets file not found")
			return nil, fmt.Errorf("secrets file not found: %s", path)
		}
		files = append(files, path)
	}

	secrets, err := r.sops.DecryptFiles(ctx, files)
	if err != nil {
		logger.Error().
			Err(err).
			Msg("Failed to decrypt secrets")
		return nil, err
	}

	logger.Info().
		Int("file_count", len(files)).
		Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
		Msg("Secrets decrypted successfully")

	ui.Success("Secrets decrypted successfully")
	return secrets, nil
}

// renderTemplates renders all templates to the staging directory.
func (r *Reconciler) renderTemplates(ctx context.Context, secrets map[string]any) error {
	start := time.Now()
	logger := log.ComponentCtx(ctx, log.ComponentTemplate)

	logger.Info().
		Str(log.FieldOperation, "render").
		Str("staging_dir", r.config.StagingDir).
		Msg("Rendering templates")

	ui.Info("Rendering templates...")

	// Clear staging directory.
	if err := os.RemoveAll(r.config.StagingDir); err != nil {
		return fmt.Errorf("failed to clear staging directory: %w", err)
	}
	if err := os.MkdirAll(r.config.StagingDir, 0755); err != nil {
		return fmt.Errorf("failed to create staging directory: %w", err)
	}

	// Create template ops with secrets data.
	r.template = NewTemplateOps(secrets)

	infraDir := filepath.Join(r.config.RepoDir, r.config.InfraSubDir)
	if err := r.template.RenderDirectory(ctx, infraDir, r.config.StagingDir, "unraid"); err != nil {
		logger.Error().
			Err(err).
			Msg("Failed to render templates")
		return err
	}

	logger.Info().
		Str("staging_dir", r.config.StagingDir).
		Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
		Msg("Templates rendered successfully")

	ui.Success("Templates rendered to %s", r.config.StagingDir)
	return nil
}

// createBackup creates a backup of current configs.
func (r *Reconciler) createBackup(ctx context.Context, secrets map[string]any) error {
	ui.Info("Creating backup...")

	var backupName string
	var err error

	if r.isLocalMode() {
		paths := []string{
			filepath.Join(r.config.LocalAppdataPath, "traefik"),
			filepath.Join(r.config.LocalAppdataPath, "authelia", "configuration.yml"),
			filepath.Join(r.config.LocalAppdataPath, "agentgateway", "config.yaml"),
			filepath.Join(r.config.LocalAppdataPath, "gatus", "config.yaml"),
		}
		backupName, err = r.deploy.Backup(ctx, r.config.BackupDir, paths)
	} else {
		host := r.getTargetHost(secrets)
		remotePaths := []string{
			filepath.Join(r.config.RemoteAppdataPath, "traefik"),
			filepath.Join(r.config.RemoteAppdataPath, "authelia", "configuration.yml"),
			filepath.Join(r.config.RemoteAppdataPath, "agentgateway", "config.yaml"),
			filepath.Join(r.config.RemoteAppdataPath, "gatus", "config.yaml"),
		}
		backupName, err = r.deploy.BackupRemote(ctx, host, r.config.BackupDir, remotePaths)
	}

	if err != nil {
		return err
	}

	// Store backup path for potential rollback
	r.lastBackupPath = filepath.Join(r.config.BackupDir, backupName)

	// Cleanup old backups.
	if err := r.deploy.CleanupBackups(r.config.BackupDir, r.config.BackupsToKeep); err != nil {
		ui.Warning("Failed to cleanup old backups: %v", err)
	}

	ui.Success("Backup saved: %s", backupName)
	return nil
}

// doDeploy performs the actual deployment.
// Returns a DeployResult with written files (local mode) or nil (remote mode).
func (r *Reconciler) doDeploy(ctx context.Context, secrets map[string]any) (*DeployResult, error) {
	if r.isLocalMode() {
		return r.deployLocal(ctx)
	}
	return nil, r.deployRemote(ctx, secrets)
}

// isLocalMode returns true if running in local mode (appdata mounted).
func (r *Reconciler) isLocalMode() bool {
	if r.config.TargetHost != "" {
		return false
	}
	_, err := os.Stat(r.config.LocalAppdataPath)
	return err == nil
}

// getTargetHost returns the target host for remote deployment.
func (r *Reconciler) getTargetHost(secrets map[string]any) string {
	return resolveTargetHost(r.config.TargetHost, secrets)
}

// deployLocal performs local deployment via mounted paths.
// Returns a DeployResult with files actually written to disk.
func (r *Reconciler) deployLocal(ctx context.Context) (*DeployResult, error) {
	ui.Info("Using local deployment mode")
	if r.config.DryRun {
		ui.Warning("DRY RUN MODE - no changes will be made")
	}

	result := &DeployResult{}
	stagingUnraid := filepath.Join(r.config.StagingDir, "unraid")
	appdata := r.config.LocalAppdataPath

	// Sync Traefik configs.
	ui.Info("  Syncing Traefik configs...")
	if err := r.deploy.DeployLocal(ctx, filepath.Join(stagingUnraid, "appdata", "traefik"), filepath.Join(appdata, "traefik"), result); err != nil {
		return nil, err
	}

	// Sync agentgateway config.
	ui.Info("  Syncing agentgateway config...")
	if err := r.deploy.DeployLocalFile(ctx, filepath.Join(stagingUnraid, "appdata", "agentgateway", "config.yaml"), filepath.Join(appdata, "agentgateway", "config.yaml"), result); err != nil {
		return nil, err
	}

	// Sync authelia config.
	ui.Info("  Syncing authelia config...")
	if err := r.deploy.DeployLocalFile(ctx, filepath.Join(stagingUnraid, "appdata", "authelia", "configuration.yml"), filepath.Join(appdata, "authelia", "configuration.yml"), result); err != nil {
		return nil, err
	}

	// Sync gatus config.
	ui.Info("  Syncing gatus config...")
	if err := r.deploy.DeployLocalFile(ctx, filepath.Join(stagingUnraid, "appdata", "gatus", "config.yaml"), filepath.Join(appdata, "gatus", "config.yaml"), result); err != nil {
		return nil, err
	}

	// Sync tailscale-gateway config.
	ui.Info("  Syncing tailscale-gateway config...")
	_ = os.MkdirAll(filepath.Join(appdata, "tailscale-gateway"), 0755)
	if err := r.deploy.DeployLocalFile(ctx, filepath.Join(stagingUnraid, "appdata", "tailscale-gateway", "serve.json"), filepath.Join(appdata, "tailscale-gateway", "serve.json"), result); err != nil {
		ui.Warning("tailscale-gateway sync failed: %v", err)
	}

	// Sync compose files.
	ui.Info("  Syncing compose files...")
	_ = os.MkdirAll(filepath.Join(appdata, "compose"), 0755)
	if err := r.deploy.DeployLocal(ctx, filepath.Join(stagingUnraid, "compose"), filepath.Join(appdata, "compose"), result); err != nil {
		return nil, err
	}

	// Reload services with rollback support.
	if !r.config.DryRun {
		ui.Info("  Reloading services...")
		composeDir := filepath.Join(appdata, "compose")
		composeFiles, err := filepath.Glob(filepath.Join(composeDir, "*.yml"))
		if err != nil {
			return nil, fmt.Errorf("failed to glob compose files: %w", err)
		}
		if len(composeFiles) == 0 {
			ui.Warning("No compose files found in %s", composeDir)
		} else {
			r.lastComposeFiles = composeFiles
			if err := r.deploy.ComposeUpMultipleWithRollback(ctx, composeFiles, r.lastBackupPath); err != nil {
				// Unhealthy containers are warnings, not failures.
				if errors.Is(err, ErrComposeUnhealthy) {
					ui.Warning("Some containers are unhealthy: %v", err)
				} else if errors.Is(err, ErrRollbackFailed) {
					return nil, fmt.Errorf("CRITICAL: service reload and rollback both failed: %w", err)
				} else if errors.Is(err, ErrRollbackSucceeded) {
					return nil, fmt.Errorf("service reload failed but rollback succeeded: %w", err)
				} else {
					return nil, fmt.Errorf("service reload failed: %w", err)
				}
			}
		}
		if err := r.deploy.SignalContainer(ctx, "agentgateway", "SIGHUP"); err != nil {
			ui.Warning("Could not reload agentgateway: %v", err)
		}
	}

	ui.Success("Deployment complete!")
	return result, nil
}

// deployRemote performs remote deployment via SSH.
func (r *Reconciler) deployRemote(ctx context.Context, secrets map[string]any) error {
	ui.Info("Using remote deployment mode (SSH)")
	if r.config.DryRun {
		ui.Warning("DRY RUN MODE - no changes will be made")
	}

	host := r.getTargetHost(secrets)
	if host == "" {
		return fmt.Errorf("no target host specified and could not find unraid_ip in secrets")
	}

	stagingUnraid := filepath.Join(r.config.StagingDir, "unraid")
	appdata := r.config.RemoteAppdataPath

	// Sync Traefik configs.
	ui.Info("  Syncing Traefik configs...")
	if err := r.deploy.DeployRemote(ctx, filepath.Join(stagingUnraid, "appdata", "traefik"), host, filepath.Join(appdata, "traefik")); err != nil {
		return err
	}

	// Sync agentgateway config.
	ui.Info("  Syncing agentgateway config...")
	if err := r.deploy.DeployRemoteFile(ctx, filepath.Join(stagingUnraid, "appdata", "agentgateway", "config.yaml"), host, filepath.Join(appdata, "agentgateway", "config.yaml")); err != nil {
		return err
	}

	// Sync authelia config.
	ui.Info("  Syncing authelia config...")
	if err := r.deploy.DeployRemoteFile(ctx, filepath.Join(stagingUnraid, "appdata", "authelia", "configuration.yml"), host, filepath.Join(appdata, "authelia", "configuration.yml")); err != nil {
		return err
	}

	// Sync gatus config.
	ui.Info("  Syncing gatus config...")
	if err := r.deploy.DeployRemoteFile(ctx, filepath.Join(stagingUnraid, "appdata", "gatus", "config.yaml"), host, filepath.Join(appdata, "gatus", "config.yaml")); err != nil {
		return err
	}

	// Sync tailscale-gateway config.
	ui.Info("  Syncing tailscale-gateway config...")
	_ = r.deploy.EnsureRemoteDir(ctx, host, filepath.Join(appdata, "tailscale-gateway"))
	if err := r.deploy.DeployRemoteFile(ctx, filepath.Join(stagingUnraid, "appdata", "tailscale-gateway", "serve.json"), host, filepath.Join(appdata, "tailscale-gateway", "serve.json")); err != nil {
		ui.Warning("tailscale-gateway sync failed: %v", err)
	}

	// Sync compose files.
	ui.Info("  Syncing compose files...")
	_ = r.deploy.EnsureRemoteDir(ctx, host, filepath.Join(appdata, "compose"))
	if err := r.deploy.DeployRemote(ctx, filepath.Join(stagingUnraid, "compose"), host, filepath.Join(appdata, "compose")); err != nil {
		return err
	}

	// Sync to Compose Manager.
	ui.Info("  Syncing core compose to Compose Manager...")
	composeManagerDir := "/boot/config/plugins/compose.manager/projects/core"
	_ = r.deploy.EnsureRemoteDir(ctx, host, composeManagerDir)
	if err := r.deploy.DeployRemoteFile(ctx, filepath.Join(stagingUnraid, "compose", "core.yml"), host, filepath.Join(composeManagerDir, "docker-compose.yml")); err != nil {
		ui.Warning("Compose Manager sync failed: %v", err)
	}

	// Reload services.
	if !r.config.DryRun {
		ui.Info("  Reloading services...")
		if err := r.deploy.ComposeUpRemote(ctx, host, composeManagerDir); err != nil {
			ui.Warning("Could not recreate core stack: %v", err)
		}
		if err := r.deploy.SignalContainerRemote(ctx, host, "agentgateway", "SIGHUP"); err != nil {
			ui.Warning("Could not reload agentgateway: %v", err)
		}
	}

	ui.Success("Deployment complete!")
	return nil
}

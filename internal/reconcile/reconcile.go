package reconcile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/log"
	sentrypkg "github.com/cameronsjo/bosun/internal/sentry"
	"github.com/cameronsjo/bosun/internal/telemetry"
	"github.com/cameronsjo/bosun/internal/ui"
)

// ReloadedConfig holds the fields that can be reloaded from the repo's bosun.yaml.
type ReloadedConfig struct {
	PostSyncHooks      []PostSyncHook
	HookSettleDelay    time.Duration
	DeployPaths        []string
	DeploySyncPaths    []string
	DeploySyncExclude  []string
	CriticalContainers []string
	DriftIgnore        []DriftIgnoreRule
	OnFailure          *bool
	OnSuccess          *bool
	RemoveOrphans      *bool
	// Targets is the reloaded target list from the repo's bosun.yaml.
	// When non-nil, the daemon uses these targets for the next reconciliation cycle.
	Targets []Target
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

	// TargetName identifies the deployment target (e.g., "unraid", "pi", "default").
	// Set by ConfigForTarget; used in alert messages and log context.
	TargetName string
	// TargetHost is empty for local deployment, or "user@host" for remote.
	TargetHost string
	// LocalAppdataPath is the path to appdata when running locally.
	LocalAppdataPath string
	// RemoteAppdataPath is the path to appdata on the remote host.
	RemoteAppdataPath string

	// Targets is the list of deployment targets. When empty, an implicit default
	// target is synthesized from the flat config fields above (TargetHost,
	// LocalAppdataPath, RemoteAppdataPath, ProjectName).
	Targets []Target

	// DryRun if true, only shows what would be done.
	DryRun bool
	// Force if true, runs deployment even if no changes detected.
	Force bool
	// Source identifies what triggered this reconciliation (e.g., "webhook:github", "poll", "cli").
	Source string

	// SecretsFiles is the list of SOPS-encrypted secret files to decrypt.
	SecretsFiles []string
	// SecretsScope is the key prefix for per-target secrets scoping.
	// When set, keys under "targets.<scope>.*" in the decrypted secrets
	// override same-named top-level keys for this target's template rendering.
	SecretsScope string
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
	PostSyncHooks ConfigField[[]PostSyncHook]

	// HookSettleDelay is a global pause after deploy but before any post-sync hooks run.
	// Allows filesystem propagation on FUSE mounts (e.g., Unraid's shfs).
	HookSettleDelay ConfigField[time.Duration]

	// ContentHashSync if true, compares file content hashes before writing.
	// Skips writes for unchanged files to avoid FUSE handle invalidation.
	ContentHashSync bool

	// RemoveOrphans if true, passes --remove-orphans to docker compose up.
	// Removes containers belonging to services deleted from the compose file.
	// Defaults to true.
	RemoveOrphans ConfigField[bool]

	// DeployPaths is an allowlist of glob patterns for deploy-relevant paths.
	// When configured, commits that only touch files outside these patterns skip the pipeline.
	DeployPaths ConfigField[[]string]

	// DeploySyncPaths is an allowlist of glob patterns for deploy sync targets.
	// When non-empty, only staging directory entries matching these patterns are deployed.
	DeploySyncPaths ConfigField[[]string]

	// DeploySyncExclude is a blocklist of glob patterns for deploy sync targets.
	// Matching entries are excluded from deployment. Exclude wins over include.
	DeploySyncExclude ConfigField[[]string]

	// CriticalContainers is a list of container names that must be healthy after compose up.
	// When configured, the health gate runs after startup grace period before state save.
	// Empty list (default) skips the health gate entirely.
	CriticalContainers ConfigField[[]string]

	// DriftIgnore is a list of rules for suppressing known drift noise.
	DriftIgnore ConfigField[[]DriftIgnoreRule]

	// HealthGateTimeout is the maximum time to poll critical container health.
	// Default 60s. Configurable via BOSUN_HEALTH_GATE_TIMEOUT.
	HealthGateTimeout time.Duration

	// OnFailure gates failure alert dispatch. When false, no failure alerts are sent.
	// Defaults to true via DefaultConfig(). A bare Config{} leaves this false.
	OnFailure bool

	// OnSuccess gates success and recovery alert dispatch. When false, neither
	// success nor recovery alerts are sent. Defaults to false.
	OnSuccess bool

	// ComposeUpTimeout is the maximum time allowed for docker compose up.
	// Zero means use DefaultComposeUpTimeout (10 minutes).
	ComposeUpTimeout time.Duration

	// ConfigReloader loads project config from a directory path.
	// Set by daemon/CLI to break the config→reconcile import cycle.
	// When nil, config reload is skipped.
	ConfigReloader ConfigReloaderFunc
}


// AlertSender sends alerts for reconciliation events.
type AlertSender interface {
	SendDeploySuccess(ctx context.Context, commit, target string, services []string, duration time.Duration) error
	SendDeployFailure(ctx context.Context, commit, target, reason string, services []string, duration time.Duration) error
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
	runStartTime     time.Time         // Pipeline start time for duration reporting
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
		deploy:   &DeployOps{DryRun: cfg.DryRun, ProjectName: cfg.ProjectName, ContentHashSync: cfg.ContentHashSync, RemoveOrphans: cfg.RemoveOrphans.Value, ComposeUpTimeout: cfg.ComposeUpTimeout},
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
func (r *Reconciler) Run(ctx context.Context) (runErr error) {
	startTime := time.Now()

	// Root OTel span for the entire reconciliation pipeline.
	ctx, rootSpan := telemetry.Tracer("reconcile").Start(ctx, "reconcile",
		trace.WithAttributes(
			telemetry.StringAttr("source", r.config.Source),
			telemetry.BoolAttr("force", r.config.Force),
		),
	)
	defer func() {
		if runErr != nil {
			telemetry.SpanError(rootSpan, runErr)
		} else {
			telemetry.SpanOK(rootSpan)
		}
		rootSpan.End()
	}()

	// Bridge correlation IDs into the span.
	if reconcileID := log.ReconcileIDFromContext(ctx); reconcileID != "" {
		rootSpan.SetAttributes(telemetry.StringAttr("reconcile_id", reconcileID))
	}

	logger := log.ComponentCtx(ctx, log.ComponentReconcile)

	logger.Info().
		Str(log.FieldSource, r.config.Source).
		Bool("force", r.config.Force).
		Msg("Reconcile pipeline starting")

	// Reset per-run state to avoid stale data from a previous Run().
	r.declaredServices = nil
	r.lastBackupPath = ""
	r.lastComposeFiles = nil
	r.runStartTime = startTime

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
	spanCtx, otelGitSpan := telemetry.Tracer("reconcile").Start(spanCtx, "reconcile.git_sync")
	changed, before, after, err := r.syncRepo(spanCtx)
	finishSpan(err)
	if err != nil {
		telemetry.SpanError(otelGitSpan, err)
	} else {
		telemetry.SpanOK(otelGitSpan)
	}
	otelGitSpan.End()
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
	// Also re-run if NeedsRedeploy is set from a previous partial failure.
	if shouldSkipDeploy(state.LastDeployedCommit, after, r.config.Force, state.NeedsRedeploy) {
		// Commit hash matches, but verify that all declared services are actually
		// running. A service added to the compose file in this commit may have never
		// started (e.g., the first deploy of the commit was interrupted, or the
		// container was removed after the fact). If any service is missing, proceed
		// with the full pipeline instead of skipping.
		skipConfirmed := true
		if r.dockerClientFn != nil {
			if client := r.dockerClientFn(); client != nil {
				actual, collectErr := CollectActualState(ctx, client, r.config.ProjectName)
				if collectErr != nil {
					logger.Warn().Err(collectErr).Msg("Could not verify service health for skip check, proceeding with deploy")
					skipConfirmed = false
				} else if hasMissingDeclaredServices(state.DeclaredServices, actual) {
					logger.Info().
						Int("declared_services", len(state.DeclaredServices)).
						Msg("Commit already deployed but declared services are missing containers, re-running pipeline")
					ui.Info("=== Commit %s deployed but services missing, re-running pipeline ===", after[:MinLen(after, 8)])
					skipConfirmed = false
				}
			}
		}
		if skipConfirmed {
			ui.Info("=== Already deployed commit %s, skipping ===", after[:MinLen(after, 8)])
			return nil
		}
	}

	if state.NeedsRedeploy {
		ui.Info("Previous deploy partially failed (configs synced, compose up failed), retrying")
	}

	// Path-aware skip: if deploy_paths is configured, check if any changed files
	// match the allowlist. If not, record the commit as deployed and skip.
	// Use state.LastDeployedCommit (not the pull's commit_before) so that files
	// from a previously failed deploy are still considered deploy-relevant.
	// When there is no prior successful deploy, skip this check entirely —
	// everything is deploy-relevant on first run.
	if len(r.config.DeployPaths.Value) > 0 && changed && !r.config.Force && state.LastDeployedCommit != "" {
		changedFiles, err := r.git.DiffFiles(ctx, state.LastDeployedCommit, after)
		if err != nil {
			logger.Warn().Err(err).Msg("Cannot diff for deploy_paths check, proceeding with full deploy")
		} else if !matchAnyPath(changedFiles, r.config.DeployPaths.Value) {
			logger.Info().
				Strs("changed_files", changedFiles).
				Strs("deploy_paths", r.config.DeployPaths.Value).
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
	spanCtx, otelDecryptSpan := telemetry.Tracer("reconcile").Start(spanCtx, "reconcile.decrypt")
	secrets, err := r.decryptSecrets(spanCtx)
	finishSpan(err)
	if err != nil {
		telemetry.SpanError(otelDecryptSpan, err)
		otelDecryptSpan.End()
		r.sendThrottledFailureAlert(ctx, state, "failed to decrypt secrets")
		return fmt.Errorf("failed to decrypt secrets: %w", err)
	}
	telemetry.SpanOK(otelDecryptSpan)
	otelDecryptSpan.End()

	// Step 2b: Apply per-target secrets scoping.
	if r.config.SecretsScope != "" {
		secrets = MergeTargetSecrets(secrets, r.config.SecretsScope)
	}

	// Step 3: Render templates.
	spanCtx, finishSpan = sentrypkg.StartSpan(ctx, "reconcile.template", "Template rendering")
	spanCtx, otelTemplateSpan := telemetry.Tracer("reconcile").Start(spanCtx, "reconcile.template")
	if err := r.renderTemplates(spanCtx, secrets); err != nil {
		finishSpan(err)
		telemetry.SpanError(otelTemplateSpan, err)
		otelTemplateSpan.End()
		r.sendThrottledFailureAlert(ctx, state, "failed to render templates")
		return fmt.Errorf("failed to render templates: %w", err)
	}
	finishSpan(nil)
	telemetry.SpanOK(otelTemplateSpan)
	otelTemplateSpan.End()

	// Extract declared state from rendered compose files.
	stagingSubDir := filepath.Join(r.config.StagingDir, r.config.InfraSubDir)
	declared, err := ExtractDeclaredState(stagingSubDir)
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
		spanCtx, otelBackupSpan := telemetry.Tracer("reconcile").Start(spanCtx, "reconcile.backup")
		if err := r.createBackup(spanCtx, secrets); err != nil {
			finishSpan(err)
			telemetry.SpanError(otelBackupSpan, err)
			otelBackupSpan.End()
			ui.Warning("Backup partially failed: %v", err)
		} else {
			finishSpan(nil)
			telemetry.SpanOK(otelBackupSpan)
			otelBackupSpan.End()
		}
	}

	// Step 5: Deploy.
	// Mark NeedsRedeploy before deploy starts so partial failures
	// (configs synced but compose up failed) trigger a retry next cycle.
	state.NeedsRedeploy = true
	if err := SaveState(r.config.StateFile, state); err != nil {
		logger.Error().Err(err).Str(log.FieldPath, r.config.StateFile).Msg("Failed to save pre-deploy state")
		return fmt.Errorf("failed to persist pre-deploy redeploy marker: %w", err)
	}

	spanCtx, finishSpan = sentrypkg.StartSpan(ctx, "reconcile.deploy", "Deployment")
	spanCtx, otelDeploySpan := telemetry.Tracer("reconcile").Start(spanCtx, "reconcile.deploy")
	deployResult, err := r.doDeploy(spanCtx, secrets)
	if err != nil {
		finishSpan(err)
		telemetry.SpanError(otelDeploySpan, err)
		otelDeploySpan.End()
		r.sendThrottledFailureAlert(ctx, state, err.Error())
		return fmt.Errorf("deployment failed: %w", err)
	}
	finishSpan(nil)
	telemetry.SpanOK(otelDeploySpan)
	otelDeploySpan.End()

	// Step 6: Cleanup staging directory after successful deployment.
	if err := r.cleanupStaging(); err != nil {
		ui.Warning("Failed to cleanup staging directory: %v", err)
	}

	// Step 7: Critical container health gate (if configured).
	_, otelHealthGateSpan := telemetry.Tracer("reconcile").Start(ctx, "reconcile.health_gate")
	if err := r.runHealthGate(ctx, state); err != nil {
		telemetry.SpanError(otelHealthGateSpan, err)
		otelHealthGateSpan.End()
		return fmt.Errorf("health gate failed: %w", err)
	}
	telemetry.SpanOK(otelHealthGateSpan)
	otelHealthGateSpan.End()

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
	state.NeedsRedeploy = false
	state.DeclaredServices = r.declaredServices
	if err := SaveState(r.config.StateFile, state); err != nil {
		logger.Error().
			Err(err).
			Str(log.FieldPath, r.config.StateFile).
			Msg("Failed to save deploy state after successful deployment")
	}

	// Execute post-sync hooks if any files changed and hooks are configured.
	if r.dockerClientFn != nil && !r.config.DryRun && len(r.config.PostSyncHooks.Value) > 0 {
		_, otelHooksSpan := telemetry.Tracer("reconcile").Start(ctx, "reconcile.post_sync_hooks",
			trace.WithAttributes(telemetry.IntAttr("hook_count", len(r.config.PostSyncHooks.Value))),
		)
		r.executePostSyncHooks(ctx, previousCommit, after, deployResult)
		telemetry.SpanOK(otelHooksSpan)
		otelHooksSpan.End()
	}

	// Post-deploy health verification: poll container health.
	if r.dockerClientFn != nil && !r.config.DryRun && len(r.declaredServices) > 0 {
		if client := r.dockerClientFn(); client != nil {
			_, otelDriftSpan := telemetry.Tracer("reconcile").Start(ctx, "reconcile.drift_check")
			if healthErr := r.verifyPostDeploy(ctx, state, client); healthErr != nil {
				telemetry.SpanError(otelDriftSpan, healthErr)
				otelDriftSpan.End()
				// Health verification failed — treat as a deploy failure.
				r.sendThrottledFailureAlert(ctx, state, healthErr.Error())
				return fmt.Errorf("post-deploy health verification failed: %w", healthErr)
			}
			telemetry.SpanOK(otelDriftSpan)
			otelDriftSpan.End()
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
	remoteMode := deployResult == nil && r.config.TargetHost != ""
	if remoteMode {
		// Remote deploys return nil DeployResult (no file-level tracking).
		// Fire all hooks unconditionally — a false-positive restart is better
		// than stale configs on a FUSE mount. See GitHub #197.
		logger.Info().Msg("Remote deploy: firing all post-sync hooks (no file-level tracking available)")
	} else if deployResult != nil && len(deployResult.WrittenFiles) > 0 {
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

	if len(changedFiles) == 0 && !diffFailed && !remoteMode {
		return
	}

	// When diff fails (shallow clone) or deploy is remote, fire all hooks unconditionally.
	// A false-positive restart is safer than stale configs on FUSE mounts.
	var matched []PostSyncHook
	if diffFailed || remoteMode {
		matched = dedupeHooksByContainer(r.config.PostSyncHooks.Value)
		if diffFailed {
			logger.Info().Int("hooks", len(matched)).Msg("Diff unavailable, firing all configured hooks")
		}
	} else {
		matched = EvaluatePostSyncHooks(changedFiles, r.config.PostSyncHooks.Value)
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
	if err := ExecutePostSyncHooks(ctx, client, matched, r.config.HookSettleDelay.Value); err != nil {
		logger.Warn().Err(err).Msg("Some post-sync hooks failed")
		ui.Warning("Post-sync hook errors: %v", err)
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
// Skipped when: DryRun, remote deploy (!isLocalMode), no Docker client,
// or empty CriticalContainers list.
// On failure: triggers rollback and sends a throttled failure alert.
func (r *Reconciler) runHealthGate(ctx context.Context, state *DeployState) error {
	containers := r.config.CriticalContainers.Value
	if len(containers) == 0 {
		return nil
	}

	logger := log.ComponentCtx(ctx, log.ComponentReconcile)

	if r.config.DryRun {
		logger.Debug().Msg("Health gate skipped. Reason: dry run")
		return nil
	}

	if !r.isLocalMode() {
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

// MergeTargetSecrets creates a copy of the secrets map with per-target
// overrides applied. Keys under "targets.<scope>.*" override same-named
// top-level keys. The original map is not modified.
//
// Example: if secrets contains {"db_password": "shared", "targets": {"unraid": {"db_password": "secret1"}}}
// and scope is "unraid", the result has {"db_password": "secret1", "targets": {...}}.
func MergeTargetSecrets(secrets map[string]any, scope string) map[string]any {
	if scope == "" || secrets == nil {
		return secrets
	}

	targetsRaw, ok := secrets["targets"]
	if !ok {
		return secrets
	}

	targetsMap, ok := targetsRaw.(map[string]any)
	if !ok {
		return secrets
	}

	scopedRaw, ok := targetsMap[scope]
	if !ok {
		return secrets
	}

	scopedMap, ok := scopedRaw.(map[string]any)
	if !ok {
		return secrets
	}

	// Shallow copy the base map, then overlay scoped keys.
	merged := make(map[string]any, len(secrets))
	for k, v := range secrets {
		merged[k] = v
	}

	logger := log.Component("secrets")
	for k, v := range scopedMap {
		logger.Debug().
			Str("key", k).
			Str("target_scope", scope).
			Msg("Per-target secret overriding shared key")
		merged[k] = v
	}

	return merged
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
	if err := r.template.RenderDirectory(ctx, infraDir, r.config.StagingDir, r.config.InfraSubDir); err != nil {
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

	// Discover targets from staging to know what to back up.
	stagingSubDir := filepath.Join(r.config.StagingDir, r.config.InfraSubDir)
	targets, err := discoverDeployTargets(stagingSubDir, r.config.DeploySyncPaths.Value, r.config.DeploySyncExclude.Value)
	if err != nil {
		return fmt.Errorf("discover deploy targets for backup: %w", err)
	}

	var backupName string

	if r.isLocalMode() {
		paths := backupPathsFromTargets(targets, r.config.LocalAppdataPath)
		backupName, err = r.deploy.Backup(ctx, r.config.BackupDir, paths)
	} else {
		host := r.getTargetHost(secrets)
		paths := backupPathsFromTargets(targets, r.config.RemoteAppdataPath)
		backupName, err = r.deploy.BackupRemote(ctx, host, r.config.BackupDir, paths)
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

// backupPathsFromTargets derives backup paths from discovered deploy targets.
// All targets including compose are backed up so per-file rollback has files to restore from.
func backupPathsFromTargets(targets []DeployTarget, appdataBase string) []string {
	var paths []string
	for _, t := range targets {
		paths = append(paths, filepath.Join(appdataBase, t.TargetPath))
	}
	return paths
}

// ErrAppdataInaccessible is returned when LocalAppdataPath is configured but
// the path cannot be accessed (e.g. mount is down).
var ErrAppdataInaccessible = errors.New("local appdata path is configured but inaccessible")

// doDeploy performs the actual deployment.
// Returns a DeployResult with written files (local mode) or nil (remote mode).
func (r *Reconciler) doDeploy(ctx context.Context, secrets map[string]any) (*DeployResult, error) {
	local, err := r.resolveDeployMode(ctx)
	if err != nil {
		return nil, err
	}
	if local {
		return r.deployLocal(ctx)
	}
	return nil, r.deployRemote(ctx, secrets)
}

// resolveDeployMode determines whether to use local or remote deployment.
// Returns (true, nil) for local mode, (false, nil) for remote mode, or an
// error when the configuration is invalid (e.g. appdata path configured but
// inaccessible and no remote host to fall back to).
func (r *Reconciler) resolveDeployMode(ctx context.Context) (bool, error) {
	logger := log.ComponentCtx(ctx, log.ComponentReconcile)

	// Explicit remote host always wins.
	if r.config.TargetHost != "" {
		logger.Debug().
			Str("target_host", r.config.TargetHost).
			Msg("Remote target host configured, using remote deploy mode")
		return false, nil
	}

	// No local path configured — fall through to remote mode.
	if r.config.LocalAppdataPath == "" {
		logger.Debug().Msg("No local appdata path configured, using remote deploy mode")
		return false, nil
	}

	// Local path configured — verify it's accessible.
	_, err := os.Stat(r.config.LocalAppdataPath)
	if err == nil {
		logger.Debug().
			Str(log.FieldPath, r.config.LocalAppdataPath).
			Msg("Local appdata path accessible, using local deploy mode")
		return true, nil
	}

	// Path configured but inaccessible (mount down, permissions, etc.).
	logger.Error().
		Str(log.FieldPath, r.config.LocalAppdataPath).
		Err(err).
		Msg("Local appdata path configured but inaccessible. Check that the mount is up")
	return false, fmt.Errorf("%w: %s: %w", ErrAppdataInaccessible, r.config.LocalAppdataPath, err)
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
	stagingSubDir := filepath.Join(r.config.StagingDir, r.config.InfraSubDir)
	appdata := r.config.LocalAppdataPath

	targets, err := discoverDeployTargets(stagingSubDir, r.config.DeploySyncPaths.Value, r.config.DeploySyncExclude.Value)
	if err != nil {
		return nil, fmt.Errorf("discover deploy targets: %w", err)
	}

	// Sync discovered targets (excluding compose, which has special handling).
	// After each DeployLocal call, prefix the newly written file paths with
	// the target's RelPath so hook globs (which use staging-relative paths
	// like "appdata/authelia/**") can match correctly.
	for _, t := range targets {
		if t.RelPath == "compose" {
			continue
		}
		src := filepath.Join(stagingSubDir, t.RelPath)
		dst := filepath.Join(appdata, t.TargetPath)
		ui.Info("  Syncing %s...", t.RelPath)
		snapshot := len(result.WrittenFiles)
		if t.IsDir {
			if err := r.deploy.DeployLocal(ctx, src, dst, result); err != nil {
				return nil, err
			}
			result.PrefixLatest(snapshot, t.RelPath)
		} else {
			_ = os.MkdirAll(filepath.Dir(dst), 0755)
			if err := r.deploy.DeployLocalFile(ctx, src, dst, result); err != nil {
				return nil, err
			}
			// For file targets, t.RelPath includes the filename (e.g., "appdata/foo.yml").
			// DeployLocalFile records filepath.Base, so prefix with the directory only.
			result.PrefixLatest(snapshot, filepath.Dir(t.RelPath))
		}
	}

	// Sync compose files (special handling: glob .yml files for ComposeUpMultipleWithRollback).
	composeStaging := filepath.Join(stagingSubDir, "compose")
	composeTarget := filepath.Join(appdata, "compose")
	if hasTarget(targets, "compose") {
		ui.Info("  Syncing compose files...")
		_ = os.MkdirAll(composeTarget, 0755)
		snapshot := len(result.WrittenFiles)
		if err := r.deploy.DeployLocal(ctx, composeStaging, composeTarget, result); err != nil {
			return nil, err
		}
		result.PrefixLatest(snapshot, "compose")
	}

	// Reload services with per-file isolated compose up and rollback.
	if !r.config.DryRun {
		ui.Info("  Reloading services...")
		composeFiles, err := filepath.Glob(filepath.Join(composeTarget, "*.yml"))
		if err != nil {
			return nil, fmt.Errorf("failed to glob compose files: %w", err)
		}
		if len(composeFiles) == 0 {
			ui.Warning("No compose files found in %s", composeTarget)
		} else {
			r.lastComposeFiles = composeFiles
			summary, composeErr := r.deploy.ComposeUpIsolated(ctx, composeFiles, r.lastBackupPath)
			if composeErr != nil {
				// All files failed — fatal.
				return nil, fmt.Errorf("CRITICAL: all compose files failed to deploy: %w", composeErr)
			}
			if summary.Failed > 0 {
				// Partial failure — log warnings for each failed file, continue.
				for _, res := range summary.Results {
					if !res.Success {
						if res.RolledBack {
							ui.Warning("Compose file %s failed, rolled back: %v", filepath.Base(res.File), res.Err)
						} else {
							ui.Warning("Compose file %s failed: %v", filepath.Base(res.File), res.Err)
						}
					}
				}
				ui.Warning("Partial deploy: %d/%d files succeeded, %d failed (%d rolled back)",
					summary.Succeeded, len(composeFiles), summary.Failed, summary.RolledBack)
			}
			// Check for unhealthy warnings.
			for _, res := range summary.Results {
				if res.Success && res.Err != nil && errors.Is(res.Err, ErrComposeUnhealthy) {
					ui.Warning("Some containers are unhealthy in %s: %v", filepath.Base(res.File), res.Err)
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

// hasTarget returns true if the target list contains a target with the given RelPath.
func hasTarget(targets []DeployTarget, relPath string) bool {
	for _, t := range targets {
		if t.RelPath == relPath {
			return true
		}
	}
	return false
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

	stagingSubDir := filepath.Join(r.config.StagingDir, r.config.InfraSubDir)
	appdata := r.config.RemoteAppdataPath

	targets, err := discoverDeployTargets(stagingSubDir, r.config.DeploySyncPaths.Value, r.config.DeploySyncExclude.Value)
	if err != nil {
		return fmt.Errorf("discover deploy targets: %w", err)
	}

	// Sync discovered targets (excluding compose, which has special handling).
	for _, t := range targets {
		if t.RelPath == "compose" {
			continue
		}
		src := filepath.Join(stagingSubDir, t.RelPath)
		dst := filepath.Join(appdata, t.TargetPath)
		ui.Info("  Syncing %s...", t.RelPath)
		if t.IsDir {
			if err := r.deploy.DeployRemote(ctx, src, host, dst); err != nil {
				return err
			}
		} else {
			_ = r.deploy.EnsureRemoteDir(ctx, host, filepath.Dir(dst))
			if err := r.deploy.DeployRemoteFile(ctx, src, host, dst); err != nil {
				return err
			}
		}
	}

	// Sync compose files (special handling for remote compose up).
	if hasTarget(targets, "compose") {
		ui.Info("  Syncing compose files...")
		_ = r.deploy.EnsureRemoteDir(ctx, host, filepath.Join(appdata, "compose"))
		if err := r.deploy.DeployRemote(ctx, filepath.Join(stagingSubDir, "compose"), host, filepath.Join(appdata, "compose")); err != nil {
			return err
		}
	}

	// Sync to Compose Manager (Unraid-specific, remote-only) only when compose is a deploy target.
	composeManagerDir := "/boot/config/plugins/compose.manager/projects/core"
	if hasTarget(targets, "compose") {
		ui.Info("  Syncing core compose to Compose Manager...")
		_ = r.deploy.EnsureRemoteDir(ctx, host, composeManagerDir)
		if err := r.deploy.DeployRemoteFile(ctx, filepath.Join(stagingSubDir, "compose", "core.yml"), host, filepath.Join(composeManagerDir, "docker-compose.yml")); err != nil {
			ui.Warning("Compose Manager sync failed: %v", err)
		}
	}

	// Reload services.
	if !r.config.DryRun {
		ui.Info("  Reloading services...")
		if err := r.deploy.ComposeUpRemote(ctx, host, composeManagerDir); err != nil {
			return fmt.Errorf("remote compose up failed: %w", err)
		}
		if err := r.deploy.SignalContainerRemote(ctx, host, "agentgateway", "SIGHUP"); err != nil {
			ui.Warning("Could not reload agentgateway: %v", err)
		}
	}

	ui.Success("Deployment complete!")
	return nil
}

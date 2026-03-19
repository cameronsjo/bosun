package reconcile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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

// DefaultTargetName is the name used for the implicit single-target backwards-compat target.
const DefaultTargetName = "default"

// Target describes a single deployment target (a server/host to deploy to).
// Each target has its own host, appdata paths, project name, state file,
// staging directory, secrets scope, and operational overrides.
type Target struct {
	// Name identifies this target (e.g., "unraid", "pi"). Used in file paths and logs.
	Name string
	// TargetHost is empty for local deployment, or "user@host" for remote.
	TargetHost string
	// LocalAppdataPath is the path to appdata when running locally.
	LocalAppdataPath string
	// RemoteAppdataPath is the path to appdata on the remote host.
	RemoteAppdataPath string
	// ProjectName is the docker compose project name for this target.
	ProjectName string
	// StateFile overrides the derived state file path. When empty, derived from Name.
	StateFile string
	// StagingDir overrides the derived staging directory. When empty, derived from Name.
	StagingDir string
	// SecretsScope is the key prefix for per-target secrets (e.g., "unraid" → targets.unraid.*).
	SecretsScope string
	// CriticalContainers overrides the global list for this target.
	CriticalContainers []string
	// PostSyncHooks overrides the global hooks for this target.
	PostSyncHooks []PostSyncHook
	// DeploySyncPaths overrides the global allowlist for this target.
	DeploySyncPaths []string
	// DeploySyncExclude overrides the global blocklist for this target.
	DeploySyncExclude []string
}

// IsDefault returns true if this is the implicit default target.
func (t Target) IsDefault() bool {
	return t.Name == DefaultTargetName
}

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

	// DeploySyncPaths is an allowlist of glob patterns for deploy sync targets.
	// When non-empty, only staging directory entries matching these patterns are deployed.
	DeploySyncPaths []string

	// DeploySyncPathsFromEnv is true when BOSUN_DEPLOY_SYNC_PATHS env var is set.
	// When true, repo config reload will not update DeploySyncPaths.
	DeploySyncPathsFromEnv bool

	// DeploySyncExclude is a blocklist of glob patterns for deploy sync targets.
	// Matching entries are excluded from deployment. Exclude wins over include.
	DeploySyncExclude []string

	// DeploySyncExcludeFromEnv is true when BOSUN_DEPLOY_SYNC_EXCLUDE env var is set.
	// When true, repo config reload will not update DeploySyncExclude.
	DeploySyncExcludeFromEnv bool

	// CriticalContainers is a list of container names that must be healthy after compose up.
	// When configured, the health gate runs after startup grace period before state save.
	// Empty list (default) skips the health gate entirely.
	CriticalContainers []string

	// CriticalContainersFromEnv is true when BOSUN_CRITICAL_CONTAINERS env var is set.
	// When true, repo config reload will not update CriticalContainers.
	CriticalContainersFromEnv bool

	// DriftIgnore is a list of rules for suppressing known drift noise.
	DriftIgnore []DriftIgnoreRule

	// DriftIgnoreFromEnv is true when BOSUN_DRIFT_IGNORE env var is set.
	// When true, repo config reload will not update DriftIgnore.
	DriftIgnoreFromEnv bool

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

// ConfigForTarget returns a shallow copy of the base config with per-target
// fields (TargetHost, appdata paths, ProjectName, StagingDir, StateFile,
// LockFile, CriticalContainers, PostSyncHooks, DeploySyncPaths/Exclude)
// overridden from the given Target. This lets the existing pipeline run
// unchanged per-target — the daemon creates a ConfigForTarget copy before
// each reconciler instantiation.
func (c *Config) ConfigForTarget(t Target) *Config {
	cp := *c // shallow copy

	// Override per-target fields.
	cp.TargetName = t.Name
	cp.TargetHost = t.TargetHost
	if t.LocalAppdataPath != "" {
		cp.LocalAppdataPath = t.LocalAppdataPath
	}
	if t.RemoteAppdataPath != "" {
		cp.RemoteAppdataPath = t.RemoteAppdataPath
	}
	if t.ProjectName != "" {
		cp.ProjectName = t.ProjectName
	}

	// Derive per-target paths from the base config's directories,
	// preserving any custom paths the caller configured.
	// For the default (implicit) target, keep the original paths unchanged
	// so the daemon/CLI share the same lock and state files as pre-multi-target.
	if !t.IsDefault() {
		stateDir := filepath.Dir(c.StateFile)
		cp.StateFile = TargetStateFile(stateDir, t)
		cp.StagingDir = TargetStagingDir(c.StagingDir, t)
		lockDir := filepath.Dir(c.LockFile)
		cp.LockFile = TargetLockFile(lockDir, t)
	}

	// Per-target overrides for operational config.
	// Use nil checks (not len > 0) so targets can explicitly clear inherited
	// slices with an empty list (e.g., critical_containers: []).
	if t.CriticalContainers != nil {
		cp.CriticalContainers = t.CriticalContainers
	}
	if t.PostSyncHooks != nil {
		cp.PostSyncHooks = t.PostSyncHooks
	}
	if t.DeploySyncPaths != nil {
		cp.DeploySyncPaths = t.DeploySyncPaths
	}
	if t.DeploySyncExclude != nil {
		cp.DeploySyncExclude = t.DeploySyncExclude
	}

	cp.SecretsScope = t.SecretsScope

	return &cp
}

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

// DefaultLockDir is the default directory for per-target lock files.
const DefaultLockDir = "/var/run/bosun"

// ResolveTargets returns the effective target list for this config.
// When Targets is non-empty, returns it as-is.
// When Targets is empty, synthesizes a single implicit default target
// from the flat config fields for backwards compatibility.
func (c *Config) ResolveTargets() []Target {
	if len(c.Targets) > 0 {
		// Validate target names before returning to prevent path traversal.
		valid := make([]Target, 0, len(c.Targets))
		for _, t := range c.Targets {
			if err := ValidateTargetName(t.Name); err != nil {
				log.Warn().Str("target", t.Name).Err(err).Msg("Skipping target with invalid name")
				continue
			}
			valid = append(valid, t)
		}
		if len(valid) > 0 {
			return valid
		}
		// Fall through to default if all targets were invalid.
	}
	return []Target{
		{
			Name:               DefaultTargetName,
			TargetHost:         c.TargetHost,
			LocalAppdataPath:   c.LocalAppdataPath,
			RemoteAppdataPath:  c.RemoteAppdataPath,
			ProjectName:        c.ProjectName,
			StateFile:          c.StateFile,
			StagingDir:         c.StagingDir,
			CriticalContainers: c.CriticalContainers,
			PostSyncHooks:      c.PostSyncHooks,
			DeploySyncPaths:    c.DeploySyncPaths,
			DeploySyncExclude:  c.DeploySyncExclude,
		},
	}
}

// safeTargetNamePattern matches only safe target names: lowercase alphanumeric, hyphens, underscores.
var safeTargetNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// ValidateTargetName checks that a target name is safe for use in filesystem paths.
// Rejects empty names, path traversal attempts, absolute paths, and special characters.
func ValidateTargetName(name string) error {
	if name == "" {
		return fmt.Errorf("target name must not be empty")
	}
	if name == DefaultTargetName {
		return nil // The implicit default is always valid
	}
	if !safeTargetNamePattern.MatchString(name) {
		return fmt.Errorf("target name %q contains unsafe characters (allowed: alphanumeric, hyphens, underscores)", name)
	}
	return nil
}

// TargetStateFile returns the state file path for a target.
// The default target uses the legacy path; named targets use deploy-state-<name>.json.
func TargetStateFile(baseStateDir string, t Target) string {
	if t.StateFile != "" {
		return t.StateFile
	}
	if t.IsDefault() {
		return filepath.Join(baseStateDir, DefaultStateFile)
	}
	return filepath.Join(baseStateDir, fmt.Sprintf("deploy-state-%s.json", t.Name))
}

// TargetStagingDir returns the staging directory for a target.
// The default target uses the base staging dir; named targets use <staging>/<name>/.
func TargetStagingDir(baseStagingDir string, t Target) string {
	if t.StagingDir != "" {
		return t.StagingDir
	}
	if t.IsDefault() {
		return baseStagingDir
	}
	return filepath.Join(baseStagingDir, t.Name)
}

// TargetLockFile returns the lock file path for a target.
// The default target uses the legacy path; named targets use reconcile-<name>.lock.
func TargetLockFile(baseLockDir string, t Target) string {
	if t.IsDefault() {
		return filepath.Join(baseLockDir, "reconcile.lock")
	}
	return filepath.Join(baseLockDir, fmt.Sprintf("reconcile-%s.lock", t.Name))
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
		ui.Info("=== Already deployed commit %s, skipping ===", after[:MinLen(after, 8)])
		return nil
	}

	if state.NeedsRedeploy {
		ui.Info("Previous deploy partially failed (configs synced, compose up failed), retrying")
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
	if r.dockerClientFn != nil && !r.config.DryRun && len(r.config.PostSyncHooks) > 0 {
		_, otelHooksSpan := telemetry.Tracer("reconcile").Start(ctx, "reconcile.post_sync_hooks",
			trace.WithAttributes(telemetry.IntAttr("hook_count", len(r.config.PostSyncHooks))),
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

// alertTarget returns the target identifier for alert messages.
// Uses TargetName when set (multi-target mode); falls back to TargetHost or "local".
func (r *Reconciler) alertTarget() string {
	if r.config.TargetName != "" && r.config.TargetName != DefaultTargetName {
		return r.config.TargetName
	}
	if r.config.TargetHost != "" {
		return r.config.TargetHost
	}
	return "local"
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

	target := r.alertTarget()

	services := r.serviceNames()
	duration := time.Since(r.runStartTime)

	if err := r.alerter.SendDeploySuccess(ctx, r.lastCommit, target, services, duration); err != nil {
		logger := log.ComponentCtx(ctx, log.ComponentReconcile)
		logger.Warn().
			Err(err).
			Str(log.FieldOperation, "alert_success").
			Str(log.FieldTarget, target).
			Msg("Failed to send success alert")
	}
}

// serviceNames extracts service names from declared services.
func (r *Reconciler) serviceNames() []string {
	if len(r.declaredServices) == 0 {
		return nil
	}
	names := make([]string, len(r.declaredServices))
	for i, s := range r.declaredServices {
		names[i] = s.Name
	}
	return names
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

	target := r.alertTarget()

	services := r.serviceNames()
	duration := time.Since(r.runStartTime)

	if err := r.alerter.SendDeployFailure(ctx, r.lastCommit, target, reason, services, duration); err != nil {
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

	target := r.alertTarget()

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

	target := r.alertTarget()

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
	// Use nil checks (not len==0) for slices so explicitly empty lists (e.g. `deploy_sync_paths: []`)
	// can clear in-memory filters during hot-reload.
	if reloaded.PostSyncHooks == nil && reloaded.HookSettleDelay == 0 && reloaded.DeployPaths == nil && reloaded.DeploySyncPaths == nil && reloaded.DeploySyncExclude == nil && reloaded.CriticalContainers == nil && reloaded.DriftIgnore == nil && reloaded.OnFailure == nil && reloaded.OnSuccess == nil && reloaded.RemoveOrphans == nil {
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

	if !r.config.DeploySyncPathsFromEnv && reloaded.DeploySyncPaths != nil {
		r.config.DeploySyncPaths = reloaded.DeploySyncPaths
		changed = true
	}

	if !r.config.DeploySyncExcludeFromEnv && reloaded.DeploySyncExclude != nil {
		r.config.DeploySyncExclude = reloaded.DeploySyncExclude
		changed = true
	}

	if !r.config.CriticalContainersFromEnv && len(reloaded.CriticalContainers) > 0 {
		r.config.CriticalContainers = reloaded.CriticalContainers
		changed = true
	}

	if !r.config.DriftIgnoreFromEnv && reloaded.DriftIgnore != nil {
		r.config.DriftIgnore = reloaded.DriftIgnore
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
// Skipped when: DryRun, remote deploy (!isLocalMode), no Docker client,
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
	targets, err := discoverDeployTargets(stagingSubDir, r.config.DeploySyncPaths, r.config.DeploySyncExclude)
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
	stagingSubDir := filepath.Join(r.config.StagingDir, r.config.InfraSubDir)
	appdata := r.config.LocalAppdataPath

	targets, err := discoverDeployTargets(stagingSubDir, r.config.DeploySyncPaths, r.config.DeploySyncExclude)
	if err != nil {
		return nil, fmt.Errorf("discover deploy targets: %w", err)
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
			if err := r.deploy.DeployLocal(ctx, src, dst, result); err != nil {
				return nil, err
			}
		} else {
			_ = os.MkdirAll(filepath.Dir(dst), 0755)
			if err := r.deploy.DeployLocalFile(ctx, src, dst, result); err != nil {
				return nil, err
			}
		}
	}

	// Sync compose files (special handling: glob .yml files for ComposeUpMultipleWithRollback).
	composeStaging := filepath.Join(stagingSubDir, "compose")
	composeTarget := filepath.Join(appdata, "compose")
	if hasTarget(targets, "compose") {
		ui.Info("  Syncing compose files...")
		_ = os.MkdirAll(composeTarget, 0755)
		if err := r.deploy.DeployLocal(ctx, composeStaging, composeTarget, result); err != nil {
			return nil, err
		}
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

	targets, err := discoverDeployTargets(stagingSubDir, r.config.DeploySyncPaths, r.config.DeploySyncExclude)
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

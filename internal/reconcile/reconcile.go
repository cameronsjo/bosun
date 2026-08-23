package reconcile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
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
	HookSettleDelay    *time.Duration
	DeployPaths        []string
	DeploySyncPaths    []string
	DeploySyncExclude  []string
	CriticalContainers []string
	DriftIgnore        []DriftIgnoreRule
	OnFailure          *bool
	OnSuccess          *bool
	RemoveOrphans      *bool
	// ProjectName is the repo bosun.yaml's root-level project_name. When
	// non-nil and non-empty, the default-target reconciler adopts it before
	// the first deploy (#390); a lone `default` target's project_name wins
	// over it.
	ProjectName *string
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

	// TargetsFromEnv records that Targets came from the BOSUN_TARGETS env var.
	// Hot-reload must not override env-provided per-target config (env wins
	// over the repo's bosun.yaml) — see applyTargetOverrides.
	TargetsFromEnv bool

	// DeployMode overrides automatic deploy mode detection.
	// Valid values: "" (auto-detect), "local", "remote".
	// When set, resolveDeployMode skips heuristics and uses the specified mode.
	DeployMode string

	// DryRun if true, only shows what would be done.
	DryRun bool
	// Force if true, runs deployment even if no changes detected. This is the
	// human-supplied override (CLI --force, socket/TCP/HTTP trigger `force`):
	// it bypasses the commit-unchanged skip AND the circuit breaker AND the
	// deploy_paths allowlist gate. An unattended trigger must never set this.
	Force bool
	// ForceRedeployUnchanged bypasses only the commit-unchanged skip in
	// shouldSkipDeploy, for triggers that need a redeploy despite the commit
	// hash not moving (drift self-heal: image_mismatch/unhealthy drift on a
	// running container doesn't change the declared commit). Unlike Force,
	// it does NOT bypass the circuit breaker or the deploy_paths allowlist
	// gate — an unattended self-heal loop must never override a human
	// decision (a tripped breaker) or silently ignore path scoping.
	ForceRedeployUnchanged bool
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

	// TemplateIncludeDir overrides the subtree that template include/fromJsonFile
	// reads are confined to. Empty resolves to <infraDir>/templates (the default
	// allowlist root). A relative value is joined to the infra directory; an
	// absolute value is used as-is. Confining reads here keeps sibling SOPS files
	// and bosun.yaml unreachable from templates. Configurable via
	// BOSUN_TEMPLATE_INCLUDE_DIR or the template_include_dir config field.
	TemplateIncludeDir string

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
	// Default 60s; non-positive disables the gate. Configurable via
	// BOSUN_HEALTH_GATE_TIMEOUT.
	HealthGateTimeout time.Duration

	// HealthGateScope selects which containers the post-compose-up health gate
	// polls and rolls back on: "critical" (default), "declared", or "off".
	// Empty resolves to "critical". Configurable via BOSUN_HEALTH_GATE_SCOPE.
	//
	//   - "critical": today's behavior — gate only on CriticalContainers members;
	//     an empty list skips the gate. A declared-but-non-critical service coming
	//     up unhealthy does NOT trigger rollback.
	//   - "declared": gate on ALL declared services (pollContainerHealth), with the
	//     #392 pre-existing-casualty exemption; any service THIS deploy made
	//     unhealthy triggers the same rollback branch. Opt-in because it adds
	//     flapping-healthcheck churn risk on top of PR #336's fail+retry+alert.
	//   - "off": no health gate at all.
	HealthGateScope string

	// OnFailure gates failure alert dispatch. When false, no failure alerts are sent.
	// Defaults to true via DefaultConfig(). A bare Config{} leaves this false.
	OnFailure bool

	// OnSuccess gates success and recovery alert dispatch. When false, neither
	// success nor recovery alerts are sent. Defaults to false.
	OnSuccess bool

	// ComposeUpTimeout is the maximum time allowed for docker compose up.
	// Zero means use DefaultComposeUpTimeout (10 minutes).
	ComposeUpTimeout time.Duration

	// BackupTimeout bounds backup creation + verification.
	// Zero means use DefaultBackupTimeout (5 minutes).
	BackupTimeout time.Duration

	// ConfigReloader loads project config from a directory path.
	// Set by daemon/CLI to break the config→reconcile import cycle.
	// When nil, config reload is skipped.
	ConfigReloader ConfigReloaderFunc

	// AllowEmptyDeclaredState relaxes the ErrNoDeclaredServices invariant when
	// the staging compose directory exists but contains no parseable services.
	// Use only for genuinely empty repos (early scaffolding, archive branches).
	// Set via BOSUN_ALLOW_EMPTY_DECLARED_STATE=true. Default false.
	// Note: ErrComposeDirMissing (compose dir does not exist at all) is always
	// fatal regardless of this setting.
	AllowEmptyDeclaredState bool

	// SkipDeployInvariant disables the post-deploy mtime + WrittenFiles
	// invariant check that runs between deploy sync and compose-up. Use for
	// diagnostic or development scenarios only — silent-success deploys are
	// the failure mode this guards against. Set via
	// BOSUN_SKIP_DEPLOY_INVARIANT=true. Default false.
	SkipDeployInvariant bool
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
	config            *Config
	git               GitOperations
	sops              SecretsDecryptor
	template          *TemplateOps
	deploy            *DeployOps
	alerter           AlertSender
	dockerClientFn    DockerClientFunc
	lockFile          string
	lockFd            *os.File
	lastBackupPath    string            // Path to the last backup for rollback support
	lastBackupIsFresh bool              // True when lastBackupPath is THIS run's own pre-deploy backup (not a stale fallback anchor); gates delete-missing (#445 security)
	lastComposeFiles  []string          // Compose files from last deploy (for health gate rollback)
	lastCommit        string            // Track commit for alerting
	declaredServices  []DeclaredService // Extracted from rendered compose after templating
	runStartTime      time.Time         // Pipeline start time for duration reporting

	// preDeployUnhealthy snapshots which declared services were already
	// unhealthy immediately before this run's `docker compose up`. Populated
	// in Run() and consumed by verifyPostDeploy so a container this deploy
	// never touched — chronically broken for an unrelated reason — doesn't
	// false-fail the reconcile (#392).
	preDeployUnhealthy map[string]bool
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
	if err := ValidatePostSyncHooks(r.config.PostSyncHooks.Value); err != nil {
		return fmt.Errorf("invalid reconcile configuration: %w", err)
	}

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

	// A panic anywhere before the pipeline's own attempt-tracking write
	// (Step: "Track this attempt before executing the pipeline", below) must
	// still count as a consecutive-failure attempt against the circuit
	// breaker -- otherwise a commit that panics syncRepo every time (e.g. a
	// defect triggered by that commit's own content) retries forever while
	// an ordinary error in the same window is tracked identically via
	// recordSyncFailureAttempt (#364 review follow-up). Registered here (as
	// opposed to daemon.go's reconcileLoop-level recover) so it runs BEFORE
	// the rootSpan defer above and can set runErr for that defer to see.
	defer func() {
		rec := recover()
		if rec == nil {
			return
		}

		logger := log.ComponentCtx(ctx, log.ComponentReconcile)
		logger.Error().
			Interface("panic", rec).
			Str("stack", string(debug.Stack())).
			Msg("Recovered from panic during reconcile pipeline")
		ui.Error("Recovered from panic during reconcile pipeline: %v", rec)

		panicMsg := fmt.Sprintf("reconcile pipeline panicked: %v", rec)
		state := LoadState(r.config.StateFile)
		// r.lastCommit is the best resolvable commit key: if the panic hit
		// before syncRepo ever determined "after" (the exact scenario this
		// guards -- a panicking Sync()), it still holds whatever commit the
		// last successful sync resolved (or "" on a fresh daemon that has
		// never synced), giving the breaker a stable, consistent key across
		// repeated panicking attempts.
		if breakerErr := r.recordSyncFailureAttempt(ctx, state, r.lastCommit, panicMsg); breakerErr != nil {
			runErr = breakerErr
			return
		}
		r.sendThrottledFailureAlert(ctx, state, panicMsg)
		runErr = errors.New(panicMsg)
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
	r.lastBackupIsFresh = false
	r.lastComposeFiles = nil
	r.runStartTime = startTime
	r.preDeployUnhealthy = nil

	// Ensure the lock file's directory exists before acquiring the lock. On a
	// fresh install, DefaultLockFile's parent (/var/run/bosun) doesn't exist
	// yet, so acquireLock's OpenFile fails with ENOENT -- which the warning
	// below then misreports as "another reconciliation may be in progress",
	// paralyzing every subsequent run. One MkdirAll here covers the default
	// target and every named target, since they all share the base lock dir.
	if err := os.MkdirAll(filepath.Dir(r.lockFile), 0755); err != nil {
		return fmt.Errorf("failed to create lock file directory: %w", err)
	}

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
		syncErrMsg := fmt.Sprintf("failed to sync repository: %v", err)
		if breakerErr := r.recordSyncFailureAttempt(ctx, state, after, syncErrMsg); breakerErr != nil {
			return breakerErr
		}
		r.sendThrottledFailureAlert(ctx, state, syncErrMsg)
		return fmt.Errorf("failed to sync repository: %w", err)
	}

	// Track commit for alerting.
	r.lastCommit = after

	// Step 2: Reload project config from repo (picks up hook changes).
	if err := r.reloadProjectConfig(); err != nil {
		return fmt.Errorf("invalid reloaded project configuration: %w", err)
	}

	// Load persistent deploy state to determine if pipeline should run.
	state := LoadState(r.config.StateFile)

	// State-based skip logic: compare last *deployed* commit, not last *fetched* commit.
	// Also re-run if NeedsRedeploy is set from a previous partial failure.
	// ForceRedeployUnchanged (drift self-heal) bypasses only this skip -- it
	// must not also bypass the circuit breaker or deploy_paths gate below,
	// which stay keyed on the real operator Force.
	if shouldSkipDeploy(state.LastDeployedCommit, after, r.config.Force || r.config.ForceRedeployUnchanged, state.NeedsRedeploy) {
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

			// The breaker's contract is CONSECUTIVE failures. Reaching here
			// means sync succeeded and the deploy state is confirmed current
			// -- a real successful cycle, even though the pipeline itself was
			// skipped as redundant. Otherwise recordSyncFailureAttempt's
			// attempt count (keyed on "", since a sync failure/panic never
			// resolves a commit) survives indefinitely: on a quiet repo,
			// unrelated outages months apart accumulate on that same key
			// until one silently tips a primed counter into a trip (review
			// follow-up to #364's breaker fix).
			if state.AttemptCount != 0 || state.LastAttemptedCommit != "" {
				state.AttemptCount = 0
				state.LastAttemptedCommit = ""
				if err := SaveState(r.config.StateFile, state); err != nil {
					logger.Error().Err(err).Str(log.FieldPath, r.config.StateFile).Msg("Failed to reset breaker state after confirmed skip")
				}
			}
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

	// Resolve deploy mode once with full context (config + secrets).
	localDeploy, err := r.resolveDeployMode(ctx, secrets)
	if err != nil {
		r.sendThrottledFailureAlert(ctx, state, err.Error())
		return fmt.Errorf("failed to resolve deploy mode: %w", err)
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
	// ErrComposeDirMissing is always fatal — it indicates a misconfigured
	// staging path. ErrNoDeclaredServices is fatal unless the operator opts
	// in via BOSUN_ALLOW_EMPTY_DECLARED_STATE for genuinely empty repos.
	// Other errors (parse failures, I/O) are non-fatal warnings as before.
	stagingSubDir := filepath.Join(r.config.StagingDir, r.config.InfraSubDir)
	declared, err := ExtractDeclaredState(stagingSubDir)
	switch {
	case errors.Is(err, ErrComposeDirMissing):
		r.sendThrottledFailureAlert(ctx, state, "staging compose directory missing")
		if hint := suggestInfraDir(r.config.InfraSubDir, findComposeCandidates(stagingSubDir)); hint != "" {
			return fmt.Errorf("declared-state invariant: %w — %s", err, hint)
		}
		return fmt.Errorf("declared-state invariant: %w", err)
	case errors.Is(err, ErrNoDeclaredServices):
		if !r.config.AllowEmptyDeclaredState {
			r.sendThrottledFailureAlert(ctx, state, "no declared services in staging compose")
			return fmt.Errorf("declared-state invariant: %w (set BOSUN_ALLOW_EMPTY_DECLARED_STATE=true to override)", err)
		}
		logger.Warn().
			Err(err).
			Bool("override", true).
			Msg("Empty declared state allowed by BOSUN_ALLOW_EMPTY_DECLARED_STATE; continuing")
	case err != nil:
		logger.Warn().Err(err).Msg("Failed to extract declared state from rendered compose")
	default:
		r.declaredServices = declared
		logger.Info().
			Int("declared_services", len(declared)).
			Msg("Extracted declared state from rendered compose")
	}

	// Step 4: Create backup (unless dry run).
	if !r.config.DryRun {
		spanCtx, finishSpan = sentrypkg.StartSpan(ctx, "reconcile.backup", "Configuration backup")
		spanCtx, otelBackupSpan := telemetry.Tracer("reconcile").Start(spanCtx, "reconcile.backup")
		if err := r.createBackup(spanCtx, secrets, localDeploy); err != nil {
			finishSpan(err)
			telemetry.SpanError(otelBackupSpan, err)
			otelBackupSpan.End()

			// A failed backup leaves no fresh rollback anchor for this cycle.
			// Policy: never deploy without a rollback anchor — otherwise a flaky
			// or hostile remote could force a no-rollback deploy by failing the
			// backup (any non-zero tar exit is now fatal, #240). Recover the most
			// recent previously-verified backup as the anchor; if none exists,
			// abort the deploy (fail-safe: no mutation without an anchor).
			ui.Warning("Backup failed: %v", err)
			if policyErr := r.applyBackupFailurePolicy(spanCtx, err); policyErr != nil {
				r.sendThrottledFailureAlert(ctx, state, policyErr.Error())
				return policyErr
			}
			ui.Warning("Using previous verified backup as rollback anchor: %s", r.lastBackupPath)
		} else {
			finishSpan(nil)
			telemetry.SpanOK(otelBackupSpan)
			otelBackupSpan.End()
		}
	}

	// Snapshot container health immediately before compose up runs, so the
	// post-deploy verification can tell "already broken for an unrelated
	// reason" apart from "broke because of this deploy" (#392). Best-effort:
	// a snapshot failure just means the exemption doesn't apply this run.
	if r.dockerClientFn != nil && !r.config.DryRun && len(r.declaredServices) > 0 {
		if client := r.dockerClientFn(); client != nil {
			if actual, snapErr := CollectActualState(ctx, client, r.config.ProjectName); snapErr == nil {
				preExisting := make(map[string]bool)
				for _, name := range classifyHealth(r.declaredServices, actual) {
					preExisting[name] = true
				}
				r.preDeployUnhealthy = preExisting
			} else {
				logger.Warn().Err(snapErr).Msg("Failed to snapshot pre-deploy container health; post-deploy gate won't exempt pre-existing casualties")
			}
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
	deployResult, err := r.doDeploy(spanCtx, secrets, localDeploy, state.DeployedFiles)
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

	// Step 7: Critical container health gate (if configured). Explicitly
	// declared critical containers (BOSUN_CRITICAL_CONTAINERS) still fail the
	// gate even when pre-existing unhealthy — the operator asked for these
	// specific containers to always be healthy, so there's no baseline
	// exemption here (contrast with the general verifyPostDeploy check
	// below, which does exempt pre-existing casualties, #392).
	_, otelHealthGateSpan := telemetry.Tracer("reconcile").Start(ctx, "reconcile.health_gate")
	rolledBack, healthGateErr := r.runHealthGate(ctx, state, localDeploy, deployResult)
	if healthGateErr != nil {
		telemetry.SpanError(otelHealthGateSpan, healthGateErr)
	} else {
		telemetry.SpanOK(otelHealthGateSpan)
	}
	otelHealthGateSpan.End()

	if healthGateErr != nil {
		// A health-gate failure leaves the deploy incomplete (NeedsRedeploy
		// stays set from Step 5, so the next reconcile retries) — but
		// post-sync hooks complete *this* deploy's file changes and must
		// still run. A chronically unhealthy critical container must not
		// permanently disable them for every future reconcile (#392).
		//
		// Exception: when runHealthGate actually rolled back, #445 has reverted
		// the FULL managed tree from the backup -- the compose files AND this
		// deploy's appdata config writes are restored together, so the tree is
		// coherent (no longer a hybrid of old compose + new configs). Hooks are
		// still skipped as belt-and-suspenders: a hook keyed on this deploy's
		// WrittenFiles could act on paths the revert just undid, and re-running it
		// against the reverted tree serves no purpose while the deploy is being
		// retried (#392 follow-up; see the rollback scope note in runHealthGate).
		if rolledBack {
			logger.Warn().Msg("Skipping post-sync hooks: health-gate rollback reverted the full managed tree; hooks skipped as belt-and-suspenders while the deploy retries")
			ui.Warning("Skipping post-sync hooks: rollback reverted the full managed tree")
		} else if r.dockerClientFn != nil && !r.config.DryRun && len(r.config.PostSyncHooks.Value) > 0 {
			r.runPostSyncHooksWithSpan(ctx, state.LastDeployedCommit, after, deployResult, localDeploy)
		}
		return fmt.Errorf("health gate failed: %w", healthGateErr)
	}

	// Capture previous commit before updating state (needed for post-sync hooks).
	previousCommit := state.LastDeployedCommit

	// Execute post-sync hooks if any files changed and hooks are configured.
	// Hooks run BEFORE health verification (so it observes the post-hook
	// container state) and before success is recorded (so a local verify failure
	// retries them on the next reconcile).
	if r.dockerClientFn != nil && !r.config.DryRun && len(r.config.PostSyncHooks.Value) > 0 {
		r.runPostSyncHooksWithSpan(ctx, previousCommit, after, deployResult, localDeploy)
	}

	// Post-deploy health verification (LOCAL deploys only). Gating success on
	// declared-container health makes a verify failure a plain deploy failure:
	// success is NOT recorded below, so NeedsRedeploy stays set (from the
	// pre-deploy marker in Step 5), this attempt counts toward the circuit
	// breaker, and the next reconcile retries — instead of a green save masking
	// an unhealthy rollout (#336). Remote deploys skip verification: it polls the
	// LOCAL docker daemon, which cannot see the remote host's containers, so
	// running it there was meaningless — and they keep today's save semantics.
	if localDeploy && r.dockerClientFn != nil && !r.config.DryRun && len(r.declaredServices) > 0 {
		if client := r.dockerClientFn(); client != nil {
			_, otelDriftSpan := telemetry.Tracer("reconcile").Start(ctx, "reconcile.drift_check")
			if healthErr := r.verifyPostDeploy(ctx, state, client); healthErr != nil {
				telemetry.SpanError(otelDriftSpan, healthErr)
				otelDriftSpan.End()
				// Health verification failed — treat as a deploy failure. Success
				// is not saved, so NeedsRedeploy stays set and the breaker counts
				// this attempt.
				r.sendThrottledFailureAlert(ctx, state, healthErr.Error())
				return fmt.Errorf("post-deploy health verification failed: %w", healthErr)
			}
			telemetry.SpanOK(otelDriftSpan)
			otelDriftSpan.End()
		}
	}

	// Deploy is verified (local) or verification-skipped (remote): record
	// success. The recovery alert and attempt-counter reset live here — after
	// verification — so a local verify failure never emits a premature
	// "recovered" alert or resets the breaker mid-failure-streak.
	if state.AttemptCount > 1 {
		r.sendRecoveryAlert(ctx, state.AttemptCount-1)
	}

	// Record successful deployment in state file.
	state.LastDeployedCommit = after
	state.DeployedAt = time.Now()
	state.DeployCount++
	state.Source = r.config.Source
	state.AttemptCount = 0
	state.LastAlertedAttempt = 0
	state.NeedsRedeploy = false
	state.DeclaredServices = r.declaredServices
	// Persist this deploy's manifest so the next reconcile prunes only files
	// bosun itself wrote. Both modes now return a manifest (remote seeds it from
	// the local staging walk, #334). A dry-run still populates ManagedFiles (the
	// source walk runs regardless of whether files were written), so guard
	// against seeding the manifest from a dry-run — otherwise the next real
	// reconcile could prune untouched paths.
	if deployResult != nil && !r.config.DryRun {
		state.DeployedFiles = deployResult.ManagedFiles
	}
	if err := SaveState(r.config.StateFile, state); err != nil {
		logger.Error().
			Err(err).
			Str(log.FieldPath, r.config.StateFile).
			Msg("Failed to save deploy state after successful deployment")
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

// recordSyncFailureAttempt tracks a consecutive-failure attempt for a
// commit that never made it to the pipeline's own attempt-tracking write
// ("Track this attempt before executing the pipeline", below) -- a git-sync
// error, or a panic recovered before that point. Both must be treated
// identically: an ordinary sync error and a panicking sync on the same
// effective commit key both count against the circuit breaker, and both
// trip it after MaxAttempts (#364 review follow-up -- a panicking sync must
// not retry forever while an ordinary error on the same commit would
// eventually trip).
//
// Returns a non-nil error (already alerted) if the breaker trips; callers
// must return that error immediately without alerting again. Returns nil
// after recording the attempt and persisting state, in which case the
// caller alerts and returns its own failure error as usual.
func (r *Reconciler) recordSyncFailureAttempt(ctx context.Context, state *DeployState, commit, reason string) error {
	logger := log.ComponentCtx(ctx, log.ComponentReconcile)

	if shouldTriggerCircuitBreaker(state.LastAttemptedCommit, commit, state.AttemptCount, MaxAttempts, r.config.Force) {
		logger.Error().
			Str("commit", commit).
			Int("attempts", state.AttemptCount).
			Msg("Circuit breaker: too many consecutive failures before commit resolution, skipping (use --force to override)")
		ui.Error("Circuit breaker: %d consecutive failures (use --force to retry): %s",
			state.AttemptCount, reason)
		breakerErr := fmt.Errorf("circuit breaker: %d consecutive failures: %s", state.AttemptCount, reason)
		r.sendThrottledFailureAlert(ctx, state, breakerErr.Error())
		return breakerErr
	}

	state.LastAttemptedCommit, state.AttemptCount = nextAttemptState(state.LastAttemptedCommit, commit, state.AttemptCount)
	if saveErr := SaveState(r.config.StateFile, state); saveErr != nil {
		logger.Error().Err(saveErr).Str(log.FieldPath, r.config.StateFile).Msg("Failed to save attempt tracking state")
	}
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
		logger.Debug().Msg("Skipping post-deploy health verification. Reason: timeout disabled")
		return nil
	}

	logger.Debug().
		Int("service_count", len(r.declaredServices)).
		Dur("timeout", r.config.HealthCheckTimeout).
		Dur("interval", r.config.HealthCheckInterval).
		Msg("Preparing to verify container health")

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
		// A container already unhealthy before this deploy touched anything
		// is a pre-existing casualty from some unrelated cause, not something
		// this reconcile broke — don't fail the whole target over it (#392).
		// Containers absent from the baseline (newly unhealthy, or missing
		// entirely) always count. Guarded on len(result.Unhealthy) > 0 so a
		// non-classification failure (context cancellation, Docker API
		// errors surfacing as an empty Unhealthy list) is never mistaken for
		// "every unhealthy container was pre-existing" and swallowed.
		blocking := blockingUnhealthy(result.Unhealthy, r.preDeployUnhealthy)
		if len(result.Unhealthy) > 0 && len(blocking) == 0 {
			logger.Warn().
				Strs("pre_existing_unhealthy", result.Unhealthy).
				Int("declared_services", len(r.declaredServices)).
				Msg("Post-deploy health check found only pre-existing unhealthy containers unrelated to this deploy, not failing reconcile")
			ui.Warning("Health verification: ignoring %d pre-existing unhealthy container(s) not caused by this deploy: %s",
				len(result.Unhealthy), strings.Join(result.Unhealthy, ", "))
			state.HealthVerificationPassed = true

			actual, collectErr := CollectActualState(ctx, client, r.config.ProjectName)
			if collectErr == nil {
				report := CompareDrift(r.declaredServices, actual)
				state.DriftCheckedAt = report.CheckedAt
				state.DriftItems = report.Items
			}
			if saveErr := SaveState(r.config.StateFile, state); saveErr != nil {
				logger.Warn().Err(saveErr).Str(log.FieldPath, r.config.StateFile).Msg("Failed to save state after health verification")
			}
			return nil
		}

		// If some (but not all) unhealthy containers are pre-existing, don't
		// let them dilute the reported failure — the propagated error should
		// name only what this reconcile is actually responsible for.
		returnErr := err
		if len(blocking) > 0 && len(blocking) < len(result.Unhealthy) {
			returnErr = fmt.Errorf("health verification timed out after %v: unhealthy containers: %s",
				r.config.HealthCheckTimeout, strings.Join(blocking, ", "))
		}

		logger.Error().
			Err(returnErr).
			Strs("unhealthy_services", result.Unhealthy).
			Strs("newly_unhealthy_services", blocking).
			Int("declared_services", len(r.declaredServices)).
			Int("iterations", result.Iterations).
			Int64(log.FieldDurationMS, result.Duration.Milliseconds()).
			Msg("Failed to verify post-deploy health")
		ui.Error("Health verification failed after %s: %s", result.Duration.Round(time.Second), returnErr)

		// Also run a drift check to populate state.DriftItems for consistency.
		actual, collectErr := CollectActualState(ctx, client, r.config.ProjectName)
		if collectErr == nil {
			report := CompareDrift(r.declaredServices, actual)
			state.DriftCheckedAt = report.CheckedAt
			state.DriftItems = report.Items
		}

		if saveErr := SaveState(r.config.StateFile, state); saveErr != nil {
			logger.Warn().Err(saveErr).Str(log.FieldPath, r.config.StateFile).Msg("Failed to save state after health verification failure")
		}

		return returnErr
	}

	logger.Info().
		Int("declared_services", len(r.declaredServices)).
		Int("iterations", result.Iterations).
		Int64(log.FieldDurationMS, result.Duration.Milliseconds()).
		Msg("Successfully verified post-deploy health")
	ui.Success("Health verification passed: all %d declared services healthy (%s)",
		len(r.declaredServices), result.Duration.Round(time.Second))

	// Run drift check for state consistency.
	actual, collectErr := CollectActualState(ctx, client, r.config.ProjectName)
	if collectErr == nil {
		report := CompareDrift(r.declaredServices, actual)
		state.DriftCheckedAt = report.CheckedAt
		state.DriftItems = report.Items
		logger.Debug().Int("drift_items", len(report.Items)).Msg("Drift check completed after health verification")
	} else {
		logger.Warn().Err(collectErr).Msg("Failed to collect actual state for post-verification drift check")
	}

	if saveErr := SaveState(r.config.StateFile, state); saveErr != nil {
		logger.Warn().Err(saveErr).Str(log.FieldPath, r.config.StateFile).Msg("Failed to save state after health verification")
	}

	return nil
}

// runPostSyncHooksWithSpan wraps executePostSyncHooks with OTel tracing.
func (r *Reconciler) runPostSyncHooksWithSpan(ctx context.Context, previousCommit, currentCommit string, deployResult *DeployResult, local bool) {
	spanCtx, span := telemetry.Tracer("reconcile").Start(ctx, "reconcile.post_sync_hooks",
		trace.WithAttributes(telemetry.IntAttr("hook_count", len(r.config.PostSyncHooks.Value))),
	)
	defer span.End()

	matched, err := r.executePostSyncHooks(spanCtx, previousCommit, currentCommit, deployResult, local)
	span.SetAttributes(telemetry.IntAttr("hooks_matched", matched))
	if err != nil {
		telemetry.SpanError(span, err)
	} else {
		telemetry.SpanOK(span)
	}
}

// executePostSyncHooks detects changed files and restarts matching containers via configured hooks.
// When deployResult is non-nil and has written files, those are used for matching instead of git diff.
// This ensures hooks only fire for files actually written to disk (content-hash sync).
func (r *Reconciler) executePostSyncHooks(ctx context.Context, previousCommit, currentCommit string, deployResult *DeployResult, local bool) (int, error) {
	logger := log.ComponentCtx(ctx, log.ComponentReconcile)
	if err := ValidatePostSyncHooks(r.config.PostSyncHooks.Value); err != nil {
		return 0, err
	}

	if previousCommit == "" {
		logger.Debug().Msg("No previous commit for post-sync hooks (first deploy), skipping")
		return 0, nil
	}

	// Prefer written-files from content-hash sync over git diff.
	var changedFiles []string
	diffFailed := false
	remoteMode := !local
	if remoteMode {
		// Remote deploys return a DeployResult with a ManagedFiles manifest but
		// no WrittenFiles (no per-file change tracking over SSH). Fire all hooks
		// unconditionally — a false-positive restart is better than stale configs
		// on a FUSE mount. See GitHub #197.
		logger.Info().Msg("Remote deploy: firing all post-sync hooks (no file-level tracking available)")
	} else if deployResult != nil && (len(deployResult.WrittenFiles) > 0 || len(deployResult.DeletedFiles) > 0) {
		// Combine writes and deletions: a commit that both writes an unrelated
		// file and deletes a hook-matched one must still fire that hook (#234).
		changedFiles = make([]string, 0, len(deployResult.WrittenFiles)+len(deployResult.DeletedFiles))
		changedFiles = append(changedFiles, deployResult.WrittenFiles...)
		changedFiles = append(changedFiles, deployResult.DeletedFiles...)
		logger.Debug().Int("files", len(changedFiles)).Msg("Using written+deleted files list for post-sync hooks")
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
		return 0, nil
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
		return 0, nil
	}

	client := r.dockerClientFn()
	if client == nil {
		hookErr := fmt.Errorf("docker client unavailable for post-sync hooks")
		logger.Warn().Int("hooks_matched", len(matched)).Msg("Cannot execute post-sync hooks: Docker client unavailable")
		return len(matched), hookErr
	}

	// Resolve the effective settle delay: an explicitly configured delay
	// always wins, otherwise a deploy path under Unraid's /mnt/user FUSE
	// mount defaults to a non-zero delay (see resolveHookSettleDelay).
	deployPath := r.config.RemoteAppdataPath
	if local {
		deployPath = r.config.LocalAppdataPath
	}
	settleDelay := resolveHookSettleDelay(r.config.HookSettleDelay.Value, deployPath)

	ui.Info("Executing %d post-sync hook(s)...", len(matched))
	if err := ExecutePostSyncHooks(ctx, client, matched, settleDelay); err != nil {
		logger.Warn().Err(err).Msg("Some post-sync hooks failed")
		ui.Warning("Post-sync hook errors: %v", err)
		return len(matched), err
	}

	return len(matched), nil
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

// Health gate scope values (Config.HealthGateScope). Selects what the
// post-compose-up gate polls and rolls back on.
const (
	HealthGateScopeCritical = "critical"
	HealthGateScopeDeclared = "declared"
	HealthGateScopeOff      = "off"
)

// ResolveHealthGateScope normalizes and validates a configured scope. Empty
// resolves to "critical". An unknown value returns an error naming the valid set
// so callers (the daemon env/config parse, and runHealthGate) can surface it
// rather than silently guessing.
func ResolveHealthGateScope(scope string) (string, error) {
	switch scope {
	case "", HealthGateScopeCritical:
		return HealthGateScopeCritical, nil
	case HealthGateScopeDeclared:
		return HealthGateScopeDeclared, nil
	case HealthGateScopeOff:
		return HealthGateScopeOff, nil
	default:
		return "", fmt.Errorf("invalid health_gate_scope %q: must be one of %q, %q, %q",
			scope, HealthGateScopeCritical, HealthGateScopeDeclared, HealthGateScopeOff)
	}
}

// runHealthGate polls container health after compose up and, on a failure this
// deploy caused, restores the backup before post-sync hooks run.
//
// The health_gate_scope config selects the target set:
//   - "critical" (default): gate only on CriticalContainers members; an empty
//     list is a no-op. Unchanged from prior behavior.
//   - "declared": gate on all declared services (with the #392 pre-existing
//     casualty exemption); an empty declared set is a no-op.
//   - "off": no gate.
//
// Skipped regardless of scope when: DryRun, remote deploy (Docker API is
// local-only), or no Docker client. On failure it triggers a full managed-tree
// rollback (#445 — deployResult.ManagedFiles drives the restore set) and sends
// throttled failure + rollback alerts.
//
// runHealthGate returns (rolledBack, err). rolledBack is true only when a
// rollback was actually attempted (regardless of whether the rollback itself
// succeeded) — the caller uses it to decide whether post-sync hooks are safe to
// run against the working tree (#392 review follow-up, see the call site in Run).
func (r *Reconciler) runHealthGate(ctx context.Context, state *DeployState, local bool, deployResult *DeployResult) (rolledBack bool, err error) {
	logger := log.ComponentCtx(ctx, log.ComponentReconcile)

	scope, scopeErr := ResolveHealthGateScope(r.config.HealthGateScope)
	if scopeErr != nil {
		// A config typo must not brick deploys: fall back to the strictest gate
		// (critical) and name the valid values loudly.
		logger.Error().Err(scopeErr).Msg("Invalid health_gate_scope; falling back to critical")
		scope = HealthGateScopeCritical
	}

	if scope == HealthGateScopeOff {
		logger.Debug().Msg("Health gate skipped. Reason: health_gate_scope=off")
		return false, nil
	}

	// Empty-target short-circuits preserve the no-op contract per scope: critical
	// with no critical containers, or declared with no declared services.
	containers := r.config.CriticalContainers.Value
	if scope == HealthGateScopeCritical && len(containers) == 0 {
		return false, nil
	}
	if scope == HealthGateScopeDeclared && len(r.declaredServices) == 0 {
		return false, nil
	}
	if r.config.HealthGateTimeout <= 0 {
		logger.Debug().Msg("Health gate skipped. Reason: timeout disabled")
		return false, nil
	}

	if r.config.DryRun {
		logger.Debug().Msg("Health gate skipped. Reason: dry run")
		return false, nil
	}

	if !local {
		logger.Warn().
			Str("scope", scope).
			Msg("Health gate skipped for remote deploy. Docker API is local-only")
		return false, nil
	}

	if r.dockerClientFn == nil {
		logger.Warn().Msg("Health gate skipped. Reason: no Docker client")
		return false, nil
	}

	client := r.dockerClientFn()
	if client == nil {
		logger.Warn().Msg("Health gate skipped. Reason: Docker client unavailable")
		return false, nil
	}

	var gateErr error
	switch scope {
	case HealthGateScopeDeclared:
		gateErr = r.checkDeclaredHealth(ctx, client, r.config.HealthGateTimeout)
	default: // HealthGateScopeCritical
		gateErr = CheckCriticalContainerHealth(ctx, client, containers, r.config.HealthGateTimeout, r.healthCheckInterval())
	}
	if gateErr == nil {
		return false, nil
	}

	logger.Error().
		Err(gateErr).
		Str("scope", scope).
		Msg("Health gate failed. Triggering rollback")

	// Trigger a FULL managed-tree rollback (#445). The health gate runs after
	// `docker compose up -d` already exited 0 (against now-unhealthy containers),
	// so re-running compose up here would be a silent no-op — restore the backup
	// directly. Unlike the old compose-only rollback, this reverts the deploy's
	// appdata config writes too (from deployResult.ManagedFiles), so hooks keyed
	// on the new commit's files no longer run against a hybrid tree — but hooks
	// stay skipped when a rollback ran (the caller's rolledBack gate) as a
	// belt-and-suspenders against any residual mismatch.
	//
	// DeleteMissing (remove files the failed deploy ADDED) is enabled only
	// against a fresh anchor — this deploy's own pre-deploy backup — never a
	// stale fallback anchor, which predates files a later legitimate deploy added.
	set := r.buildRollbackSet(deployResult)
	var rollbackErr error
	// The rollback TRIGGER stays compose-driven (r.lastComposeFiles present),
	// unchanged from the compose-only era: an appdata-only deploy with no compose
	// files does not roll back, so a chronically-unhealthy container can't
	// permanently strand its post-sync hooks (#392/#364). #445 widens only the
	// restore SET — when a rollback DOES fire, it reverts the full managed tree
	// (appdata configs too), not just the compose files.
	if r.lastBackupPath != "" && len(set.ComposeFiles) > 0 {
		rolledBack = true
		rollbackErr = r.deploy.RollbackFromBackupSet(ctx, set, r.lastBackupPath)
		if rollbackErr != nil {
			logger.Error().Err(rollbackErr).Msg("Rollback after health gate failure also failed")
		}
	}

	// Alert by scope, via SEPARATE call sites so critical's no-op is self-evident:
	//   - critical calls the unchanged sendThrottledFailureAlert — literally the
	//     same call as before this PR, so its alert surface is byte-for-byte (no
	//     rollback alert; critical never emitted one).
	//   - declared additionally emits a throttled rollback alert on the SAME
	//     ShouldAlert window, so a flapping healthcheck alerts on the 1/3/10/30
	//     cadence rather than once per cycle (#339 owner finding).
	// The rollback TRIGGER above is scope-agnostic; only the alert differs.
	if scope == HealthGateScopeDeclared {
		r.sendGateFailureAlerts(ctx, state, gateErr, rolledBack, rollbackErr)
	} else {
		r.sendThrottledFailureAlert(ctx, state, gateErr.Error())
	}
	return rolledBack, gateErr
}

// buildRollbackSet assembles the full managed-tree restore set for a health-gate
// rollback: every managed file this deploy wrote (mapped from its appdata-
// relative ManagedFiles entry to the live absolute path), the live compose files
// to re-apply last, and whether deleting backup-absent files is safe. Deletion
// is gated on a fresh anchor — this deploy's own pre-deploy backup — so a stale
// fallback anchor never triggers destructive pruning.
//
// The delete-missing classification "absent from the backup ⇒ added by THIS
// deploy" is only sound while the backup's file enumeration and ManagedFiles'
// enumeration agree on which files the deploy manages. Today they do: a single
// reloadProjectConfig runs before both the backup walk and the deploy walk. A
// future change to either enumeration path must not silently diverge them — that
// is exactly the correctness #360/#353 shore up.
func (r *Reconciler) buildRollbackSet(deployResult *DeployResult) RollbackSet {
	appdata := r.config.LocalAppdataPath
	var files []string
	if deployResult != nil {
		for _, m := range deployResult.ManagedFiles {
			files = append(files, filepath.Join(appdata, filepath.FromSlash(m)))
		}
	}
	return RollbackSet{
		Files:        files,
		ComposeFiles: r.lastComposeFiles,
		Root:         appdata,
		// Gate on the tracked bool, not a re-derived archive mtime: the mtime is
		// attacker-influenceable (host write to the backup dir can `touch` a stale
		// archive forward), whereas lastBackupIsFresh records which branch the
		// reconciler actually took (own pre-deploy backup vs. stale fallback).
		DeleteMissing: r.lastBackupPath != "" && r.lastBackupIsFresh,
	}
}

// checkDeclaredHealth polls all declared services and returns a gate error only
// when a service THIS deploy made unhealthy (the #392 exemption spares a service
// already unhealthy before the deploy — a pre-existing casualty). Returns nil
// when every unhealthy service was pre-existing, or when all are healthy.
func (r *Reconciler) checkDeclaredHealth(ctx context.Context, client *docker.Client, timeout time.Duration) error {
	result, err := pollContainerHealth(ctx, client, r.declaredServices, r.config.ProjectName, timeout, r.healthCheckInterval())
	if err == nil {
		return nil
	}
	if result == nil {
		return err
	}

	blocking := blockingUnhealthy(result.Unhealthy, r.preDeployUnhealthy)
	if len(result.Unhealthy) > 0 && len(blocking) == 0 {
		// Every unhealthy service was already unhealthy before this deploy (#392);
		// not this deploy's fault, so no rollback.
		return nil
	}
	if len(blocking) > 0 {
		return fmt.Errorf("declared health gate failed: unhealthy containers: %s", strings.Join(blocking, ", "))
	}
	// No container was ever classified (a persistent Docker error timed the poll
	// out); still a gate failure, framed consistently.
	return fmt.Errorf("declared health gate failed: %w", err)
}

func (r *Reconciler) healthCheckInterval() time.Duration {
	if r.config.HealthCheckInterval > 0 {
		return r.config.HealthCheckInterval
	}
	return HealthGatePollInterval
}

// sendGateFailureAlerts is the DECLARED-scope health-gate alerter: it emits the
// failure alert and, when a rollback ran, the rollback alert — BOTH on the same
// ShouldAlert throttle window, so a flapping healthcheck under declared scope
// alerts on the 1/3/10/30 attempt cadence rather than once per cycle (#339 owner
// finding). rollbackErr distinguishes a restored backup (SendRollbackSuccess)
// from a failed restore (SendRollbackFailure). Critical scope does NOT use this
// path — it keeps the bare sendThrottledFailureAlert (no rollback alert).
func (r *Reconciler) sendGateFailureAlerts(ctx context.Context, state *DeployState, gateErr error, rolledBack bool, rollbackErr error) {
	if r.alerter == nil || !r.config.OnFailure {
		return
	}
	if !ShouldAlert(state.AttemptCount, state.LastAlertedAttempt) {
		return
	}

	logger := log.ComponentCtx(ctx, log.ComponentReconcile)
	target := r.alertTarget()

	if err := r.alerter.SendDeployFailure(ctx, r.lastCommit, target, gateErr.Error(), r.serviceNames(), time.Since(r.runStartTime)); err != nil {
		// Do NOT consume the throttle window when the failure alert never sent —
		// mirror sendThrottledFailureAlert exactly, so a transient alert-provider
		// outage lets the next attempt retry the alert instead of silently eating
		// the window. The rollback alert is skipped too: it only makes sense as a
		// companion to a delivered failure alert.
		logger.Warn().Err(err).Str(log.FieldOperation, "alert_failure").Msg("Failed to send health-gate failure alert")
		return
	}

	// The failure alert landed; the window is now consumed regardless of whether
	// the companion rollback alert sends (its failure is logged, not retried).
	if rolledBack {
		if rollbackErr == nil {
			if err := r.alerter.SendRollbackSuccess(ctx, target, filepath.Base(r.lastBackupPath)); err != nil {
				logger.Warn().Err(err).Str(log.FieldOperation, "alert_rollback").Msg("Failed to send rollback success alert")
			}
		} else {
			if err := r.alerter.SendRollbackFailure(ctx, target, rollbackErr.Error()); err != nil {
				logger.Warn().Err(err).Str(log.FieldOperation, "alert_rollback").Msg("Failed to send rollback failure alert")
			}
		}
	}

	state.LastAlertedAttempt = state.AttemptCount
	if err := SaveState(r.config.StateFile, state); err != nil {
		logger.Warn().Err(err).Msg("Failed to persist alert throttle state")
	}
}

// syncRepo syncs the git repository.
func (r *Reconciler) syncRepo(ctx context.Context) (bool, string, string, error) {
	start := time.Now()
	logger := log.ComponentCtx(ctx, log.ComponentGit)

	logger.Debug().
		Str(log.FieldOperation, "sync").
		Str(log.FieldURL, r.config.RepoURL).
		Str(log.FieldBranch, r.config.RepoBranch).
		Msg("Preparing to sync repository")

	ui.Info("Syncing repository...")

	changed, before, after, err := r.git.Sync(ctx)
	if err != nil {
		logger.Error().
			Err(err).
			Str(log.FieldURL, r.config.RepoURL).
			Str(log.FieldBranch, r.config.RepoBranch).
			Int64(log.FieldDurationMS, log.DurationMS(start)).
			Msg("Failed to sync repository")
		return changed, before, after, err
	}

	logger.Info().
		Bool("changed", changed).
		Str(log.FieldCommit, before).
		Str("commit_after", after).
		Int64(log.FieldDurationMS, log.DurationMS(start)).
		Msg("Successfully synced repository")

	return changed, before, after, nil
}

// decryptSecrets decrypts SOPS secret files.
func (r *Reconciler) decryptSecrets(ctx context.Context) (map[string]any, error) {
	start := time.Now()
	logger := log.ComponentCtx(ctx, log.ComponentSOPS)

	logger.Debug().
		Int("file_count", len(r.config.SecretsFiles)).
		Msg("Preparing to decrypt secrets")

	ui.Info("Decrypting secrets...")

	if len(r.config.SecretsFiles) == 0 {
		logger.Debug().Msg("No secret files configured, skipping decryption")
		return make(map[string]any), nil
	}

	// Build full paths to secret files.
	var files []string
	for _, f := range r.config.SecretsFiles {
		path := filepath.Join(r.config.RepoDir, f)
		if _, err := os.Stat(path); err != nil {
			logger.Error().
				Err(err).
				Str(log.FieldPath, path).
				Msg("Failed to decrypt secrets. Reason: secrets file not found")
			return nil, fmt.Errorf("secrets file not found: %s", path)
		}
		files = append(files, path)
	}

	secrets, err := r.sops.DecryptFiles(ctx, files)
	if err != nil {
		logger.Error().
			Err(err).
			Int("file_count", len(files)).
			Int64(log.FieldDurationMS, log.DurationMS(start)).
			Msg("Failed to decrypt secrets")
		return nil, err
	}

	logger.Info().
		Int("file_count", len(files)).
		Int64(log.FieldDurationMS, log.DurationMS(start)).
		Msg("Successfully decrypted secrets")

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

// suggestInfraDir formats an operator hint naming the BOSUN_INFRA_DIR value(s)
// that would point at a discovered infra root, given the current InfraSubDir and
// candidate sibling directory names (from findComposeCandidates). Each candidate
// is joined with InfraSubDir so the suggestion is the value to set, not just the
// sibling name. Returns "" when there are no candidates. See GH#214.
func suggestInfraDir(infraSubDir string, candidates []string) string {
	if len(candidates) == 0 {
		return ""
	}
	vals := make([]string, len(candidates))
	for i, c := range candidates {
		vals[i] = filepath.Join(infraSubDir, c)
	}
	if len(vals) == 1 {
		return fmt.Sprintf("did you mean BOSUN_INFRA_DIR=%q?", vals[0])
	}
	quoted := make([]string, len(vals))
	for i, v := range vals {
		quoted[i] = fmt.Sprintf("%q", v)
	}
	return fmt.Sprintf("set BOSUN_INFRA_DIR to one of: %s", strings.Join(quoted, ", "))
}

// renderTemplates renders all templates to the staging directory.
func (r *Reconciler) renderTemplates(ctx context.Context, secrets map[string]any) error {
	start := time.Now()
	logger := log.ComponentCtx(ctx, log.ComponentTemplate)

	infraDir := filepath.Join(r.config.RepoDir, r.config.InfraSubDir)
	logger.Debug().
		Str(log.FieldPath, infraDir).
		Str("staging_dir", r.config.StagingDir).
		Msg("Preparing to render templates")

	ui.Info("Rendering templates...")

	// Clear staging directory.
	if err := os.RemoveAll(r.config.StagingDir); err != nil {
		logger.Error().
			Err(err).
			Str(log.FieldPath, r.config.StagingDir).
			Msg("Failed to render templates. Reason: cannot clear staging directory")
		return fmt.Errorf("failed to clear staging directory: %w", err)
	}
	if err := os.MkdirAll(r.config.StagingDir, 0755); err != nil {
		logger.Error().
			Err(err).
			Str(log.FieldPath, r.config.StagingDir).
			Msg("Failed to render templates. Reason: cannot create staging directory")
		return fmt.Errorf("failed to create staging directory: %w", err)
	}

	// Create template ops with secrets data.
	r.template = NewTemplateOps(secrets)

	// Confine include/fromJsonFile reads to the configured subtree or the
	// <infraDir>/templates default.
	r.template.IncludeDir = ResolveTemplateIncludeDir(infraDir, r.config.TemplateIncludeDir)

	if err := r.template.RenderDirectory(ctx, infraDir, r.config.StagingDir, r.config.InfraSubDir); err != nil {
		logger.Error().
			Err(err).
			Str(log.FieldPath, infraDir).
			Int64(log.FieldDurationMS, log.DurationMS(start)).
			Msg("Failed to render templates")
		return err
	}

	logger.Info().
		Str(log.FieldPath, r.config.StagingDir).
		Int64(log.FieldDurationMS, log.DurationMS(start)).
		Msg("Successfully rendered templates")

	ui.Success("Templates rendered to %s", r.config.StagingDir)
	return nil
}

// createBackup creates a backup of current configs.
func (r *Reconciler) createBackup(ctx context.Context, secrets map[string]any, local bool) error {
	logger := log.ComponentCtx(ctx, log.ComponentDeploy)

	ui.Info("Creating backup...")

	// Bound backup creation + verification so a stuck tar/ssh (#319) cannot
	// wedge the reconcile. On timeout the error propagates to the caller, which
	// already treats backup failures as non-fatal (warn + continue).
	timeout := r.config.BackupTimeout
	if timeout <= 0 {
		timeout = DefaultBackupTimeout
	}
	logger.Debug().
		Str(log.FieldOperation, "backup").
		Int64("timeout_ms", timeout.Milliseconds()).
		Bool("local", local).
		Str(log.FieldPath, r.config.BackupDir).
		Msg("Preparing to create backup")
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Discover targets from staging to know what to back up.
	stagingSubDir := filepath.Join(r.config.StagingDir, r.config.InfraSubDir)
	targets, err := discoverDeployTargets(stagingSubDir, r.config.DeploySyncPaths.Value, r.config.DeploySyncExclude.Value)

	var backupName string

	// Back up only the deployed config footprint (the files bosun renders into
	// staging), not whole appdata target directories — those co-locate large
	// runtime data (media, databases, caches) that made the archive grow without
	// bound and time out (bosun-5qx).
	appdataBase := r.config.LocalAppdataPath
	if !local {
		appdataBase = r.config.RemoteAppdataPath
	}

	var paths []string
	if err != nil {
		// Discovery failed, so the rendered footprint is unknown. Fall back to the
		// full appdata path for rollback protection rather than a no-op backup; the
		// BackupTimeout above bounds it so a large tree cannot wedge the deploy.
		logger.Warn().Err(err).Str(log.FieldPath, appdataBase).
			Msg("Failed to discover deploy targets for backup; falling back to full appdata backup")
		paths = []string{appdataBase}
	} else {
		var ferr error
		paths, ferr = backupFilesFromTargets(stagingSubDir, targets, appdataBase)
		if ferr != nil {
			logger.Warn().Err(ferr).Msg("Failed to enumerate full backup footprint; backing up the files discovered so far")
		}
	}

	if local {
		logger.Debug().Int("path_count", len(paths)).Msg("Creating local backup")
		backupName, err = r.deploy.Backup(ctx, r.config.BackupDir, paths)
	} else {
		host := r.getTargetHost(secrets)
		logger.Debug().Str(log.FieldTarget, host).Int("path_count", len(paths)).Msg("Creating remote backup")
		backupName, err = r.deploy.BackupRemote(ctx, host, r.config.BackupDir, paths)
	}

	if err != nil {
		// Distinguish a timeout from a genuine backup failure so operators see
		// "backup timed out" rather than a downstream symptom (e.g. "archive not
		// found"). The call site treats either as non-fatal (warn + continue).
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("backup timed out after %s: %w", timeout, ctxErr)
		}
		logger.Error().Err(err).Msg("Failed to create backup")
		return err
	}

	// A successful call that produced NO archive means no live paths existed to
	// back up — a fresh host's first deploy (#360). There is no rollback anchor to
	// record, so leave lastBackupPath empty and lastBackupIsFresh false: no later
	// rollback fires against a non-anchor, and the misleading "Backup saved" is
	// skipped. Local Backup signals this with an empty name; BackupRemote fails
	// closed at verification instead, so this only fires on the local path.
	if backupName == "" {
		logger.Info().Msg("No rollback anchor created: no existing paths to back up")
		return nil
	}

	// Store backup path for potential rollback. This is THIS run's own pre-deploy
	// backup, so it is a fresh anchor: delete-missing is safe against it (a file
	// absent from it was genuinely added by this deploy). The stale-fallback path
	// (applyBackupFailurePolicy) clears this bool.
	r.lastBackupPath = filepath.Join(r.config.BackupDir, backupName)
	r.lastBackupIsFresh = true
	logger.Info().Str(log.FieldPath, r.lastBackupPath).Msg("Successfully created backup")

	// Cleanup old backups.
	if err := r.deploy.CleanupBackups(ctx, r.config.BackupDir, r.config.BackupsToKeep); err != nil {
		logger.Warn().Err(err).Int("backups_to_keep", r.config.BackupsToKeep).Msg("Failed to cleanup old backups")
		ui.Warning("Failed to cleanup old backups: %v", err)
	}

	ui.Success("Backup saved: %s", backupName)
	return nil
}

// applyBackupFailurePolicy enforces the "never deploy without a rollback anchor"
// invariant after a failed backup (#240). Because any non-zero remote tar exit is
// now fatal, a flaky or hostile remote could otherwise force a no-rollback deploy
// by failing the backup. This recovers the most recent previously-verified backup
// as the rollback anchor (set on r.lastBackupPath); when no verified backup
// exists, it returns an error so the caller aborts the deploy — no mutation
// without an anchor.
func (r *Reconciler) applyBackupFailurePolicy(ctx context.Context, backupErr error) error {
	logger := log.ComponentCtx(ctx, log.ComponentDeploy)

	anchor, anchorErr := r.deploy.LatestVerifiedBackup(ctx, r.config.BackupDir)
	if anchorErr != nil {
		logger.Error().
			Err(backupErr).
			Str("anchor_lookup_error", anchorErr.Error()).
			Msg("Backup failed and no verified prior backup exists; aborting deploy to preserve rollback safety")
		return fmt.Errorf("backup failed and no verified prior backup available for rollback: %w", backupErr)
	}

	r.lastBackupPath = anchor
	// This anchor is a PRIOR run's backup, not this deploy's own — a stale
	// anchor. It predates files a later legitimate deploy added, so delete-missing
	// must NOT run against it (deleting a "missing" file would destroy real data,
	// the #331 incident class). Clearing the bool fails the delete gate closed.
	r.lastBackupIsFresh = false
	logger.Warn().
		Err(backupErr).
		Str(log.FieldPath, anchor).
		Msg("Backup failed; using most recent verified backup as rollback anchor")
	return nil
}

// ErrAppdataInaccessible is returned when LocalAppdataPath is configured but
// the path cannot be accessed (e.g. mount is down).
var ErrAppdataInaccessible = errors.New("local appdata path is configured but inaccessible")

// doDeploy performs the actual deployment.
// Returns a DeployResult with the deployed-files manifest for both modes. In
// remote mode WrittenFiles stays empty (no file-level change tracking over SSH,
// so hooks fire unconditionally), but ManagedFiles is populated from the local
// staging walk so state.DeployedFiles is seeded either way (#334).
func (r *Reconciler) doDeploy(ctx context.Context, secrets map[string]any, local bool, prevManaged []string) (*DeployResult, error) {
	if local {
		return r.deployLocal(ctx, prevManaged)
	}
	return r.deployRemote(ctx, secrets)
}

// resolveDeployMode determines whether to use local or remote deployment.
// Returns (true, nil) for local mode, (false, nil) for remote mode, or an
// error when the configuration is invalid (e.g. appdata path configured but
// inaccessible and no remote host to fall back to).
// Secrets are consulted for the target host fallback (e.g. network.unraid_ip).
// When DeployMode is explicitly set ("local" or "remote"), auto-detection is skipped.
func (r *Reconciler) resolveDeployMode(ctx context.Context, secrets map[string]any) (bool, error) {
	logger := log.ComponentCtx(ctx, log.ComponentReconcile)

	// Explicit deploy mode overrides auto-detection.
	switch r.config.DeployMode {
	case "local":
		// Verify the appdata path is accessible when configured — forcing local mode with an
		// unmounted path would silently fail during deployLocal().
		if r.config.LocalAppdataPath != "" {
			if _, err := os.Stat(r.config.LocalAppdataPath); err != nil {
				return false, fmt.Errorf("BOSUN_DEPLOY_MODE=local but local_appdata_path %q is inaccessible: %w",
					r.config.LocalAppdataPath, err)
			}
		}
		logger.Info().Msg("Deploy mode forced to local via BOSUN_DEPLOY_MODE")
		return true, nil
	case "remote":
		logger.Info().Msg("Deploy mode forced to remote via BOSUN_DEPLOY_MODE")
		return false, nil
	case "":
		// Auto-detect — fall through.
	default:
		logger.Warn().
			Str("deploy_mode", r.config.DeployMode).
			Msg("Unknown BOSUN_DEPLOY_MODE value, falling back to auto-detection")
	}

	local, err := resolveDeployModeWithSecrets(r.config.TargetHost, r.config.LocalAppdataPath, secrets, os.Stat)
	if err != nil {
		logger.Error().
			Str(log.FieldPath, r.config.LocalAppdataPath).
			Err(err).
			Msg("Local appdata path configured but inaccessible. Check that the mount is up")
		return false, err
	}

	if local {
		logger.Debug().
			Str(log.FieldPath, r.config.LocalAppdataPath).
			Msg("Local appdata path accessible, using local deploy mode")
	} else {
		// Warn when remote mode was selected purely from secrets (network.unraid_ip)
		// with no explicit target_host in config. Implicit fallback is convenient
		// but hard to debug — nudge users toward explicit configuration.
		if r.config.TargetHost == "" && resolveTargetHost("", secrets) != "" {
			logger.Warn().
				Str("resolved_via", "secrets").
				Str("secrets_key", "network.unraid_ip").
				Str("recommendation", "set target_host in bosun.yaml or BOSUN_DEPLOY_MODE env var").
				Msg("Remote deploy mode resolved from secrets fallback — explicit config recommended for predictable behavior")
			ui.Warning("Remote deploy mode selected from secrets (network.unraid_ip) — set target_host in config or BOSUN_DEPLOY_MODE for explicit control")
		}
		logger.Debug().Msg("Using remote deploy mode")
	}

	return local, nil
}

// getTargetHost returns the target host for remote deployment.
func (r *Reconciler) getTargetHost(secrets map[string]any) string {
	return resolveTargetHost(r.config.TargetHost, secrets)
}

// deployLocal performs local deployment via mounted paths.
// Returns a DeployResult with files actually written to disk and the full
// managed-file manifest for this deploy. prevManaged is the prior deploy's
// manifest (appdata-relative), which scopes stale-file pruning.
func (r *Reconciler) deployLocal(ctx context.Context, prevManaged []string) (*DeployResult, error) {
	ui.Info("Using local deployment mode")
	if r.config.DryRun {
		ui.Warning("DRY RUN MODE - no changes will be made")
	}

	deployStart := time.Now()
	invariantsActive := !r.config.SkipDeployInvariant && !r.config.DryRun
	if !invariantsActive && !r.config.DryRun {
		logger := log.ComponentCtx(ctx, log.ComponentReconcile)
		logger.Warn().
			Bool("override", true).
			Msg("Deploy-sync invariants disabled by BOSUN_SKIP_DEPLOY_INVARIANT — silent-sync failures will not be caught")
	}

	result := &DeployResult{}
	stagingSubDir := filepath.Join(r.config.StagingDir, r.config.InfraSubDir)
	appdata := r.config.LocalAppdataPath
	if err := validateTransitionSourceNamespace(stagingSubDir); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("validate deploy source namespace: %w", err)
	}
	if err := validatePriorManagedTransitionArtifacts(appdata, prevManaged); err != nil {
		return nil, fmt.Errorf("validate prior managed transition artifacts: %w", err)
	}

	targets, err := discoverDeployTargets(stagingSubDir, r.config.DeploySyncPaths.Value, r.config.DeploySyncExclude.Value)
	if err != nil {
		return nil, fmt.Errorf("discover deploy targets: %w", err)
	}

	// Invariants run against writtenRel BEFORE PrefixLatest renames the paths.
	verifyTarget := func(src, dst string, writtenRel []string) error {
		if !invariantsActive {
			return nil
		}
		return verifyDeployTarget(src, dst, writtenRel, deployStart)
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
		deletedSnapshot := len(result.DeletedFiles)
		prevForTarget := filterManagedForTarget(prevManaged, t.TargetPath)
		if t.IsDir {
			if err := r.deploy.DeployLocal(ctx, src, dst, result, prevForTarget); err != nil {
				return nil, err
			}
			if err := verifyTarget(src, dst, result.WrittenFiles[snapshot:]); err != nil {
				return nil, err
			}
			result.PrefixLatest(snapshot, t.RelPath)
			result.PrefixLatestDeleted(deletedSnapshot, t.RelPath)
			if err := recordManaged(result, src, t.TargetPath); err != nil {
				return nil, err
			}
		} else {
			targetDir := filepath.Dir(dst)
			if !r.config.DryRun {
				if err := os.MkdirAll(targetDir, 0755); err != nil {
					return nil, fmt.Errorf("create local deploy directory %q: %w", targetDir, err)
				}
			}
			if err := r.deploy.deployLocalFileManaged(ctx, src, dst, result, prevForTarget); err != nil {
				return nil, err
			}
			// DeployLocalFile records filepath.Base, so verify against dst's
			// parent dir and prefix t.RelPath with its dir for hook matching.
			if err := verifyTarget(src, filepath.Dir(dst), result.WrittenFiles[snapshot:]); err != nil {
				return nil, err
			}
			result.PrefixLatest(snapshot, filepath.Dir(t.RelPath))
			result.PrefixLatestDeleted(deletedSnapshot, t.RelPath)
			// Single-file targets are not walked by removeStaleFiles, but record
			// them in the manifest so the set reflects everything bosun deployed.
			result.AddManaged(filepath.ToSlash(t.TargetPath))
		}
	}

	// Sync compose files (special handling: glob .yml files for ComposeUpIsolated).
	composeStaging := filepath.Join(stagingSubDir, "compose")
	composeTarget := filepath.Join(appdata, "compose")
	if hasTarget(targets, "compose") {
		ui.Info("  Syncing compose files...")
		if !r.config.DryRun {
			if err := os.MkdirAll(composeTarget, 0755); err != nil {
				return nil, fmt.Errorf("create local compose directory %q: %w", composeTarget, err)
			}
		}
		snapshot := len(result.WrittenFiles)
		deletedSnapshot := len(result.DeletedFiles)
		prevForCompose := filterManagedForTarget(prevManaged, "compose")
		if err := r.deploy.DeployLocal(ctx, composeStaging, composeTarget, result, prevForCompose); err != nil {
			return nil, err
		}
		if err := verifyTarget(composeStaging, composeTarget, result.WrittenFiles[snapshot:]); err != nil {
			return nil, err
		}
		result.PrefixLatest(snapshot, "compose")
		result.PrefixLatestDeleted(deletedSnapshot, "compose")
		if err := recordManaged(result, composeStaging, "compose"); err != nil {
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
				// Partial failure — the started stacks stay up, but the deploy is
				// NOT a success: report it so NeedsRedeploy stays set, a failure
				// alert fires, the breaker counts it, and the next reconcile retries
				// the failed stack (healthy ones are idempotent no-ops). Mirrors the
				// post-deploy health gate, which also treats failure as deploy failure.
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
				return nil, partialDeployError(summary, len(composeFiles))
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

// recordManaged walks a deployed source dir and appends each regular file to the
// result's managed-file manifest, prefixed with the target path (appdata-relative,
// "/"-separated) so it round-trips through filterManagedForTarget on the next run.
func recordManaged(result *DeployResult, sourceDir, targetPath string) error {
	managed, err := listManagedFiles(sourceDir)
	if err != nil {
		return err
	}
	prefix := filepath.ToSlash(targetPath) + "/"
	for _, m := range managed {
		result.AddManaged(prefix + m)
	}
	return nil
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

// deployRemote performs remote deployment via SSH. It returns a DeployResult
// whose ManagedFiles manifest is built from the local staging walk (mirroring
// deployLocal), so state.DeployedFiles is seeded on remote deploys too —
// harmless while hooks fire unconditionally in remote mode, and correct if the
// target later flips to local. Transfer integrity (#334) and compose rollback
// (#340) are handled here; the mtime-based verifyDeployTarget invariant does not
// apply remotely (it cannot stat a remote FS), and the SHA-256 transfer check
// replaces it.
func (r *Reconciler) deployRemote(ctx context.Context, secrets map[string]any) (*DeployResult, error) {
	logger := log.ComponentCtx(ctx, log.ComponentReconcile)
	ui.Info("Using remote deployment mode (SSH)")
	if r.config.DryRun {
		ui.Warning("DRY RUN MODE - no changes will be made")
	}

	host := r.getTargetHost(secrets)
	if host == "" {
		return nil, fmt.Errorf("no target host specified and could not find unraid_ip in secrets")
	}
	// Validate the host BEFORE the first ssh contact (the sha256sum probe below).
	// TargetHost is re-read from the watched repo after every pull, so a
	// repo-push attacker controls it; an unvalidated value like
	// `-oProxyCommand=<cmd>` would reach ssh as an option and execute on the
	// daemon. Every other ssh/scp boundary validates first; this restores that
	// invariant for the whole deployRemote scope (probe + all subsequent calls).
	if err := validateHost(host); err != nil {
		return nil, fmt.Errorf("invalid SSH host: %w", err)
	}

	stagingSubDir := filepath.Join(r.config.StagingDir, r.config.InfraSubDir)
	appdata := r.config.RemoteAppdataPath

	targets, err := discoverDeployTargets(stagingSubDir, r.config.DeploySyncPaths.Value, r.config.DeploySyncExclude.Value)
	if err != nil {
		return nil, fmt.Errorf("discover deploy targets: %w", err)
	}

	// Probe once per deploy whether the remote can verify transfers; a host
	// lacking sha256sum degrades to the pre-#334 behavior (no integrity gate)
	// rather than hard-failing (#334). Skip the probe on dry-run and when there
	// is no target to sync.
	verifyChecksums := false
	if !r.config.DryRun {
		verifyChecksums = r.deploy.remoteHasSha256sum(ctx, host)
		if !verifyChecksums {
			logger.Warn().
				Str(log.FieldTarget, host).
				Msg("Remote host lacks sha256sum; deploying WITHOUT transfer integrity verification (a truncated tar-over-SSH transfer will not be caught)")
			ui.Warning("Remote host lacks sha256sum — transfer integrity verification disabled for this deploy")
		}
	}

	result := &DeployResult{}

	// Sync discovered targets (excluding compose, which has special handling).
	for _, t := range targets {
		if t.RelPath == "compose" {
			continue
		}
		src := filepath.Join(stagingSubDir, t.RelPath)
		dst := filepath.Join(appdata, t.TargetPath)
		ui.Info("  Syncing %s...", t.RelPath)
		if t.IsDir {
			if err := r.deploy.DeployRemote(ctx, src, host, dst, verifyChecksums); err != nil {
				return nil, err
			}
			if err := recordManaged(result, src, t.TargetPath); err != nil {
				return nil, err
			}
		} else {
			targetDir := filepath.Dir(dst)
			if err := r.deploy.EnsureRemoteDir(ctx, host, targetDir); err != nil {
				return nil, fmt.Errorf("ensure remote deploy directory %q: %w", targetDir, err)
			}
			if err := r.deploy.DeployRemoteFile(ctx, src, host, dst); err != nil {
				return nil, err
			}
			result.AddManaged(filepath.ToSlash(t.TargetPath))
		}
	}

	// Sync compose files (special handling for remote compose up).
	if hasTarget(targets, "compose") {
		ui.Info("  Syncing compose files...")
		composeStaging := filepath.Join(stagingSubDir, "compose")
		composeTarget := filepath.Join(appdata, "compose")
		if err := r.deploy.EnsureRemoteDir(ctx, host, composeTarget); err != nil {
			return nil, fmt.Errorf("ensure remote compose directory %q: %w", composeTarget, err)
		}
		if err := r.deploy.DeployRemote(ctx, composeStaging, host, composeTarget, verifyChecksums); err != nil {
			return nil, err
		}
		if err := recordManaged(result, composeStaging, "compose"); err != nil {
			return nil, err
		}
	}

	// Sync to Compose Manager (Unraid-specific, remote-only) only when compose is a deploy target.
	// This is optional — failure is non-fatal because not all hosts have the Compose Manager plugin.
	composeManagerDir := "/boot/config/plugins/compose.manager/projects/core"
	if hasTarget(targets, "compose") {
		ui.Info("  Syncing core compose to Compose Manager...")
		if err := r.deploy.EnsureRemoteDir(ctx, host, composeManagerDir); err != nil {
			ui.Warning("Compose Manager sync skipped: could not create %s: %v", composeManagerDir, err)
		} else if err := r.deploy.DeployRemoteFile(ctx, filepath.Join(stagingSubDir, "compose", "core.yml"), host, filepath.Join(composeManagerDir, "docker-compose.yml")); err != nil {
			ui.Warning("Compose Manager sync failed: %v", err)
		}
	}

	// Reload services from the actual compose directory (not Compose Manager).
	// composeDir is the remote compose dir just deployed — captured here where
	// it is in scope so RollbackRemoteCompose can restore exactly it, rather
	// than reaching for r.lastComposeFiles (only deployLocal sets that).
	composeDir := filepath.Join(appdata, "compose")
	if !r.config.DryRun {
		ui.Info("  Reloading services...")
		if err := r.deploy.ComposeUpRemote(ctx, host, composeDir); err != nil {
			// Unhealthy-only is recoverable — downgrade to a warning, matching
			// the local branch's ErrComposeUnhealthy handling (#340). The deploy
			// stays a success; the health gate below re-checks critical
			// containers.
			if errors.Is(err, ErrComposeUnhealthy) {
				ui.Warning("Some containers are unhealthy after remote compose up: %v", err)
				logger.Warn().Err(err).Str(log.FieldTarget, host).Msg("Remote compose up reported unhealthy containers, continuing")
			} else {
				// A real compose failure — attempt remote rollback to the backup
				// anchor (#340). RollbackRemoteCompose always returns a non-nil
				// error: ErrRollbackSucceeded (restored) or ErrRollbackFailed
				// (both failed).
				rbErr := r.deploy.RollbackRemoteCompose(ctx, host, composeDir, r.lastBackupPath, err)
				return nil, fmt.Errorf("remote compose up failed: %w", rbErr)
			}
		}
		if err := r.deploy.SignalContainerRemote(ctx, host, "agentgateway", "SIGHUP"); err != nil {
			ui.Warning("Could not reload agentgateway: %v", err)
		}
	}

	ui.Success("Deployment complete!")
	return result, nil
}

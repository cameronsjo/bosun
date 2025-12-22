package reconcile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cameronsjo/bosun/internal/ui"
)

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

	// SecretsFiles is the list of SOPS-encrypted secret files to decrypt.
	SecretsFiles []string
	// InfraSubDir is the subdirectory within the repo containing infrastructure configs.
	InfraSubDir string

	// BackupsToKeep is the number of backups to retain.
	BackupsToKeep int
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		RepoBranch:        "main",
		RepoDir:           "/app/repo",
		StagingDir:        "/app/staging",
		BackupDir:         "/app/backups",
		LogDir:            "/app/logs",
		LocalAppdataPath:  "/mnt/appdata",
		RemoteAppdataPath: "/mnt/user/appdata",
		InfraSubDir:       "infrastructure",
		BackupsToKeep:     5,
	}
}

// Reconciler orchestrates the GitOps reconciliation workflow.
type Reconciler struct {
	config         *Config
	git            GitOperations
	sops           SecretsDecryptor
	template       *TemplateOps
	deploy         *DeployOps
	lockFile       string
	lockFd         *os.File
	lastBackupPath string // Path to the last backup for rollback support
}

// NewReconciler creates a new Reconciler with the given configuration.
func NewReconciler(cfg *Config, opts ...ReconcilerOption) *Reconciler {
	r := &Reconciler{
		config:   cfg,
		git:      NewGitOps(cfg.RepoURL, cfg.RepoBranch, cfg.RepoDir),
		sops:     NewSOPSOps(),
		deploy:   NewDeployOps(cfg.DryRun),
		lockFile: "/tmp/reconcile.lock",
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

// Run executes the full reconciliation workflow.
func (r *Reconciler) Run(ctx context.Context) error {
	startTime := time.Now()

	// Acquire lock to prevent concurrent runs.
	if err := r.acquireLock(); err != nil {
		return fmt.Errorf("failed to acquire lock (another reconciliation may be in progress): %w", err)
	}
	defer r.releaseLock()

	ui.Header("=== Starting reconciliation ===")

	// Step 1: Sync repository.
	changed, before, after, err := r.syncRepo(ctx)
	if err != nil {
		return fmt.Errorf("failed to sync repository: %w", err)
	}

	// Skip if no changes and not forced.
	if !changed && !r.config.Force {
		ui.Info("=== No changes, skipping deployment ===")
		return nil
	}

	if changed {
		ui.Success("Updated: %s -> %s", before, after)
	} else {
		ui.Info("Force mode enabled, proceeding with deployment")
	}

	// Step 2: Decrypt secrets.
	secrets, err := r.decryptSecrets(ctx)
	if err != nil {
		return fmt.Errorf("failed to decrypt secrets: %w", err)
	}

	// Step 3: Render templates.
	if err := r.renderTemplates(ctx, secrets); err != nil {
		return fmt.Errorf("failed to render templates: %w", err)
	}

	// Step 4: Create backup (unless dry run).
	if !r.config.DryRun {
		if err := r.createBackup(ctx, secrets); err != nil {
			ui.Warning("Backup partially failed: %v", err)
		}
	}

	// Step 5: Deploy.
	if err := r.doDeploy(ctx, secrets); err != nil {
		return fmt.Errorf("deployment failed: %w", err)
	}

	// Step 6: Cleanup staging directory after successful deployment.
	if err := r.cleanupStaging(); err != nil {
		ui.Warning("Failed to cleanup staging directory: %v", err)
	}

	duration := time.Since(startTime)
	ui.Success("=== Reconciliation completed in %s ===", duration.Round(time.Second))

	return nil
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

// syncRepo syncs the git repository.
func (r *Reconciler) syncRepo(ctx context.Context) (bool, string, string, error) {
	ui.Info("Syncing repository...")
	return r.git.Sync(ctx)
}

// decryptSecrets decrypts SOPS secret files.
func (r *Reconciler) decryptSecrets(ctx context.Context) (map[string]any, error) {
	ui.Info("Decrypting secrets...")

	if len(r.config.SecretsFiles) == 0 {
		return make(map[string]any), nil
	}

	// Build full paths to secret files.
	var files []string
	for _, f := range r.config.SecretsFiles {
		path := filepath.Join(r.config.RepoDir, f)
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("secrets file not found: %s", path)
		}
		files = append(files, path)
	}

	secrets, err := r.sops.DecryptFiles(ctx, files)
	if err != nil {
		return nil, err
	}

	ui.Success("Secrets decrypted successfully")
	return secrets, nil
}

// renderTemplates renders all templates to the staging directory.
func (r *Reconciler) renderTemplates(ctx context.Context, secrets map[string]any) error {
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
		return err
	}

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
func (r *Reconciler) doDeploy(ctx context.Context, secrets map[string]any) error {
	if r.isLocalMode() {
		return r.deployLocal(ctx)
	}
	return r.deployRemote(ctx, secrets)
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
	if r.config.TargetHost != "" {
		return r.config.TargetHost
	}

	// Try to get from secrets.
	if network, ok := secrets["network"].(map[string]any); ok {
		if ip, ok := network["unraid_ip"].(string); ok {
			return "root@" + ip
		}
	}

	return ""
}

// deployLocal performs local deployment via mounted paths.
func (r *Reconciler) deployLocal(ctx context.Context) error {
	ui.Info("Using local deployment mode")
	if r.config.DryRun {
		ui.Warning("DRY RUN MODE - no changes will be made")
	}

	stagingUnraid := filepath.Join(r.config.StagingDir, "unraid")
	appdata := r.config.LocalAppdataPath

	// Sync Traefik configs.
	ui.Info("  Syncing Traefik configs...")
	if err := r.deploy.DeployLocal(ctx, filepath.Join(stagingUnraid, "appdata", "traefik"), filepath.Join(appdata, "traefik")); err != nil {
		return err
	}

	// Sync agentgateway config.
	ui.Info("  Syncing agentgateway config...")
	if err := r.deploy.DeployLocalFile(ctx, filepath.Join(stagingUnraid, "appdata", "agentgateway", "config.yaml"), filepath.Join(appdata, "agentgateway", "config.yaml")); err != nil {
		return err
	}

	// Sync authelia config.
	ui.Info("  Syncing authelia config...")
	if err := r.deploy.DeployLocalFile(ctx, filepath.Join(stagingUnraid, "appdata", "authelia", "configuration.yml"), filepath.Join(appdata, "authelia", "configuration.yml")); err != nil {
		return err
	}

	// Sync gatus config.
	ui.Info("  Syncing gatus config...")
	if err := r.deploy.DeployLocalFile(ctx, filepath.Join(stagingUnraid, "appdata", "gatus", "config.yaml"), filepath.Join(appdata, "gatus", "config.yaml")); err != nil {
		return err
	}

	// Sync tailscale-gateway config.
	ui.Info("  Syncing tailscale-gateway config...")
	os.MkdirAll(filepath.Join(appdata, "tailscale-gateway"), 0755)
	if err := r.deploy.DeployLocalFile(ctx, filepath.Join(stagingUnraid, "appdata", "tailscale-gateway", "serve.json"), filepath.Join(appdata, "tailscale-gateway", "serve.json")); err != nil {
		ui.Warning("tailscale-gateway sync failed: %v", err)
	}

	// Sync compose files.
	ui.Info("  Syncing compose files...")
	os.MkdirAll(filepath.Join(appdata, "compose"), 0755)
	if err := r.deploy.DeployLocal(ctx, filepath.Join(stagingUnraid, "compose"), filepath.Join(appdata, "compose")); err != nil {
		return err
	}

	// Reload services with rollback support.
	if !r.config.DryRun {
		ui.Info("  Reloading services...")
		composeFile := filepath.Join(appdata, "compose", "core.yml")
		if err := r.deploy.ComposeUpWithRollback(ctx, composeFile, r.lastBackupPath); err != nil {
			// Check if rollback succeeded or failed
			if errors.Is(err, ErrRollbackFailed) {
				return fmt.Errorf("CRITICAL: service reload and rollback both failed: %w", err)
			} else if errors.Is(err, ErrRollbackSucceeded) {
				return fmt.Errorf("service reload failed but rollback succeeded: %w", err)
			}
			// Other errors (no backup available, etc.)
			return fmt.Errorf("service reload failed: %w", err)
		}
		if err := r.deploy.SignalContainer(ctx, "agentgateway", "SIGHUP"); err != nil {
			ui.Warning("Could not reload agentgateway: %v", err)
		}
	}

	ui.Success("Deployment complete!")
	return nil
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
	r.deploy.EnsureRemoteDir(ctx, host, filepath.Join(appdata, "tailscale-gateway"))
	if err := r.deploy.DeployRemoteFile(ctx, filepath.Join(stagingUnraid, "appdata", "tailscale-gateway", "serve.json"), host, filepath.Join(appdata, "tailscale-gateway", "serve.json")); err != nil {
		ui.Warning("tailscale-gateway sync failed: %v", err)
	}

	// Sync compose files.
	ui.Info("  Syncing compose files...")
	r.deploy.EnsureRemoteDir(ctx, host, filepath.Join(appdata, "compose"))
	if err := r.deploy.DeployRemote(ctx, filepath.Join(stagingUnraid, "compose"), host, filepath.Join(appdata, "compose")); err != nil {
		return err
	}

	// Sync to Compose Manager.
	ui.Info("  Syncing core compose to Compose Manager...")
	composeManagerDir := "/boot/config/plugins/compose.manager/projects/core"
	r.deploy.EnsureRemoteDir(ctx, host, composeManagerDir)
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

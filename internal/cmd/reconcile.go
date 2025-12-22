package cmd

import (
	"context"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/cameronsjo/bosun/internal/reconcile"
	"github.com/cameronsjo/bosun/internal/ui"
)

var (
	reconcileDryRun bool
	reconcileForce  bool
	reconcileLocal  bool
	reconcileRemote string
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
6. Deploy (rsync or local copy)
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

	rootCmd.AddCommand(reconcileCmd)
}

func runReconcile(cmd *cobra.Command, args []string) {
	// Build configuration from environment and flags.
	cfg := reconcile.DefaultConfig()

	// Required: repo URL.
	cfg.RepoURL = os.Getenv("REPO_URL")
	if cfg.RepoURL == "" {
		ui.Fatal("REPO_URL environment variable is required")
	}

	// Optional settings from environment.
	if branch := os.Getenv("REPO_BRANCH"); branch != "" {
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
		cfg.TargetHost = ""
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

	// Create context with cancellation on SIGINT/SIGTERM.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		ui.Warning("Received shutdown signal, cancelling...")
		cancel()
	}()

	// Run reconciliation.
	r := reconcile.NewReconciler(cfg)
	if err := r.Run(ctx); err != nil {
		ui.Fatal("Reconciliation failed: %v", err)
	}
}

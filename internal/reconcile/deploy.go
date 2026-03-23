package reconcile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cameronsjo/bosun/internal/fileutil"
	"github.com/cameronsjo/bosun/internal/log"
)

// DeployOps provides deployment operations including backup, file sync, and service management.
type DeployOps struct {
	// DryRun if true, only shows what would be done without making changes.
	DryRun bool
	// ProjectName is the docker compose project name for consistent container namespacing.
	ProjectName string
	// ContentHashSync if true, skips writing files whose content has not changed.
	// Reduces unnecessary FUSE handle invalidation on copy-on-write filesystems.
	ContentHashSync bool
	// RemoveOrphans if true, passes --remove-orphans to docker compose up.
	// Removes containers belonging to services deleted from the compose file.
	RemoveOrphans bool
	// ComposeUpTimeout is the maximum time allowed for docker compose up.
	// Zero means use DefaultComposeUpTimeout.
	ComposeUpTimeout time.Duration

	// composeUpFn overrides the compose-up call in ComposeUpMultipleWithRollback.
	// Defaults to ComposeUpMultiple when nil. Exposed for testing the rollback
	// decision logic without requiring Docker.
	composeUpFn func(ctx context.Context, files []string) error
}

// composeUpTimeout returns the configured timeout or the default.
func (d *DeployOps) composeUpTimeout() time.Duration {
	if d.ComposeUpTimeout > 0 {
		return d.ComposeUpTimeout
	}
	return DefaultComposeUpTimeout
}

// DeployResult tracks which files were actually written during deployment.
// Used to inform post-sync hooks about actual on-disk changes.
type DeployResult struct {
	// WrittenFiles contains relative paths of files that were written to disk.
	WrittenFiles []string
}

// AddWritten appends file paths to the result's written files list.
func (r *DeployResult) AddWritten(files ...string) {
	r.WrittenFiles = append(r.WrittenFiles, files...)
}

// PrefixLatest prepends prefix to all WrittenFiles entries added after
// the snapshot index. Call with len(r.WrittenFiles) before a DeployLocal
// call, then PrefixLatest after, to give the new entries context needed
// for hook glob matching.
func (r *DeployResult) PrefixLatest(snapshot int, prefix string) {
	if snapshot < 0 {
		snapshot = 0
	}
	if snapshot >= len(r.WrittenFiles) {
		return
	}
	for i := snapshot; i < len(r.WrittenFiles); i++ {
		r.WrittenFiles[i] = filepath.Join(prefix, r.WrittenFiles[i])
	}
}

// NewDeployOps creates a new DeployOps instance.
func NewDeployOps(dryRun bool, projectName string) *DeployOps {
	return &DeployOps{DryRun: dryRun, ProjectName: projectName, RemoveOrphans: true}
}

// composeArgs returns docker compose arguments with project name if set.
func (d *DeployOps) composeArgs(files ...string) []string {
	return buildComposeArgs(d.ProjectName, files)
}

// buildUpArgs returns the argument list for docker compose up with explicit orphan control.
func (d *DeployOps) buildUpArgs(composeFiles []string, removeOrphans bool) []string {
	args := d.composeArgs(composeFiles...)
	args = append(args, "up", "-d")
	if removeOrphans {
		args = append(args, "--remove-orphans")
	}
	return args
}

// composeUpArgs returns the full argument list for a local docker compose up command.
// Uses the DeployOps-level RemoveOrphans setting.
func (d *DeployOps) composeUpArgs(composeFiles []string) []string {
	return d.buildUpArgs(composeFiles, d.RemoveOrphans)
}

// remoteComposeUpCmd returns the SSH command string for running docker compose up on a remote host.
func (d *DeployOps) remoteComposeUpCmd(composeDir string) string {
	composeCmd := "docker compose"
	if d.ProjectName != "" {
		composeCmd = fmt.Sprintf("docker compose -p %s", d.ProjectName)
	}
	upArgs := "up -d"
	if d.RemoveOrphans {
		upArgs += " --remove-orphans"
	}
	return fmt.Sprintf("cd %s && %s %s", composeDir, composeCmd, upArgs)
}

// DeployLocal syncs files locally using native Go file operations.
// Performs atomic copy: copies to temp directory first, then replaces target.
// Uses --delete semantics: removes files in target that don't exist in source.
func (d *DeployOps) DeployLocal(ctx context.Context, sourceDir, targetDir string, result *DeployResult) error {
	start := time.Now()
	logger := log.ComponentCtx(ctx, log.ComponentDeploy)

	if d.DryRun {
		logger.Debug().
			Str("source", sourceDir).
			Str("target", targetDir).
			Msg("Dry run: would deploy locally")
		return nil
	}

	logger.Debug().
		Str(log.FieldOperation, "deploy_local").
		Str("source", sourceDir).
		Str("target", targetDir).
		Msg("Deploying files locally")

	// Verify source directory exists
	srcInfo, err := os.Stat(sourceDir)
	if err != nil {
		logger.Error().Err(err).Str("source", sourceDir).Msg("Source directory error")
		return fmt.Errorf("source directory: %w", err)
	}
	if !srcInfo.IsDir() {
		logger.Error().Str("source", sourceDir).Msg("Source is not a directory")
		return fmt.Errorf("source is not a directory: %s", sourceDir)
	}

	// Content-hash mode: compare per-file against existing target, skip unchanged.
	if d.ContentHashSync {
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return fmt.Errorf("create target directory: %w", err)
		}

		written, err := fileutil.CopyDirIfChanged(sourceDir, targetDir)
		if err != nil {
			return fmt.Errorf("copy with content hash: %w", err)
		}

		// Remove files in target that aren't in source (--delete semantics).
		if err := removeStaleFiles(sourceDir, targetDir); err != nil {
			return fmt.Errorf("remove stale files: %w", err)
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}

		if result != nil {
			result.AddWritten(written...)
		}

		logger.Debug().
			Str(log.FieldOperation, "deploy_local").
			Str("target", targetDir).
			Int("files_written", len(written)).
			Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
			Msg("Local deployment completed (content-hash sync)")
		return nil
	}

	// Standard mode: nuke-and-replace for atomic directory swap.
	targetParent := filepath.Dir(targetDir)
	if err := os.MkdirAll(targetParent, 0755); err != nil {
		return fmt.Errorf("create target parent: %w", err)
	}

	tmpDir, err := os.MkdirTemp(targetParent, ".deploy-tmp-*")
	if err != nil {
		return fmt.Errorf("create temp directory: %w", err)
	}

	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(tmpDir)
		}
	}()

	if err := fileutil.CopyDir(sourceDir, tmpDir); err != nil {
		return fmt.Errorf("copy to temp: %w", err)
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Rename-aside pattern: move existing target out of the way, rename new
	// into place, then clean up. This avoids the window where the target is
	// missing (between remove and rename) that the old remove-then-rename
	// approach had.
	backupDir := targetDir + ".bak"
	hadExisting := false

	// Guard against stale backup from a previous crash.
	if _, err := os.Stat(backupDir); err == nil {
		return fmt.Errorf("stale deploy backup exists at %s; restore or remove it before redeploying", backupDir)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat backup path: %w", err)
	}

	if _, err := os.Stat(targetDir); err == nil {
		hadExisting = true
		if err := os.Rename(targetDir, backupDir); err != nil {
			return fmt.Errorf("rename existing target aside: %w", err)
		}
	}

	if err := os.Rename(tmpDir, targetDir); err != nil {
		logger.Error().Err(err).Str("target", targetDir).Msg("Failed to rename to target")
		// Restore the backup so the original target is not lost.
		if hadExisting {
			if rbErr := os.Rename(backupDir, targetDir); rbErr != nil {
				logger.Error().Err(rbErr).Msg("Failed to restore backup after rename failure")
				return fmt.Errorf("rename to target failed: %w; restore backup from %s failed: %v", err, backupDir, rbErr)
			}
		}
		return fmt.Errorf("rename to target: %w", err)
	}

	if hadExisting {
		if err := os.RemoveAll(backupDir); err != nil {
			logger.Warn().Err(err).Str(log.FieldPath, backupDir).Msg("Deployment succeeded but backup cleanup failed")
		}
	}

	success = true
	logger.Debug().
		Str(log.FieldOperation, "deploy_local").
		Str("target", targetDir).
		Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
		Msg("Local deployment completed")
	return nil
}

// removeStaleFiles removes files in targetDir that don't exist in sourceDir.
// Preserves --delete semantics when using per-file content-hash sync.
func removeStaleFiles(sourceDir, targetDir string) error {
	return filepath.WalkDir(targetDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(targetDir, path)
		if err != nil {
			return err
		}
		if relPath == "." {
			return nil
		}

		srcPath := filepath.Join(sourceDir, relPath)
		if _, err := os.Stat(srcPath); os.IsNotExist(err) {
			if d.IsDir() {
				_ = os.RemoveAll(path)
				return filepath.SkipDir
			}
			_ = os.Remove(path)
		}
		return nil
	})
}

// DeployLocalFile syncs a single file locally using native Go file operations.
// Uses atomic copy via temp file. When ContentHashSync is enabled, skips writing
// if the file content has not changed.
func (d *DeployOps) DeployLocalFile(ctx context.Context, sourceFile, targetFile string, result *DeployResult) error {
	if d.DryRun {
		return nil
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	if d.ContentHashSync {
		changed, err := fileutil.CopyFileIfChanged(sourceFile, targetFile)
		if err != nil {
			return err
		}
		if changed && result != nil {
			// Use Base() so the path is relative (matches CopyDirIfChanged).
			// The caller prefixes with the target's RelPath via PrefixLatest.
			result.AddWritten(filepath.Base(targetFile))
		}
		return nil
	}

	return fileutil.CopyFile(sourceFile, targetFile)
}

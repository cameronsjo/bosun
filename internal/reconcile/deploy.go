package reconcile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cameronsjo/bosun/internal/fileutil"
	"github.com/cameronsjo/bosun/internal/log"
	"github.com/kballard/go-shellquote"
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

	// ManagedFiles is the full set of files bosun deployed this run (every
	// regular file in the source tree, not just the changed ones in
	// WrittenFiles), as appdata-relative paths. Persisted to DeployState as the
	// manifest that scopes the next reconcile's stale-file pruning.
	ManagedFiles []string
}

// AddWritten appends file paths to the result's written files list.
func (r *DeployResult) AddWritten(files ...string) {
	r.WrittenFiles = append(r.WrittenFiles, files...)
}

// AddManaged appends file paths to the result's managed-files manifest.
func (r *DeployResult) AddManaged(files ...string) {
	r.ManagedFiles = append(r.ManagedFiles, files...)
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
// Path and project name are shell-quoted to prevent injection from commit-authored config values.
func (d *DeployOps) remoteComposeUpCmd(composeDir string) string {
	args := []string{"docker", "compose"}
	if d.ProjectName != "" {
		args = append(args, "-p", d.ProjectName)
	}
	args = append(args, "up", "-d")
	if d.RemoveOrphans {
		args = append(args, "--remove-orphans")
	}
	return fmt.Sprintf("cd %s && %s", shellquote.Join(composeDir), shellquote.Join(args...))
}

// DeployLocal syncs files locally using native Go file operations.
// Performs atomic copy: copies to temp directory first, then replaces target.
// Uses --delete semantics: removes files in target that don't exist in source.
func (d *DeployOps) DeployLocal(ctx context.Context, sourceDir, targetDir string, result *DeployResult, prevManaged map[string]bool) error {
	start := time.Now()
	logger := log.ComponentCtx(ctx, log.ComponentDeploy)

	if d.DryRun {
		logger.Debug().
			Str(log.FieldPath, sourceDir).
			Str("target", targetDir).
			Msg("Skipping local deployment. Reason: dry-run mode")
		return nil
	}

	logger.Debug().
		Str(log.FieldPath, sourceDir).
		Str("target", targetDir).
		Msg("Preparing to deploy files locally")

	// Verify source directory exists
	srcInfo, err := os.Stat(sourceDir)
	if err != nil {
		logger.Error().
			Err(err).
			Str(log.FieldPath, sourceDir).
			Msg("Failed to deploy locally. Reason: cannot stat source directory")
		return fmt.Errorf("source directory: %w", err)
	}
	if !srcInfo.IsDir() {
		logger.Error().Str(log.FieldPath, sourceDir).Msg("Failed to deploy locally. Reason: source is not a directory")
		return fmt.Errorf("source is not a directory: %s", sourceDir)
	}

	// Content-hash mode: compare per-file against existing target, skip unchanged.
	if d.ContentHashSync {
		logger.Debug().Msg("Using content-hash sync mode for deployment")
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			logger.Error().Err(err).Str("target", targetDir).Msg("Failed to deploy locally. Reason: cannot create target directory")
			return fmt.Errorf("create target directory: %w", err)
		}

		written, err := fileutil.CopyDirIfChanged(sourceDir, targetDir)
		if err != nil {
			logger.Error().Err(err).Msg("Failed to deploy locally. Reason: content-hash sync failed")
			return fmt.Errorf("copy with content hash: %w", err)
		}

		// Prune files bosun previously deployed that are gone from source.
		if err := removeStaleFiles(ctx, sourceDir, targetDir, prevManaged); err != nil {
			logger.Error().Err(err).Msg("Failed to deploy locally. Reason: stale file removal failed")
			return fmt.Errorf("remove stale files: %w", err)
		}

		if ctx.Err() != nil {
			logger.Error().Err(ctx.Err()).Msg("Local deployment cancelled by context")
			return ctx.Err()
		}

		if result != nil {
			result.AddWritten(written...)
		}

		logger.Info().
			Str("target", targetDir).
			Int("files_written", len(written)).
			Int64(log.FieldDurationMS, log.DurationMS(start)).
			Msg("Successfully deployed files locally (content-hash sync)")
		return nil
	}

	// Standard mode: nuke-and-replace for atomic directory swap.
	logger.Debug().Msg("Using atomic swap mode for deployment")
	targetParent := filepath.Dir(targetDir)
	if err := os.MkdirAll(targetParent, 0755); err != nil {
		logger.Error().Err(err).Str("target_parent", targetParent).Msg("Failed to deploy locally. Reason: cannot create target parent directory")
		return fmt.Errorf("create target parent: %w", err)
	}

	tmpDir, err := os.MkdirTemp(targetParent, ".deploy-tmp-*")
	if err != nil {
		logger.Error().Err(err).Str("target_parent", targetParent).Msg("Failed to deploy locally. Reason: cannot create temp directory")
		return fmt.Errorf("create temp directory: %w", err)
	}

	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(tmpDir)
		}
	}()

	if err := fileutil.CopyDir(sourceDir, tmpDir); err != nil {
		logger.Error().Err(err).Msg("Failed to deploy locally. Reason: cannot copy to temp directory")
		return fmt.Errorf("copy to temp: %w", err)
	}

	if ctx.Err() != nil {
		logger.Error().Err(ctx.Err()).Msg("Local deployment cancelled by context")
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
		logger.Error().Str(log.FieldPath, backupDir).Msg("Failed to deploy locally. Reason: stale backup exists from previous crash")
		return fmt.Errorf("stale deploy backup exists at %s; restore or remove it before redeploying", backupDir)
	} else if !os.IsNotExist(err) {
		logger.Error().Err(err).Str(log.FieldPath, backupDir).Msg("Failed to deploy locally. Reason: cannot stat backup path")
		return fmt.Errorf("stat backup path: %w", err)
	}

	if _, err := os.Stat(targetDir); err == nil {
		hadExisting = true
		logger.Debug().Str("target", targetDir).Msg("Moving existing target aside for atomic swap")
		if err := os.Rename(targetDir, backupDir); err != nil {
			logger.Error().Err(err).Msg("Failed to deploy locally. Reason: cannot move existing target aside")
			return fmt.Errorf("rename existing target aside: %w", err)
		}
	}

	if err := os.Rename(tmpDir, targetDir); err != nil {
		logger.Error().Err(err).Str("target", targetDir).Msg("Failed to deploy locally. Reason: cannot rename temp to target")
		// Restore the backup so the original target is not lost.
		if hadExisting {
			if rbErr := os.Rename(backupDir, targetDir); rbErr != nil {
				logger.Error().Err(rbErr).Str(log.FieldPath, backupDir).Msg("Recovery failed: cannot restore backup after rename failure")
				return fmt.Errorf("rename to target failed: %w; restore backup from %s failed: %v", err, backupDir, rbErr)
			}
			logger.Info().Str(log.FieldPath, backupDir).Msg("Restored backup after failed rename")
		}
		return fmt.Errorf("rename to target: %w", err)
	}

	if hadExisting {
		if err := os.RemoveAll(backupDir); err != nil {
			logger.Warn().Err(err).Str(log.FieldPath, backupDir).Msg("Deployment succeeded but backup cleanup failed")
		}
	}

	success = true
	logger.Info().
		Str("target", targetDir).
		Int64(log.FieldDurationMS, log.DurationMS(start)).
		Msg("Successfully deployed files locally (atomic swap)")
	return nil
}

// removeStaleFiles prunes files under targetDir that bosun deployed on a prior
// reconcile (present in prevManaged) but are now absent from sourceDir —
// implementing --delete semantics scoped to bosun's own files. Files NOT in
// prevManaged are never touched: bosun did not write them, so they are not ours
// to delete (e.g. container runtime data like *.sqlite3 colocated with config).
//
// Two safety gates:
//   - Empty manifest (prevManaged) — a fresh state file or first deploy after
//     upgrade. Prune nothing; the caller seeds the manifest from this deploy.
//   - Empty source — sourceDir has no regular files but prevManaged is non-empty,
//     which signals a render failure rather than a legitimate full deletion.
//     Prune nothing and warn, so a bad render cannot wipe a populated target.
//
// prevManaged keys are targetDir-relative paths. Logs a warning for each file
// that cannot be removed and returns a summary error if any removals failed.
func removeStaleFiles(ctx context.Context, sourceDir, targetDir string, prevManaged map[string]bool) error {
	logger := log.ComponentCtx(ctx, log.ComponentDeploy)
	logger.Debug().
		Str(log.FieldPath, sourceDir).
		Str("target", targetDir).
		Int("managed_count", len(prevManaged)).
		Msg("Preparing to remove stale files")

	if len(prevManaged) == 0 {
		logger.Debug().Str("target", targetDir).Msg("No prior managed-file manifest; skipping stale-file pruning")
		return nil
	}

	hasFiles, err := dirHasRegularFiles(sourceDir)
	if err != nil {
		return fmt.Errorf("inspect source %q for stale-file pruning: %w", sourceDir, err)
	}
	if !hasFiles {
		logger.Warn().
			Str(log.FieldPath, sourceDir).
			Str("target", targetDir).
			Int("managed_count", len(prevManaged)).
			Msg("Source has no regular files but a prior deploy did; skipping stale-file pruning (suspected render failure)")
		return nil
	}

	var removalErrors []error
	var removedCount int

	walkErr := filepath.WalkDir(targetDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Always descend directories — a managed file may live in a subdir whose
		// own entry is not in the manifest. Never delete directories themselves.
		if d.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(targetDir, path)
		if err != nil {
			return err
		}
		// Only files bosun deployed last time are ours to prune. Manifest keys
		// are "/"-separated, so normalize before the lookup.
		if !prevManaged[filepath.ToSlash(relPath)] {
			return nil
		}

		// Managed file — remove it only if it is gone from the current source.
		srcPath := filepath.Join(sourceDir, relPath)
		if _, statErr := os.Lstat(srcPath); statErr != nil {
			if !os.IsNotExist(statErr) {
				logger.Warn().Err(statErr).Str(log.FieldPath, relPath).Msg("Failed to stat source for stale file check")
				return fmt.Errorf("stat source path %s: %w", relPath, statErr)
			}
			if rmErr := os.Remove(path); rmErr != nil {
				logger.Warn().Err(rmErr).Str(log.FieldPath, relPath).Msg("Failed to remove stale file")
				removalErrors = append(removalErrors, fmt.Errorf("remove file %s: %w", relPath, rmErr))
			} else {
				logger.Debug().Str(log.FieldPath, relPath).Msg("Removed stale file")
				removedCount++
			}
		}
		return nil
	})

	if walkErr != nil {
		logger.Error().Err(walkErr).Msg("Failed to remove stale files. Reason: directory walk failed")
		return walkErr
	}
	if len(removalErrors) > 0 {
		logger.Warn().Int("failed_removals", len(removalErrors)).Msg("Some stale files could not be removed")
		return fmt.Errorf("%d stale file(s) could not be removed: %w", len(removalErrors), errors.Join(removalErrors...))
	}

	if removedCount > 0 {
		logger.Info().Int("removed", removedCount).Msg("Successfully removed stale files")
	} else {
		logger.Debug().Msg("No stale files found")
	}
	return nil
}

// listManagedFiles walks sourceDir and returns the targetDir-relative paths of
// all regular files — the set of files bosun manages for this target. Symlinks
// are skipped to match CopyDirIfChanged's Lstat-based handling, so a symlinked
// entry is never recorded as managed (and thus never pruned). Returns paths with
// "/" separators regardless of OS, matching how the manifest is persisted.
func listManagedFiles(sourceDir string) ([]string, error) {
	var files []string
	walkErr := filepath.WalkDir(sourceDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("list managed files in %q: %w", sourceDir, walkErr)
	}
	return files, nil
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

	// Standard mode. A symlink source is intentionally not deployed — CopyDir
	// and CopyFileIfChanged both Lstat-skip symlinks, so the single-file path
	// must too. Treat ErrSymlinkSkipped as a benign no-op, not a deploy failure.
	if err := fileutil.CopyFile(sourceFile, targetFile); err != nil {
		if errors.Is(err, fileutil.ErrSymlinkSkipped) {
			return nil
		}
		return err
	}
	return nil
}

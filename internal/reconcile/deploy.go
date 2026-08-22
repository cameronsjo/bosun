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
	"github.com/rs/zerolog"
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

	// composeUpFn overrides the per-file compose-up call in ComposeUpIsolated.
	// Defaults to ComposeUpMultiple when nil. Exposed for testing the isolated
	// deploy/rollback decision logic without requiring Docker.
	composeUpFn func(ctx context.Context, files []string) error
	// copyDirIfChangedFn fault-injects post-transition copy failures in tests.
	// Nil uses fileutil.CopyDirIfChanged.
	copyDirIfChangedFn func(src, dst string) ([]string, error)
}

// composeUpTimeout returns the configured timeout or the default.
func (d *DeployOps) composeUpTimeout() time.Duration {
	if d.ComposeUpTimeout > 0 {
		return d.ComposeUpTimeout
	}
	return DefaultComposeUpTimeout
}

// DeployResult tracks which files were actually written or deleted during
// deployment. Used to inform post-sync hooks about actual on-disk changes.
type DeployResult struct {
	// WrittenFiles contains relative paths of files that were written to disk.
	WrittenFiles []string

	// DeletedFiles contains relative paths of files removed from disk by
	// removeStaleFiles's --delete-style pruning. Tracked separately from
	// WrittenFiles because a deletion is not a write; callers that need "every
	// file touched this deploy" (e.g. post-sync hook matching) combine both
	// lists explicitly.
	DeletedFiles []string

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

// AddDeleted appends file paths to the result's deleted files list.
func (r *DeployResult) AddDeleted(files ...string) {
	r.DeletedFiles = append(r.DeletedFiles, files...)
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

// PrefixLatestDeleted prepends prefix to all DeletedFiles entries added after
// the snapshot index. Mirrors PrefixLatest so deleted paths get the same
// staging-relative prefix needed for hook glob matching.
func (r *DeployResult) PrefixLatestDeleted(snapshot int, prefix string) {
	if snapshot < 0 {
		snapshot = 0
	}
	if snapshot >= len(r.DeletedFiles) {
		return
	}
	for i := snapshot; i < len(r.DeletedFiles); i++ {
		r.DeletedFiles[i] = filepath.Join(prefix, r.DeletedFiles[i])
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
		transitions, err := prepareManagedTypeTransitions(sourceDir, targetDir, prevManaged)
		if err != nil {
			logger.Error().Err(err).Msg("Failed to deploy locally. Reason: file type transition failed")
			return fmt.Errorf("prepare file type transitions: %w", err)
		}
		if err := transitions.Promote(); err != nil {
			return fmt.Errorf("promote file type transitions: %w", err)
		}
		rollback := func(cause error) error { return transitions.Rollback(cause) }

		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return rollback(fmt.Errorf("create target directory: %w", err))
		}

		copyFn := d.copyDirIfChangedFn
		if copyFn == nil {
			copyFn = fileutil.CopyDirIfChanged
		}
		written, err := copyFn(sourceDir, targetDir)
		if err != nil {
			logger.Error().Err(err).Msg("Failed to deploy locally. Reason: content-hash sync failed")
			return rollback(fmt.Errorf("copy with content hash: %w", err))
		}

		// Prune files bosun previously deployed that are gone from source.
		if err := removeStaleFiles(ctx, sourceDir, targetDir, result, prevManaged); err != nil {
			logger.Error().Err(err).Msg("Failed to deploy locally. Reason: stale file removal failed")
			return rollback(fmt.Errorf("remove stale files: %w", err))
		}

		if ctx.Err() != nil {
			logger.Error().Err(ctx.Err()).Msg("Local deployment cancelled by context")
			return rollback(ctx.Err())
		}

		transitions.Commit(logger)
		if result != nil {
			result.AddWritten(transitions.WrittenFiles()...)
			result.AddDeleted(transitions.DeletedFiles()...)
			result.AddWritten(written...)
		}

		logger.Info().
			Str("target", targetDir).
			Int("files_written", len(written)+len(transitions.WrittenFiles())).
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

const managedTargetRoot = "."

type managedTypeTransition struct {
	sourcePath, targetPath, relPath string
	sourceIsDir                     bool
	targetInfo, parentInfo          os.FileInfo
	stageRoot, replacement, old     string
	written, deleted                []string
	prevManaged                     map[string]bool
	quarantined, promoted           bool
}

type managedTypeTransitions struct{ items []*managedTypeTransition }

func prepareManagedTypeTransitions(sourceRoot, targetRoot string, prevManaged map[string]bool) (*managedTypeTransitions, error) {
	tx := &managedTypeTransitions{}
	sourceInfo, err := os.Lstat(sourceRoot)
	if err != nil {
		return tx, err
	}
	rootConflict, err := discoverManagedTypeConflict(sourceRoot, targetRoot, managedTargetRoot, sourceInfo, prevManaged)
	if err != nil {
		return tx, err
	}
	if rootConflict != nil {
		tx.items = append(tx.items, rootConflict)
	} else if sourceInfo.IsDir() {
		walkErr := filepath.WalkDir(sourceRoot, func(sourcePath string, sourceEntry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if sourcePath == sourceRoot || sourceEntry.Type()&os.ModeSymlink != 0 {
				return nil
			}
			relPath, err := filepath.Rel(sourceRoot, sourcePath)
			if err != nil {
				return fmt.Errorf("calculate source path: %w", err)
			}
			info, err := sourceEntry.Info()
			if err != nil {
				return err
			}
			conflict, err := discoverManagedTypeConflict(sourcePath, filepath.Join(targetRoot, relPath), filepath.ToSlash(relPath), info, prevManaged)
			if err != nil {
				return err
			}
			if conflict != nil {
				tx.items = append(tx.items, conflict)
				if sourceEntry.IsDir() {
					return filepath.SkipDir
				}
			}
			return nil
		})
		if walkErr != nil {
			return tx, walkErr
		}
	}
	for _, item := range tx.items {
		if err := item.stage(); err != nil {
			tx.discardStages()
			return tx, err
		}
	}
	return tx, nil
}

func discoverManagedTypeConflict(sourcePath, targetPath, relPath string, sourceInfo os.FileInfo, prevManaged map[string]bool) (*managedTypeTransition, error) {
	targetInfo, err := os.Lstat(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("inspect destination %s: %w", relPath, err)
	}
	if sourceInfo.IsDir() == targetInfo.IsDir() {
		if sourceInfo.Mode().IsRegular() && !targetInfo.Mode().IsRegular() {
			return nil, fmt.Errorf("destination %s is not a regular file", relPath)
		}
		return nil, nil
	}
	if !sourceInfo.IsDir() && !sourceInfo.Mode().IsRegular() {
		return nil, nil
	}
	parentInfo, err := os.Lstat(filepath.Dir(targetPath))
	if err != nil {
		return nil, fmt.Errorf("inspect destination parent %s: %w", relPath, err)
	}
	item := &managedTypeTransition{sourcePath: sourcePath, targetPath: targetPath, relPath: relPath, sourceIsDir: sourceInfo.IsDir(), targetInfo: targetInfo, parentInfo: parentInfo, prevManaged: prevManaged}
	if sourceInfo.IsDir() {
		if !targetInfo.Mode().IsRegular() || !prevManaged[relPath] {
			return nil, fmt.Errorf("destination %s blocks directory deployment and is not a managed regular file", relPath)
		}
		item.deleted = []string{relPath}
	} else {
		deleted, err := validateManagedDirectory(targetPath, relPath, prevManaged)
		if err != nil {
			return nil, fmt.Errorf("inspect directory blocking file deployment at %s: %w", relPath, err)
		}
		item.deleted = deleted
	}
	return item, nil
}

func validateManagedDirectory(conflictDir, conflictRel string, prevManaged map[string]bool) ([]string, error) {
	var files, physicalFiles, dirs []string
	err := filepath.WalkDir(conflictDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			dirs = append(dirs, path)
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("destination entry %s is not a regular managed file", path)
		}
		under, err := filepath.Rel(conflictDir, path)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(filepath.Join(conflictRel, under))
		if !prevManaged[relSlash] {
			return fmt.Errorf("destination entry %s is not in the managed-file manifest", relSlash)
		}
		files = append(files, relSlash)
		physicalFiles = append(physicalFiles, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, dir := range dirs {
		hasManaged := false
		for _, file := range physicalFiles {
			under, relErr := filepath.Rel(dir, file)
			if relErr == nil && under != "." && under != ".." && !filepath.IsAbs(under) && (len(under) < 3 || under[:3] != ".."+string(filepath.Separator)) {
				hasManaged = true
				break
			}
		}
		if !hasManaged {
			return nil, fmt.Errorf("destination directory %s has no managed descendants", dir)
		}
	}
	return files, nil
}

func (t *managedTypeTransition) stage() error {
	stageRoot, err := os.MkdirTemp(filepath.Dir(t.targetPath), ".bosun-transition-*")
	if err != nil {
		return fmt.Errorf("stage transition %s: %w", t.relPath, err)
	}
	t.stageRoot = stageRoot
	t.replacement = filepath.Join(stageRoot, "replacement")
	t.old = filepath.Join(stageRoot, "original")
	if t.sourceIsDir {
		info, err := os.Stat(t.sourcePath)
		if err != nil {
			return err
		}
		if err := os.Mkdir(t.replacement, info.Mode().Perm()); err != nil {
			return fmt.Errorf("stage replacement directory %s: %w", t.relPath, err)
		}
		if err := fileutil.CopyDir(t.sourcePath, t.replacement); err != nil {
			return fmt.Errorf("stage replacement directory %s: %w", t.relPath, err)
		}
		managed, err := listManagedFiles(t.sourcePath)
		if err != nil {
			return err
		}
		for _, path := range managed {
			if t.relPath == managedTargetRoot {
				t.written = append(t.written, path)
			} else {
				t.written = append(t.written, filepath.Join(t.relPath, path))
			}
		}
		return nil
	}
	if err := fileutil.CopyFile(t.sourcePath, t.replacement); err != nil {
		return fmt.Errorf("stage replacement file %s: %w", t.relPath, err)
	}
	t.written = []string{t.relPath}
	return nil
}

func (ts *managedTypeTransitions) discardStages() {
	for _, item := range ts.items {
		if item.stageRoot != "" {
			_ = os.RemoveAll(item.stageRoot)
		}
	}
}

func (ts *managedTypeTransitions) Promote() error {
	for _, item := range ts.items {
		parent, err := os.Lstat(filepath.Dir(item.targetPath))
		if err != nil || !os.SameFile(parent, item.parentInfo) {
			return ts.Rollback(fmt.Errorf("destination parent changed before promoting %s", item.relPath))
		}
		current, err := os.Lstat(item.targetPath)
		if err != nil || !os.SameFile(current, item.targetInfo) {
			return ts.Rollback(fmt.Errorf("destination changed before promoting %s", item.relPath))
		}
		if err := os.Rename(item.targetPath, item.old); err != nil {
			return ts.Rollback(fmt.Errorf("quarantine destination %s: %w", item.relPath, err))
		}
		item.quarantined = true
		if !item.sourceIsDir {
			deleted, err := validateManagedDirectory(item.old, item.relPath, item.prevManaged)
			if err != nil {
				return ts.Rollback(fmt.Errorf("destination changed while quarantining %s: %w", item.relPath, err))
			}
			item.deleted = deleted
		}
		if _, err := os.Lstat(item.targetPath); !os.IsNotExist(err) {
			return ts.Rollback(fmt.Errorf("destination %s was recreated during transition", item.relPath))
		}
		if err := os.Rename(item.replacement, item.targetPath); err != nil {
			return ts.Rollback(fmt.Errorf("promote replacement %s: %w", item.relPath, err))
		}
		item.promoted = true
	}
	return nil
}

func (ts *managedTypeTransitions) Rollback(cause error) error {
	errs := []error{cause}
	recoveryRequired := false
	for i := len(ts.items) - 1; i >= 0; i-- {
		item := ts.items[i]
		if item.promoted {
			failed := filepath.Join(item.stageRoot, "failed-replacement")
			if err := os.Rename(item.targetPath, failed); err != nil {
				errs = append(errs, fmt.Errorf("quarantine failed replacement %s: %w", item.relPath, err))
				recoveryRequired = true
				continue
			}
			recoveryRequired = true
			item.promoted = false
		}
		if item.quarantined {
			if err := os.Rename(item.old, item.targetPath); err != nil {
				errs = append(errs, fmt.Errorf("restore original destination %s: %w", item.relPath, err))
				recoveryRequired = true
				continue
			}
			item.quarantined = false
		}
	}
	if recoveryRequired {
		for _, item := range ts.items {
			if item.stageRoot != "" {
				errs = append(errs, fmt.Errorf("transition artifacts retained for recovery at %s", item.stageRoot))
			}
		}
	} else {
		ts.discardStages()
	}
	return errors.Join(errs...)
}

func (ts *managedTypeTransitions) Commit(logger zerolog.Logger) {
	for _, item := range ts.items {
		// A process with an already-open directory descriptor can still write into
		// the quarantined tree after its atomic rename. Revalidate immediately
		// before cleanup and retain the quarantine if new unmanaged data appeared.
		if !item.sourceIsDir {
			if _, err := validateManagedDirectory(item.old, item.relPath, item.prevManaged); err != nil {
				logger.Warn().Err(err).Str(log.FieldPath, item.stageRoot).Msg("Transition succeeded but quarantine gained unmanaged data; preserving it")
				continue
			}
		}
		if err := os.RemoveAll(item.stageRoot); err != nil {
			logger.Warn().Err(err).Str(log.FieldPath, item.stageRoot).Msg("Transition succeeded but quarantine cleanup failed")
		}
	}
}

func (ts *managedTypeTransitions) WrittenFiles() []string {
	var files []string
	for _, item := range ts.items {
		files = append(files, item.written...)
	}
	return files
}

func (ts *managedTypeTransitions) DeletedFiles() []string {
	var files []string
	for _, item := range ts.items {
		files = append(files, item.deleted...)
	}
	return files
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
// Removed paths are recorded on result (targetDir-relative, "/"-separated) so
// post-sync hooks can match against deletions, not just writes; result may be
// nil when the caller doesn't need that tracking (e.g. direct unit tests).
func removeStaleFiles(ctx context.Context, sourceDir, targetDir string, result *DeployResult, prevManaged map[string]bool) error {
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
				if result != nil {
					result.AddDeleted(filepath.ToSlash(relPath))
				}
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
	return d.deployLocalFileManaged(ctx, sourceFile, targetFile, result, nil)
}

func (d *DeployOps) deployLocalFileManaged(ctx context.Context, sourceFile, targetFile string, result *DeployResult, prevManaged map[string]bool) error {
	if d.DryRun {
		return nil
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}
	transitions, err := prepareManagedTypeTransitions(sourceFile, targetFile, prevManaged)
	if err != nil {
		return fmt.Errorf("prepare file type transition: %w", err)
	}
	if err := transitions.Promote(); err != nil {
		return fmt.Errorf("promote file type transition: %w", err)
	}
	if len(transitions.items) > 0 {
		if ctx.Err() != nil {
			return transitions.Rollback(ctx.Err())
		}
		transitions.Commit(log.ComponentCtx(ctx, log.ComponentDeploy))
		if result != nil {
			result.AddWritten(filepath.Base(targetFile))
			result.AddDeleted(transitions.DeletedFiles()...)
		}
		return nil
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

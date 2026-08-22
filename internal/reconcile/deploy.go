package reconcile

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
		defer transitions.Close()
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

const (
	managedTargetRoot            = "."
	managedTransitionOldSuffix   = ".bosun-transition-old"
	managedTransitionNewSuffix   = ".bosun-transition-new"
	managedTransitionStageSuffix = ".bosun-transition-stage"
)

func validateTransitionSourceNamespace(sourceRoot string) error {
	return filepath.WalkDir(sourceRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		name := strings.ToLower(entry.Name())
		if strings.HasSuffix(name, managedTransitionOldSuffix) ||
			strings.HasSuffix(name, managedTransitionNewSuffix) ||
			strings.HasSuffix(name, managedTransitionStageSuffix) {
			return fmt.Errorf("source path uses reserved transition suffix: %s", path)
		}
		return nil
	})
}

func validatePriorManagedTransitionArtifacts(targetRoot string, managed []string) error {
	return validateManagedTransitionArtifacts(targetRoot, managed)
}

func validateManagedMapTransitionArtifacts(targetRoot string, managed map[string]bool) error {
	paths := make([]string, 0, len(managed))
	for path, owned := range managed {
		if owned {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return validateManagedTransitionArtifacts(targetRoot, paths)
}

func validateManagedTransitionArtifacts(targetRoot string, managed []string) error {
	candidates := make(map[string]struct{})
	for _, manifestPath := range managed {
		rel := filepath.Clean(filepath.FromSlash(manifestPath))
		if rel == managedTargetRoot {
			candidates[filepath.Clean(targetRoot)] = struct{}{}
			continue
		}
		if rel == "" || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("invalid managed path %q while checking transition artifacts", manifestPath)
		}
		for rel != managedTargetRoot {
			candidate := filepath.Join(targetRoot, rel)
			candidates[candidate] = struct{}{}
			rel = filepath.Dir(rel)
		}
	}
	ordered := make([]string, 0, len(candidates))
	for candidate := range candidates {
		ordered = append(ordered, candidate)
	}
	sort.Strings(ordered)
	for _, candidate := range ordered {
		if err := validateTransitionArtifactsAbsent(candidate); err != nil {
			return err
		}
	}
	return nil
}

func validateTransitionArtifactsAbsent(targetPath string) error {
	for _, artifact := range []string{
		targetPath + managedTransitionOldSuffix,
		targetPath + managedTransitionNewSuffix,
		targetPath + managedTransitionStageSuffix,
	} {
		if _, err := os.Lstat(artifact); err == nil {
			return fmt.Errorf("transition artifact already exists at %s", artifact)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect transition artifact %s: %w", artifact, err)
		}
	}
	return nil
}

type managedTypeTransition struct {
	sourcePath, targetPath, relPath string
	sourceIsDir                     bool
	targetInfo, parentInfo          os.FileInfo
	newInfo                         os.FileInfo
	targetFingerprint               [sha256.Size]byte
	newFingerprint                  [sha256.Size]byte
	targetHandle, parentHandle      *os.File
	newHandle                       *os.File
	privatePath                     string
	privateInfo                     os.FileInfo
	privateHandle                   *os.File
	oldPath, newPath                string
	written, deleted                []string
	prevManaged                     map[string]bool
	quarantined, promoted           bool
}

type managedTypeTransitions struct {
	items []*managedTypeTransition
}

func prepareManagedTypeTransitions(sourceRoot, targetRoot string, prevManaged map[string]bool) (*managedTypeTransitions, error) {
	tx := &managedTypeTransitions{}
	if err := validateManagedMapTransitionArtifacts(targetRoot, prevManaged); err != nil {
		return tx, err
	}
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
		err = filepath.WalkDir(sourceRoot, func(sourcePath string, sourceEntry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
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
		if err != nil {
			tx.Close()
			return tx, err
		}
	}
	for _, item := range tx.items {
		if err := item.stage(); err != nil {
			tx.Close()
			return tx, err
		}
	}
	return tx, nil
}

func discoverManagedTypeConflict(sourcePath, targetPath, relPath string, sourceInfo os.FileInfo, prevManaged map[string]bool) (*managedTypeTransition, error) {
	if err := validateTransitionArtifactsAbsent(targetPath); err != nil {
		return nil, err
	}
	targetPathInfo, err := os.Lstat(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("inspect destination %s: %w", relPath, err)
	}
	parentPath := filepath.Dir(targetPath)
	parentPathInfo, err := os.Lstat(parentPath)
	if err != nil {
		return nil, fmt.Errorf("inspect destination parent %s: %w", relPath, err)
	}
	parentHandle, err := openPinnedPath(parentPath)
	if err != nil {
		return nil, fmt.Errorf("open destination parent for %s: %w", relPath, err)
	}
	targetHandle, err := openPinnedPath(targetPath)
	if err != nil {
		_ = parentHandle.Close()
		return nil, fmt.Errorf("open destination %s: %w", relPath, err)
	}
	keepHandles := false
	defer func() {
		if !keepHandles {
			_ = targetHandle.Close()
			_ = parentHandle.Close()
		}
	}()
	parentInfo, parentStatErr := parentHandle.Stat()
	targetInfo, targetStatErr := targetHandle.Stat()
	if parentStatErr != nil || !os.SameFile(parentPathInfo, parentInfo) {
		return nil, fmt.Errorf("destination parent changed while discovering %s", relPath)
	}
	if targetStatErr != nil || !os.SameFile(targetPathInfo, targetInfo) {
		return nil, fmt.Errorf("destination changed while discovering %s", relPath)
	}
	item := &managedTypeTransition{
		sourcePath: sourcePath, targetPath: targetPath, relPath: relPath,
		sourceIsDir: sourceInfo.IsDir(), targetInfo: targetInfo,
		parentInfo: parentInfo, targetHandle: targetHandle, parentHandle: parentHandle,
		prevManaged: prevManaged,
		oldPath:     targetPath + managedTransitionOldSuffix,
		newPath:     targetPath + managedTransitionNewSuffix,
	}
	if err := item.revalidateParent(); err != nil {
		return nil, fmt.Errorf("destination parent changed while discovering %s: %w", relPath, err)
	}
	if err := revalidatePinned(targetPath, targetHandle, targetInfo); err != nil {
		return nil, fmt.Errorf("destination changed while discovering %s: %w", relPath, err)
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
	if err := validateTransitionArtifactsAbsent(targetPath); err != nil {
		return nil, err
	}
	if err := item.revalidateParent(); err != nil {
		return nil, fmt.Errorf("destination parent changed while validating %s: %w", relPath, err)
	}
	if err := revalidatePinned(targetPath, targetHandle, targetInfo); err != nil {
		return nil, fmt.Errorf("destination changed while validating %s: %w", relPath, err)
	}
	item.targetFingerprint, err = fingerprintPinnedPath(targetPath, targetHandle, targetInfo)
	if err != nil {
		return nil, fmt.Errorf("fingerprint destination %s: %w", relPath, err)
	}
	keepHandles = true
	return item, nil
}

func validateManagedDirectory(conflictDir, conflictRel string, prevManaged map[string]bool) ([]string, error) {
	var files, physicalFiles, dirs []string
	err := filepath.WalkDir(conflictDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			dirs = append(dirs, path)
			return nil
		}
		regular, err := dirEntryIsRegular(entry)
		if err != nil {
			return fmt.Errorf("inspect destination entry %s: %w", path, err)
		}
		if !regular {
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

func dirEntryIsRegular(entry os.DirEntry) (bool, error) {
	info, err := entry.Info()
	if err != nil {
		return false, err
	}
	return info.Mode().IsRegular(), nil
}

func (t *managedTypeTransition) stage() (stageErr error) {
	if err := t.createPrivateStage(); err != nil {
		return err
	}
	published := false
	defer func() {
		if stageErr == nil {
			return
		}
		if !published {
			if t.privatePath != "" {
				stageErr = fmt.Errorf("%w; private transition stage retained at %s", stageErr, t.privatePath)
			}
			return
		}
		if err := t.cleanupPrivateStage(); err != nil {
			stageErr = errors.Join(stageErr, err)
		}
	}()

	replacement := filepath.Join(t.privatePath, "replacement")
	if t.sourceIsDir {
		sourceInfo, err := os.Stat(t.sourcePath)
		if err != nil {
			return err
		}
		if err := os.Mkdir(replacement, sourceInfo.Mode().Perm()); err != nil {
			return fmt.Errorf("create private replacement directory for %s: %w", t.relPath, err)
		}
		t.newHandle, err = openPinnedPath(replacement)
		if err != nil {
			return fmt.Errorf("pin private replacement directory for %s: %w", t.relPath, err)
		}
		t.newInfo, err = t.newHandle.Stat()
		if err != nil {
			return fmt.Errorf("stat private replacement directory for %s: %w", t.relPath, err)
		}
		if err := fileutil.CopyDir(t.sourcePath, replacement); err != nil {
			return fmt.Errorf("copy private replacement directory for %s: %w", t.relPath, err)
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
	} else {
		if err := t.copyPrivateFile(replacement); err != nil {
			return fmt.Errorf("copy private replacement file for %s: %w", t.relPath, err)
		}
		t.written = []string{t.relPath}
	}

	var err error
	t.newFingerprint, err = fingerprintPinnedPath(replacement, t.newHandle, t.newInfo)
	if err != nil {
		return fmt.Errorf("fingerprint private replacement for %s: %w", t.relPath, err)
	}
	if err := renameNoReplace(replacement, t.newPath); err != nil {
		return fmt.Errorf("publish staged replacement for %s at %s: %w", t.relPath, t.newPath, err)
	}
	published = true
	if err := revalidatePinned(t.newPath, t.newHandle, t.newInfo); err != nil {
		return fmt.Errorf("published replacement changed for %s at %s: %w", t.relPath, t.newPath, err)
	}
	if err := t.cleanupPrivateStage(); err != nil {
		return fmt.Errorf("published replacement retained at %s; %w", t.newPath, err)
	}
	return nil
}

func (t *managedTypeTransition) createPrivateStage() error {
	path := t.targetPath + managedTransitionStageSuffix
	if err := os.Mkdir(path, 0700); err != nil {
		return fmt.Errorf("create private transition stage for %s at %s: %w", t.relPath, path, err)
	}
	t.privatePath = path
	var err error
	t.privateHandle, err = openPinnedPath(path)
	if err != nil {
		return fmt.Errorf("open private transition stage retained at %s: %w", path, err)
	}
	t.privateInfo, err = t.privateHandle.Stat()
	if err != nil {
		return fmt.Errorf("stat private transition stage retained at %s: %w", path, err)
	}
	if err := revalidatePinned(path, t.privateHandle, t.privateInfo); err != nil {
		return fmt.Errorf("pin private transition stage retained at %s: %w", path, err)
	}
	return nil
}

func (t *managedTypeTransition) copyPrivateFile(replacement string) error {
	source, err := os.Open(t.sourcePath)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	sourceBefore, err := source.Stat()
	if err != nil {
		return err
	}
	if !sourceBefore.Mode().IsRegular() {
		return fmt.Errorf("source is not a regular file")
	}
	t.newHandle, err = os.OpenFile(replacement, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(t.newHandle, source); err != nil {
		return err
	}
	if err := t.newHandle.Chmod(sourceBefore.Mode()); err != nil {
		return err
	}
	if err := t.newHandle.Sync(); err != nil {
		return err
	}
	sourceAfter, err := source.Stat()
	if err != nil || !sameFileMetadata(sourceBefore, sourceAfter) {
		return fmt.Errorf("source changed while staging")
	}
	closeErr := t.newHandle.Close()
	t.newHandle = nil
	if closeErr != nil {
		return closeErr
	}
	t.newHandle, err = openPinnedPath(replacement)
	if err != nil {
		return err
	}
	t.newInfo, err = t.newHandle.Stat()
	return err
}

func (t *managedTypeTransition) cleanupPrivateStage() error {
	if t.privatePath == "" {
		return nil
	}
	if err := revalidatePinned(t.privatePath, t.privateHandle, t.privateInfo); err != nil {
		return fmt.Errorf("private transition stage retained at %s: %w", t.privatePath, err)
	}
	if err := os.Remove(t.privatePath); err != nil {
		return fmt.Errorf("remove private transition stage %s: %w", t.privatePath, err)
	}
	_ = t.privateHandle.Close()
	t.privateHandle = nil
	t.privatePath = ""
	t.privateInfo = nil
	return nil
}

func (ts *managedTypeTransitions) Close() {
	for _, item := range ts.items {
		if item.newHandle != nil {
			_ = item.newHandle.Close()
		}
		if item.targetHandle != nil {
			_ = item.targetHandle.Close()
		}
		if item.parentHandle != nil {
			_ = item.parentHandle.Close()
		}
		if item.privateHandle != nil {
			_ = item.privateHandle.Close()
		}
	}
}

func revalidatePinned(path string, handle *os.File, pinned os.FileInfo) error {
	if handle == nil || pinned == nil {
		return fmt.Errorf("path %s has no pinned identity", path)
	}
	handleInfo, err := handle.Stat()
	if err != nil || !os.SameFile(handleInfo, pinned) {
		return fmt.Errorf("pinned identity changed for %s", path)
	}
	pathInfo, err := os.Lstat(path)
	if err != nil || !os.SameFile(pathInfo, handleInfo) {
		return fmt.Errorf("path identity changed for %s", path)
	}
	return nil
}

func (t *managedTypeTransition) revalidateParent() error {
	return revalidatePinned(filepath.Dir(t.targetPath), t.parentHandle, t.parentInfo)
}

func (ts *managedTypeTransitions) Promote() error {
	for _, item := range ts.items {
		if err := item.revalidateParent(); err != nil {
			return ts.Rollback(fmt.Errorf("destination parent changed before promoting %s: %w", item.relPath, err))
		}
		if err := revalidatePinned(item.targetPath, item.targetHandle, item.targetInfo); err != nil {
			return ts.Rollback(fmt.Errorf("destination changed before promoting %s: %w", item.relPath, err))
		}
		newFingerprint, err := fingerprintPinnedPath(item.newPath, item.newHandle, item.newInfo)
		if err != nil {
			return ts.Rollback(fmt.Errorf("staged replacement changed before promoting %s at %s: %w", item.relPath, item.newPath, err))
		}
		if newFingerprint != item.newFingerprint {
			return ts.Rollback(fmt.Errorf("staged replacement changed before promoting %s at %s", item.relPath, item.newPath))
		}
		if err := renameNoReplace(item.targetPath, item.oldPath); err != nil {
			return ts.Rollback(fmt.Errorf("quarantine destination %s at %s: %w", item.relPath, item.oldPath, err))
		}
		item.quarantined = true
		if !item.sourceIsDir {
			deleted, err := validateManagedDirectory(item.oldPath, item.relPath, item.prevManaged)
			if err != nil {
				return ts.Rollback(fmt.Errorf("destination changed while quarantining %s: %w", item.relPath, err))
			}
			item.deleted = deleted
		}
		if err := renameNoReplace(item.newPath, item.targetPath); err != nil {
			return ts.Rollback(fmt.Errorf("promote replacement %s from %s: %w", item.relPath, item.newPath, err))
		}
		item.promoted = true
	}
	return nil
}

func (ts *managedTypeTransitions) Rollback(cause error) error {
	defer ts.Close()
	var rollbackErrs []error
	for i := len(ts.items) - 1; i >= 0; i-- {
		item := ts.items[i]
		if item.promoted {
			if err := item.revalidateParent(); err != nil {
				rollbackErrs = append(rollbackErrs, err)
				continue
			}
			if err := revalidatePinned(item.targetPath, item.newHandle, item.newInfo); err != nil {
				rollbackErrs = append(rollbackErrs, fmt.Errorf("target %s changed during rollback; preserving it and %s: %w", item.relPath, item.oldPath, err))
				continue
			}
			if err := renameNoReplace(item.targetPath, item.newPath); err != nil {
				rollbackErrs = append(rollbackErrs, fmt.Errorf("quarantine failed replacement %s at %s: %w", item.relPath, item.newPath, err))
				continue
			}
			item.promoted = false
		}
		if item.quarantined {
			if _, err := os.Lstat(item.targetPath); err == nil {
				rollbackErrs = append(rollbackErrs, fmt.Errorf("target %s was concurrently recreated; original retained at %s", item.relPath, item.oldPath))
				continue
			} else if !os.IsNotExist(err) {
				rollbackErrs = append(rollbackErrs, fmt.Errorf("inspect target %s before rollback: %w", item.relPath, err))
				continue
			}
			if err := revalidatePinned(item.oldPath, item.targetHandle, item.targetInfo); err != nil {
				rollbackErrs = append(rollbackErrs, fmt.Errorf("quarantined original changed at %s: %w", item.oldPath, err))
				continue
			}
			if err := renameNoReplace(item.oldPath, item.targetPath); err != nil {
				rollbackErrs = append(rollbackErrs, fmt.Errorf("restore original destination %s from %s: %w", item.relPath, item.oldPath, err))
				continue
			}
			item.quarantined = false
		}
	}
	for _, item := range ts.items {
		if _, err := os.Lstat(item.newPath); err == nil {
			if err := item.removePinnedNew(); err != nil {
				rollbackErrs = append(rollbackErrs, err)
			}
		} else if !os.IsNotExist(err) {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("inspect staged replacement %s: %w", item.newPath, err))
		}
	}
	if len(rollbackErrs) == 0 {
		return cause
	}
	return errors.Join(append([]error{cause}, rollbackErrs...)...)
}

func (ts *managedTypeTransitions) Commit(logger zerolog.Logger) {
	defer ts.Close()
	for _, item := range ts.items {
		if err := item.removePinnedOld(); err != nil {
			logger.Warn().Err(err).Str(log.FieldPath, item.oldPath).Msg("Transition succeeded but quarantined original was preserved")
		}
	}
}

func (t *managedTypeTransition) removePinnedOld() error {
	if err := removePinnedTree(t.oldPath, t.targetHandle, t.targetInfo, t.targetFingerprint, "managed original", nil); err != nil {
		return fmt.Errorf("remove quarantined original %s: %w", t.oldPath, err)
	}
	return nil
}

func (t *managedTypeTransition) removePinnedNew() error {
	if err := removePinnedTree(t.newPath, t.newHandle, t.newInfo, t.newFingerprint, "staged replacement", nil); err != nil {
		return fmt.Errorf("remove staged replacement %s: %w", t.newPath, err)
	}
	return nil
}

type pinnedRemovalEntry struct {
	path   string
	handle *os.File
	info   os.FileInfo
	owned  bool
}

func removePinnedTree(path string, rootHandle *os.File, rootInfo os.FileInfo, expected [sha256.Size]byte, changedLabel string, beforeRemove func()) error {
	entries, err := pinRemovalTree(path, rootHandle, rootInfo)
	if err != nil {
		return err
	}
	defer func() {
		for _, entry := range entries {
			if entry.owned {
				_ = entry.handle.Close()
			}
		}
	}()
	current, err := fingerprintPinnedPath(path, rootHandle, rootInfo)
	if err != nil || current != expected {
		return fmt.Errorf("%s changed at %s", changedLabel, path)
	}
	if beforeRemove != nil {
		beforeRemove()
	}
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if err := revalidatePinned(entry.path, entry.handle, entry.info); err != nil {
			return err
		}
		if err := os.Remove(entry.path); err != nil {
			return fmt.Errorf("remove fingerprinted path %s: %w", entry.path, err)
		}
		if entry.owned {
			_ = entry.handle.Close()
			entries[i].owned = false
		}
	}
	return nil
}

func pinRemovalTree(path string, rootHandle *os.File, rootInfo os.FileInfo) ([]pinnedRemovalEntry, error) {
	entries := []pinnedRemovalEntry{{path: path, handle: rootHandle, info: rootInfo}}
	if !rootInfo.IsDir() {
		return entries, nil
	}
	err := filepath.WalkDir(path, func(entryPath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entryPath == path {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("refuse to remove irregular path %s", entryPath)
		}
		handle, err := openPinnedPath(entryPath)
		if err != nil {
			return err
		}
		opened, err := handle.Stat()
		if err != nil || !os.SameFile(info, opened) {
			_ = handle.Close()
			return fmt.Errorf("path changed while pinning removal: %s", entryPath)
		}
		entries = append(entries, pinnedRemovalEntry{path: entryPath, handle: handle, info: opened, owned: true})
		return nil
	})
	if err != nil {
		for _, entry := range entries {
			if entry.owned {
				_ = entry.handle.Close()
			}
		}
		return nil, err
	}
	return entries, nil
}

func sameFileMetadata(before, after os.FileInfo) bool {
	return before != nil && after != nil && os.SameFile(before, after) && before.Mode() == after.Mode() &&
		before.Size() == after.Size() && before.ModTime().Equal(after.ModTime())
}

func fingerprintPinnedPath(path string, handle *os.File, pinned os.FileInfo) ([sha256.Size]byte, error) {
	var empty [sha256.Size]byte
	if err := revalidatePinned(path, handle, pinned); err != nil {
		return empty, err
	}
	rootBefore, err := handle.Stat()
	if err != nil {
		return empty, err
	}
	digest := sha256.New()
	if rootBefore.Mode().IsRegular() {
		if err := fingerprintFile(digest, managedTargetRoot, handle, rootBefore); err != nil {
			return empty, err
		}
	} else if rootBefore.IsDir() {
		writeFingerprintMetadata(digest, managedTargetRoot, rootBefore)
		if err := filepath.WalkDir(path, func(entryPath string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entryPath == path {
				return nil
			}
			rel, err := filepath.Rel(path, entryPath)
			if err != nil {
				return err
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.IsDir() {
				writeFingerprintMetadata(digest, filepath.ToSlash(rel), info)
				return nil
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("fingerprint entry %s is not regular", entryPath)
			}
			file, err := os.Open(entryPath)
			if err != nil {
				return err
			}
			opened, err := file.Stat()
			if err != nil || !os.SameFile(opened, info) {
				_ = file.Close()
				return fmt.Errorf("fingerprint entry changed before reading: %s", entryPath)
			}
			fingerprintErr := fingerprintFile(digest, filepath.ToSlash(rel), file, opened)
			closeErr := file.Close()
			return errors.Join(fingerprintErr, closeErr)
		}); err != nil {
			return empty, err
		}
	} else {
		return empty, fmt.Errorf("fingerprint root is neither a regular file nor directory: %s", path)
	}
	rootAfter, err := handle.Stat()
	if err != nil || !sameFileMetadata(rootBefore, rootAfter) {
		return empty, fmt.Errorf("fingerprint root changed while reading: %s", path)
	}
	if err := revalidatePinned(path, handle, pinned); err != nil {
		return empty, err
	}
	var fingerprint [sha256.Size]byte
	copy(fingerprint[:], digest.Sum(nil))
	return fingerprint, nil
}

func fingerprintFile(digest hash.Hash, rel string, file *os.File, before os.FileInfo) error {
	writeFingerprintMetadata(digest, rel, before)
	read := io.NewSectionReader(file, 0, before.Size())
	if copied, err := io.Copy(digest, read); err != nil {
		return err
	} else if copied != before.Size() {
		return fmt.Errorf("fingerprint file size changed while reading: %s", rel)
	}
	after, err := file.Stat()
	if err != nil || !sameFileMetadata(before, after) {
		return fmt.Errorf("fingerprint file changed while reading: %s", rel)
	}
	return nil
}

func writeFingerprintMetadata(digest hash.Hash, rel string, info os.FileInfo) {
	size := info.Size()
	if info.IsDir() {
		size = -1
	}
	_, _ = fmt.Fprintf(digest, "%d:%s:%d:%d\n", len(rel), rel, uint32(info.Mode()), size)
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
	defer transitions.Close()
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

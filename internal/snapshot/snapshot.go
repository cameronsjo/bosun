// Package snapshot provides snapshot management for manifest output files.
package snapshot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cameronsjo/bosun/internal/fileutil"
	"github.com/cameronsjo/bosun/internal/log"
	"github.com/google/uuid"
)

const (
	// SnapshotPrefix is the prefix for snapshot directory names.
	SnapshotPrefix = "snapshot-"
	// PreRollbackPrefix is the prefix for the copy of the current output that
	// Restore saves before it swaps a snapshot in.
	PreRollbackPrefix = "pre-rollback-"
	// DateFormat is the timestamp format used in snapshot names (legacy, for parsing).
	DateFormat = "20060102-150405"
	// DateFormatPrecise includes nanoseconds to prevent same-second collisions.
	DateFormatPrecise = "20060102-150405.000000000"
	// MaxSnapshots is the maximum number of snapshots to retain.
	MaxSnapshots = 20
	// MinFreeDiskBytes is the minimum free disk space required (100MB).
	MinFreeDiskBytes = 100 * 1024 * 1024
)

// ErrInvalidName reports a snapshot or backup name that is not a single,
// prefixed path element.
var ErrInvalidName = errors.New("invalid name")

// restorableSuffixes are the output file suffixes GetRestoredFiles reports.
// Compose output is written as <stack>.yml.tmpl, so filepath.Ext alone misses it.
var restorableSuffixes = []string{".yml", ".yaml", ".yml.tmpl", ".yaml.tmpl"}

// ValidateEntryName checks that name is one local path element that starts
// with prefix and has a non-empty remainder. Callers join the name under a
// trusted root, so anything else could resolve outside that root.
func ValidateEntryName(name, prefix string) error {
	switch {
	case name == "":
		return fmt.Errorf("%w %q: empty", ErrInvalidName, name)
	case !filepath.IsLocal(name):
		return fmt.Errorf("%w %q: not a local path", ErrInvalidName, name)
	case strings.ContainsAny(name, `/\`):
		return fmt.Errorf("%w %q: contains a path separator", ErrInvalidName, name)
	case filepath.Clean(name) != name:
		return fmt.Errorf("%w %q: not in clean form", ErrInvalidName, name)
	case !strings.HasPrefix(name, prefix):
		return fmt.Errorf("%w %q: expected prefix %q", ErrInvalidName, name, prefix)
	case name == prefix:
		return fmt.Errorf("%w %q: missing suffix after prefix", ErrInvalidName, name)
	}
	return nil
}

// SnapshotInfo holds metadata about a snapshot.
type SnapshotInfo struct {
	Name      string
	Path      string
	Created   time.Time
	FileCount int
}

type restoreOps struct {
	stat         func(string) (os.FileInfo, error)
	lstat        func(string) (os.FileInfo, error)
	dirSize      func(string) (int64, error)
	diskSpace    func(string, int64) error
	hasContent   func(string) (bool, error)
	mkdirAll     func(string, os.FileMode) error
	copyDir      func(context.Context, string, string) error
	removeAll    func(string) error
	rename       func(string, string) error
	now          func() time.Time
	newRestoreID func() string
	cleanup      func(string) error
}

type cleanupErrors []error

func (errs cleanupErrors) Error() string {
	messages := make([]string, len(errs))
	for i, err := range errs {
		messages[i] = err.Error()
	}
	return strings.Join(messages, "; ")
}

func (errs cleanupErrors) Unwrap() []error {
	return errs
}

func defaultRestoreOps() restoreOps {
	return restoreOps{
		stat:         os.Stat,
		lstat:        os.Lstat,
		dirSize:      getDirSize,
		diskSpace:    checkDiskSpace,
		hasContent:   dirHasContentForRestore,
		mkdirAll:     os.MkdirAll,
		copyDir:      fileutil.CopyDir,
		removeAll:    os.RemoveAll,
		rename:       os.Rename,
		now:          time.Now,
		newRestoreID: func() string { return uuid.New().String()[:8] },
		cleanup:      Cleanup,
	}
}

// snapshotsDir returns the path to the snapshots directory.
func snapshotsDir(manifestDir string) string {
	return filepath.Join(manifestDir, ".bosun", "snapshots")
}

// outputDir returns the path to the output directory.
func outputDir(manifestDir string) string {
	return filepath.Join(manifestDir, "output")
}

// Create creates a snapshot of the current output directory.
// Returns the snapshot name, or an empty string if there was nothing to snapshot.
func Create(manifestDir string) (string, error) {
	start := time.Now()
	logger := log.Component("snapshot")
	outDir := outputDir(manifestDir)

	// Check if output directory exists and has content
	if !dirHasContent(outDir) {
		return "", nil
	}

	logger.Info().
		Str(log.FieldOperation, "create").
		Str(log.FieldPath, manifestDir).
		Msg("Creating snapshot")

	snapDir := snapshotsDir(manifestDir)

	// Check disk space before creating snapshot
	dirSize, err := getDirSize(outDir)
	if err != nil {
		return "", fmt.Errorf("calculate output directory size: %w", err)
	}

	// Ensure snapshots directory exists for disk check
	if err := os.MkdirAll(snapDir, 0755); err != nil {
		return "", fmt.Errorf("create snapshots directory: %w", err)
	}

	// Check available disk space (need at least dirSize + MinFreeDiskBytes)
	requiredSpace := dirSize + MinFreeDiskBytes
	if err := checkDiskSpace(snapDir, requiredSpace); err != nil {
		return "", fmt.Errorf("insufficient disk space for snapshot: %w", err)
	}

	// Create snapshot name with timestamp (nanosecond precision to prevent collisions)
	snapshotName := SnapshotPrefix + time.Now().Format(DateFormatPrecise)
	snapshotPath := filepath.Join(snapDir, snapshotName)

	// Ensure snapshot directory exists
	if err := os.MkdirAll(snapshotPath, 0755); err != nil {
		return "", fmt.Errorf("create snapshot directory: %w", err)
	}

	// Copy output directory contents to snapshot
	if err := fileutil.CopyDir(context.Background(), outDir, snapshotPath); err != nil {
		// Clean up partial snapshot on error
		if cleanupErr := os.RemoveAll(snapshotPath); cleanupErr != nil {
			return "", fmt.Errorf("copy output to snapshot: %w (cleanup also failed: %v)", err, cleanupErr)
		}
		return "", fmt.Errorf("copy output to snapshot: %w", err)
	}

	// Cleanup old snapshots
	if err := Cleanup(manifestDir); err != nil {
		// Log but don't fail on cleanup errors
		log.Warn().Err(err).Str(log.FieldPath, snapDir).Msg("Failed to cleanup old snapshots")
	}

	logger.Info().
		Str(log.FieldOperation, "create").
		Str("snapshot", snapshotName).
		Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
		Msg("Snapshot created successfully")

	return snapshotName, nil
}

// List returns available snapshots sorted by date (newest first).
func List(manifestDir string) ([]SnapshotInfo, error) {
	return listWithPrefix(manifestDir, SnapshotPrefix)
}

// listPreRollback returns the pre-rollback copies Restore saves, newest first.
// They are kept out of List so they never appear as rollback targets.
func listPreRollback(manifestDir string) ([]SnapshotInfo, error) {
	return listWithPrefix(manifestDir, PreRollbackPrefix)
}

func listWithPrefix(manifestDir, prefix string) ([]SnapshotInfo, error) {
	snapDir := snapshotsDir(manifestDir)

	entries, err := os.ReadDir(snapDir)
	if os.IsNotExist(err) {
		return nil, nil // No snapshots directory means no snapshots
	}
	if err != nil {
		return nil, fmt.Errorf("read snapshots directory: %w", err)
	}

	var snapshots []SnapshotInfo
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}

		path := filepath.Join(snapDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			// Log warning for unreadable snapshot but continue with others
			log.Warn().Err(err).Str("snapshot", entry.Name()).Msg("Cannot read snapshot info")
			continue
		}

		// Count files in snapshot
		fileCount := countFiles(path)

		// Parse timestamp from name (try precise format first, then legacy)
		timestamp := strings.TrimPrefix(entry.Name(), prefix)
		created, err := time.Parse(DateFormatPrecise, timestamp)
		if err != nil {
			// Try legacy format without nanoseconds
			created, err = time.Parse(DateFormat, timestamp)
			if err != nil {
				// Use file modification time as fallback
				created = info.ModTime()
			}
		}

		snapshots = append(snapshots, SnapshotInfo{
			Name:      entry.Name(),
			Path:      path,
			Created:   created,
			FileCount: fileCount,
		})
	}

	// Sort by date, newest first
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].Created.After(snapshots[j].Created)
	})

	return snapshots, nil
}

// Restore restores a snapshot atomically, creating a pre-rollback backup first.
// Uses temp directory + atomic rename pattern to prevent broken state on failure.
func Restore(manifestDir, snapshotName string) error {
	return restoreWithOps(manifestDir, snapshotName, defaultRestoreOps())
}

func restoreWithOps(manifestDir, snapshotName string, ops restoreOps) error {
	start := time.Now()
	logger := log.Component("snapshot")
	if err := ValidateEntryName(snapshotName, SnapshotPrefix); err != nil {
		return fmt.Errorf("restore snapshot: %w", err)
	}
	snapDir := snapshotsDir(manifestDir)
	snapshotPath := filepath.Join(snapDir, snapshotName)
	outDir := outputDir(manifestDir)

	logger.Info().
		Str(log.FieldOperation, "restore").
		Str("snapshot", snapshotName).
		Str(log.FieldPath, manifestDir).
		Msg("Restoring snapshot")

	// Verify snapshot exists. Lstat, not Stat: a symlinked snapshot would
	// otherwise be followed, and CopyDir skips a symlinked root, so the
	// restore would replace output with an empty directory.
	snapshotInfo, err := ops.lstat(snapshotPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("snapshot not found: %s", snapshotName)
		}
		return fmt.Errorf("inspect snapshot: %w", err)
	}
	if snapshotInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("snapshot is a symlink: %s", snapshotName)
	}
	if !snapshotInfo.IsDir() {
		return fmt.Errorf("snapshot is not a directory: %s", snapshotName)
	}

	// Check disk space for atomic restore (need space for temp copy)
	snapshotSize, err := ops.dirSize(snapshotPath)
	if err != nil {
		return fmt.Errorf("calculate snapshot size: %w", err)
	}
	if err := ops.diskSpace(filepath.Dir(outDir), snapshotSize+MinFreeDiskBytes); err != nil {
		return fmt.Errorf("insufficient disk space for restore: %w", err)
	}

	// Create pre-rollback backup if output exists
	hasOutputContent, err := ops.hasContent(outDir)
	if err != nil {
		return fmt.Errorf("inspect current output for backup: %w", err)
	}
	if hasOutputContent {
		backupName := PreRollbackPrefix + ops.now().Format(DateFormatPrecise)
		backupPath := filepath.Join(snapDir, backupName)

		if err := ops.mkdirAll(backupPath, 0755); err != nil {
			return fmt.Errorf("create backup directory: %w", err)
		}

		if err := ops.copyDir(context.Background(), outDir, backupPath); err != nil {
			_ = ops.removeAll(backupPath)
			return fmt.Errorf("create pre-rollback backup: %w", err)
		}
	}

	// Atomic restore: copy to temp directory first, then rename
	// Use UUID to prevent race conditions with concurrent restores
	restoreID := ops.newRestoreID()
	tempDir := outDir + ".restore-temp-" + restoreID
	oldDir := outDir + ".restore-old-" + restoreID

	// Step 1: Copy snapshot to temp directory
	if err := ops.mkdirAll(tempDir, 0755); err != nil {
		return fmt.Errorf("create temp restore directory: %w", err)
	}

	if err := ops.copyDir(context.Background(), snapshotPath, tempDir); err != nil {
		_ = ops.removeAll(tempDir)
		return fmt.Errorf("copy snapshot to temp: %w", err)
	}

	// Step 2: Check if output directory exists (even if empty)
	_, statErr := ops.stat(outDir)
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		_ = ops.removeAll(tempDir)
		return fmt.Errorf("inspect current output: %w", statErr)
	}
	outputExists := statErr == nil

	// Step 3: Atomic swap - rename current output to old (if exists)
	if outputExists {
		if err := ops.rename(outDir, oldDir); err != nil {
			_ = ops.removeAll(tempDir)
			return fmt.Errorf("rename current output: %w", err)
		}
	}

	// Step 4: Rename temp to output
	if err := ops.rename(tempDir, outDir); err != nil {
		// Try to restore old directory on failure
		if outputExists {
			if recoverErr := ops.rename(oldDir, outDir); recoverErr != nil {
				// Recovery failed - surface both errors
				_ = ops.removeAll(tempDir) // Best effort cleanup
				return fmt.Errorf("rename temp to output: %w (recovery also failed: %w)", err, recoverErr)
			}
		}
		_ = ops.removeAll(tempDir) // Best effort cleanup
		return fmt.Errorf("rename temp to output: %w", err)
	}

	// Step 5: Cleanup old directory
	if outputExists {
		_ = ops.removeAll(oldDir)
	}

	// Prune both buckets so pre-rollback copies stay bounded. The restore
	// already succeeded, so a pruning failure is a warning, not an error.
	if err := ops.cleanup(manifestDir); err != nil {
		logger.Warn().
			Err(err).
			Str(log.FieldOperation, "restore").
			Str(log.FieldPath, snapDir).
			Msg("Failed to prune snapshots after restore")
	}

	logger.Info().
		Str(log.FieldOperation, "restore").
		Str("snapshot", snapshotName).
		Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
		Msg("Snapshot restored successfully")

	return nil
}

// Cleanup removes snapshots and pre-rollback copies beyond the retention
// limit, keeping MaxSnapshots of each. Continues deleting even if individual
// removals fail, returning a summary of all errors.
func Cleanup(manifestDir string) error {
	return cleanupBuckets(
		manifestDir,
		[]func(string) ([]SnapshotInfo, error){List, listPreRollback},
		removeWithRetry,
	)
}

// cleanupBuckets prunes each bucket independently. A failed bucket does not
// stop the others; every bucket's error is joined into the result.
func cleanupBuckets(
	manifestDir string,
	lists []func(string) ([]SnapshotInfo, error),
	remove func(string, int) error,
) error {
	var errs []error
	for _, list := range lists {
		if err := cleanupWithOps(manifestDir, list, remove); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func cleanupWithOps(
	manifestDir string,
	list func(string) ([]SnapshotInfo, error),
	remove func(string, int) error,
) error {
	logger := log.Component("snapshot")
	snapshots, err := list(manifestDir)
	if err != nil {
		return err
	}

	if len(snapshots) <= MaxSnapshots {
		return nil
	}

	// Remove oldest snapshots (keeping MaxSnapshots)
	// Continue on errors to clean up as many as possible
	toRemove := snapshots[MaxSnapshots:]
	var errs []error
	for _, snap := range toRemove {
		if err := remove(snap.Path, 3); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", snap.Name, err))
		}
	}

	removed := len(toRemove) - len(errs)
	if removed > 0 {
		logger.Debug().
			Str(log.FieldOperation, "cleanup").
			Int("removed", removed).
			Int("kept", MaxSnapshots).
			Msg("Old snapshots removed")
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to remove %d snapshot(s): %w", len(errs), cleanupErrors(errs))
	}

	return nil
}

// GetRestoredFiles returns a list of files in the output directory.
func GetRestoredFiles(manifestDir string) ([]string, error) {
	outDir := outputDir(manifestDir)
	var files []string

	err := filepath.WalkDir(outDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && hasRestorableSuffix(d.Name()) {
			relPath, _ := filepath.Rel(manifestDir, path)
			files = append(files, relPath)
		}
		return nil
	})

	return files, err
}

func hasRestorableSuffix(name string) bool {
	for _, suffix := range restorableSuffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// dirHasContent checks if a directory exists and has at least one file.
func dirHasContent(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	return len(entries) > 0
}

// dirHasContentForRestore distinguishes an absent output directory from an
// unreadable one. Restore must not silently skip its pre-rollback backup when
// it cannot inspect the current output.
func dirHasContentForRestore(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return len(entries) > 0, nil
}

// countFiles counts the number of files in a directory tree.
func countFiles(dir string) int {
	count := 0
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			count++
		}
		return nil
	})
	return count
}

// getDirSize calculates the total size of a directory tree.
func getDirSize(dir string) (int64, error) {
	var size int64
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			size += info.Size()
		}
		return nil
	})
	return size, err
}

// removeWithRetry attempts to remove a directory with retries for transient failures.
func removeWithRetry(path string, maxRetries int) error {
	return removeWithRetryWithOps(path, maxRetries, os.RemoveAll, time.Sleep)
}

func removeWithRetryWithOps(
	path string,
	maxRetries int,
	remove func(string) error,
	sleep func(time.Duration),
) error {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		if err := remove(path); err != nil {
			lastErr = err
			// Short delay before retry (10ms, 20ms, 40ms)
			sleep(time.Duration(10*(1<<i)) * time.Millisecond)
			continue
		}
		return nil
	}
	return lastErr
}

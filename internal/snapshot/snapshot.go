// Package snapshot provides snapshot management for manifest output files.
package snapshot

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/cameronsjo/bosun/internal/fileutil"
	"github.com/google/uuid"
)

const (
	// SnapshotPrefix is the prefix for snapshot directory names.
	SnapshotPrefix = "snapshot-"
	// DateFormat is the timestamp format used in snapshot names (legacy, for parsing).
	DateFormat = "20060102-150405"
	// DateFormatPrecise includes nanoseconds to prevent same-second collisions.
	DateFormatPrecise = "20060102-150405.000000000"
	// MaxSnapshots is the maximum number of snapshots to retain.
	MaxSnapshots = 20
	// MinFreeDiskBytes is the minimum free disk space required (100MB).
	MinFreeDiskBytes = 100 * 1024 * 1024
)

// SnapshotInfo holds metadata about a snapshot.
type SnapshotInfo struct {
	Name      string
	Path      string
	Created   time.Time
	FileCount int
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
	outDir := outputDir(manifestDir)

	// Check if output directory exists and has content
	if !dirHasContent(outDir) {
		return "", nil
	}

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
	if err := fileutil.CopyDir(outDir, snapshotPath); err != nil {
		// Clean up partial snapshot on error
		if cleanupErr := os.RemoveAll(snapshotPath); cleanupErr != nil {
			return "", fmt.Errorf("copy output to snapshot: %w (cleanup also failed: %v)", err, cleanupErr)
		}
		return "", fmt.Errorf("copy output to snapshot: %w", err)
	}

	// Cleanup old snapshots
	if err := Cleanup(manifestDir); err != nil {
		// Log but don't fail on cleanup errors
		fmt.Fprintf(os.Stderr, "warning: failed to cleanup old snapshots: %v\n", err)
	}

	return snapshotName, nil
}

// List returns available snapshots sorted by date (newest first).
func List(manifestDir string) ([]SnapshotInfo, error) {
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
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), SnapshotPrefix) {
			continue
		}

		path := filepath.Join(snapDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			// Log warning for unreadable snapshot but continue with others
			fmt.Fprintf(os.Stderr, "warning: cannot read snapshot %s: %v\n", entry.Name(), err)
			continue
		}

		// Count files in snapshot
		fileCount := countFiles(path)

		// Parse timestamp from name (try precise format first, then legacy)
		timestamp := strings.TrimPrefix(entry.Name(), SnapshotPrefix)
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
	snapDir := snapshotsDir(manifestDir)
	snapshotPath := filepath.Join(snapDir, snapshotName)
	outDir := outputDir(manifestDir)

	// Verify snapshot exists
	if _, err := os.Stat(snapshotPath); os.IsNotExist(err) {
		return fmt.Errorf("snapshot not found: %s", snapshotName)
	}

	// Check disk space for atomic restore (need space for temp copy)
	snapshotSize, err := getDirSize(snapshotPath)
	if err != nil {
		return fmt.Errorf("calculate snapshot size: %w", err)
	}
	if err := checkDiskSpace(filepath.Dir(outDir), snapshotSize+MinFreeDiskBytes); err != nil {
		return fmt.Errorf("insufficient disk space for restore: %w", err)
	}

	// Create pre-rollback backup if output exists
	if dirHasContent(outDir) {
		backupName := "pre-rollback-" + time.Now().Format(DateFormatPrecise)
		backupPath := filepath.Join(snapDir, backupName)

		if err := os.MkdirAll(backupPath, 0755); err != nil {
			return fmt.Errorf("create backup directory: %w", err)
		}

		if err := fileutil.CopyDir(outDir, backupPath); err != nil {
			os.RemoveAll(backupPath)
			return fmt.Errorf("create pre-rollback backup: %w", err)
		}
	}

	// Atomic restore: copy to temp directory first, then rename
	// Use UUID to prevent race conditions with concurrent restores
	restoreID := uuid.New().String()[:8]
	tempDir := outDir + ".restore-temp-" + restoreID
	oldDir := outDir + ".restore-old-" + restoreID

	// Step 1: Copy snapshot to temp directory
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return fmt.Errorf("create temp restore directory: %w", err)
	}

	if err := fileutil.CopyDir(snapshotPath, tempDir); err != nil {
		os.RemoveAll(tempDir)
		return fmt.Errorf("copy snapshot to temp: %w", err)
	}

	// Step 2: Check if output directory exists (even if empty)
	_, statErr := os.Stat(outDir)
	outputExists := statErr == nil

	// Step 3: Atomic swap - rename current output to old (if exists)
	if outputExists {
		if err := os.Rename(outDir, oldDir); err != nil {
			os.RemoveAll(tempDir)
			return fmt.Errorf("rename current output: %w", err)
		}
	}

	// Step 4: Rename temp to output
	if err := os.Rename(tempDir, outDir); err != nil {
		// Try to restore old directory on failure
		if outputExists {
			if recoverErr := os.Rename(oldDir, outDir); recoverErr != nil {
				// Recovery failed - surface both errors
				os.RemoveAll(tempDir) // Best effort cleanup
				return fmt.Errorf("rename temp to output: %w (recovery also failed: %v)", err, recoverErr)
			}
		}
		os.RemoveAll(tempDir) // Best effort cleanup
		return fmt.Errorf("rename temp to output: %w", err)
	}

	// Step 5: Cleanup old directory
	if outputExists {
		os.RemoveAll(oldDir)
	}

	return nil
}

// Cleanup removes snapshots beyond the retention limit.
// Continues deleting even if individual removals fail, returning a summary of all errors.
func Cleanup(manifestDir string) error {
	snapshots, err := List(manifestDir)
	if err != nil {
		return err
	}

	if len(snapshots) <= MaxSnapshots {
		return nil
	}

	// Remove oldest snapshots (keeping MaxSnapshots)
	// Continue on errors to clean up as many as possible
	var errs []string
	for _, snap := range snapshots[MaxSnapshots:] {
		if err := removeWithRetry(snap.Path, 3); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", snap.Name, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to remove %d snapshot(s): %s", len(errs), strings.Join(errs, "; "))
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
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".yml") {
			relPath, _ := filepath.Rel(manifestDir, path)
			files = append(files, relPath)
		}
		return nil
	})

	return files, err
}

// dirHasContent checks if a directory exists and has at least one file.
func dirHasContent(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	return len(entries) > 0
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

// checkDiskSpace checks if there's enough disk space available.
func checkDiskSpace(dir string, requiredBytes int64) error {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		return fmt.Errorf("failed to check disk space: %w", err)
	}

	available := int64(stat.Bavail) * int64(stat.Bsize)
	if available < requiredBytes {
		return fmt.Errorf("need %d bytes, only %d available", requiredBytes, available)
	}
	return nil
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
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		if err := os.RemoveAll(path); err != nil {
			lastErr = err
			// Short delay before retry (10ms, 20ms, 40ms)
			time.Sleep(time.Duration(10*(1<<i)) * time.Millisecond)
			continue
		}
		return nil
	}
	return lastErr
}

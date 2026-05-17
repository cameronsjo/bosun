// Package fileutil provides common file operations.
package fileutil

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/cameronsjo/bosun/internal/log"
)

// ErrSymlinkSkipped is returned by CopyFile when the source is a symlink.
// Callers that walk directories should treat this as "not written" rather than
// a failure.
var ErrSymlinkSkipped = errors.New("symlink skipped")

// warnSymlinkSkipped emits a structured warning when a symlink is encountered
// during a file copy operation and will be skipped.
func warnSymlinkSkipped(path string) {
	linkTarget, readErr := os.Readlink(path)
	l := log.Component(log.ComponentReconcile)
	e := l.Warn().Str(log.FieldPath, path)
	if readErr != nil {
		e.Err(readErr).Msg("Skipping symlink during file copy")
	} else {
		e.Str("target", linkTarget).Msg("Skipping symlink during file copy")
	}
}

// CopyFile copies a single file from src to dst.
// It creates parent directories if needed and preserves permissions.
// Uses atomic write via temp file to prevent partial writes on failure.
// Symlinks are skipped with a warning rather than causing an error.
func CopyFile(src, dst string) error {
	// Check if source is a symlink - Lstat doesn't follow symlinks
	srcLstat, err := os.Lstat(src)
	if err != nil {
		return err // Return unwrapped to preserve os.IsNotExist compatibility
	}
	if srcLstat.Mode()&os.ModeSymlink != 0 {
		warnSymlinkSkipped(src)
		return ErrSymlinkSkipped
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer func() { _ = srcFile.Close() }()

	// Get source file info for permissions.
	srcInfo, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}

	// Create parent directories if needed.
	dstDir := filepath.Dir(dst)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return fmt.Errorf("create parent directories: %w", err)
	}

	// Create temp file in the same directory for atomic rename
	tmpFile, err := os.CreateTemp(dstDir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	// Ensure cleanup on any failure
	success := false
	defer func() {
		if !success {
			_ = tmpFile.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	// Copy content to temp file
	if _, err := io.Copy(tmpFile, srcFile); err != nil {
		return fmt.Errorf("copy content: %w", err)
	}

	// Sync to ensure data is written to disk
	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("sync temp file: %w", err)
	}

	// Close temp file before rename
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	// Set permissions to match source
	if err := os.Chmod(tmpPath, srcInfo.Mode()); err != nil {
		return fmt.Errorf("set permissions: %w", err)
	}

	// Atomic rename to destination
	if err := os.Rename(tmpPath, dst); err != nil {
		return fmt.Errorf("rename to destination: %w", err)
	}

	success = true
	return nil
}

// FileHash computes the SHA-256 hash of a file's contents.
// Returns the hash as a byte slice, or an error if the file cannot be read.
func FileHash(path string) ([sha256.Size]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("hash file: %w", err)
	}

	var sum [sha256.Size]byte
	copy(sum[:], h.Sum(nil))
	return sum, nil
}

// ContentEqual reports whether the file at path has content matching
// the given SHA-256 hash. Returns false if the file does not exist.
func ContentEqual(path string, srcHash [sha256.Size]byte) (bool, error) {
	dstHash, err := FileHash(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return srcHash == dstHash, nil
}

// CopyFileIfChanged copies src to dst only if the content differs.
// Returns true if the file was written, false if skipped (content identical).
// Uses SHA-256 content comparison to avoid unnecessary writes on FUSE filesystems.
// Includes a size-based confidence check to catch FUSE stale-read scenarios where
// the cached hash appears to match but the actual file content has diverged.
func CopyFileIfChanged(src, dst string) (bool, error) {
	srcHash, err := FileHash(src)
	if err != nil {
		return false, fmt.Errorf("hash source: %w", err)
	}

	equal, err := ContentEqual(dst, srcHash)
	if err != nil {
		return false, fmt.Errorf("compare destination: %w", err)
	}
	if equal {
		logger := log.Component(log.ComponentReconcile)
		logger.Debug().
			Str("src", src).
			Str("dst", dst).
			Msg("Content hash matched, verifying file content before skip")

		// Confidence check: compare file sizes to catch FUSE stale-read scenarios.
		// If the kernel-level file sizes differ, the hashes cannot truly match —
		// the FUSE cache served stale content during hash comparison.
		if sizesDiffer(src, dst) {
			logger.Warn().
				Str("src", src).
				Str("dst", dst).
				Msg("FUSE staleness detected: content hash matched but file sizes differ, forcing write")
		} else {
			// Read-back verification: compare raw bytes to catch hash computation
			// bugs or FUSE cache inconsistencies that the size check didn't catch.
			srcBytes, srcErr := os.ReadFile(src)
			dstBytes, dstErr := os.ReadFile(dst)
			if srcErr != nil || dstErr != nil {
				// Read-back failed — log a warning and proceed with copy to be safe.
				// Silently skipping on I/O error could mask disk failures.
				logger.Warn().
					AnErr("src_err", srcErr).
					AnErr("dst_err", dstErr).
					Str("src", src).
					Str("dst", dst).
					Msg("Read-back verification failed, proceeding with copy as precaution")
				// fall through to copy
			} else if !bytes.Equal(srcBytes, dstBytes) {
				logger.Warn().
					Str("src", src).
					Str("dst", dst).
					Msg("FUSE staleness detected: content hash matched but byte comparison differs, forcing write")
				// fall through to copy
			} else {
				logger.Debug().
					Str("src", src).
					Str("dst", dst).
					Str("reason", "hash_match").
					Msg("skipped")
				return false, nil
			}
		}
	}

	if err := CopyFile(src, dst); err != nil {
		if errors.Is(err, ErrSymlinkSkipped) {
			return false, nil
		}
		return false, err
	}

	// Post-write verification: re-read destination hash to confirm the write landed.
	// On FUSE mounts, the atomic rename may not immediately invalidate cached handles.
	verifyLogger := log.Component(log.ComponentReconcile)
	verifyLogger.Debug().Str(log.FieldPath, dst).Msg("Post-write verification: re-reading destination hash")
	dstHash, verifyErr := FileHash(dst)
	if verifyErr != nil {
		return false, fmt.Errorf("post-write verification failed: cannot re-read destination %s: %w", dst, verifyErr)
	} else if dstHash != srcHash {
		return false, fmt.Errorf("post-write verification failed: destination hash mismatch after write (possible FUSE cache staleness): %s", dst)
	}

	dstSize := int64(-1)
	if info, statErr := os.Stat(dst); statErr == nil {
		dstSize = info.Size()
	}
	verifyLogger.Debug().
		Str("src", src).
		Str("dst", dst).
		Int64("bytes", dstSize).
		Msg("wrote")

	return true, nil
}

// sizesDiffer returns true if the two files have different sizes.
// Returns false if either file cannot be stat'd (caller falls through to byte comparison).
func sizesDiffer(a, b string) bool {
	aInfo, err := os.Stat(a)
	if err != nil {
		return false
	}
	bInfo, err := os.Stat(b)
	if err != nil {
		return false
	}
	return aInfo.Size() != bInfo.Size()
}

// CopyDirIfChanged recursively copies a directory from src to dst,
// skipping files whose content has not changed. Returns relative paths
// of files that were actually written.
// Symlinks are skipped with a warning rather than causing an error.
func CopyDirIfChanged(src, dst string) ([]string, error) {
	var written []string
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.Type()&os.ModeSymlink != 0 {
			warnSymlinkSkipped(path)
			return nil
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return fmt.Errorf("calculate relative path: %w", err)
		}
		dstPath := filepath.Join(dst, relPath)

		if d.IsDir() {
			return os.MkdirAll(dstPath, 0755)
		}

		changed, err := CopyFileIfChanged(path, dstPath)
		if err != nil {
			return err
		}
		if changed {
			written = append(written, relPath)
		}
		return nil
	})
	return written, err
}

// CopyDir recursively copies a directory from src to dst.
// Symlinks are skipped with a warning rather than causing an error.
func CopyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Symlinks are skipped: they may reference paths outside the staging dir
		// and are not meaningful for the deploy target.
		if d.Type()&os.ModeSymlink != 0 {
			warnSymlinkSkipped(path)
			return nil
		}

		// Calculate destination path.
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return fmt.Errorf("calculate relative path: %w", err)
		}
		dstPath := filepath.Join(dst, relPath)

		if d.IsDir() {
			return os.MkdirAll(dstPath, 0755)
		}

		return CopyFile(path, dstPath)
	})
}

// Package fileutil provides common file operations.
package fileutil

import (
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/cameronsjo/bosun/internal/log"
)

// warnSymlinkSkipped emits a structured warning when a symlink is encountered
// during a file copy operation and will be skipped.
func warnSymlinkSkipped(path string) {
	linkTarget, _ := os.Readlink(path)
	l := log.Component(log.ComponentReconcile)
	l.Warn().Str(log.FieldPath, path).Str("target", linkTarget).Msg("Skipping symlink during file copy")
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
		return nil
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
		return false, nil
	}

	if err := CopyFile(src, dst); err != nil {
		return false, err
	}
	return true, nil
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

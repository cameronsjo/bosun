// Package fileutil provides common file operations.
package fileutil

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/cameronsjo/bosun/internal/log"
)

// ErrSymlinkSkipped is returned by CopyFile when the source is a symlink.
// Callers that walk directories should treat this as "not written" rather than
// a failure.
var ErrSymlinkSkipped = errors.New("symlink skipped")

// ErrPostWriteVerification marks a copy that was renamed into place but could
// not be verified afterward. Callers must treat the destination as written for
// change tracking while still surfacing the error.
var ErrPostWriteVerification = errors.New("post-write verification failed")

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
func CopyFile(ctx context.Context, src, dst string) error {
	return copyFileWithOps(ctx, src, dst, (*os.File).Chmod, syncDestinationDir)
}

// copyFileWithChmod exposes the permission operation as an explicit dependency
// so its failure ordering can be tested without a package-global seam.
func copyFileWithChmod(src, dst string, chmod func(*os.File, fs.FileMode) error) error {
	return copyFileWithOps(context.Background(), src, dst, chmod, syncDestinationDir)
}

// copyFileWithoutDirSync performs the atomic file replacement while leaving
// destination-directory synchronization to a surrounding batch operation.
func copyFileWithoutDirSync(src, dst string) error {
	return copyFileWithOps(context.Background(), src, dst, (*os.File).Chmod, nil)
}

func copyFileWithoutDirSyncContext(ctx context.Context, src, dst string) error {
	return copyFileWithOps(ctx, src, dst, (*os.File).Chmod, nil)
}

// copyFileWithOps exposes the permission and destination-directory sync
// operations as explicit dependencies. A nil syncParent batches the latter at
// a higher level; the public CopyFile path always supplies one.
func copyFileWithOps(
	ctx context.Context,
	src, dst string,
	chmod func(*os.File, fs.FileMode) error,
	syncParent func(string) error,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

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
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return fmt.Errorf("create parent directories: %w", err)
	}

	// Validate chmod support on a disposable empty file before copying payload
	// bytes. The probe is never used for content: applying a broad or privileged
	// source mode to the real named temp file would let another local user open
	// it before the copy completes and retain that access after a later chmod.
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateCopyPermissions(dstDir, srcInfo.Mode(), os.CreateTemp, chmod, os.Remove); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// Create the private payload temp file in the same directory for atomic
	// rename. It stays at CreateTemp's 0600 mode until the copy is complete.
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
	if _, err := io.Copy(tmpFile, contextReader{ctx: ctx, reader: srcFile}); err != nil {
		return fmt.Errorf("copy content: %w", err)
	}

	// Apply the final source mode only after the payload is complete. This also
	// restores setuid/setgid bits that Unix may clear during payload writes.
	if err := chmod(tmpFile, srcInfo.Mode()); err != nil {
		return fmt.Errorf("set final permissions: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// Sync to ensure data is written to disk
	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("sync temp file: %w", err)
	}

	// Close temp file before rename
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// Atomic rename to destination
	if err := os.Rename(tmpPath, dst); err != nil {
		return fmt.Errorf("rename to destination: %w", err)
	}

	// fsync the destination directory so the rename's directory-entry update
	// is durable and visible to any other process/handle observing this
	// directory — notably a second FUSE handle on Unraid's shfs, which can
	// otherwise keep serving a stale directory listing after an unfsynced
	// rename. Windows has no equivalent directory-fsync semantics, so this
	// is skipped there.
	if syncParent != nil {
		if err := syncParent(dstDir); err != nil {
			return fmt.Errorf("sync destination directory: %w", err)
		}
	}

	success = true
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func validateCopyPermissions(
	dstDir string,
	mode fs.FileMode,
	createTemp func(string, string) (*os.File, error),
	chmod func(*os.File, fs.FileMode) error,
	remove func(string) error,
) error {
	probeFile, err := createTemp(dstDir, ".tmp-perm-*")
	if err != nil {
		return fmt.Errorf("create permission probe: %w", err)
	}
	probePath := probeFile.Name()
	defer func() {
		_ = probeFile.Close()
		_ = remove(probePath)
	}()
	if err := chmod(probeFile, mode); err != nil {
		return fmt.Errorf("set permissions: %w", err)
	}
	if err := probeFile.Close(); err != nil {
		return fmt.Errorf("close permission probe: %w", err)
	}
	if err := remove(probePath); err != nil {
		return fmt.Errorf("remove permission probe: %w", err)
	}
	return nil
}

// syncDir opens dir and calls Sync on it, flushing the directory entry
// (e.g. a rename target) to durable storage.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open directory: %w", err)
	}
	defer func() { _ = d.Close() }()
	return d.Sync()
}

// syncDestinationDir provides CopyFile's portable per-call durability contract.
// Windows has no equivalent directory-fsync semantics.
func syncDestinationDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	return syncDir(dir)
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
// A post-write verification failure returns true together with
// ErrPostWriteVerification because the atomic rename already happened.
// Uses SHA-256 content comparison to avoid unnecessary writes on FUSE filesystems.
// Includes a size-based confidence check to catch FUSE stale-read scenarios where
// the cached hash appears to match but the actual file content has diverged.
func CopyFileIfChanged(ctx context.Context, src, dst string) (bool, error) {
	return copyFileIfChanged(ctx, src, dst, FileHash)
}

// copyFileIfChanged accepts the post-write hash operation explicitly so tests
// can reproduce a verification failure after the atomic rename without a
// package-global fault-injection seam.
func copyFileIfChanged(ctx context.Context, src, dst string, verifyHash func(string) ([sha256.Size]byte, error)) (bool, error) {
	return copyFileIfChangedWithCopy(ctx, src, dst, verifyHash, CopyFile)
}

func copyFileIfChangedWithCopy(
	ctx context.Context,
	src, dst string,
	verifyHash func(string) ([sha256.Size]byte, error),
	copyFile func(context.Context, string, string) error,
) (bool, error) {
	changed, verify, err := copyFileIfChangedDeferredWithCopy(ctx, src, dst, verifyHash, copyFile)
	if err != nil || verify == nil {
		return changed, err
	}
	return changed, verify()
}

type postWriteVerification func() error

func copyFileIfChangedDeferredWithoutDirSync(ctx context.Context, src, dst string) (bool, postWriteVerification, error) {
	return copyFileIfChangedDeferredWithCopy(ctx, src, dst, FileHash, copyFileWithoutDirSyncContext)
}

func copyFileIfChangedDeferredWithCopy(
	ctx context.Context,
	src, dst string,
	verifyHash func(string) ([sha256.Size]byte, error),
	copyFile func(context.Context, string, string) error,
) (bool, postWriteVerification, error) {
	if err := ctx.Err(); err != nil {
		return false, nil, err
	}
	srcHash, err := FileHash(src)
	if err != nil {
		return false, nil, fmt.Errorf("hash source: %w", err)
	}

	equal, err := ContentEqual(dst, srcHash)
	if err != nil {
		return false, nil, fmt.Errorf("compare destination: %w", err)
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
				return false, nil, nil
			}
		}
	}

	// Capture source size once for the "wrote" log; src and dst sizes match
	// after a successful hash-verified copy, so an extra stat(dst) is wasteful.
	var srcSize int64 = -1
	if info, statErr := os.Stat(src); statErr == nil {
		srcSize = info.Size()
	}

	if err := ctx.Err(); err != nil {
		return false, nil, err
	}
	if err := copyFile(ctx, src, dst); err != nil {
		if errors.Is(err, ErrSymlinkSkipped) {
			return false, nil, nil
		}
		return false, nil, err
	}

	return true, func() error {
		// Re-read only after the destination parent has been synchronized. On
		// FUSE mounts, an unsynchronized rename may remain stale or invisible
		// through the verification handle.
		verifyLogger := log.Component(log.ComponentReconcile)
		verifyLogger.Debug().Str(log.FieldPath, dst).Msg("Post-write verification: re-reading destination hash")
		dstHash, verifyErr := verifyHash(dst)
		if verifyErr != nil {
			return fmt.Errorf("%w: cannot re-read destination %s: %w", ErrPostWriteVerification, dst, verifyErr)
		} else if dstHash != srcHash {
			return fmt.Errorf("%w: destination hash mismatch after write (possible FUSE cache staleness): %s", ErrPostWriteVerification, dst)
		}

		verifyLogger.Debug().
			Str("src", src).
			Str("dst", dst).
			Int64("bytes", srcSize).
			Msg("wrote")
		return nil
	}, nil
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

var errCopyDestinationWithinSource = errors.New("copy destination must not be the source or its descendant")

// canonicalPathForContainment resolves symlinks through the nearest existing
// ancestor, then rejoins any missing suffix. Copy destinations commonly do not
// exist yet, but a symlinked parent must still participate in containment checks.
func canonicalPathForContainment(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}

	current := filepath.Clean(absPath)
	var missing []string
	for {
		_, err := os.Lstat(current)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("inspect path %s: %w", current, err)
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("find existing ancestor for %s", absPath)
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}

	resolved, err := filepath.EvalSymlinks(current)
	if err != nil {
		return "", fmt.Errorf("resolve path %s: %w", current, err)
	}
	for i := len(missing) - 1; i >= 0; i-- {
		resolved = filepath.Join(resolved, missing[i])
	}
	return filepath.Clean(resolved), nil
}

// destinationHasSourceAncestor detects equal or nested paths by file identity.
// This supplements filepath.Rel on case-insensitive filesystems, where distinct
// path spellings can refer to the same source directory.
func destinationHasSourceAncestor(src, dst string) (bool, error) {
	return destinationHasSourceAncestorWithStat(src, dst, os.Stat)
}

func destinationHasSourceAncestorWithStat(
	src, dst string,
	stat func(string) (fs.FileInfo, error),
) (bool, error) {
	srcInfo, err := stat(src)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("inspect source %s: %w", src, err)
	}

	current := dst
	for {
		info, err := stat(current)
		if err == nil {
			if os.SameFile(srcInfo, info) {
				return true, nil
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return false, fmt.Errorf("inspect destination ancestor %s: %w", current, err)
		}

		parent := filepath.Dir(current)
		if parent == current {
			return false, nil
		}
		current = parent
	}
}

// validateCopyRoots rejects the recursive-copy shape before any destination
// mutation. File identity covers case-insensitive aliases, while filepath.Rel
// keeps the lexical check component-aware so siblings such as source
// "/config/app" and destination "/config/application" remain valid.
func validateCopyRoots(src, dst string) error {
	canonicalSrc, err := canonicalPathForContainment(src)
	if err != nil {
		return fmt.Errorf("resolve copy source: %w", err)
	}
	canonicalDst, err := canonicalPathForContainment(dst)
	if err != nil {
		return fmt.Errorf("resolve copy destination: %w", err)
	}
	if !strings.EqualFold(filepath.VolumeName(canonicalSrc), filepath.VolumeName(canonicalDst)) {
		return nil
	}

	overlapsByIdentity, err := destinationHasSourceAncestor(canonicalSrc, canonicalDst)
	if err != nil {
		return fmt.Errorf("compare copy source and destination identities: %w", err)
	}

	rel, err := filepath.Rel(canonicalSrc, canonicalDst)
	if err != nil {
		return fmt.Errorf("compare copy source and destination: %w", err)
	}
	if overlapsByIdentity || rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return fmt.Errorf("%w: source %s, destination %s", errCopyDestinationWithinSource, src, dst)
	}
	return nil
}

// CopyDirIfChanged recursively copies a directory from src to dst,
// skipping files whose content has not changed. Returns relative paths
// of files that were actually written and descendant directories that were
// created. The destination root itself is never returned. A final file whose
// atomic rename succeeded before post-write verification failed is returned.
// Changed destination parents are synchronized once each after the walk,
// before post-write verification. Both steps still run for completed renames
// when a later walk or copy operation fails.
// Symlinks are skipped with a warning rather than causing an error. A destination
// at or below the source is rejected before the destination is changed.
// Cancellation stops the walk before its next destination mutation; completed
// atomic renames are still synchronized and verified before returning.
func CopyDirIfChanged(ctx context.Context, src, dst string) ([]string, error) {
	return copyDirIfChangedWithOps(ctx, src, dst, copyFileIfChangedDeferredWithoutDirSync, syncDestinationDir)
}

func copyDirIfChangedWithOps(
	ctx context.Context,
	src, dst string,
	copyFile func(context.Context, string, string) (bool, postWriteVerification, error),
	syncParent func(string) error,
) ([]string, error) {
	if err := validateCopyRoots(src, dst); err != nil {
		return nil, err
	}

	var written []string
	var verifications []postWriteVerification
	changedParents := make(map[string]struct{})
	walkErr := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
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
			// The root may have missing ancestors when CopyDirIfChanged is used
			// directly. It is deployment plumbing rather than a source-tree
			// change, so create it without adding "." to the returned paths.
			if relPath == "." {
				return os.MkdirAll(dstPath, 0755)
			}

			created, err := mkdirIfMissing(dstPath, 0755)
			if created {
				written = append(written, relPath)
			}
			return err
		}

		changed, verify, err := copyFile(ctx, path, dstPath)
		if changed {
			written = append(written, relPath)
			changedParents[filepath.Dir(dstPath)] = struct{}{}
			if verify != nil {
				verifications = append(verifications, verify)
			}
		}
		if err != nil {
			return err
		}
		return nil
	})
	flushErr := syncParentDirs(changedParents, syncParent)
	verifyErr := runPostWriteVerifications(verifications)
	return written, joinErrorsPreservingSingles(walkErr, flushErr, verifyErr)
}

func runPostWriteVerifications(verifications []postWriteVerification) error {
	verifyErrs := make([]error, 0, len(verifications))
	for _, verify := range verifications {
		if err := verify(); err != nil {
			verifyErrs = append(verifyErrs, err)
		}
	}
	return joinErrorsPreservingSingles(verifyErrs...)
}

// syncParentDirs synchronizes changed destination parents in lexical order.
// Every parent is attempted so one failing filesystem does not prevent already
// completed renames in other parents from receiving their durability flush.
func syncParentDirs(parents map[string]struct{}, syncParent func(string) error) error {
	if syncParent == nil {
		return nil
	}

	dirs := make([]string, 0, len(parents))
	for dir := range parents {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)

	var syncErrs []error
	for _, dir := range dirs {
		if err := syncParent(dir); err != nil {
			syncErrs = append(syncErrs, fmt.Errorf("sync destination directory %s: %w", dir, err))
		}
	}
	return joinErrorsPreservingSingles(syncErrs...)
}

func joinErrorsPreservingSingles(errs ...error) error {
	nonNil := make([]error, 0, len(errs))
	for _, err := range errs {
		if err != nil {
			nonNil = append(nonNil, err)
		}
	}
	if len(nonNil) == 0 {
		return nil
	}
	if len(nonNil) == 1 {
		return nonNil[0]
	}
	return errors.Join(nonNil...)
}

// mkdirIfMissing creates path and reports whether this call created it. WalkDir
// visits parents before children, so every descendant's parent already exists.
// A concurrent creator is treated as an existing directory, while a file or
// another non-directory entry still surfaces os.Mkdir's collision error.
func mkdirIfMissing(path string, mode fs.FileMode) (bool, error) {
	return mkdirIfMissingWithOps(path, mode, os.Mkdir, os.Lstat)
}

func mkdirIfMissingWithOps(
	path string,
	mode fs.FileMode,
	mkdir func(string, fs.FileMode) error,
	inspect func(string) (fs.FileInfo, error),
) (bool, error) {
	err := mkdir(path, mode)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, fs.ErrExist) {
		return false, err
	}

	info, inspectErr := inspect(path)
	if inspectErr != nil {
		return false, inspectErr
	}
	if !info.IsDir() {
		return false, err
	}
	return false, nil
}

// CopyDir recursively copies a directory from src to dst.
// Destination parents are synchronized once each after the walk, including
// when a later walk or copy operation fails.
// Symlinks are skipped with a warning rather than causing an error. A destination
// at or below the source is rejected before the destination is changed.
// Cancellation stops the walk before its next destination mutation; completed
// atomic renames are still synchronized before returning.
func CopyDir(ctx context.Context, src, dst string) error {
	return copyDirWithOps(ctx, src, dst, copyFileWithoutDirSyncContext, syncDestinationDir)
}

func copyDirWithOps(
	ctx context.Context,
	src, dst string,
	copyFile func(context.Context, string, string) error,
	syncParent func(string) error,
) error {
	if err := validateCopyRoots(src, dst); err != nil {
		return err
	}

	changedParents := make(map[string]struct{})
	walkErr := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
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

		if err := copyFile(ctx, path, dstPath); err != nil {
			return err
		}
		changedParents[filepath.Dir(dstPath)] = struct{}{}
		return nil
	})
	return joinErrorsPreservingSingles(walkErr, syncParentDirs(changedParents, syncParent))
}

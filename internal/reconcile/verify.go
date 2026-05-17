package reconcile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Sentinel errors returned by verifyDeployTarget. Tests and callers use
// errors.Is to distinguish the failure modes from incidental I/O errors.
var (
	// ErrDeployInvariantEmptyWrite indicates a deploy target's source staging
	// directory has regular files but the target's WrittenFiles slice is empty.
	// This is the silent-success signature from GH#214: CopyDirIfChanged
	// returned no writes against stale destination content.
	ErrDeployInvariantEmptyWrite = errors.New("deploy invariant: source has files but no writes recorded")

	// ErrDeployInvariantStaleMtime indicates a file recorded as written has a
	// modification time older than the reconcile start time at the destination.
	// Either the write silently failed or another process touched the file
	// without going through the deploy path.
	ErrDeployInvariantStaleMtime = errors.New("deploy invariant: destination file has stale mtime")

	// ErrDeployInvariantMissingFile indicates a file recorded as written does
	// not exist at the destination. Either the write was rolled back, the
	// filesystem dropped it, or WrittenFiles is recording paths that never
	// landed on disk.
	ErrDeployInvariantMissingFile = errors.New("deploy invariant: destination file missing")
)

// verifyDeployTarget enforces the post-deploy invariants for a single deploy
// target (Layer 1.3 of #214).
//
// Inputs:
//   - src: absolute path to the source. For directory targets this is the
//     staging directory; for single-file targets this is the source file.
//   - dst: absolute destination path. Mirrors src's shape: directory target →
//     destination directory; file target → destination file.
//   - writtenRel: paths recorded in DeployResult.WrittenFiles for this target,
//     captured BEFORE PrefixLatest renames them. For directory targets these
//     are relative to src/dst; for file targets it contains the destination
//     filename (filepath.Base).
//   - startTime: the reconcile start time. Any destination file with an
//     earlier mtime is treated as stale.
//
// Returns nil when the invariants hold. Returns one of the sentinel errors
// wrapped with context when they don't.
//
// Three invariants are enforced:
//
//  1. Empty WrittenFiles against a non-empty source is an error
//     (ErrDeployInvariantEmptyWrite). This is the silent-success signature
//     from #214.
//  2. Every destination listed in writtenRel must exist
//     (ErrDeployInvariantMissingFile).
//  3. Every destination must have mtime >= startTime
//     (ErrDeployInvariantStaleMtime).
func verifyDeployTarget(src, dst string, writtenRel []string, startTime time.Time) error {
	if len(writtenRel) == 0 {
		hasFiles, err := dirHasRegularFiles(src)
		if err != nil {
			return fmt.Errorf("inspect source %q: %w", src, err)
		}
		if hasFiles {
			return fmt.Errorf("%w: src=%q dst=%q", ErrDeployInvariantEmptyWrite, src, dst)
		}
		return nil
	}

	for _, rel := range writtenRel {
		path := filepath.Join(dst, rel)
		info, err := os.Stat(path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("%w: path=%q", ErrDeployInvariantMissingFile, path)
			}
			return fmt.Errorf("stat destination %q: %w", path, err)
		}
		// Truncate to seconds — some filesystems (notably FAT, and Unraid's
		// FUSE layer historically) have second-level mtime resolution, so a
		// write within the same second as startTime can otherwise read as
		// "before" by a few hundred microseconds.
		mt := info.ModTime().Truncate(time.Second)
		st := startTime.Truncate(time.Second)
		if mt.Before(st) {
			return fmt.Errorf("%w: path=%q mtime=%s start=%s", ErrDeployInvariantStaleMtime, path, info.ModTime(), startTime)
		}
	}
	return nil
}

// dirHasRegularFiles reports whether src contains at least one regular file.
// Walks src recursively because nested-only-empty-dirs should count as empty.
// If src is itself a regular file, returns true. If src does not exist,
// returns (false, nil) — the caller will already have handled the missing-src
// case higher up; here we just say "no files to compare against."
func dirHasRegularFiles(src string) (bool, error) {
	info, err := os.Stat(src)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if !info.IsDir() {
		return info.Mode().IsRegular(), nil
	}

	found := false
	walkErr := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	if walkErr != nil {
		return false, walkErr
	}
	return found, nil
}

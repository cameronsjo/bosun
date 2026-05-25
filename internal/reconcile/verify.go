package reconcile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/cameronsjo/bosun/internal/fileutil"
	"github.com/cameronsjo/bosun/internal/log"
)

// Sentinel errors returned by verifyDeployTarget. Callers branch via errors.Is.
var (
	ErrDeployInvariantEmptyWrite  = errors.New("deploy invariant: source has files but no writes recorded")
	ErrDeployInvariantStaleMtime  = errors.New("deploy invariant: destination file has stale mtime")
	ErrDeployInvariantMissingFile = errors.New("deploy invariant: destination file missing")
)

// verifyDeployTarget asserts every entry in writtenRel exists at dst with
// mtime >= startTime. writtenRel paths are joined under dst — for directory
// targets dst is the destination directory; for file targets dst is its parent
// dir.
//
// When writtenRel is empty, the deploy recorded zero writes. This is NOT
// inherently a failure: a content-hash sync legitimately writes nothing when
// the destination already byte-matches the source (a no-op), and the standard
// rename-aside path leaves dst content-identical to src without populating
// writtenRel. So instead of inferring corruption from the write counter, verify
// on-disk truth — every regular file in src must exist and be content-equal at
// dst. ErrDeployInvariantEmptyWrite fires only for a genuine silent-sync
// failure (a non-empty source whose files are missing or differ at dst), which
// is the failure GH#214 added this guard for; a satisfied no-op passes (GH#330).
func verifyDeployTarget(src, dst string, writtenRel []string, startTime time.Time) error {
	logger := log.Component(log.ComponentReconcile)
	logger.Debug().
		Str(log.FieldPath, src).
		Str("destination", dst).
		Int("written_count", len(writtenRel)).
		Msg("Preparing to verify deploy target invariant")

	if len(writtenRel) == 0 {
		sawFiles, matched, err := destinationSatisfiesSource(src, dst)
		if err != nil {
			logger.Error().Err(err).
				Str(log.FieldPath, src).
				Str("destination", dst).
				Msg("Failed to verify deploy target. Reason: cannot compare source against destination")
			return fmt.Errorf("compare source %q against destination %q: %w", src, dst, err)
		}
		if !sawFiles {
			logger.Debug().Msg("Deploy target verification passed: source has no regular files to deploy")
			return nil
		}
		if !matched {
			logger.Error().
				Str(log.FieldPath, src).
				Str("destination", dst).
				Msg("Failed to verify deploy target. Reason: zero writes recorded and destination does not content-match source (silent-sync failure)")
			return fmt.Errorf("%w: src=%q dst=%q", ErrDeployInvariantEmptyWrite, src, dst)
		}
		logger.Debug().Msg("Deploy target verification passed: no-op sync, destination already content-matches source")
		return nil
	}

	for _, rel := range writtenRel {
		path := filepath.Join(dst, rel)
		info, err := os.Stat(path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				logger.Error().
					Str(log.FieldPath, path).
					Msg("Failed to verify deploy target. Reason: written file missing from destination")
				return fmt.Errorf("%w: path=%q", ErrDeployInvariantMissingFile, path)
			}
			logger.Error().Err(err).Str(log.FieldPath, path).Msg("Failed to verify deploy target. Reason: cannot stat destination")
			return fmt.Errorf("stat destination %q: %w", path, err)
		}
		// Truncate to seconds — some filesystems (notably FAT, and Unraid's
		// FUSE layer historically) have second-level mtime resolution, so a
		// write within the same second as startTime can otherwise read as
		// "before" by a few hundred microseconds.
		mt := info.ModTime().Truncate(time.Second)
		st := startTime.Truncate(time.Second)
		if mt.Before(st) {
			logger.Error().
				Str(log.FieldPath, path).
				Time("file_mtime", info.ModTime()).
				Time("start_time", startTime).
				Msg("Failed to verify deploy target. Reason: file has stale mtime")
			return fmt.Errorf("%w: path=%q mtime=%s start=%s", ErrDeployInvariantStaleMtime, path, info.ModTime(), startTime)
		}
	}

	logger.Info().Int("written_count", len(writtenRel)).Msg("Successfully verified deploy target invariant")
	return nil
}

// dirHasRegularFiles reports whether src is, or recursively contains, at
// least one regular file. Returns (false, nil) for a missing path.
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

// destinationSatisfiesSource reports whether the destination already mirrors the
// source when the deploy recorded zero writes. It is the on-disk truth check
// behind the empty-write invariant: rather than treating "source has files but
// no writes recorded" as corruption, it confirms each regular file in src has a
// content-equal counterpart at dst.
//
// Symlinks are skipped (Lstat semantics) to match CopyDir/CopyDirIfChanged,
// which never deploy them — so a symlink-only source imposes no requirement.
// Content equality reuses fileutil.FileHash/ContentEqual, the same primitives
// CopyFileIfChanged uses to decide a write is skippable, so "skipped because
// equal" and "verified present" share one definition of equal.
//
// Returns:
//   - sawFiles: whether src contained at least one regular file (Lstat).
//   - matched:  whether every such file is present and content-equal at dst.
//
// For directory sources, dst is the parallel destination directory. For
// single-file sources, dst is the destination's parent directory and the file
// keeps its basename (discovery preserves the filename across the appdata
// rebase).
func destinationSatisfiesSource(src, dst string) (sawFiles bool, matched bool, err error) {
	info, statErr := os.Lstat(src)
	if statErr != nil {
		if errors.Is(statErr, fs.ErrNotExist) {
			return false, true, nil // nothing staged → nothing to verify
		}
		return false, false, statErr
	}

	// Single-file source: a symlink or other irregular file imposes no write
	// requirement (it would have been skipped during deploy).
	if !info.IsDir() {
		if !info.Mode().IsRegular() {
			return false, true, nil
		}
		eq, cmpErr := filesContentEqual(src, filepath.Join(dst, filepath.Base(src)))
		if cmpErr != nil {
			return true, false, cmpErr
		}
		return true, eq, nil
	}

	// Directory source: every regular file must content-match at the parallel
	// destination path. One mismatch is enough to fail the invariant.
	allMatched := true
	walkErr := filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.Type().IsRegular() { // skips directories and symlinks (Lstat semantics)
			return nil
		}
		sawFiles = true
		rel, relErr := filepath.Rel(src, path)
		if relErr != nil {
			return relErr
		}
		eq, cmpErr := filesContentEqual(path, filepath.Join(dst, rel))
		if cmpErr != nil {
			return cmpErr
		}
		if !eq {
			allMatched = false
			return fs.SkipAll
		}
		return nil
	})
	if walkErr != nil {
		return sawFiles, false, walkErr
	}
	return sawFiles, allMatched, nil
}

// filesContentEqual reports whether the files at src and dst have identical
// content. A missing dst is reported as not-equal (false, nil), never an error.
func filesContentEqual(src, dst string) (bool, error) {
	srcHash, err := fileutil.FileHash(src)
	if err != nil {
		return false, fmt.Errorf("hash source %q: %w", src, err)
	}
	return fileutil.ContentEqual(dst, srcHash)
}

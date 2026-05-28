package reconcile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/cameronsjo/bosun/internal/log"
)

// Sentinel errors returned by verifyDeployTarget. Callers branch via errors.Is.
var (
	ErrDeployInvariantEmptyWrite  = errors.New("deploy invariant: source has files but no writes recorded")
	ErrDeployInvariantStaleMtime  = errors.New("deploy invariant: destination file has stale mtime")
	ErrDeployInvariantMissingFile = errors.New("deploy invariant: destination file missing")
)

// verifyDeployTarget asserts every entry in writtenRel exists at dst with
// mtime >= startTime, and that an empty writtenRel against a non-empty src is
// an error. writtenRel paths are joined under dst — for directory targets dst
// is the destination directory; for file targets dst is its parent dir.
func verifyDeployTarget(src, dst string, writtenRel []string, startTime time.Time) error {
	logger := log.Component(log.ComponentReconcile)
	logger.Debug().
		Str(log.FieldPath, src).
		Str("destination", dst).
		Int("written_count", len(writtenRel)).
		Msg("Preparing to verify deploy target invariant")

	if len(writtenRel) == 0 {
		hasFiles, err := dirHasRegularFiles(src)
		if err != nil {
			logger.Error().Err(err).Str(log.FieldPath, src).Msg("Failed to verify deploy target. Reason: cannot inspect source")
			return fmt.Errorf("inspect source %q: %w", src, err)
		}
		if !hasFiles {
			logger.Debug().Msg("Deploy target verification passed: empty source and no files written")
			return nil
		}
		// Source has files but the sync recorded zero writes. With content-hash
		// sync this is the legitimate no-op case: the destination already
		// byte-matches the source, so CopyDirIfChanged correctly skipped every
		// file (GH#330). Distinguish that from a genuine silent-sync failure by
		// inspecting the destination — existence only, no mtime check, since the
		// files were written on a prior run. Fail only when files are missing.
		missing, err := firstMissingSourceFile(src, dst)
		if err != nil {
			logger.Error().Err(err).Str(log.FieldPath, dst).Msg("Failed to verify deploy target. Reason: cannot inspect destination")
			return fmt.Errorf("inspect destination %q: %w", dst, err)
		}
		if missing != "" {
			logger.Error().
				Str(log.FieldPath, src).
				Str("destination", dst).
				Str("missing", missing).
				Msg("Failed to verify deploy target. Reason: source file missing from destination after zero-write sync")
			return fmt.Errorf("%w: src=%q dst=%q missing=%q", ErrDeployInvariantEmptyWrite, src, dst, missing)
		}
		logger.Debug().
			Str("destination", dst).
			Msg("Deploy target verification passed: no-op sync, destination already content-matches source")
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

// firstMissingSourceFile reports the first regular file under src that has no
// counterpart at dst, or "" if every source file is present. It mirrors how
// the deploy maps source files onto the destination: a directory source maps
// each file to its same relative path under dst; a single-file source maps to
// dst/<basename>. Only existence is checked — callers use this for the no-op
// (zero-write) sync case, where matching files were written on a prior run and
// therefore carry old mtimes. A missing source is treated as "nothing missing".
func firstMissingSourceFile(src, dst string) (string, error) {
	info, err := os.Stat(src)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", err
	}

	if !info.IsDir() {
		target := filepath.Join(dst, filepath.Base(src))
		if _, err := os.Stat(target); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return target, nil
			}
			return "", err
		}
		return "", nil
	}

	var missing string
	walkErr := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, relErr := filepath.Rel(src, path)
		if relErr != nil {
			return relErr
		}
		target := filepath.Join(dst, rel)
		if _, statErr := os.Stat(target); statErr != nil {
			if errors.Is(statErr, fs.ErrNotExist) {
				missing = target
				return fs.SkipAll
			}
			return statErr
		}
		return nil
	})
	if walkErr != nil {
		return "", walkErr
	}
	return missing, nil
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

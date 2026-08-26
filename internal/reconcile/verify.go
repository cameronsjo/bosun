package reconcile

import (
	"context"
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
	ErrDeployInvariantEmptyWrite  = errors.New("deploy invariant: destination regular file missing or content-different without a recorded regular-file write")
	ErrDeployInvariantStaleMtime  = errors.New("deploy invariant: destination path has stale mtime")
	ErrDeployInvariantMissingFile = errors.New("deploy invariant: destination path missing")
	ErrDeployInvariantWrongType   = errors.New("deploy invariant: destination path has wrong type")
)

// verifyDeployTarget asserts every entry in writtenRel exists at dst with
// mtime >= startTime. expectedSourceType is the source's file type captured
// immediately before sync; retaining it makes a zero-write verification fail
// closed if the source disappears or changes type during sync. writtenRel paths
// are joined under dst — for directory targets dst is the destination
// directory; for file targets dst is its parent dir.
//
// When writtenRel contains no regular-file writes (it may be empty or contain
// only newly created directories), that is NOT inherently a failure:
// content-hash sync writes no file when the destination already byte-matches
// the source. Rather than inferring success or corruption from the path count,
// verify on-disk truth — every regular file in src must be present and
// content-equal at dst. ErrDeployInvariantEmptyWrite fires only for a genuine
// silent-sync failure (a destination regular file is missing or differs from
// its source), the failure GH#214 added this guard for; a satisfied no-op
// passes (GH#330).
func verifyDeployTarget(src, dst string, writtenRel []string, startTime time.Time, expectedSourceType fs.FileMode) error {
	logger := log.Component(log.ComponentReconcile)
	logger.Debug().
		Str(log.FieldPath, src).
		Str("destination", dst).
		Int("written_path_count", len(writtenRel)).
		Msg("Preparing to verify deploy target invariant")

	sourceRoot, sourceErr := os.Lstat(src)
	if sourceErr != nil && !errors.Is(sourceErr, fs.ErrNotExist) {
		return fmt.Errorf("stat deploy source %q: %w", src, sourceErr)
	}
	sourceIsDir := sourceErr == nil && sourceRoot.IsDir()
	wroteRegularFile := false

	for _, rel := range writtenRel {
		sourceEntryIsDir := false
		sourceEntryIsRegular := false
		if sourceIsDir {
			sourcePath := filepath.Join(src, rel)
			sourceInfo, err := os.Lstat(sourcePath)
			if err != nil {
				return fmt.Errorf("stat written source path %q: %w", sourcePath, err)
			}
			sourceEntryIsDir = sourceInfo.IsDir()
			sourceEntryIsRegular = sourceInfo.Mode().IsRegular()
			if !sourceEntryIsDir && !sourceEntryIsRegular {
				return fmt.Errorf("written source path %q has unsupported type %s", sourcePath, sourceInfo.Mode().Type())
			}
			wroteRegularFile = wroteRegularFile || sourceEntryIsRegular
		} else {
			// Single-file targets record filepath.Base(src). A reported write without
			// a regular source entry cannot establish the required source/destination
			// type match, so fail closed instead of accepting the destination alone.
			if sourceErr != nil {
				return fmt.Errorf("stat written source target %q: %w", src, sourceErr)
			}
			if !sourceRoot.Mode().IsRegular() {
				return fmt.Errorf("written source target %q has unsupported type %s", src, sourceRoot.Mode().Type())
			}
			wroteRegularFile = true
			sourceEntryIsRegular = true
		}

		path := filepath.Join(dst, rel)
		info, err := os.Lstat(path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				logger.Error().
					Str(log.FieldPath, path).
					Msg("Failed to verify deploy target. Reason: written path missing from destination")
				return fmt.Errorf("%w: path=%q", ErrDeployInvariantMissingFile, path)
			}
			logger.Error().Err(err).Str(log.FieldPath, path).Msg("Failed to verify deploy target. Reason: cannot stat destination")
			return fmt.Errorf("stat destination %q: %w", path, err)
		}
		if sourceEntryIsDir && !info.IsDir() {
			logger.Error().Str(log.FieldPath, path).Msg("Failed to verify deploy target. Reason: written directory has wrong destination type")
			return fmt.Errorf("%w: path=%q want=directory got=%s", ErrDeployInvariantWrongType, path, info.Mode().Type())
		}
		if sourceEntryIsRegular && !info.Mode().IsRegular() {
			logger.Error().Str(log.FieldPath, path).Msg("Failed to verify deploy target. Reason: written file has wrong destination type")
			return fmt.Errorf("%w: path=%q want=regular-file got=%s", ErrDeployInvariantWrongType, path, info.Mode().Type())
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
				Time("path_mtime", info.ModTime()).
				Time("start_time", startTime).
				Msg("Failed to verify deploy target. Reason: path has stale mtime")
			return fmt.Errorf("%w: path=%q mtime=%s start=%s", ErrDeployInvariantStaleMtime, path, info.ModTime(), startTime)
		}
	}

	if !wroteRegularFile {
		if sourceErr != nil {
			logger.Error().
				Str(log.FieldPath, src).
				Str("expected_type", deploySourceTypeName(expectedSourceType)).
				Msg("Failed to verify deploy target. Reason: source disappeared during sync")
			return fmt.Errorf("deploy invariant: source path missing after sync: path=%q want=%s", src, deploySourceTypeName(expectedSourceType))
		}
		actualSourceType := sourceRoot.Mode().Type()
		if actualSourceType != expectedSourceType {
			logger.Error().
				Str(log.FieldPath, src).
				Str("expected_type", deploySourceTypeName(expectedSourceType)).
				Str("actual_type", deploySourceTypeName(actualSourceType)).
				Msg("Failed to verify deploy target. Reason: source changed type during sync")
			return fmt.Errorf("deploy invariant: source path changed type after sync: path=%q want=%s got=%s", src, deploySourceTypeName(expectedSourceType), deploySourceTypeName(actualSourceType))
		}
		sawFiles, mismatch, err := destinationSatisfiesSource(src, dst)
		if err != nil {
			logger.Error().Err(err).
				Str(log.FieldPath, src).
				Str("destination", dst).
				Msg("Failed to verify deploy target. Reason: cannot compare source against destination")
			return fmt.Errorf("compare source %q against destination %q: %w", src, dst, err)
		}
		if !sawFiles {
			logger.Debug().Msg("Deploy target verification passed: source has no regular files and all written directories were verified")
			return nil
		}
		if mismatch != "" {
			logger.Error().
				Str(log.FieldPath, src).
				Str("destination", dst).
				Str("mismatch", mismatch).
				Msg("Failed to verify deploy target. Reason: no regular-file writes recorded and destination does not content-match source (silent-sync failure)")
			return fmt.Errorf("%w: src=%q dst=%q mismatch=%q", ErrDeployInvariantEmptyWrite, src, dst, mismatch)
		}
		logger.Debug().
			Str("destination", dst).
			Msg("Deploy target verification passed: no regular-file writes, destination already content-matches source")
		return nil
	}

	logger.Info().Int("written_path_count", len(writtenRel)).Msg("Successfully verified deploy target invariant")
	return nil
}

func deploySourceTypeName(mode fs.FileMode) string {
	switch {
	case mode.IsRegular():
		return "regular-file"
	case mode.IsDir():
		return "directory"
	case mode&fs.ModeSymlink != 0:
		return "symlink"
	default:
		return mode.Type().String()
	}
}

// destinationSatisfiesSource reports whether a deploy with no regular-file
// writes left the destination already mirroring the source — the on-disk truth
// check behind the empty-write invariant. Instead of treating "source has files
// but no file writes recorded" as corruption, it confirms each regular file in
// src is present and content-equal at dst.
//
// Symlinks are skipped (Lstat semantics) to match CopyDir/CopyDirIfChanged,
// which never deploy them, so a symlink-only source imposes no requirement.
// Content equality reuses fileutil.FileHash/ContentEqual — the same primitives
// CopyFileIfChanged uses to decide a write is skippable — so "skipped because
// equal" and "verified present" share one definition of equal.
//
// Returns (sawFiles, mismatch, err): sawFiles is whether src held at least one
// regular file; mismatch is the first dst path that is missing or content-
// differing ("" when every file is satisfied). It mirrors how the deploy maps
// source onto destination: a directory source maps each file to its same
// relative path under dst; a single-file source maps to dst/<basename>. A
// missing source yields (false, "", nil).
func destinationSatisfiesSource(src, dst string) (sawFiles bool, mismatch string, err error) {
	info, err := os.Lstat(src)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, "", nil
		}
		return false, "", err
	}

	if !info.IsDir() {
		// A symlink (or other non-regular entry) is never deployed, so it
		// imposes no requirement on the destination.
		if !info.Mode().IsRegular() {
			return false, "", nil
		}
		target := filepath.Join(dst, filepath.Base(src))
		equal, eqErr := fileEqual(src, target)
		if eqErr != nil {
			return true, "", eqErr
		}
		if !equal {
			return true, target, nil
		}
		return true, "", nil
	}

	walkErr := filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		// IsRegular() is false for directories and symlinks alike, matching the
		// copy path's symlink skip.
		if !d.Type().IsRegular() {
			return nil
		}
		sawFiles = true
		rel, relErr := filepath.Rel(src, path)
		if relErr != nil {
			return relErr
		}
		target := filepath.Join(dst, rel)
		equal, eqErr := fileEqual(path, target)
		if eqErr != nil {
			return eqErr
		}
		if !equal {
			mismatch = target
			return fs.SkipAll
		}
		return nil
	})
	if walkErr != nil {
		return sawFiles, "", walkErr
	}
	return sawFiles, mismatch, nil
}

// fileEqual reports whether dst exists and is byte-identical to src, using the
// same SHA-256 comparison CopyFileIfChanged relies on. A missing dst is
// (false, nil) — i.e. a mismatch, not an error.
func fileEqual(src, dst string) (bool, error) {
	srcHash, err := fileutil.FileHash(src)
	if err != nil {
		return false, err
	}
	return fileutil.ContentEqual(dst, srcHash)
}

// dirHasRegularFiles reports whether src is, or recursively contains, at
// least one regular file. Symlinks are not counted (Lstat semantics), matching
// the copy path which never deploys them. Returns (false, nil) for a missing path.
func dirHasRegularFiles(src string) (bool, error) {
	return dirHasRegularFilesContext(context.Background(), src)
}

func dirHasRegularFilesContext(ctx context.Context, src string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	info, err := os.Lstat(src)
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
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
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

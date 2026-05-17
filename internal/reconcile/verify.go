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
		Msg("Verifying deploy target invariant")
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

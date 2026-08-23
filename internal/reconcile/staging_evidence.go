package reconcile

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/cameronsjo/bosun/internal/log"
)

const (
	stagingDirMode  fs.FileMode = 0o700
	stagingFileMode fs.FileMode = 0o600
)

type stagingEvidenceOps struct {
	lstat     func(string) (fs.FileInfo, error)
	chmod     func(*os.File, fs.FileMode) error
	removeAll func(string) error
}

func defaultStagingEvidenceOps() stagingEvidenceOps {
	return stagingEvidenceOps{
		lstat: os.Lstat,
		chmod: func(f *os.File, mode fs.FileMode) error {
			return f.Chmod(mode)
		},
		removeAll: os.RemoveAll,
	}
}

func (ops stagingEvidenceOps) withDefaults() stagingEvidenceOps {
	defaults := defaultStagingEvidenceOps()
	if ops.lstat == nil {
		ops.lstat = defaults.lstat
	}
	if ops.chmod == nil {
		ops.chmod = defaults.chmod
	}
	if ops.removeAll == nil {
		ops.removeAll = defaults.removeAll
	}
	return ops
}

// ValidateStagingEvidenceTargets proves that no target's recursive staging
// lifecycle can reach another target's slot. Existing ancestors are resolved so
// explicit paths that differ textually but meet through a symlink still collide.
func ValidateStagingEvidenceTargets(base *Config, targets []Target) error {
	type canonicalTarget struct {
		name string
		path string
	}

	canonical := make([]canonicalTarget, 0, len(targets))
	for _, target := range targets {
		effective := base.ConfigForTarget(target)
		path, err := canonicalStagingPath(effective.StagingDir)
		if err != nil {
			return fmt.Errorf("target %q staging path: %w", target.Name, err)
		}
		for _, prior := range canonical {
			if stagingPathsOverlap(prior.path, path) {
				// Do not include canonical paths: resolving an existing symlink is
				// required for the comparison, but its target is not operator-safe
				// diagnostic material.
				return fmt.Errorf("targets %q and %q have equal or nested staging paths", prior.name, target.Name)
			}
		}
		canonical = append(canonical, canonicalTarget{name: target.Name, path: path})
	}
	return nil
}

// PreflightStagingEvidence secures evidence left by an older binary before any
// target performs Git sync or decrypts secrets. Deletion is an acceptable
// fail-closed fallback; only protect-plus-delete failure blocks the whole cycle.
func PreflightStagingEvidence(ctx context.Context, base *Config, targets []Target) error {
	return preflightStagingEvidence(ctx, base, targets, defaultStagingEvidenceOps())
}

func preflightStagingEvidence(ctx context.Context, base *Config, targets []Target, ops stagingEvidenceOps) error {
	if err := ValidateStagingEvidenceTargets(base, targets); err != nil {
		return err
	}
	for _, target := range targets {
		effective := base.ConfigForTarget(target)
		if _, err := protectOrDeleteStaging(ctx, target.Name, effective.StagingDir, ops, "preflight"); err != nil {
			return fmt.Errorf("target %q staging evidence preflight: %w", target.Name, err)
		}
	}
	return nil
}

func canonicalStagingPath(path string) (string, error) {
	root, err := safeStagingRoot(path)
	if err != nil {
		return "", err
	}

	probe := root
	var suffix []string
	for {
		_, statErr := os.Lstat(probe)
		if statErr == nil {
			resolved, evalErr := filepath.EvalSymlinks(probe)
			if evalErr != nil {
				return "", fmt.Errorf("resolve existing staging ancestor %q", probe)
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			root = filepath.Clean(resolved)
			break
		}
		if !errors.Is(statErr, fs.ErrNotExist) {
			return "", fmt.Errorf("inspect staging ancestor %q: %w", probe, statErr)
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return "", fmt.Errorf("no existing ancestor for staging path %q", root)
		}
		suffix = append(suffix, filepath.Base(probe))
		probe = parent
	}

	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		root = strings.ToLower(root)
	}
	return root, nil
}

func safeStagingRoot(path string) (string, error) {
	if path == "" || filepath.Clean(path) == "." {
		return "", errors.New("staging path must not be empty or the current directory")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve staging path: %w", err)
	}
	abs = filepath.Clean(abs)
	volumeRoot := filepath.Clean(filepath.VolumeName(abs) + string(filepath.Separator))
	if abs == volumeRoot {
		return "", fmt.Errorf("staging path must not be filesystem root %q", abs)
	}
	return abs, nil
}

func stagingPathsOverlap(a, b string) bool {
	if a == b {
		return true
	}
	return pathContains(a, b) || pathContains(b, a)
}

func pathContains(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil || rel == "." || rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func verifyActiveStaging(path string, ops stagingEvidenceOps) error {
	return walkStaging(path, false, ops.withDefaults())
}

func hardenStaging(path string, ops stagingEvidenceOps) error {
	return walkStaging(path, true, ops.withDefaults())
}

func walkStaging(path string, harden bool, ops stagingEvidenceOps) error {
	root, err := safeStagingRoot(path)
	if err != nil {
		return err
	}

	before, err := ops.lstat(root)
	if err != nil {
		return fmt.Errorf("inspect staging root %q: %w", root, err)
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.IsDir() {
		return fmt.Errorf("staging root is not a real directory: %q", root)
	}

	// os.Root confines every descendant lookup to the pinned root. Each child
	// directory gets its own pinned Root before recursion, so replacing an
	// ancestor with a symlink cannot redirect a later lookup outside the tree.
	namespace, err := os.OpenRoot(root)
	if err != nil {
		return fmt.Errorf("pin staging root %q: %w", root, err)
	}
	defer func() { _ = namespace.Close() }()
	pinned, err := namespace.Open(".")
	if err != nil {
		return fmt.Errorf("open pinned staging root %q: %w", root, err)
	}
	defer func() { _ = pinned.Close() }()
	opened, err := pinned.Stat()
	if err != nil || !os.SameFile(before, opened) || !opened.IsDir() {
		return fmt.Errorf("staging root changed while being inspected: %q", root)
	}
	if err := walkPinnedStagingDir(namespace, pinned, opened, root, harden, ops, true); err != nil {
		return err
	}
	named, err := ops.lstat(root)
	if err != nil || named.Mode()&os.ModeSymlink != 0 || !os.SameFile(opened, named) {
		return fmt.Errorf("staging root changed while being protected: %q", root)
	}
	return nil
}

func walkPinnedStagingDir(namespace *os.Root, pinned *os.File, opened fs.FileInfo, displayPath string, harden bool, ops stagingEvidenceOps, isRoot bool) error {
	if err := applyStagingMode(pinned, opened, displayPath, harden, isRoot, ops); err != nil {
		return err
	}

	entries, err := pinned.ReadDir(-1)
	if err != nil {
		return fmt.Errorf("read staging directory %q: %w", displayPath, err)
	}
	inspected := make(map[string]fs.FileInfo, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		entryPath := filepath.Join(displayPath, name)
		if filepath.Base(name) != name || name == "." || name == ".." || !pathContains(displayPath, entryPath) {
			return fmt.Errorf("staging entry escapes effective root: %q", entryPath)
		}
		before, err := namespace.Lstat(name)
		if err != nil {
			return fmt.Errorf("inspect staging entry %q: %w", entryPath, err)
		}
		if before.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("staging entry is a symlink: %q", entryPath)
		}
		if !before.IsDir() && !before.Mode().IsRegular() {
			return fmt.Errorf("staging entry has unsupported type %s: %q", before.Mode().Type(), entryPath)
		}
		inspected[name] = before

		if before.IsDir() {
			childRoot, err := namespace.OpenRoot(name)
			if err != nil {
				return fmt.Errorf("pin staging directory %q: %w", entryPath, err)
			}
			child, openErr := childRoot.Open(".")
			if openErr != nil {
				_ = childRoot.Close()
				return fmt.Errorf("open pinned staging directory %q: %w", entryPath, openErr)
			}
			childInfo, statErr := child.Stat()
			if statErr != nil || !os.SameFile(before, childInfo) || !childInfo.IsDir() {
				_ = child.Close()
				_ = childRoot.Close()
				return fmt.Errorf("staging entry changed while being inspected: %q", entryPath)
			}
			walkErr := walkPinnedStagingDir(childRoot, child, childInfo, entryPath, harden, ops, false)
			_ = child.Close()
			_ = childRoot.Close()
			if walkErr != nil {
				return walkErr
			}
		} else {
			child, err := namespace.Open(name)
			if err != nil {
				return fmt.Errorf("pin staging file %q: %w", entryPath, err)
			}
			childInfo, statErr := child.Stat()
			if statErr != nil || !os.SameFile(before, childInfo) || !childInfo.Mode().IsRegular() {
				_ = child.Close()
				return fmt.Errorf("staging entry changed while being inspected: %q", entryPath)
			}
			modeErr := applyStagingMode(child, childInfo, entryPath, harden, false, ops)
			_ = child.Close()
			if modeErr != nil {
				return modeErr
			}
		}

		named, err := namespace.Lstat(name)
		if err != nil || named.Mode()&os.ModeSymlink != 0 || !os.SameFile(before, named) {
			return fmt.Errorf("staging entry changed while being protected: %q", entryPath)
		}
	}
	return verifyStagingDirectoryStable(namespace, opened, inspected, displayPath, harden, isRoot)
}

// verifyStagingDirectoryStable closes the initial-enumeration gap: an entry
// added after ReadDir, or an already-checked name replaced near the end of the
// walk, must not escape the hardening decision.
func verifyStagingDirectoryStable(namespace *os.Root, opened fs.FileInfo, inspected map[string]fs.FileInfo, displayPath string, harden, isRoot bool) error {
	current, err := namespace.Open(".")
	if err != nil {
		return fmt.Errorf("reopen staging directory %q: %w", displayPath, err)
	}
	defer func() { _ = current.Close() }()
	currentInfo, err := current.Stat()
	if err != nil || !os.SameFile(opened, currentInfo) || !currentInfo.IsDir() {
		return fmt.Errorf("staging directory changed while being verified: %q", displayPath)
	}
	if (harden || isRoot) && currentInfo.Mode().Perm() != stagingDirMode {
		return fmt.Errorf("staging directory mode changed while being verified: %q", displayPath)
	}
	entries, err := current.ReadDir(-1)
	if err != nil {
		return fmt.Errorf("reread staging directory %q: %w", displayPath, err)
	}
	if len(entries) != len(inspected) {
		return fmt.Errorf("staging directory entries changed while being protected: %q", displayPath)
	}
	for _, entry := range entries {
		expected, ok := inspected[entry.Name()]
		if !ok {
			return fmt.Errorf("staging directory entries changed while being protected: %q", displayPath)
		}
		named, statErr := namespace.Lstat(entry.Name())
		if statErr != nil || named.Mode()&os.ModeSymlink != 0 || !os.SameFile(expected, named) {
			return fmt.Errorf("staging entry changed during final verification: %q", filepath.Join(displayPath, entry.Name()))
		}
		if harden {
			mode := stagingFileMode
			if named.IsDir() {
				mode = stagingDirMode
			}
			if named.Mode().Perm() != mode {
				return fmt.Errorf("staging entry mode changed during final verification: %q", filepath.Join(displayPath, entry.Name()))
			}
		}
	}
	return nil
}

func applyStagingMode(pinned *os.File, opened fs.FileInfo, path string, harden, isRoot bool, ops stagingEvidenceOps) error {
	mode := stagingFileMode
	if opened.IsDir() {
		mode = stagingDirMode
	}
	if harden {
		if err := ops.chmod(pinned, mode); err != nil {
			return fmt.Errorf("restrict staging entry %q: %w", path, err)
		}
		verified, err := pinned.Stat()
		if err != nil {
			return fmt.Errorf("verify restricted staging entry %q: %w", path, err)
		}
		if !os.SameFile(opened, verified) || verified.Mode().Perm() != mode {
			return fmt.Errorf("staging entry %q was not restricted to %04o", path, mode)
		}
	} else if isRoot && opened.Mode().Perm() != stagingDirMode {
		return fmt.Errorf("active staging root %q has mode %04o, expected %04o", path, opened.Mode().Perm(), stagingDirMode)
	}
	return nil
}

func protectOrDeleteStaging(ctx context.Context, target, path string, ops stagingEvidenceOps, reason string) (string, error) {
	ops = ops.withDefaults()
	root, err := safeStagingRoot(path)
	if err != nil {
		return "", err
	}
	if _, err := ops.lstat(root); errors.Is(err, fs.ErrNotExist) {
		return "absent", nil
	}

	logger := log.ComponentCtx(ctx, log.ComponentReconcile)
	if err := hardenStaging(root, ops); err == nil {
		logger.Warn().
			Str("target", stagingTargetLabel(target)).
			Str(log.FieldPath, root).
			Str("staging_evidence_outcome", "retained").
			Str("reason", reason).
			Msg("Retained secured staging evidence")
		return "retained", nil
	} else {
		hardenErr := err
		if removeErr := removeStagingTree(root, ops); removeErr == nil {
			logger.Error().
				Err(hardenErr).
				Str("target", stagingTargetLabel(target)).
				Str(log.FieldPath, root).
				Str("staging_evidence_outcome", "discarded").
				Str("reason", reason).
				Msg("Discarded staging evidence that could not be secured")
			return "discarded", nil
		} else {
			securityErr := errors.Join(
				fmt.Errorf("secure staging evidence: %w", hardenErr),
				fmt.Errorf("delete unsafe staging evidence: %w", removeErr),
			)
			logger.Error().
				Err(securityErr).
				Str("target", stagingTargetLabel(target)).
				Str(log.FieldPath, root).
				Str("staging_evidence_outcome", "unsafe").
				Str("reason", reason).
				Msg("Unable to secure or delete staging evidence")
			return "unsafe", securityErr
		}
	}
}

func removeStagingTree(path string, ops stagingEvidenceOps) error {
	root, err := safeStagingRoot(path)
	if err != nil {
		return err
	}
	if err := ops.removeAll(root); err != nil {
		return err
	}
	if _, err := ops.lstat(root); err == nil {
		return fmt.Errorf("staging root still exists after removal: %q", root)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("verify staging removal: %w", err)
	}
	return nil
}

func stagingTargetLabel(target string) string {
	if target == "" {
		return DefaultTargetName
	}
	return target
}

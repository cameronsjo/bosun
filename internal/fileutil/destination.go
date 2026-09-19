package fileutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// errDestinationEscapesRoot reports a destination path that does not lie under
// the root a copy pinned. The helpers that produce it fail closed: they never
// fall back to unpinned path resolution.
var errDestinationEscapesRoot = errors.New("destination path escapes its pinned root")

// errTempPatternHasSeparator mirrors os.CreateTemp's refusal of a pattern that
// contains a path separator.
var errTempPatternHasSeparator = errors.New("temp file pattern contains a path separator")

// destination is the set of destination-side mutations an atomic copy performs.
// A name is interpreted in the destination's own namespace: an ordinary
// filesystem path for pathDestination, and a path relative to a directory
// handle opened once up front for rootDestination.
//
// Pinning matters because bosun deploys as host root into directories a
// container can write. Resolving each mutation by path lets a container replace
// an intermediate directory with a symlink between the check and the write and
// redirect that write outside the deploy tree (CWE-367). Every rootDestination
// mutation is resolved from the pinned handle instead, so a component swapped
// after the handle was opened cannot redirect it.
//
// Residual, by design: os.Root follows a symlink whose target stays inside the
// pinned root, so a redirect within the root remains possible. What pinning
// closes is the escape out of the root.
type destination interface {
	mkdirAll(name string, mode fs.FileMode) error
	mkdir(name string, mode fs.FileMode) error
	lstat(name string) (fs.FileInfo, error)
	// open opens name for reading through this destination's own resolution,
	// so a change decision reads the same entry the write would replace.
	open(name string) (*os.File, error)
	// createTemp creates a private 0600 file in dir and returns it with the
	// name this destination's other methods accept for it.
	createTemp(dir, pattern string) (*os.File, string, error)
	remove(name string) error
	rename(oldName, newName string) error
	// syncDir flushes a directory's entries to durable storage.
	syncDir(name string) error
}

// destinationSync flushes a copy's destination directory. A nil value batches
// the flush at a higher level.
type destinationSync func(dest destination, dir string) error

// pathDestination resolves every name as an ordinary filesystem path. This is
// the historic behaviour of CopyFile, CopyDir and their rollback, snapshot and
// emergency callers.
type pathDestination struct{}

func (pathDestination) mkdirAll(name string, mode fs.FileMode) error { return os.MkdirAll(name, mode) }

func (pathDestination) mkdir(name string, mode fs.FileMode) error { return os.Mkdir(name, mode) }

func (pathDestination) lstat(name string) (fs.FileInfo, error) { return os.Lstat(name) }

func (pathDestination) open(name string) (*os.File, error) { return os.Open(name) }

func (pathDestination) createTemp(dir, pattern string) (*os.File, string, error) {
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return nil, "", err
	}
	return file, file.Name(), nil
}

func (pathDestination) remove(name string) error { return os.Remove(name) }

func (pathDestination) rename(oldName, newName string) error { return os.Rename(oldName, newName) }

func (pathDestination) syncDir(name string) error { return syncDir(name) }

// rootDestination resolves every name from a pinned directory handle.
type rootDestination struct {
	root *os.Root
}

func (d rootDestination) mkdirAll(name string, mode fs.FileMode) error {
	return d.root.MkdirAll(name, mode)
}

func (d rootDestination) mkdir(name string, mode fs.FileMode) error {
	return d.root.Mkdir(name, mode)
}

func (d rootDestination) lstat(name string) (fs.FileInfo, error) { return d.root.Lstat(name) }

func (d rootDestination) open(name string) (*os.File, error) { return d.root.Open(name) }

func (d rootDestination) createTemp(dir, pattern string) (*os.File, string, error) {
	return rootCreateTemp(d.root, dir, pattern)
}

func (d rootDestination) remove(name string) error { return d.root.Remove(name) }

func (d rootDestination) rename(oldName, newName string) error {
	return d.root.Rename(oldName, newName)
}

func (d rootDestination) syncDir(name string) error {
	dir, err := d.root.Open(name)
	if err != nil {
		return fmt.Errorf("open directory: %w", err)
	}
	defer func() { _ = dir.Close() }()
	return dir.Sync()
}

// tempNameAttempts bounds the search for an unused temp name, matching
// os.CreateTemp's own bound.
const tempNameAttempts = 10000

// rootCreateTemp is os.CreateTemp's contract performed through a pinned root:
// an exclusive create at 0600, a bounded number of name attempts, and a refusal
// of any pattern containing a path separator. Names are drawn from crypto/rand
// so a local attacker cannot pre-create the next name the copy will try.
func rootCreateTemp(root *os.Root, dir, pattern string) (*os.File, string, error) {
	prefix, suffix, err := splitTempPattern(pattern)
	if err != nil {
		return nil, "", err
	}
	for attempt := 0; attempt < tempNameAttempts; attempt++ {
		token, err := randomTempToken()
		if err != nil {
			return nil, "", err
		}
		name := filepath.Join(dir, prefix+token+suffix)
		file, err := root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
		if err == nil {
			return file, name, nil
		}
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		return nil, "", err
	}
	return nil, "", fmt.Errorf("create temp file in %s: %w", dir, fs.ErrExist)
}

func splitTempPattern(pattern string) (prefix, suffix string, err error) {
	for i := 0; i < len(pattern); i++ {
		if os.IsPathSeparator(pattern[i]) {
			return "", "", fmt.Errorf("%w: %q", errTempPatternHasSeparator, pattern)
		}
	}
	if star := strings.LastIndexByte(pattern, '*'); star >= 0 {
		return pattern[:star], pattern[star+1:], nil
	}
	return pattern, "", nil
}

func randomTempToken() (string, error) {
	var token [8]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", fmt.Errorf("generate temp file name: %w", err)
	}
	return hex.EncodeToString(token[:]), nil
}

// destinationName converts a full destination path into the name a root pinned
// at root accepts. The comparison is lexical on cleaned paths, which is exactly
// how callers build the path in the first place (filepath.Join(root, rel)).
func destinationName(root, path string) (string, error) {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("%w: %s under %s: %w", errDestinationEscapesRoot, path, root, err)
	}
	if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %s under %s", errDestinationEscapesRoot, path, root)
	}
	return rel, nil
}

// pinnedDir owns an os.Root handle on a copy destination root and exposes the
// copy operations bound to it. It is not safe for concurrent use; a copy walk
// is sequential.
//
// The handle is opened on the first destination mutation rather than up front,
// so a copy that fails before mutating anything — a walk that cannot read its
// source, for instance — still leaves no destination directory behind.
type pinnedDir struct {
	path string
	root *os.Root
}

func newPinnedDir(path string) *pinnedDir {
	return &pinnedDir{path: path}
}

// destination opens the pinned handle on first use, creating the destination
// root and any missing ancestors as the path-based copy helpers do. Pinning
// starts at the root: the root's own path is resolved normally, and everything
// beneath it is resolved from the handle.
func (p *pinnedDir) destination() (destination, error) {
	if p.root != nil {
		return rootDestination{root: p.root}, nil
	}
	if err := os.MkdirAll(p.path, 0755); err != nil {
		return nil, fmt.Errorf("create destination root: %w", err)
	}
	root, err := os.OpenRoot(p.path)
	if err != nil {
		return nil, fmt.Errorf("pin destination root: %w", err)
	}
	p.root = root
	return rootDestination{root: root}, nil
}

func (p *pinnedDir) close() {
	if p.root != nil {
		_ = p.root.Close()
		p.root = nil
	}
}

func (p *pinnedDir) name(path string) (string, error) {
	return destinationName(p.path, path)
}

// copyFileInto copies src to dst through the pinned root. A nil sync leaves the
// destination-directory flush to a surrounding batch.
func (p *pinnedDir) copyFileInto(ctx context.Context, src, dst string, sync destinationSync) error {
	dest, err := p.destination()
	if err != nil {
		return err
	}
	name, err := p.name(dst)
	if err != nil {
		return err
	}
	return copyFileIntoDestination(ctx, src, dest, name, (*os.File).Chmod, sync, io.Copy, openRegularSource)
}

func (p *pinnedDir) copyFileWithoutDirSync(ctx context.Context, src, dst string) error {
	return p.copyFileInto(ctx, src, dst, nil)
}

func (p *pinnedDir) copyFileSyncingDir(ctx context.Context, src, dst string) error {
	return p.copyFileInto(ctx, src, dst, portableDestinationSync)
}

func (p *pinnedDir) copyFileIfChangedDeferred(ctx context.Context, src, dst string) (bool, postWriteVerification, error) {
	if err := p.assertDestinationInRoot(dst); err != nil {
		return false, nil, err
	}
	return copyFileIfChangedDeferredWithCopy(ctx, src, dst, fileHashContext, p.copyFileWithoutDirSync)
}

// openExistingRoot pins the root without creating it, so a caller that only
// needs to resolve a destination leaves no directory behind. A missing root
// yields a nil handle: the destination under it cannot exist either.
func (p *pinnedDir) openExistingRoot() (*os.Root, error) {
	if p.root != nil {
		return p.root, nil
	}
	root, err := os.OpenRoot(p.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("pin destination root: %w", err)
	}
	p.root = root
	return root, nil
}

// assertDestinationInRoot resolves dst through the pinned handle before a
// change decision reads it.
//
// The decision hashes and byte-compares the destination by path. Left ungated,
// a container that replaces a directory under the root with a symlink to a
// tree it owns, then pre-places a copy of the rendered file there, makes the
// comparison read its file, find it equal, and skip the write. Nothing lands
// inside the root, the skipped file never enters WrittenFiles so
// verifyDeployTarget cannot catch it, and the deploy reports success while the
// file the operator believes is deployed is attacker-owned. A silent skip is
// an unreported deploy failure, so an escaping destination is refused here.
//
// A destination that resolves inside the root is still compared by path. The
// residual is the in-root redirect os.Root allows by design, documented on the
// destination interface.
func (p *pinnedDir) assertDestinationInRoot(dst string) error {
	name, err := p.name(dst)
	if err != nil {
		return err
	}
	root, err := p.openExistingRoot()
	if err != nil {
		return err
	}
	if root == nil {
		return nil
	}
	file, err := rootDestination{root: root}.open(name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// Absent inside the root: the pinned copy creates it.
			return nil
		}
		// An escaping component reports here, and so does a destination that
		// cannot be read at all. Neither can be compared safely.
		return fmt.Errorf("%w: %s under %s: %w", errDestinationEscapesRoot, dst, p.path, err)
	}
	return file.Close()
}

// mkdirRoot creates the copy's destination root. When that is the pinned path
// itself, opening the handle already creates it at 0755 — the mode the walk
// asks for. When the copy destination sits below the pinned root, the missing
// directories are created through the handle, so the same swapped-component
// check the file copies get applies here too. A path outside the pinned root is
// refused.
func (p *pinnedDir) mkdirRoot(path string, mode fs.FileMode) error {
	dest, err := p.destination()
	if err != nil {
		return err
	}
	name, err := p.name(path)
	if err != nil {
		return err
	}
	if name == "." {
		return nil
	}
	return dest.mkdirAll(name, mode)
}

func (p *pinnedDir) mkdirIfMissing(path string, mode fs.FileMode) (bool, error) {
	dest, err := p.destination()
	if err != nil {
		return false, err
	}
	name, err := p.name(path)
	if err != nil {
		return false, err
	}
	return mkdirIfMissingWithOps(name, mode, dest.mkdir, dest.lstat)
}

func (p *pinnedDir) syncParent(dir string) error {
	dest, err := p.destination()
	if err != nil {
		return err
	}
	name, err := p.name(dir)
	if err != nil {
		return err
	}
	return portableDestinationSync(dest, name)
}

func (p *pinnedDir) dirOps() destinationDirOps {
	return destinationDirOps{
		mkdirRoot:      p.mkdirRoot,
		mkdirIfMissing: p.mkdirIfMissing,
		copyFile:       p.copyFileIfChangedDeferred,
		syncParent:     p.syncParent,
	}
}

// portableDestinationSync provides the per-call durability contract CopyFile
// documents. Windows has no equivalent directory-fsync semantics.
func portableDestinationSync(dest destination, dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	return dest.syncDir(dir)
}

// adaptPathSync lets a caller that supplies a path-based directory sync drive
// the destination-aware copy core.
func adaptPathSync(syncParent func(string) error) destinationSync {
	if syncParent == nil {
		return nil
	}
	return func(_ destination, dir string) error {
		return syncParent(dir)
	}
}

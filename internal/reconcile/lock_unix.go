//go:build !windows

package reconcile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/cameronsjo/bosun/internal/fileutil"
)

// acquireLock acquires an exclusive lock to prevent concurrent runs.
// On Unix systems, this uses flock(2) for file locking.
//
// The lock parent and lock basename are selected through descriptor-bound
// handles. The parent chain rejects untrusted owners, and the final directory
// is daemon-owned and owner-only before the lock file is opened. The file is
// then checked through its already-open descriptor so a pre-planted symlink,
// replacement, non-regular file, or untrusted owner cannot become the lock.
func (r *Reconciler) acquireLock() error {
	parent, err := ensureTrustedLockParent(filepath.Dir(r.lockFile))
	if err != nil {
		return err
	}
	defer func() { _ = parent.Close() }()

	name := filepath.Base(r.lockFile)
	if name == "." || name == string(filepath.Separator) || name == "" || name == ".." {
		return fmt.Errorf("refusing reconcile lock %q: invalid lock basename; use a regular owner-only lock file under a trusted directory", r.lockFile)
	}
	before, statErr := parent.Lstat(name)
	if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
		return fmt.Errorf("refusing reconcile lock %q: inspect existing basename (chown %d and chmod %#o): %w", r.lockFile, os.Geteuid(), lockFileMode, statErr)
	}
	if statErr == nil && before.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing reconcile lock %q: lock path is a symlink; replace it with an owner-only regular file (chown %d and chmod %#o)", r.lockFile, os.Geteuid(), lockFileMode)
	}
	if errors.Is(statErr, fs.ErrNotExist) {
		before = nil
	}

	// O_EXCL separates the two cases so the created file never exists with a
	// broader mode, not even momentarily. os.Root adds O_NOFOLLOW to the
	// descriptor-relative open itself.
	fd, err := parent.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR, lockFileMode)
	if errors.Is(err, fs.ErrExist) {
		fd, err = parent.OpenFile(name, os.O_RDWR, lockFileMode)
	}
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return fmt.Errorf("refusing reconcile lock %q: lock path is a symlink; replace it with an owner-only regular file (chown %d and chmod %#o)", r.lockFile, os.Geteuid(), lockFileMode)
		}
		return fmt.Errorf("refusing reconcile lock %q: open owner-only lock file (chown %d and chmod %#o): %w", r.lockFile, os.Geteuid(), lockFileMode, err)
	}

	if err := validateOpenedLockFile(fd, r.lockFile); err != nil {
		_ = fd.Close()
		return err
	}
	if err := validateLockPathIdentity(parent, name, before, fd, r.lockFile); err != nil {
		_ = fd.Close()
		return err
	}

	// Try non-blocking exclusive lock.
	if err := syscall.Flock(int(fd.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = fd.Close()
		return fmt.Errorf("lock already held: %w", err)
	}

	r.lockFd = fd
	return nil
}

func validateLockPathIdentity(parent *os.Root, name string, before os.FileInfo, fd *os.File, path string) error {
	after, err := parent.Lstat(name)
	if err != nil {
		return fmt.Errorf("refusing reconcile lock %q: cannot verify basename after open (chown %d and chmod %#o): %w", path, os.Geteuid(), lockFileMode, err)
	}
	if after.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing reconcile lock %q: lock path became a symlink during open; replace it with an owner-only regular file (chown %d and chmod %#o)", path, os.Geteuid(), lockFileMode)
	}
	if before != nil && !os.SameFile(before, after) {
		return fmt.Errorf("refusing reconcile lock %q: lock path identity changed during open; restore a regular file owned by uid %d and chmod %#o", path, os.Geteuid(), lockFileMode)
	}
	info, err := fd.Stat()
	if err != nil {
		return fmt.Errorf("refusing reconcile lock %q: cannot stat opened descriptor (chown %d and chmod %#o): %w", path, os.Geteuid(), lockFileMode, err)
	}
	if !os.SameFile(after, info) {
		return fmt.Errorf("refusing reconcile lock %q: opened descriptor does not match the basename; restore a regular file owned by uid %d and chmod %#o", path, os.Geteuid(), lockFileMode)
	}
	return nil
}

func validateOpenedLockFile(fd *os.File, path string) error {
	info, err := fd.Stat()
	if err != nil {
		return fmt.Errorf("refusing reconcile lock %q: cannot inspect opened file (chown %d and chmod %#o): %w", path, os.Geteuid(), lockFileMode, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing reconcile lock %q: opened object is not a regular file; replace it with a regular file owned by uid %d and chmod %#o", path, os.Geteuid(), lockFileMode)
	}
	uid, ok := unixFileOwner(info)
	if !ok || uid != uint32(os.Geteuid()) {
		return fmt.Errorf("refusing reconcile lock %q: opened file is owned by uid %d, need daemon uid %d; chown %d and chmod %#o", path, uid, os.Geteuid(), os.Geteuid(), lockFileMode)
	}
	if info.Mode().Perm() != lockFileMode.Perm() {
		if err := fd.Chmod(lockFileMode); err != nil {
			return fmt.Errorf("refusing reconcile lock %q: cannot set owner-only permissions %#o; chown %d and chmod %#o: %w", path, lockFileMode, os.Geteuid(), lockFileMode, err)
		}
		info, err = fd.Stat()
		if err != nil {
			return fmt.Errorf("refusing reconcile lock %q: cannot verify owner-only permissions; chown %d and chmod %#o: %w", path, os.Geteuid(), lockFileMode, err)
		}
		if info.Mode().Perm() != lockFileMode.Perm() {
			return fmt.Errorf("refusing reconcile lock %q: mode is %#o, need owner-only %#o; chown %d and chmod %#o", path, info.Mode().Perm(), lockFileMode, os.Geteuid(), lockFileMode)
		}
	}
	return nil
}

// ensureTrustedLockParent opens the complete lock-parent chain through Root
// handles and returns the final directory handle. Missing components are
// created owner-only. Existing ancestors may be root-owned (including the
// documented sticky system temporary directory), or daemon-owned without
// group/other write access. The final directory must be daemon-owned and 0700;
// an owned directory with a different mode is rejected before the basename is
// opened.
func ensureTrustedLockParent(parent string) (*os.Root, error) {
	absParent, err := fileutil.CanonicalTrustedPath(parent)
	if err != nil {
		return nil, fmt.Errorf("refusing reconcile lock parent %q: resolve trusted path (fix the path, owner, and permissions): %w", parent, err)
	}
	filesystemRoot := filepath.VolumeName(absParent) + string(filepath.Separator)
	relative, err := filepath.Rel(filesystemRoot, absParent)
	if err != nil {
		return nil, fmt.Errorf("refusing reconcile lock parent %q: resolve relative path (fix the path, owner, and permissions): %w", parent, err)
	}
	root, err := os.OpenRoot(filesystemRoot)
	if err != nil {
		return nil, fmt.Errorf("refusing reconcile lock parent %q: open filesystem root (fix the path and owner-only permissions): %w", parent, err)
	}
	if relative == "." || relative == "" {
		_ = root.Close()
		return nil, fmt.Errorf("refusing reconcile lock parent %q: filesystem root cannot be the final lock directory; use a daemon-owned directory with mode %#o", parent, lockDirMode)
	}

	current := root
	components := strings.FieldsFunc(relative, func(r rune) bool { return r == '/' })
	for index, component := range components {
		final := index == len(components)-1
		next, openErr := current.OpenRoot(component)
		if errors.Is(openErr, fs.ErrNotExist) {
			if mkdirErr := current.Mkdir(component, lockDirMode); mkdirErr != nil && !errors.Is(mkdirErr, fs.ErrExist) {
				_ = current.Close()
				return nil, fmt.Errorf("refusing reconcile lock parent %q: create %q (mkdir %#o, chown %d): %w", parent, component, lockDirMode, os.Geteuid(), mkdirErr)
			}
			next, openErr = current.OpenRoot(component)
		}
		if openErr != nil {
			_ = current.Close()
			return nil, fmt.Errorf("refusing reconcile lock parent %q: open %q (fix the path, owner, and permissions): %w", parent, component, openErr)
		}

		expected, statErr := current.Lstat(component)
		if statErr != nil {
			_ = next.Close()
			_ = current.Close()
			return nil, fmt.Errorf("refusing reconcile lock parent %q: verify %q (fix the path, owner, and permissions): %w", parent, component, statErr)
		}
		actual, statErr := next.Stat(".")
		if statErr != nil {
			_ = next.Close()
			_ = current.Close()
			return nil, fmt.Errorf("refusing reconcile lock parent %q: stat opened %q (fix the path, owner, and permissions): %w", parent, component, statErr)
		}
		if !actual.IsDir() || !os.SameFile(expected, actual) {
			_ = next.Close()
			_ = current.Close()
			return nil, fmt.Errorf("refusing reconcile lock parent %q: %q is not the stable directory selected from its trusted parent; replace it with a real directory owned by root or uid %d", parent, component, os.Geteuid())
		}
		if err := validateTrustedLockDir(parent, component, actual, final); err != nil {
			_ = next.Close()
			_ = current.Close()
			return nil, err
		}

		// Only the final directory is returned. Close the filesystem-root and
		// every intermediate traversal handle as soon as the next descriptor has
		// passed its identity checks.
		_ = current.Close()
		current = next
	}
	return current, nil
}

func validateTrustedLockDir(parent, component string, info os.FileInfo, final bool) error {
	path := filepath.Join(parent, component)
	if !info.IsDir() {
		return fmt.Errorf("refusing reconcile lock path %q: trusted parent is not a real directory; replace it with an owner-only directory (chown %d and chmod %#o)", path, os.Geteuid(), lockDirMode)
	}
	uid, ok := unixFileOwner(info)
	if !ok {
		return fmt.Errorf("refusing reconcile lock path %q: cannot verify directory owner; replace it with a root-owned ancestor or daemon-owned directory (chown %d and chmod %#o)", path, os.Geteuid(), lockDirMode)
	}
	if final {
		if uid != uint32(os.Geteuid()) {
			return fmt.Errorf("refusing reconcile lock path %q: final directory is owned by uid %d, need daemon uid %d; chown %d and chmod %#o", path, uid, os.Geteuid(), os.Geteuid(), lockDirMode)
		}
		if info.Mode().Perm() != lockDirMode.Perm() {
			return fmt.Errorf("refusing reconcile lock path %q: final directory mode is %#o, need owner-only %#o; chmod %#o", path, info.Mode().Perm(), lockDirMode, lockDirMode)
		}
		return nil
	}

	if uid != 0 && uid != uint32(os.Geteuid()) {
		return fmt.Errorf("refusing reconcile lock path %q: ancestor is owned by uid %d, need root or daemon uid %d; chown %d and remove group/other write access", path, uid, os.Geteuid(), os.Geteuid())
	}
	if info.Mode().Perm()&0o022 != 0 && !(uid == 0 && info.Mode()&os.ModeSticky != 0) {
		return fmt.Errorf("refusing reconcile lock path %q: ancestor mode is %#o; make it root-owned and non-writable, or daemon-owned without group/other write access", path, info.Mode().Perm())
	}
	return nil
}

func unixFileOwner(info os.FileInfo) (uint32, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return stat.Uid, true
}

// releaseLock releases the lock file.
func (r *Reconciler) releaseLock() {
	if r.lockFd != nil {
		_ = syscall.Flock(int(r.lockFd.Fd()), syscall.LOCK_UN)
		_ = r.lockFd.Close()
		r.lockFd = nil
	}
}

//go:build !windows

package reconcile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// acquireLock acquires an exclusive lock to prevent concurrent runs.
// On Unix systems, this uses flock(2) for file locking.
//
// The lock file is created owner-only (lockFileMode) and opened O_NOFOLLOW:
// flock hands LOCK_EX to any descriptor that can open the file, so a
// world-readable lock file -- or one reached through a pre-planted symlink in
// a lock directory others can write -- lets an unrelated local principal block
// every deploy.
func (r *Reconciler) acquireLock() error {
	if err := ensureTrustedLockParent(filepath.Dir(r.lockFile)); err != nil {
		return err
	}
	// O_EXCL separates the two cases so the created file never exists with a
	// broader mode, not even momentarily.
	fd, err := os.OpenFile(r.lockFile, os.O_CREATE|os.O_EXCL|os.O_RDWR|syscall.O_NOFOLLOW, lockFileMode)
	if errors.Is(err, fs.ErrExist) {
		// Pre-existing lock file: releases before this change created it 0644,
		// so an upgraded deployment would keep the permissive mode forever.
		// Tighten it on the open descriptor (fchmod), which cannot be
		// redirected by swapping the path after the open.
		fd, err = os.OpenFile(r.lockFile, os.O_RDWR|syscall.O_NOFOLLOW, lockFileMode)
		if err == nil {
			if chmodErr := fd.Chmod(lockFileMode); chmodErr != nil {
				_ = fd.Close()
				return fmt.Errorf("refusing reconcile lock %q: cannot set owner-only permissions %#o: %w; fix the owner and permissions (chmod %#o)", r.lockFile, lockFileMode, chmodErr, lockFileMode)
			}
			info, statErr := fd.Stat()
			if statErr != nil {
				_ = fd.Close()
				return fmt.Errorf("refusing reconcile lock %q: cannot verify owner-only permissions: %w; fix the owner and permissions (chmod %#o)", r.lockFile, statErr, lockFileMode)
			}
			if info.Mode().Perm() != lockFileMode.Perm() {
				_ = fd.Close()
				return fmt.Errorf("refusing reconcile lock %q: mode is %#o, need owner-only %#o; fix the owner and permissions (chmod %#o)", r.lockFile, info.Mode().Perm(), lockFileMode, lockFileMode)
			}
		}
	}
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return fmt.Errorf("refusing reconcile lock %q: lock path is a symlink; replace it with an owner-only regular file (chmod %#o)", r.lockFile, lockFileMode)
		}
		return fmt.Errorf("failed to open lock file: %w", err)
	}
	info, statErr := fd.Stat()
	if statErr != nil {
		_ = fd.Close()
		return fmt.Errorf("refusing reconcile lock %q: cannot verify owner-only permissions: %w; fix the owner and permissions (chmod %#o)", r.lockFile, statErr, lockFileMode)
	}
	if info.Mode().Perm()&0o077 != 0 {
		_ = fd.Close()
		return fmt.Errorf("refusing reconcile lock %q: mode is %#o, need owner-only %#o; fix the owner and permissions (chmod %#o)", r.lockFile, info.Mode().Perm(), lockFileMode, lockFileMode)
	}

	// Try non-blocking exclusive lock.
	if err := syscall.Flock(int(fd.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = fd.Close()
		return fmt.Errorf("lock already held: %w", err)
	}

	r.lockFd = fd
	return nil
}

// ensureTrustedLockParent creates missing lock ancestors owner-only and
// rejects existing symlinks, non-directories, or directories writable by a
// group/other principal. Without this check an attacker who controls a lock
// ancestor can replace it between the caller's check and OpenFile.
func ensureTrustedLockParent(parent string) error {
	absParent, err := filepath.Abs(parent)
	if err != nil {
		return fmt.Errorf("refusing reconcile lock parent %q: resolve path: %w; fix the path and owner-only permissions", parent, err)
	}
	absParent = filepath.Clean(absParent)
	var missing []string
	for current := absParent; ; current = filepath.Dir(current) {
		info, statErr := os.Lstat(current)
		if statErr == nil {
			if err := validateTrustedLockDir(current, info); err != nil {
				return err
			}
			break
		}
		if !errors.Is(statErr, fs.ErrNotExist) {
			return fmt.Errorf("refusing reconcile lock parent %q: inspect %q: %w; fix the path and owner-only permissions", parent, current, statErr)
		}
		missing = append(missing, current)
		next := filepath.Dir(current)
		if next == current {
			return fmt.Errorf("refusing reconcile lock parent %q: no trusted filesystem root; fix the path and owner-only permissions", parent)
		}
	}
	for i := len(missing) - 1; i >= 0; i-- {
		current := missing[i]
		if err := os.Mkdir(current, lockDirMode); err != nil && !errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("refusing reconcile lock parent %q: create %q: %w; fix the owner and permissions (mkdir %#o)", parent, current, err, lockDirMode)
		}
		info, statErr := os.Lstat(current)
		if statErr != nil {
			return fmt.Errorf("refusing reconcile lock parent %q: verify %q: %w; fix the owner-only permissions", parent, current, statErr)
		}
		if err := validateTrustedLockDir(current, info); err != nil {
			return err
		}
	}
	return nil
}

func validateTrustedLockDir(path string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("refusing reconcile lock path %q: trusted parent is not a real directory; replace it with an owner-only directory", path)
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("refusing reconcile lock path %q: trusted parent mode is %#o; fix the owner and permissions (chmod %#o)", path, info.Mode().Perm(), lockDirMode)
	}
	return nil
}

// releaseLock releases the lock file.
func (r *Reconciler) releaseLock() {
	if r.lockFd != nil {
		_ = syscall.Flock(int(r.lockFd.Fd()), syscall.LOCK_UN)
		_ = r.lockFd.Close()
		r.lockFd = nil
	}
}

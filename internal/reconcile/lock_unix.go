//go:build !windows

package reconcile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"

	"github.com/cameronsjo/bosun/internal/log"
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
				// Not fatal: bosun does not own the file's mode, but it can
				// still lock it. Failing here would block deploys outright,
				// which is the availability loss this hardening prevents.
				logger := log.Component(log.ComponentReconcile)
				logger.Warn().
					Err(chmodErr).
					Str(log.FieldPath, r.lockFile).
					Msgf("Failed to tighten lock file permissions to %#o; a local reader may be able to hold the lock", lockFileMode)
			}
		}
	}
	if err != nil {
		return fmt.Errorf("failed to open lock file: %w", err)
	}

	// Try non-blocking exclusive lock.
	if err := syscall.Flock(int(fd.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = fd.Close()
		return fmt.Errorf("lock already held: %w", err)
	}

	r.lockFd = fd
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

//go:build !windows

package reconcile

import (
	"fmt"
	"os"
	"syscall"
)

// acquireLock acquires an exclusive lock to prevent concurrent runs.
// On Unix systems, this uses flock(2) for file locking.
func (r *Reconciler) acquireLock() error {
	fd, err := os.OpenFile(r.lockFile, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("failed to open lock file: %w", err)
	}

	// Try non-blocking exclusive lock.
	if err := syscall.Flock(int(fd.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		fd.Close()
		return fmt.Errorf("lock already held: %w", err)
	}

	r.lockFd = fd
	return nil
}

// releaseLock releases the lock file.
func (r *Reconciler) releaseLock() {
	if r.lockFd != nil {
		_ = syscall.Flock(int(r.lockFd.Fd()), syscall.LOCK_UN)
		r.lockFd.Close()
		r.lockFd = nil
	}
}

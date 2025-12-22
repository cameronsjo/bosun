//go:build windows

package reconcile

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// acquireLock acquires an exclusive lock to prevent concurrent runs.
// On Windows, this uses LockFileEx for file locking.
func (r *Reconciler) acquireLock() error {
	fd, err := os.OpenFile(r.lockFile, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("failed to open lock file: %w", err)
	}

	// Try non-blocking exclusive lock using LockFileEx.
	// LOCKFILE_EXCLUSIVE_LOCK | LOCKFILE_FAIL_IMMEDIATELY
	overlapped := &windows.Overlapped{}
	err = windows.LockFileEx(
		windows.Handle(fd.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,          // reserved
		1,          // lock 1 byte
		0,          // high-order size (0 for small files)
		overlapped, // overlapped structure
	)
	if err != nil {
		fd.Close()
		return fmt.Errorf("lock already held: %w", err)
	}

	r.lockFd = fd
	return nil
}

// releaseLock releases the lock file.
func (r *Reconciler) releaseLock() {
	if r.lockFd != nil {
		overlapped := &windows.Overlapped{}
		// Unlock the file region.
		windows.UnlockFileEx(
			windows.Handle(r.lockFd.Fd()),
			0,          // reserved
			1,          // unlock 1 byte
			0,          // high-order size
			overlapped, // overlapped structure
		)
		r.lockFd.Close()
		r.lockFd = nil
	}
}

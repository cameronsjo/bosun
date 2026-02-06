//go:build windows

// Package lock provides file-based locking for bosun operations.
package lock

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sys/windows"
)

// Lock represents a file-based lock.
// The Lock struct is safe for concurrent use from multiple goroutines.
type Lock struct {
	path string
	file *os.File
	mu   sync.Mutex // protects file field
}

// New creates a new lock for the given operation in the manifest directory.
func New(manifestDir, operation string) *Lock {
	lockDir := filepath.Join(manifestDir, ".bosun", "locks")
	return &Lock{
		path: filepath.Join(lockDir, operation+".lock"),
	}
}

// Acquire attempts to acquire the lock.
// On Windows, this uses LockFileEx for exclusive non-blocking locking.
// Returns an error if the lock is already held by another process.
// This method is safe for concurrent calls.
func (l *Lock) Acquire() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(l.path), 0755); err != nil {
		return fmt.Errorf("create lock directory: %w", err)
	}

	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("open lock file: %w", err)
	}

	// Acquire exclusive non-blocking lock via LockFileEx.
	overlapped := &windows.Overlapped{}
	err = windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,          // reserved
		1,          // lock 1 byte
		0,          // high-order size
		overlapped, // overlapped structure
	)
	if err != nil {
		f.Close()
		return fmt.Errorf("another %s operation is already running", filepath.Base(l.path[:len(l.path)-5]))
	}

	// Write PID to lock file for debugging
	_ = f.Truncate(0)
	_, _ = f.Seek(0, 0)
	fmt.Fprintf(f, "%d\n", os.Getpid())

	l.file = f
	return nil
}

// Release releases the lock.
// This method is safe for concurrent calls.
func (l *Lock) Release() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file == nil {
		return nil
	}

	// Unlock the file region.
	overlapped := &windows.Overlapped{}
	if err := windows.UnlockFileEx(
		windows.Handle(l.file.Fd()),
		0,          // reserved
		1,          // unlock 1 byte
		0,          // high-order size
		overlapped, // overlapped structure
	); err != nil {
		l.file.Close()
		l.file = nil
		return fmt.Errorf("release lock: %w", err)
	}

	l.file.Close()
	os.Remove(l.path)
	l.file = nil

	return nil
}

// WithLock executes a function while holding the lock.
// The lock is automatically released when the function returns.
func WithLock(manifestDir, operation string, fn func() error) error {
	lock := New(manifestDir, operation)
	if err := lock.Acquire(); err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()

	return fn()
}

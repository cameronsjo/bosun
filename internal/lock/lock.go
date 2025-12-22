// Package lock provides file-based locking for bosun operations.
package lock

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Lock represents a file-based lock.
type Lock struct {
	path string
	file *os.File
}

// New creates a new lock for the given operation in the manifest directory.
func New(manifestDir, operation string) *Lock {
	lockDir := filepath.Join(manifestDir, ".bosun", "locks")
	return &Lock{
		path: filepath.Join(lockDir, operation+".lock"),
	}
}

// Acquire attempts to acquire the lock.
// Returns an error if the lock is already held by another process.
func (l *Lock) Acquire() error {
	// Ensure lock directory exists
	if err := os.MkdirAll(filepath.Dir(l.path), 0755); err != nil {
		return fmt.Errorf("create lock directory: %w", err)
	}

	// Open or create the lock file
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("open lock file: %w", err)
	}

	// Try to acquire exclusive lock (non-blocking)
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		l.file = nil // Ensure file handle is nil on error
		if err == syscall.EWOULDBLOCK {
			return fmt.Errorf("another %s operation is already running", filepath.Base(l.path[:len(l.path)-5]))
		}
		return fmt.Errorf("acquire lock: %w", err)
	}

	// Write PID to lock file for debugging
	f.Truncate(0)
	f.Seek(0, 0)
	fmt.Fprintf(f, "%d\n", os.Getpid())

	l.file = f
	return nil
}

// Release releases the lock.
func (l *Lock) Release() error {
	if l.file == nil {
		return nil
	}

	// Unlock the file
	if err := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN); err != nil {
		l.file.Close()
		return fmt.Errorf("release lock: %w", err)
	}

	// Close and remove the lock file
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
	defer lock.Release()

	return fn()
}

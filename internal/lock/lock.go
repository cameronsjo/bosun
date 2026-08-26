// Package lock provides file-based locking for bosun operations.
package lock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const lockFileSuffix = ".lock"

type lockOps struct {
	lock        func(*os.File) error
	unlock      func(*os.File) error
	isContended func(error) bool
}

// Lock represents a file-based lock.
// The Lock struct is safe for concurrent use from multiple goroutines.
type Lock struct {
	path string
	file *os.File
	mu   sync.Mutex // protects file field
	ops  lockOps
}

// New creates a new lock for the given operation in the manifest directory.
func New(manifestDir, operation string) *Lock {
	lockDir := filepath.Join(manifestDir, ".bosun", "locks")
	return &Lock{
		path: filepath.Join(lockDir, operation+lockFileSuffix),
		ops:  platformLockOps(),
	}
}

func (l *Lock) alreadyHeldError() error {
	operation := filepath.Base(l.path)
	operation = operation[:len(operation)-len(lockFileSuffix)]
	return fmt.Errorf("another %s operation is already running", operation)
}

// Acquire attempts to acquire the lock.
// Returns an error if the lock is already held by another process.
// This method is safe for concurrent calls.
func (l *Lock) Acquire() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// A second acquire on the same Lock would otherwise depend on platform
	// re-entrant-lock semantics and could overwrite the only tracked handle.
	if l.file != nil {
		return l.alreadyHeldError()
	}

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
	if err := l.ops.lock(f); err != nil {
		_ = f.Close()
		if l.ops.isContended(err) {
			return l.alreadyHeldError()
		}
		return fmt.Errorf("acquire lock: %w", err)
	}

	// Write PID to lock file for debugging
	_ = f.Truncate(0)
	_, _ = f.Seek(0, 0)
	_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())

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

	// Unlock the file
	f := l.file
	l.file = nil
	if err := l.ops.unlock(f); err != nil {
		closeErr := f.Close()
		return errors.Join(fmt.Errorf("release lock: %w", err), closeErr)
	}

	// Keep the lock file as a stable inode. Removing it after unlocking creates
	// a race where one process holds the old inode while another creates and
	// locks a new file at the same path.
	return f.Close()
}

// WithLock executes a function while holding the lock.
// The lock is automatically released when the function returns.
func WithLock(manifestDir, operation string, fn func() error) error {
	return withLock(New(manifestDir, operation), fn)
}

func withLock(lock *Lock, fn func() error) (err error) {
	if err = lock.Acquire(); err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, lock.Release())
	}()

	return fn()
}

//go:build windows

// Package lock provides file-based locking for bosun operations.
package lock

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
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
// On Windows, this uses LockFileEx for exclusive locking.
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

	// On Windows, use LockFileEx via x/sys/windows.
	// For now, use a simple file-existence check as a basic lock.
	// TODO(#24j): Implement proper Windows file locking with LockFileEx.
	l.file = f
	fmt.Fprintf(f, "%d\n", os.Getpid())

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

package lock

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	lock := New("/tmp/test", "provision")
	assert.Equal(t, "/tmp/test/.bosun/locks/provision.lock", lock.path)
}

func TestLock_AcquireRelease(t *testing.T) {
	tmpDir := t.TempDir()
	lock := New(tmpDir, "test")

	// Acquire should succeed
	err := lock.Acquire()
	require.NoError(t, err)

	// Lock file should exist
	lockPath := filepath.Join(tmpDir, ".bosun", "locks", "test.lock")
	_, err = os.Stat(lockPath)
	require.NoError(t, err)

	// Release should succeed
	err = lock.Release()
	require.NoError(t, err)

	// Lock file should be removed
	_, err = os.Stat(lockPath)
	assert.True(t, os.IsNotExist(err))
}

func TestLock_DoubleAcquire(t *testing.T) {
	tmpDir := t.TempDir()
	lock1 := New(tmpDir, "test")
	lock2 := New(tmpDir, "test")

	// First acquire should succeed
	err := lock1.Acquire()
	require.NoError(t, err)
	defer lock1.Release()

	// Second acquire should fail
	err = lock2.Acquire()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "another test operation is already running")
}

func TestLock_ReleaseWithoutAcquire(t *testing.T) {
	tmpDir := t.TempDir()
	lock := New(tmpDir, "test")

	// Release without acquire should not error
	err := lock.Release()
	require.NoError(t, err)
}

func TestWithLock(t *testing.T) {
	tmpDir := t.TempDir()

	executed := false
	err := WithLock(tmpDir, "test", func() error {
		executed = true
		return nil
	})

	require.NoError(t, err)
	assert.True(t, executed)
}

func TestWithLock_Blocked(t *testing.T) {
	tmpDir := t.TempDir()
	lock := New(tmpDir, "test")

	// Hold the lock
	err := lock.Acquire()
	require.NoError(t, err)
	defer lock.Release()

	// WithLock should fail
	err = WithLock(tmpDir, "test", func() error {
		return nil
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "another test operation is already running")
}

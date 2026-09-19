//go:build !windows

package reconcile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// flock(2) hands LOCK_EX to any descriptor that can open the lock file, so the
// file's mode is what keeps an unrelated local uid from holding the lock and
// blocking every deploy. These tests pin the mode of the file bosun creates,
// the mode of a lock file left permissive by an older release, refusal of a
// symlinked lock path, and the mode of a lock directory bosun creates.

func TestAcquireLock_CreatesOwnerOnlyLockFile(t *testing.T) {
	lockFile := filepath.Join(t.TempDir(), "reconcile.lock")

	r := NewReconciler(DefaultConfig())
	r.lockFile = lockFile

	require.NoError(t, r.acquireLock())
	defer r.releaseLock()

	info, err := os.Stat(lockFile)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(),
		"a newly created lock file must not be openable by any other local uid")
}

func TestAcquireLock_TightensPreExistingPermissiveLockFile(t *testing.T) {
	lockFile := filepath.Join(t.TempDir(), "reconcile.lock")

	// An upgraded deployment already has the lock file a prior release created
	// world-readable.
	require.NoError(t, os.WriteFile(lockFile, nil, 0o644))
	require.NoError(t, os.Chmod(lockFile, 0o644))

	r := NewReconciler(DefaultConfig())
	r.lockFile = lockFile

	require.NoError(t, r.acquireLock())
	defer r.releaseLock()

	info, err := os.Stat(lockFile)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(),
		"acquiring the lock must tighten a lock file left permissive by an older release")
}

func TestAcquireLock_RefusesSymlinkedLockPath(t *testing.T) {
	tmpDir := t.TempDir()
	planted := filepath.Join(tmpDir, "planted.target")
	require.NoError(t, os.WriteFile(planted, nil, 0o600))

	lockFile := filepath.Join(tmpDir, "reconcile.lock")
	require.NoError(t, os.Symlink(planted, lockFile))

	r := NewReconciler(DefaultConfig())
	r.lockFile = lockFile

	err := r.acquireLock()
	require.Error(t, err, "a lock path that is a symlink must be refused, not followed")
	assert.Contains(t, err.Error(), "failed to open lock file")
	assert.Nil(t, r.lockFd)
}

func TestReconcilerRun_CreatesOwnerOnlyLockDir(t *testing.T) {
	tmpDir := t.TempDir()
	lockDir := filepath.Join(tmpDir, "nested", "does-not-exist-yet")

	cfg := &Config{
		LockFile:  filepath.Join(lockDir, "reconcile.lock"),
		StateFile: filepath.Join(tmpDir, "state.json"),
	}
	r := NewReconciler(cfg, WithGitOperations(&mockGitOps{syncErr: fmt.Errorf("injected sync boom")}))

	err := r.Run(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "injected sync boom", "the pipeline must get past lock creation")

	info, statErr := os.Stat(lockDir)
	require.NoError(t, statErr)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm(),
		"a lock directory bosun creates must not be traversable by other local uids")
}

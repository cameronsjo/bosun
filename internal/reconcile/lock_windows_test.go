//go:build windows

package reconcile

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAcquireLockCreatesNestedParentWindows(t *testing.T) {
	base := t.TempDir()
	lockFile := filepath.Join(base, "nested", "locks", "reconcile.lock")
	r := NewReconciler(&Config{LockFile: lockFile})

	require.NoError(t, r.acquireLock())
	assert.FileExists(t, lockFile)
	assert.DirExists(t, filepath.Dir(lockFile))

	r.releaseLock()
	assert.Nil(t, r.lockFd)
}

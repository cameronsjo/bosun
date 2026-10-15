//go:build !windows

package reconcile

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type lockConfinementFileInfo struct {
	mode os.FileMode
	sys  any
}

func (i lockConfinementFileInfo) Name() string       { return "lock-fixture" }
func (i lockConfinementFileInfo) Size() int64        { return 0 }
func (i lockConfinementFileInfo) Mode() os.FileMode  { return i.mode }
func (i lockConfinementFileInfo) ModTime() time.Time { return time.Time{} }
func (i lockConfinementFileInfo) IsDir() bool        { return i.mode.IsDir() }
func (i lockConfinementFileInfo) Sys() any           { return i.sys }

func lockConfinementInfo(mode os.FileMode, uid uint32) os.FileInfo {
	return lockConfinementFileInfo{mode: mode, sys: &syscall.Stat_t{Uid: uid}}
}

func TestAcquireLockRejectsInvalidBasenamesAndOpenFailures(t *testing.T) {
	t.Run("invalid basename", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())
		require.NoError(t, os.Chmod(tmpDir, 0o700))

		r := NewReconciler(DefaultConfig())
		r.lockFile = tmpDir + string(filepath.Separator) + "."

		err := r.acquireLock()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid lock basename")
		assert.Nil(t, r.lockFd)
	})

	t.Run("basename inspection error", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())
		require.NoError(t, os.Chmod(tmpDir, 0o700))

		r := NewReconciler(DefaultConfig())
		r.lockFile = filepath.Join(tmpDir, "\x00")

		err := r.acquireLock()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "inspect existing basename")
		assert.Nil(t, r.lockFd)
	})

	t.Run("existing directory cannot become a lock file", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())
		lockFile := privateLockPath(t, tmpDir, "reconcile.lock")
		require.NoError(t, os.Mkdir(lockFile, 0o700))

		r := NewReconciler(DefaultConfig())
		r.lockFile = lockFile

		err := r.acquireLock()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "open owner-only lock file")
		assert.Nil(t, r.lockFd)
	})
}

func TestValidateOpenedLockFileRejectsClosedDescriptor(t *testing.T) {
	tmpDir := evalSymlinks(t, t.TempDir())
	path := filepath.Join(tmpDir, "reconcile.lock")
	fd, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	require.NoError(t, err)
	require.NoError(t, fd.Close())

	err = validateOpenedLockFile(fd, path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot inspect opened file")
}

func TestValidateLockPathIdentityRejectsDescriptorAndPathChanges(t *testing.T) {
	t.Run("closed parent", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())
		path := filepath.Join(tmpDir, "reconcile.lock")
		require.NoError(t, os.WriteFile(path, nil, 0o600))
		parent, err := os.OpenRoot(tmpDir)
		require.NoError(t, err)
		fd, err := os.Open(path)
		require.NoError(t, err)
		t.Cleanup(func() { _ = fd.Close() })
		require.NoError(t, parent.Close())

		err = validateLockPathIdentity(parent, filepath.Base(path), nil, fd, path)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot verify basename after open")
	})

	t.Run("closed descriptor", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())
		path := filepath.Join(tmpDir, "reconcile.lock")
		require.NoError(t, os.WriteFile(path, nil, 0o600))
		parent, err := os.OpenRoot(tmpDir)
		require.NoError(t, err)
		t.Cleanup(func() { _ = parent.Close() })
		fd, err := os.Open(path)
		require.NoError(t, err)
		require.NoError(t, fd.Close())

		err = validateLockPathIdentity(parent, filepath.Base(path), nil, fd, path)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot stat opened descriptor")
	})

	t.Run("basename became symlink", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())
		target := filepath.Join(tmpDir, "target")
		link := filepath.Join(tmpDir, "reconcile.lock")
		require.NoError(t, os.WriteFile(target, nil, 0o600))
		fd, err := os.Open(target)
		require.NoError(t, err)
		t.Cleanup(func() { _ = fd.Close() })
		require.NoError(t, os.Symlink(filepath.Base(target), link))
		parent, err := os.OpenRoot(tmpDir)
		require.NoError(t, err)
		t.Cleanup(func() { _ = parent.Close() })

		err = validateLockPathIdentity(parent, filepath.Base(link), nil, fd, link)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "became a symlink")
	})

	t.Run("basename identity changed", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())
		oldPath := filepath.Join(tmpDir, "old")
		newPath := filepath.Join(tmpDir, "new")
		require.NoError(t, os.WriteFile(oldPath, nil, 0o600))
		before, err := os.Lstat(oldPath)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(newPath, nil, 0o600))
		fd, err := os.Open(newPath)
		require.NoError(t, err)
		t.Cleanup(func() { _ = fd.Close() })
		parent, err := os.OpenRoot(tmpDir)
		require.NoError(t, err)
		t.Cleanup(func() { _ = parent.Close() })

		err = validateLockPathIdentity(parent, filepath.Base(newPath), before, fd, newPath)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "identity changed")
	})

	t.Run("opened descriptor does not match basename", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())
		oldPath := filepath.Join(tmpDir, "old")
		newPath := filepath.Join(tmpDir, "new")
		require.NoError(t, os.WriteFile(oldPath, nil, 0o600))
		require.NoError(t, os.WriteFile(newPath, nil, 0o600))
		fd, err := os.Open(oldPath)
		require.NoError(t, err)
		t.Cleanup(func() { _ = fd.Close() })
		parent, err := os.OpenRoot(tmpDir)
		require.NoError(t, err)
		t.Cleanup(func() { _ = parent.Close() })

		err = validateLockPathIdentity(parent, filepath.Base(newPath), nil, fd, newPath)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not match the basename")
	})
}

func TestValidateTrustedLockDirRejectsUnsafeMetadata(t *testing.T) {
	foreignUID := uint32(os.Geteuid()) + 1
	tests := []struct {
		name    string
		info    os.FileInfo
		final   bool
		wantErr string
	}{
		{name: "not a directory", info: lockConfinementInfo(0o600, uint32(os.Geteuid())), final: true, wantErr: "not a real directory"},
		{name: "owner metadata unavailable", info: lockConfinementFileInfo{mode: os.ModeDir | 0o700}, final: true, wantErr: "cannot verify directory owner"},
		{name: "final directory foreign owner", info: lockConfinementInfo(os.ModeDir|0o700, foreignUID), final: true, wantErr: "need daemon uid"},
		{name: "final directory permissive mode", info: lockConfinementInfo(os.ModeDir|0o755, uint32(os.Geteuid())), final: true, wantErr: "final directory mode"},
		{name: "ancestor foreign owner", info: lockConfinementInfo(os.ModeDir|0o700, foreignUID), final: false, wantErr: "ancestor is owned"},
		{name: "ancestor writable", info: lockConfinementInfo(os.ModeDir|0o775, uint32(os.Geteuid())), final: false, wantErr: "ancestor mode"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTrustedLockDir("/requested", "/requested/lock", tt.info, tt.final)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}

	for _, test := range []struct {
		name  string
		info  os.FileInfo
		final bool
	}{
		{name: "final owner-only", info: lockConfinementInfo(os.ModeDir|0o700, uint32(os.Geteuid())), final: true},
		{name: "daemon-owned ancestor", info: lockConfinementInfo(os.ModeDir|0o700, uint32(os.Geteuid())), final: false},
		{name: "root-owned sticky ancestor", info: lockConfinementInfo(os.ModeDir|os.ModeSticky|0o1777, 0), final: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, validateTrustedLockDir("/requested", "/requested/lock", test.info, test.final))
		})
	}
}

func TestEnsureTrustedLockParentRejectsRootAndNonDirectory(t *testing.T) {
	t.Run("filesystem root cannot be final lock directory", func(t *testing.T) {
		root, err := ensureTrustedLockParent(string(filepath.Separator))
		require.Error(t, err)
		assert.Nil(t, root)
		assert.Contains(t, err.Error(), "filesystem root cannot be the final lock directory")
	})

	t.Run("regular file cannot be a parent component", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())
		require.NoError(t, os.Chmod(tmpDir, 0o700))
		file := filepath.Join(tmpDir, "not-a-directory")
		require.NoError(t, os.WriteFile(file, nil, 0o600))

		root, err := ensureTrustedLockParent(filepath.Join(file, "child"))
		require.Error(t, err)
		assert.Nil(t, root)
		assert.Contains(t, err.Error(), "open")
	})

	t.Run("missing components are created owner-only", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())
		require.NoError(t, os.Chmod(tmpDir, 0o700))
		parentPath := filepath.Join(tmpDir, "nested", "lock")

		root, err := ensureTrustedLockParent(parentPath)
		require.NoError(t, err)
		require.NoError(t, root.Close())
		info, err := os.Stat(parentPath)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
	})

	t.Run("missing component creation failure is actionable", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())
		require.NoError(t, os.Chmod(tmpDir, 0o500))
		t.Cleanup(func() { _ = os.Chmod(tmpDir, 0o700) })
		probe, probeErr := os.CreateTemp(tmpDir, ".permission-check-")
		if probeErr == nil {
			require.NoError(t, probe.Close())
			require.NoError(t, os.Remove(probe.Name()))
			t.Skip("filesystem does not enforce owner write permission in this test environment")
		}

		root, err := ensureTrustedLockParent(filepath.Join(tmpDir, "missing"))
		require.Error(t, err)
		assert.Nil(t, root)
		assert.Contains(t, err.Error(), "create")
	})
}

func TestUnixFileOwnerRejectsUnsupportedMetadata(t *testing.T) {
	uid, ok := unixFileOwner(lockConfinementFileInfo{mode: os.ModeDir | 0o700})
	assert.False(t, ok)
	assert.Zero(t, uid)
}

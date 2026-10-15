package fileutil

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDestinationAdapters_ExerciseFilesystemContracts(t *testing.T) {
	base := t.TempDir()
	pathDest := pathDestination{}
	nested := filepath.Join(base, "nested")
	require.NoError(t, pathDest.mkdirAll(nested, 0o755))
	require.NoError(t, pathDest.mkdir(filepath.Join(nested, "child"), 0o755))
	info, err := pathDest.lstat(filepath.Join(nested, "child"))
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	file, name, err := pathDest.createTemp(nested, ".tmp-*")
	require.NoError(t, err)
	require.NoError(t, file.Close())
	renamed := filepath.Join(nested, "renamed")
	require.NoError(t, pathDest.rename(name, renamed))
	require.NoError(t, pathDest.remove(renamed))
	_, _, err = pathDest.createTemp(filepath.Join(base, "missing"), ".tmp-*")
	require.ErrorIs(t, err, fs.ErrNotExist)
	if runtime.GOOS != "windows" {
		require.NoError(t, pathDest.syncDir(nested))
	}

	rootPath := filepath.Join(base, "root")
	require.NoError(t, os.Mkdir(rootPath, 0o700))
	root, err := os.OpenRoot(rootPath)
	require.NoError(t, err)
	defer func() { _ = root.Close() }()
	rootDest := rootDestination{root: root}
	require.NoError(t, rootDest.mkdirAll("nested", 0o755))
	require.NoError(t, rootDest.mkdir("nested/child", 0o755))
	info, err = rootDest.lstat("nested/child")
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	file, name, err = rootDest.createTemp("nested", ".tmp-*")
	require.NoError(t, err)
	require.NoError(t, file.Close())
	require.NoError(t, rootDest.rename(name, "nested/renamed"))
	require.NoError(t, rootDest.remove("nested/renamed"))
	_, _, err = rootDest.createTemp("missing", ".tmp-*")
	require.ErrorIs(t, err, fs.ErrNotExist)
	_, _, err = rootDest.createTemp(".", "../escape-*")
	require.ErrorIs(t, err, errTempPatternHasSeparator)
	require.Error(t, rootDest.syncDir("missing"))
	if runtime.GOOS != "windows" {
		require.NoError(t, rootDest.syncDir("nested"))
	}
}

func TestDestinationHelpers_ValidateAndPropagateErrors(t *testing.T) {
	prefix, suffix, err := splitTempPattern("plain")
	require.NoError(t, err)
	assert.Equal(t, "plain", prefix)
	assert.Empty(t, suffix)

	base := t.TempDir()
	root, err := os.OpenRoot(base)
	require.NoError(t, err)
	defer func() { _ = root.Close() }()
	_, _, err = rootCreateTemp(root, "missing", ".tmp-*")
	require.ErrorIs(t, err, fs.ErrNotExist)

	missingPath := filepath.Join(base, "missing-root")
	pinned := newPinnedDir(missingPath)
	got, err := pinned.openExistingRoot()
	require.NoError(t, err)
	assert.Nil(t, got)
	assert.NoDirExists(t, missingPath)

	dest, err := pinned.destination()
	require.NoError(t, err)
	assert.NotNil(t, dest)
	assert.DirExists(t, missingPath)
	pinned.close()

	got, err = pinned.openExistingRoot()
	require.NoError(t, err)
	require.NotNil(t, got)
	pinned.close()

	if runtime.GOOS != "windows" {
		realParent := filepath.Join(base, "real-parent")
		require.NoError(t, os.Mkdir(realParent, 0o700))
		alias := filepath.Join(base, "alias")
		require.NoError(t, os.Symlink(realParent, alias))
		_, err = newPinnedDir(filepath.Join(alias, "child")).openExistingRoot()
		require.Error(t, err)
	}
}

func TestPinnedDir_PropagatesNamespaceAndLifecycleErrors(t *testing.T) {
	base := t.TempDir()
	rootPath := filepath.Join(base, "root")
	require.NoError(t, os.Mkdir(rootPath, 0o700))
	src := filepath.Join(base, "source")
	require.NoError(t, os.WriteFile(src, []byte("source"), 0o600))
	pinned := newPinnedDir(rootPath)

	_, err := pinned.name(filepath.Join(base, "outside"))
	require.ErrorIs(t, err, errDestinationEscapesRoot)
	require.ErrorIs(t, pinned.copyFileInto(context.Background(), src, filepath.Join(base, "outside"), nil), errDestinationEscapesRoot)
	_, _, err = pinned.copyFileIfChangedDeferredWithCopy(context.Background(), src, filepath.Join(base, "outside"), func(context.Context, string, string) error {
		return nil
	})
	require.ErrorIs(t, err, errDestinationEscapesRoot)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = pinned.copyFileIfChangedDeferredWithCopy(ctx, src, filepath.Join(rootPath, "cancelled"), func(context.Context, string, string) error {
		t.Fatal("cancelled copy reached its callback")
		return nil
	})
	require.ErrorIs(t, err, context.Canceled)

	_, _, err = pinned.copyFileIfChangedDeferredWithCopy(context.Background(), filepath.Join(base, "missing-source"), filepath.Join(rootPath, "missing-source"), func(context.Context, string, string) error {
		t.Fatal("missing source reached its callback")
		return nil
	})
	require.Error(t, err)

	got, err := pinned.openExistingRoot()
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NoError(t, pinned.mkdirRoot(rootPath, 0o755))
	require.NoError(t, pinned.mkdirRoot(filepath.Join(rootPath, "nested"), 0o755))
	require.ErrorIs(t, pinned.mkdirRoot(filepath.Join(base, "outside-root"), 0o755), errDestinationEscapesRoot)

	created, err := pinned.mkdirIfMissing(filepath.Join(rootPath, "created"), 0o755)
	require.NoError(t, err)
	assert.True(t, created)
	created, err = pinned.mkdirIfMissing(filepath.Join(rootPath, "created"), 0o755)
	require.NoError(t, err)
	assert.False(t, created)
	filePath := filepath.Join(rootPath, "file")
	require.NoError(t, os.WriteFile(filePath, []byte("file"), 0o600))
	created, err = pinned.mkdirIfMissing(filePath, 0o755)
	require.Error(t, err)
	assert.False(t, created)
	_, err = pinned.mkdirIfMissing(filepath.Join(base, "outside-dir"), 0o755)
	require.ErrorIs(t, err, errDestinationEscapesRoot)

	if runtime.GOOS != "windows" {
		require.NoError(t, pinned.syncParent(rootPath))
	}
	require.ErrorIs(t, pinned.syncParent(filepath.Join(base, "outside-sync")), errDestinationEscapesRoot)
	ops := pinned.dirOps()
	assert.NotNil(t, ops.mkdirRoot)
	assert.NotNil(t, ops.mkdirIfMissing)
	assert.NotNil(t, ops.copyFile)
	assert.NotNil(t, ops.syncParent)
	pinned.close()
	assert.Nil(t, pinned.root)
}

func TestPinnedDestinationCopyDecisionErrorsAndVerification(t *testing.T) {
	base := t.TempDir()
	rootPath := filepath.Join(base, "root")
	require.NoError(t, os.Mkdir(rootPath, 0o700))
	src := filepath.Join(base, "source")
	const content = "trusted source content"
	require.NoError(t, os.WriteFile(src, []byte(content), 0o600))

	root, err := os.OpenRoot(rootPath)
	require.NoError(t, err)
	defer func() { _ = root.Close() }()

	t.Run("cancellation stops before hashing", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		changed, verify, err := copyFileIfChangedDeferredAtRoot(ctx, src, "cancelled", root, "cancelled", func(context.Context, string, string) error {
			t.Fatal("cancelled copy reached its callback")
			return nil
		})
		require.ErrorIs(t, err, context.Canceled)
		assert.False(t, changed)
		assert.Nil(t, verify)
	})

	t.Run("source hash error is reported", func(t *testing.T) {
		_, verify, err := copyFileIfChangedDeferredAtRoot(context.Background(), filepath.Join(base, "missing-source"), "missing-source", root, "missing-source", func(context.Context, string, string) error {
			t.Fatal("missing source reached its callback")
			return nil
		})
		require.ErrorContains(t, err, "hash source")
		assert.Nil(t, verify)
	})

	t.Run("destination comparison error is reported", func(t *testing.T) {
		directory := filepath.Join(rootPath, "directory")
		require.NoError(t, os.Mkdir(directory, 0o700))
		changed, verify, err := copyFileIfChangedDeferredAtRoot(context.Background(), src, directory, root, "directory", func(context.Context, string, string) error {
			t.Fatal("directory comparison reached its callback")
			return nil
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "compare destination")
		assert.False(t, changed)
		assert.Nil(t, verify)
	})

	t.Run("symlink skip is treated as no write", func(t *testing.T) {
		changed, verify, err := copyFileIfChangedDeferredAtRoot(context.Background(), src, "symlink-skip", root, "symlink-skip", func(context.Context, string, string) error {
			return ErrSymlinkSkipped
		})
		require.NoError(t, err)
		assert.False(t, changed)
		assert.Nil(t, verify)
	})

	t.Run("copy error is returned", func(t *testing.T) {
		copyErr := errors.New("copy failed")
		changed, verify, err := copyFileIfChangedDeferredAtRoot(context.Background(), src, "copy-error", root, "copy-error", func(context.Context, string, string) error {
			return copyErr
		})
		require.ErrorIs(t, err, copyErr)
		assert.False(t, changed)
		assert.Nil(t, verify)
	})

	t.Run("post-write reread failure is surfaced", func(t *testing.T) {
		changed, verify, err := copyFileIfChangedDeferredAtRoot(context.Background(), src, "missing-after-copy", root, "missing-after-copy", func(context.Context, string, string) error {
			return nil
		})
		require.NoError(t, err)
		assert.True(t, changed)
		require.NotNil(t, verify)
		require.ErrorIs(t, verify(), ErrPostWriteVerification)
	})

	t.Run("post-write hash mismatch is surfaced", func(t *testing.T) {
		changed, verify, err := copyFileIfChangedDeferredAtRoot(context.Background(), src, "mismatch", root, "mismatch", func(_ context.Context, _, _ string) error {
			return os.WriteFile(filepath.Join(rootPath, "mismatch"), []byte("wrong"), 0o600)
		})
		require.NoError(t, err)
		assert.True(t, changed)
		require.NotNil(t, verify)
		require.ErrorIs(t, verify(), ErrPostWriteVerification)
	})

	t.Run("post-write matching content verifies", func(t *testing.T) {
		changed, verify, err := copyFileIfChangedDeferredAtRoot(context.Background(), src, "match", root, "match", func(_ context.Context, _, _ string) error {
			return os.WriteFile(filepath.Join(rootPath, "match"), []byte(content), 0o600)
		})
		require.NoError(t, err)
		assert.True(t, changed)
		require.NotNil(t, verify)
		require.NoError(t, verify())
	})
}

type destinationSyncStub struct {
	syncErr error
}

func (destinationSyncStub) mkdirAll(string, fs.FileMode) error { return nil }
func (destinationSyncStub) mkdir(string, fs.FileMode) error    { return nil }
func (destinationSyncStub) lstat(string) (fs.FileInfo, error)  { return nil, nil }
func (destinationSyncStub) createTemp(string, string) (*os.File, string, error) {
	return nil, "", nil
}
func (destinationSyncStub) remove(string) error         { return nil }
func (destinationSyncStub) rename(string, string) error { return nil }
func (d destinationSyncStub) syncDir(string) error      { return d.syncErr }

func TestDestinationSyncAdaptersPropagateDurabilityErrors(t *testing.T) {
	syncErr := errors.New("sync failed")
	dest := destinationSyncStub{syncErr: syncErr}
	if runtime.GOOS == "windows" {
		require.NoError(t, portableDestinationSync(dest, "."))
	} else {
		require.ErrorIs(t, portableDestinationSync(dest, "."), syncErr)
	}

	assert.Nil(t, adaptPathSync(nil))
	called := false
	adapted := adaptPathSync(func(dir string) error {
		called = true
		assert.Equal(t, "parent", dir)
		return syncErr
	})
	require.NotNil(t, adapted)
	require.ErrorIs(t, adapted(dest, "parent"), syncErr)
	assert.True(t, called)
}

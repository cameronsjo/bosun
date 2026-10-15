package fileutil

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPinnedRootFS_RejectsInvalidPathsAcrossInterfaces(t *testing.T) {
	rootPath := filepath.Join(t.TempDir(), "root")
	require.NoError(t, os.Mkdir(rootPath, 0o700))
	root, err := os.OpenRoot(rootPath)
	require.NoError(t, err)
	defer func() { _ = root.Close() }()

	filesystem := PinnedRootFS(root)
	statFS, ok := filesystem.(fs.StatFS)
	require.True(t, ok)
	readDirFS, ok := filesystem.(fs.ReadDirFS)
	require.True(t, ok)

	names := []string{"", "..", "nested/../file", "/absolute", "nested//file"}
	if runtime.GOOS == "windows" {
		names = append(names, `nested\file`)
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			_, err := filesystem.Open(name)
			assertPathInvalid(t, err, "open", name)

			_, err = statFS.Stat(name)
			assertPathInvalid(t, err, "stat", name)

			_, err = readDirFS.ReadDir(name)
			assertPathInvalid(t, err, "readdir", name)
		})
	}
}

func TestPinnedRootFS_PropagatesPinnedRootErrors(t *testing.T) {
	rootPath := filepath.Join(t.TempDir(), "root")
	require.NoError(t, os.Mkdir(rootPath, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(rootPath, "file"), []byte("content"), 0o600))

	root, err := os.OpenRoot(rootPath)
	require.NoError(t, err)
	require.NoError(t, root.Close())

	filesystem := PinnedRootFS(root)
	statFS, ok := filesystem.(fs.StatFS)
	require.True(t, ok)
	readDirFS, ok := filesystem.(fs.ReadDirFS)
	require.True(t, ok)

	_, err = filesystem.Open("file")
	require.Error(t, err)
	_, err = statFS.Stat("file")
	require.Error(t, err)
	_, err = readDirFS.ReadDir(".")
	require.Error(t, err)
}

func TestPinnedRootFS_PropagatesMissingEntryErrors(t *testing.T) {
	rootPath := filepath.Join(t.TempDir(), "root")
	require.NoError(t, os.Mkdir(rootPath, 0o700))
	root, err := os.OpenRoot(rootPath)
	require.NoError(t, err)
	defer func() { _ = root.Close() }()

	filesystem := PinnedRootFS(root)
	statFS, ok := filesystem.(fs.StatFS)
	require.True(t, ok)
	readDirFS, ok := filesystem.(fs.ReadDirFS)
	require.True(t, ok)

	_, err = filesystem.Open("missing")
	require.ErrorIs(t, err, fs.ErrNotExist)
	_, err = statFS.Stat("missing")
	require.ErrorIs(t, err, fs.ErrNotExist)
	_, err = readDirFS.ReadDir("missing")
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func assertPathInvalid(t *testing.T, err error, operation, name string) {
	t.Helper()
	var pathErr *fs.PathError
	require.ErrorAs(t, err, &pathErr)
	assert.Equal(t, operation, pathErr.Op)
	assert.Equal(t, name, pathErr.Path)
	assert.ErrorIs(t, pathErr.Err, fs.ErrInvalid)
}

package fileutil

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPinnedRootFS_ReadDirInfoRemainsBoundAfterPathReplacement(t *testing.T) {
	parent := t.TempDir()
	rootPath := filepath.Join(parent, "source")
	retainedPath := filepath.Join(parent, "source-original")
	const entryName = "trusted-file"
	const trustedContent = "trusted"
	const foreignContent = "foreign-content"
	require.NoError(t, os.Mkdir(rootPath, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(rootPath, entryName), []byte(trustedContent), 0600))

	root, err := os.OpenRoot(rootPath)
	require.NoError(t, err)
	defer func() { _ = root.Close() }()
	entries, err := fs.ReadDir(PinnedRootFS(root), ".")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, entryName, entries[0].Name())
	assert.True(t, entries[0].Type().IsRegular())

	require.NoError(t, os.Rename(rootPath, retainedPath))
	require.NoError(t, os.Mkdir(rootPath, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(rootPath, entryName), []byte(foreignContent), 0600))

	info, err := entries[0].Info()
	require.NoError(t, err)
	assert.Equal(t, entryName, info.Name())
	assert.True(t, info.Mode().IsRegular())
	assert.Equal(t, int64(len(trustedContent)), info.Size(), "directory-entry metadata must remain bound to the pinned root")
}

func TestPinnedRootFS_ReadDirListsPinnedNamesAndTypesAfterPathReplacement(t *testing.T) {
	parent := t.TempDir()
	rootPath := filepath.Join(parent, "source")
	retainedPath := filepath.Join(parent, "source-original")
	require.NoError(t, os.MkdirAll(filepath.Join(rootPath, "trusted-dir"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(rootPath, "trusted-file"), []byte("trusted"), 0600))

	root, err := os.OpenRoot(rootPath)
	require.NoError(t, err)
	defer func() { _ = root.Close() }()
	entries, err := fs.ReadDir(PinnedRootFS(root), ".")
	require.NoError(t, err)
	require.Len(t, entries, 2)

	require.NoError(t, os.Rename(rootPath, retainedPath))
	require.NoError(t, os.Mkdir(rootPath, 0700))
	require.NoError(t, os.Mkdir(filepath.Join(rootPath, "trusted-file"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(rootPath, "trusted-dir"), []byte("foreign"), 0600))

	byName := make(map[string]fs.DirEntry, len(entries))
	for _, entry := range entries {
		byName[entry.Name()] = entry
	}
	require.Contains(t, byName, "trusted-dir")
	require.Contains(t, byName, "trusted-file")
	assert.True(t, byName["trusted-dir"].Type().IsDir())
	assert.True(t, byName["trusted-file"].Type().IsRegular())
	info, err := byName["trusted-dir"].Info()
	require.NoError(t, err)
	assert.True(t, info.IsDir())
	info, err = byName["trusted-file"].Info()
	require.NoError(t, err)
	assert.True(t, info.Mode().IsRegular())
}

func TestPinnedRootFS_NestedReadDirInfoRemainsBoundAfterAncestorReplacement(t *testing.T) {
	parent := t.TempDir()
	rootPath := filepath.Join(parent, "source")
	retainedPath := filepath.Join(parent, "source-original")
	nestedPath := filepath.Join(rootPath, "trusted-dir")
	const entryName = "trusted-file"
	const trustedContent = "trusted nested content"
	const foreignContent = "foreign nested content with a different size"
	require.NoError(t, os.MkdirAll(nestedPath, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(nestedPath, entryName), []byte(trustedContent), 0600))

	root, err := os.OpenRoot(rootPath)
	require.NoError(t, err)
	defer func() { _ = root.Close() }()
	entries, err := fs.ReadDir(PinnedRootFS(root), "trusted-dir")
	require.NoError(t, err)
	require.Len(t, entries, 1)

	require.NoError(t, os.Rename(rootPath, retainedPath))
	require.NoError(t, os.MkdirAll(nestedPath, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(nestedPath, entryName), []byte(foreignContent), 0600))

	info, err := entries[0].Info()
	require.NoError(t, err)
	assert.Equal(t, entryName, info.Name())
	assert.Equal(t, int64(len(trustedContent)), info.Size(), "nested directory-entry metadata must remain bound to the pinned sub-root")
}

func TestPinnedRootFS_OpenHidesUnderlyingDirectoryMethods(t *testing.T) {
	rootPath := filepath.Join(t.TempDir(), "source")
	require.NoError(t, os.Mkdir(rootPath, 0700))

	root, err := os.OpenRoot(rootPath)
	require.NoError(t, err)
	defer func() { _ = root.Close() }()
	file, err := PinnedRootFS(root).Open(".")
	require.NoError(t, err)
	defer func() { _ = file.Close() }()

	_, exposesRawDirectoryMethods := file.(interface {
		Readdirnames(int) ([]string, error)
	})
	assert.False(t, exposesRawDirectoryMethods)
}

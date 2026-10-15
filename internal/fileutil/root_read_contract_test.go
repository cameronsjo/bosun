package fileutil

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func canonicalReadTestDir(t *testing.T) string {
	t.Helper()
	path, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return path
}

func TestPinnedRootSelectionContract(t *testing.T) {
	t.Run("read only selection leaves a missing root absent", func(t *testing.T) {
		parent := canonicalReadTestDir(t)
		path := filepath.Join(parent, "missing", "root")
		root, err := OpenPinnedRoot(path, false)
		require.ErrorIs(t, err, fs.ErrNotExist)
		assert.Nil(t, root)
		assert.NoDirExists(t, filepath.Join(parent, "missing"))
	})
	t.Run("create selects nested directories through parents", func(t *testing.T) {
		path := filepath.Join(canonicalReadTestDir(t), "nested", "root")
		root, err := OpenPinnedRoot(path, true)
		require.NoError(t, err)
		defer func() { _ = root.Close() }()
		info, err := root.Stat(".")
		require.NoError(t, err)
		assert.True(t, info.IsDir())
		assert.DirExists(t, path)
	})
	t.Run("file cannot become a directory selector", func(t *testing.T) {
		file := filepath.Join(canonicalReadTestDir(t), "file")
		require.NoError(t, os.WriteFile(file, []byte("unchanged"), 0600))
		for _, path := range []string{file, filepath.Join(file, "child")} {
			root, err := OpenPinnedRoot(path, true)
			require.Error(t, err)
			assert.Nil(t, root)
		}
		content, err := os.ReadFile(file)
		require.NoError(t, err)
		assert.Equal(t, "unchanged", string(content))
	})
	t.Run("unix backslash is part of a component", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("backslash is a Windows separator")
		}
		path := filepath.Join(canonicalReadTestDir(t), `root\name`)
		require.NoError(t, os.Mkdir(path, 0700))
		root, err := OpenPinnedRoot(path, false)
		require.NoError(t, err)
		require.NoError(t, root.Close())
	})
}

func TestRootRegularReadContract(t *testing.T) {
	parent := canonicalReadTestDir(t)
	path := filepath.Join(parent, "root")
	require.NoError(t, os.Mkdir(path, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(path, "config"), []byte("private config"), 0600))
	require.NoError(t, os.Mkdir(filepath.Join(path, "directory"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(parent, "outside"), []byte("outside secret"), 0600))
	root, err := OpenPinnedRoot(path, false)
	require.NoError(t, err)
	defer func() { _ = root.Close() }()

	t.Run("regular contents read from checked descriptor", func(t *testing.T) {
		file, err := OpenRootRegular(root, "config", false)
		require.NoError(t, err)
		defer func() { _ = file.Close() }()
		data, err := io.ReadAll(file)
		require.NoError(t, err)
		assert.Equal(t, "private config", string(data))
	})
	for _, test := range []struct{ name, path string }{
		{"directory is not regular", "directory"},
		{"missing entry", "missing"},
		{"parent escape", "../outside"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file, err := OpenRootRegular(root, test.path, true)
			require.Error(t, err)
			assert.Nil(t, file)
		})
	}
	t.Run("closed root cannot be reopened by pathname", func(t *testing.T) {
		closed, err := OpenPinnedRoot(path, false)
		require.NoError(t, err)
		require.NoError(t, closed.Close())
		file, err := OpenRootRegular(closed, "config", true)
		require.Error(t, err)
		assert.Nil(t, file)
	})
	t.Run("symlinks obey explicit follow policy", func(t *testing.T) {
		if err := os.Symlink("config", filepath.Join(path, "inside-link")); err != nil {
			if runtime.GOOS == "windows" {
				t.Skipf("symlink capability unavailable: %v", err)
			}
			require.NoError(t, err)
		}
		require.NoError(t, os.Symlink(filepath.Join(parent, "outside"), filepath.Join(path, "outside-link")))
		denied, err := OpenRootRegular(root, "inside-link", false)
		require.ErrorIs(t, err, ErrSymlinkSkipped)
		assert.Nil(t, denied)
		permitted, err := OpenRootRegular(root, "inside-link", true)
		require.NoError(t, err)
		data, err := io.ReadAll(permitted)
		require.NoError(t, err)
		require.NoError(t, permitted.Close())
		assert.Equal(t, "private config", string(data))
		escaped, err := OpenRootRegular(root, "outside-link", true)
		require.Error(t, err)
		assert.Nil(t, escaped)
	})
}

func TestRootReadRetainsNamespaceAcrossPathReplacement(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses Unix directory replacement semantics")
	}
	parent := canonicalReadTestDir(t)
	path := filepath.Join(parent, "selected")
	outside := filepath.Join(parent, "outside")
	require.NoError(t, os.Mkdir(path, 0700))
	require.NoError(t, os.Mkdir(outside, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(path, "config"), []byte("selected"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "config"), []byte("outside secret"), 0600))
	root, err := OpenPinnedRoot(path, false)
	require.NoError(t, err)
	defer func() { _ = root.Close() }()
	require.NoError(t, os.Rename(path, filepath.Join(parent, "original")))
	require.NoError(t, os.Symlink(outside, path))
	file, err := OpenRootRegular(root, "config", true)
	require.NoError(t, err)
	defer func() { _ = file.Close() }()
	content, err := io.ReadAll(file)
	require.NoError(t, err)
	assert.Equal(t, "selected", string(content), "a post-selection pathname replacement must not select the outside tree")
}

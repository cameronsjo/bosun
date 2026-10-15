package fileutil

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopyFileFromRootRetainsSourceNamespace(t *testing.T) {
	base := t.TempDir()
	source := filepath.Join(base, "source")
	require.NoError(t, os.Mkdir(source, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(source, "config"), []byte("trusted"), 0o600))
	root, err := os.OpenRoot(source)
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })
	require.NoError(t, os.Rename(source, filepath.Join(base, "retained")))
	require.NoError(t, os.Mkdir(source, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(source, "config"), []byte("foreign"), 0o600))
	destination := filepath.Join(base, "output", "config")

	require.NoError(t, CopyFileFromRoot(context.Background(), root, "config", destination))
	content, err := os.ReadFile(destination)
	require.NoError(t, err)
	assert.Equal(t, "trusted", string(content))
	_, err = root.Stat("config")
	require.NoError(t, err, "the caller retains ownership of its source root")
}

func TestCopyFileFromRootRefusesInvalidSourcesWithoutDestinationMutation(t *testing.T) {
	t.Run("nil root", func(t *testing.T) {
		destination := filepath.Join(t.TempDir(), "absent", "config")
		require.ErrorContains(t, CopyFileFromRoot(context.Background(), nil, "config", destination), "nil root")
		assert.NoDirExists(t, filepath.Dir(destination))
	})
	for _, kind := range []string{"missing", "directory", "closed root", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			base := t.TempDir()
			root, err := os.OpenRoot(base)
			require.NoError(t, err)
			t.Cleanup(func() { _ = root.Close() })
			ctx := context.Background()
			if kind == "directory" {
				require.NoError(t, os.Mkdir(filepath.Join(base, "config"), 0o700))
			} else if kind != "missing" {
				require.NoError(t, os.WriteFile(filepath.Join(base, "config"), []byte("trusted"), 0o600))
			}
			if kind == "closed root" {
				require.NoError(t, root.Close())
			}
			if kind == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			destination := filepath.Join(base, "absent", "config")
			err = CopyFileFromRoot(ctx, root, "config", destination)
			require.Error(t, err)
			if kind == "cancelled" {
				assert.ErrorIs(t, err, context.Canceled)
			}
			assert.NoDirExists(t, filepath.Dir(destination))
		})
	}
}

func TestRootCopyPublicAPIsVerifyWritesAndRetainCallerRoot(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	source := filepath.Join(base, "source")
	require.NoError(t, os.Mkdir(source, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(source, "config"), []byte("trusted"), 0o600))
	destinationRoot := filepath.Join(base, "destination")
	changed, err := CopyFileUnderRootIfChanged(context.Background(), filepath.Join(source, "config"), destinationRoot, filepath.Join(destinationRoot, "single"))
	require.NoError(t, err)
	assert.True(t, changed)
	root, err := os.OpenRoot(destinationRoot)
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })
	written, err := CopyDirUnderPinnedRootIfChanged(context.Background(), source, destinationRoot, filepath.Join(destinationRoot, "nested"), root)
	require.NoError(t, err)
	assert.Contains(t, written, "config")
	content, err := root.ReadFile("nested/config")
	require.NoError(t, err)
	assert.Equal(t, "trusted", string(content))
	_, err = CopyDirUnderPinnedRootIfChanged(context.Background(), source, destinationRoot, destinationRoot, nil)
	require.ErrorContains(t, err, "no pinned root")
}

func TestOpenRegularNoFollowPreservesRegularFileContract(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "config")
	require.NoError(t, os.WriteFile(path, []byte("trusted"), 0o600))
	file, err := OpenRegularNoFollow(path)
	require.NoError(t, err)
	content, readErr := io.ReadAll(file)
	require.NoError(t, file.Close())
	require.NoError(t, readErr)
	assert.Equal(t, "trusted", string(content))
	_, err = OpenRegularNoFollow(filepath.Join(base, "missing"))
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = OpenRegularNoFollow(base)
	require.ErrorIs(t, err, ErrUnsupportedFileType)
}

func TestCopyFileReportsRenameAndDurabilityFailures(t *testing.T) {
	t.Run("rename cannot replace a directory", func(t *testing.T) {
		base := t.TempDir()
		source := filepath.Join(base, "source")
		destination := filepath.Join(base, "destination")
		require.NoError(t, os.WriteFile(source, []byte("trusted"), 0o600))
		require.NoError(t, os.Mkdir(destination, 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(destination, "keep"), []byte("existing"), 0o600))
		err := copyFileWithOps(context.Background(), source, destination, (*os.File).Chmod, nil, io.Copy)
		require.ErrorContains(t, err, "rename to destination")
		assert.FileExists(t, filepath.Join(destination, "keep"))
		matches, err := filepath.Glob(filepath.Join(base, ".tmp-*"))
		require.NoError(t, err)
		assert.Empty(t, matches, "failed atomic rename must remove the private payload")
	})
	t.Run("directory sync failure is returned after the write", func(t *testing.T) {
		base := t.TempDir()
		source := filepath.Join(base, "source")
		destination := filepath.Join(base, "destination")
		require.NoError(t, os.WriteFile(source, []byte("trusted"), 0o600))
		fault := errors.New("injected directory sync failure")
		err := copyFileWithOps(context.Background(), source, destination, (*os.File).Chmod, func(string) error { return fault }, io.Copy)
		require.ErrorIs(t, err, fault)
		assert.ErrorContains(t, err, "sync destination directory")
		content, readErr := os.ReadFile(destination)
		require.NoError(t, readErr)
		assert.Equal(t, "trusted", string(content), "durability failure must not remove the renamed payload")
	})
}

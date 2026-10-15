//go:build windows

package reconcile

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func TestRenameNoReplaceAtWindowsMovesFileAndDirectory(t *testing.T) {
	parent := t.TempDir()
	parentHandle, err := openPinnedPath(parent)
	require.NoError(t, err)
	defer func() { _ = parentHandle.Close() }()

	t.Run("file", func(t *testing.T) {
		source := filepath.Join(parent, "source-file")
		destination := filepath.Join(parent, "destination-file")
		require.NoError(t, os.WriteFile(source, []byte("payload"), 0o600))

		require.NoError(t, renameNoReplaceAt(parentHandle, filepath.Base(source), parentHandle, filepath.Base(destination)))
		assert.NoFileExists(t, source)
		content, err := os.ReadFile(destination)
		require.NoError(t, err)
		assert.Equal(t, "payload", string(content))
	})

	t.Run("directory", func(t *testing.T) {
		source := filepath.Join(parent, "source-directory")
		destination := filepath.Join(parent, "destination-directory")
		require.NoError(t, os.Mkdir(source, 0o700))

		require.NoError(t, renameNoReplaceAt(parentHandle, filepath.Base(source), parentHandle, filepath.Base(destination)))
		assert.NoDirExists(t, source)
		assert.DirExists(t, destination)
	})

	t.Run("single-character-file", func(t *testing.T) {
		source := filepath.Join(parent, "s")
		destination := filepath.Join(parent, "d")
		require.NoError(t, os.WriteFile(source, []byte("payload"), 0o600))

		require.NoError(t, renameNoReplaceAt(parentHandle, filepath.Base(source), parentHandle, filepath.Base(destination)))
		assert.NoFileExists(t, source)
		assert.FileExists(t, destination)
	})
}

func TestRenameNoReplaceAtWindowsRefusesExistingDestination(t *testing.T) {
	parent := t.TempDir()
	source := filepath.Join(parent, "source")
	destination := filepath.Join(parent, "destination")
	require.NoError(t, os.WriteFile(source, []byte("source"), 0o600))
	require.NoError(t, os.WriteFile(destination, []byte("destination"), 0o600))

	parentHandle, err := openPinnedPath(parent)
	require.NoError(t, err)
	defer func() { _ = parentHandle.Close() }()

	err = renameNoReplaceAt(parentHandle, filepath.Base(source), parentHandle, filepath.Base(destination))
	require.Error(t, err)
	assert.FileExists(t, source)
	assert.FileExists(t, destination)
	sourceContent, err := os.ReadFile(source)
	require.NoError(t, err)
	destinationContent, err := os.ReadFile(destination)
	require.NoError(t, err)
	assert.Equal(t, "source", string(sourceContent))
	assert.Equal(t, "destination", string(destinationContent))
}

func TestRenameNoReplaceAtWindowsRejectsEmptyBasename(t *testing.T) {
	parent := t.TempDir()
	parentHandle, err := openPinnedPath(parent)
	require.NoError(t, err)
	defer func() { _ = parentHandle.Close() }()

	err = renameNoReplaceAt(parentHandle, "", parentHandle, "destination")
	assert.ErrorIs(t, err, syscall.EINVAL)
}

func TestRenameNoReplaceAtWindowsBindsParentHandles(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "parent")
	movedParent := filepath.Join(root, "moved-parent")
	replacementParent := filepath.Join(root, "parent")
	require.NoError(t, os.Mkdir(parent, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(parent, "source"), []byte("payload"), 0o600))

	parentHandle, err := openPinnedPath(parent)
	require.NoError(t, err)
	defer func() { _ = parentHandle.Close() }()

	if err := os.Rename(parent, movedParent); err != nil {
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			t.Skipf("filesystem does not permit renaming an open parent directory: %v", err)
		}
		require.NoError(t, err)
	}
	require.NoError(t, os.Mkdir(replacementParent, 0o700))

	require.NoError(t, renameNoReplaceAt(parentHandle, "source", parentHandle, "destination"))
	assert.FileExists(t, filepath.Join(movedParent, "destination"))
	assert.NoFileExists(t, filepath.Join(replacementParent, "destination"))
}

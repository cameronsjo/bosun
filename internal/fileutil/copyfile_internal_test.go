package fileutil

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopyFileWithChmod_PermissionFailurePrecedesPayloadCopy(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "source.txt")
	dstPath := filepath.Join(tmpDir, "dest.txt")
	require.NoError(t, os.WriteFile(srcPath, []byte("new content"), 0640))
	require.NoError(t, os.WriteFile(dstPath, []byte("old content"), 0600))

	chmodErr := errors.New("chmod unsupported")
	chmodCalled := false
	err := copyFileWithChmod(srcPath, dstPath, func(tmpFile *os.File, mode fs.FileMode) error {
		chmodCalled = true
		info, statErr := tmpFile.Stat()
		require.NoError(t, statErr)
		assert.Zero(t, info.Size(), "temporary file must be empty when permissions are applied")
		assert.Equal(t, fs.FileMode(0640), mode.Perm())
		return chmodErr
	})

	require.ErrorIs(t, err, chmodErr)
	assert.ErrorContains(t, err, "set permissions")
	assert.True(t, chmodCalled)

	dstContent, readErr := os.ReadFile(dstPath)
	require.NoError(t, readErr)
	assert.Equal(t, "old content", string(dstContent), "failed atomic copy must preserve the destination")

	tempMatches, globErr := filepath.Glob(filepath.Join(tmpDir, ".tmp-*"))
	require.NoError(t, globErr)
	assert.Empty(t, tempMatches, "failed atomic copy must remove its temporary file")
}

func TestCopyFileWithChmod_KeepsPayloadPrivateAndRestoresSpecialPermissions(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	srcPath, srcMode := createSetuidSource(t, tmpDir)
	dstPath := filepath.Join(tmpDir, "dest")

	var sizesAtChmod []int64
	var modesAtChmod []fs.FileMode
	var requestedModes []fs.FileMode
	var tempNames []string
	err := copyFileWithChmod(srcPath, dstPath, func(tmpFile *os.File, mode fs.FileMode) error {
		info, statErr := tmpFile.Stat()
		require.NoError(t, statErr)
		sizesAtChmod = append(sizesAtChmod, info.Size())
		modesAtChmod = append(modesAtChmod, info.Mode())
		requestedModes = append(requestedModes, mode)
		tempNames = append(tempNames, tmpFile.Name())
		return tmpFile.Chmod(mode)
	})
	require.NoError(t, err)
	assert.Equal(t, []int64{0, int64(len("executable"))}, sizesAtChmod)
	assert.Equal(t, []fs.FileMode{srcMode, srcMode}, requestedModes)
	assert.Equal(t, fs.FileMode(0600), modesAtChmod[0].Perm())
	assert.Equal(t, fs.FileMode(0600), modesAtChmod[1].Perm(), "payload must be copied under a private temp mode")
	assert.NotEqual(t, tempNames[0], tempNames[1], "permission probe must not become the payload temp file")

	dstInfo, err := os.Stat(dstPath)
	require.NoError(t, err)
	assert.Equal(t, srcMode, dstInfo.Mode())
}

func TestCopyFileWithChmod_SpecialPermissionRestoreFailurePreservesDestination(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	srcPath, _ := createSetuidSource(t, tmpDir)
	dstPath := filepath.Join(tmpDir, "dest")
	require.NoError(t, os.WriteFile(dstPath, []byte("old content"), 0600))

	restoreErr := errors.New("restore failed")
	chmodCalls := 0
	err := copyFileWithChmod(srcPath, dstPath, func(tmpFile *os.File, mode fs.FileMode) error {
		chmodCalls++
		if chmodCalls == 2 {
			return restoreErr
		}
		return tmpFile.Chmod(mode)
	})

	require.ErrorIs(t, err, restoreErr)
	assert.ErrorContains(t, err, "set final permissions")
	assert.Equal(t, 2, chmodCalls)

	dstContent, readErr := os.ReadFile(dstPath)
	require.NoError(t, readErr)
	assert.Equal(t, "old content", string(dstContent))

	tempMatches, globErr := filepath.Glob(filepath.Join(tmpDir, ".tmp-*"))
	require.NoError(t, globErr)
	assert.Empty(t, tempMatches)
}

func createSetuidSource(t *testing.T, dir string) (string, fs.FileMode) {
	t.Helper()

	srcPath := filepath.Join(dir, "source")
	require.NoError(t, os.WriteFile(srcPath, []byte("executable"), 0750))
	require.NoError(t, os.Chmod(srcPath, fs.FileMode(0750)|fs.ModeSetuid))

	srcInfo, err := os.Stat(srcPath)
	require.NoError(t, err)
	if srcInfo.Mode()&fs.ModeSetuid == 0 {
		t.Skip("filesystem does not retain setuid mode bits")
	}
	return srcPath, srcInfo.Mode()
}

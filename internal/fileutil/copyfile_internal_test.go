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

func TestValidateRegularFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mode    fs.FileMode
		wantErr bool
	}{
		{name: "regular file", mode: 0o640},
		{name: "directory", mode: fs.ModeDir | 0o755, wantErr: true},
		{name: "named pipe", mode: fs.ModeNamedPipe | 0o600, wantErr: true},
		{name: "device", mode: fs.ModeDevice | 0o600, wantErr: true},
		{name: "socket", mode: fs.ModeSocket | 0o600, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateRegularFile("source", fakeFileInfo{mode: tt.mode})
			if tt.wantErr {
				require.ErrorIs(t, err, ErrUnsupportedFileType)
				assert.ErrorContains(t, err, "source has mode")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestValidateOpenedRegularFileRejectsUnfollowedSymlink(t *testing.T) {
	t.Parallel()

	err := validateOpenedRegularFile("source", false, fakeFileInfo{mode: fs.ModeSymlink | 0o777})

	require.ErrorIs(t, err, ErrSymlinkSkipped)
}

func TestOpenRegularSourceWith(t *testing.T) {
	t.Parallel()

	t.Run("open failure", func(t *testing.T) {
		t.Parallel()

		openErr := errors.New("open failed")
		file, info, err := openRegularSourceWith("source", true, func(string, bool) (*os.File, error) {
			return nil, openErr
		})

		require.ErrorIs(t, err, openErr)
		assert.ErrorContains(t, err, "open source")
		assert.Nil(t, file)
		assert.Nil(t, info)
	})

	t.Run("nofollow open failure on symlink preserves skip sentinel", func(t *testing.T) {
		t.Parallel()

		tmpDir, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)
		target := filepath.Join(tmpDir, "target")
		link := filepath.Join(tmpDir, "link")
		require.NoError(t, os.WriteFile(target, []byte("content"), 0o600))
		require.NoError(t, os.Symlink(target, link))

		file, info, err := openRegularSourceWith(link, false, func(string, bool) (*os.File, error) {
			return nil, errors.New("nofollow open failed")
		})

		require.ErrorIs(t, err, ErrSymlinkSkipped)
		assert.Nil(t, file)
		assert.Nil(t, info)
	})

	t.Run("stat failure closes descriptor", func(t *testing.T) {
		t.Parallel()

		closed, err := os.CreateTemp(t.TempDir(), "closed-source")
		require.NoError(t, err)
		require.NoError(t, closed.Close())

		file, info, err := openRegularSourceWith("source", true, func(string, bool) (*os.File, error) {
			return closed, nil
		})

		require.ErrorIs(t, err, os.ErrClosed)
		assert.ErrorContains(t, err, "stat source")
		assert.Nil(t, file)
		assert.Nil(t, info)
	})

	t.Run("rejected descriptor is closed", func(t *testing.T) {
		t.Parallel()

		reader, writer, err := os.Pipe()
		require.NoError(t, err)
		defer func() { _ = writer.Close() }()

		file, info, err := openRegularSourceWith("source", true, func(string, bool) (*os.File, error) {
			return reader, nil
		})

		require.ErrorIs(t, err, ErrUnsupportedFileType)
		assert.Nil(t, file)
		assert.Nil(t, info)
		assert.ErrorIs(t, reader.Close(), os.ErrClosed)
	})
}

func TestValidateCopyPermissions_ReportsLifecycleFailures(t *testing.T) {
	t.Parallel()

	t.Run("create", func(t *testing.T) {
		t.Parallel()

		createErr := errors.New("create failed")
		err := validateCopyPermissions(t.TempDir(), 0640,
			func(string, string) (*os.File, error) { return nil, createErr },
			(*os.File).Chmod,
			os.Remove,
		)

		require.ErrorIs(t, err, createErr)
		assert.ErrorContains(t, err, "create permission probe")
	})

	t.Run("close", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		var probePath string
		err := validateCopyPermissions(tmpDir, 0640,
			func(dir, pattern string) (*os.File, error) {
				probe, createErr := os.CreateTemp(dir, pattern)
				if createErr == nil {
					probePath = probe.Name()
				}
				return probe, createErr
			},
			func(probe *os.File, _ fs.FileMode) error { return probe.Close() },
			os.Remove,
		)

		require.Error(t, err)
		assert.ErrorContains(t, err, "close permission probe")
		assert.NoFileExists(t, probePath, "deferred cleanup must remove a probe whose close already failed")
	})

	t.Run("remove", func(t *testing.T) {
		t.Parallel()

		removeErr := errors.New("remove failed")
		removeCalls := 0
		var probePath string
		err := validateCopyPermissions(t.TempDir(), 0640,
			func(dir, pattern string) (*os.File, error) {
				probe, createErr := os.CreateTemp(dir, pattern)
				if createErr == nil {
					probePath = probe.Name()
				}
				return probe, createErr
			},
			(*os.File).Chmod,
			func(path string) error {
				removeCalls++
				if removeCalls == 1 {
					return removeErr
				}
				return os.Remove(path)
			},
		)

		require.ErrorIs(t, err, removeErr)
		assert.ErrorContains(t, err, "remove permission probe")
		assert.Equal(t, 2, removeCalls, "deferred cleanup must retry a failed probe removal")
		assert.NoFileExists(t, probePath)
	})
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

//go:build darwin || linux

package fileutil

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileOperationsRejectNamedPipesWithoutBlocking(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		recursive         bool
		destinationSeeded bool
		run               func(context.Context, string, string) error
	}{
		{
			name: "CopyFile",
			run: func(ctx context.Context, source, destination string) error {
				return CopyFile(ctx, source, destination)
			},
		},
		{
			name: "FileHash",
			run: func(_ context.Context, source, _ string) error {
				_, err := FileHash(source)
				return err
			},
		},
		{
			name: "ContentEqual",
			run: func(_ context.Context, source, _ string) error {
				_, err := ContentEqual(source, [32]byte{})
				return err
			},
		},
		{
			name: "CopyFileIfChanged",
			run: func(ctx context.Context, source, destination string) error {
				_, err := CopyFileIfChanged(ctx, source, destination)
				return err
			},
		},
		{
			name:              "filesEqualContext",
			destinationSeeded: true,
			run: func(ctx context.Context, source, destination string) error {
				if err := os.WriteFile(destination, []byte("payload"), 0o600); err != nil {
					return err
				}
				_, err := filesEqualContext(ctx, source, destination)
				return err
			},
		},
		{
			name:      "CopyDir",
			recursive: true,
			run: func(ctx context.Context, source, destination string) error {
				return CopyDir(ctx, filepath.Dir(source), destination)
			},
		},
		{
			name:      "CopyDirIfChanged",
			recursive: true,
			run: func(ctx context.Context, source, destination string) error {
				_, err := CopyDirIfChanged(ctx, filepath.Dir(source), destination)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()
			sourceDir := filepath.Join(tmpDir, "source")
			require.NoError(t, os.Mkdir(sourceDir, 0o755))
			pipePath := filepath.Join(sourceDir, "payload")
			require.NoError(t, syscall.Mkfifo(pipePath, 0o600))
			destination := filepath.Join(tmpDir, "destination")

			err := runNamedPipeOperation(t, pipePath, func() error {
				return tt.run(context.Background(), pipePath, destination)
			})

			require.ErrorIs(t, err, ErrUnsupportedFileType)
			assert.ErrorContains(t, err, pipePath)
			if tt.recursive {
				assert.NoFileExists(t, filepath.Join(destination, filepath.Base(pipePath)))
			} else if !tt.destinationSeeded {
				assert.NoFileExists(t, destination)
			}
		})
	}
}

func TestCopyFileRevalidatesSourceAfterLstat(t *testing.T) {
	t.Parallel()

	t.Run("replacement named pipe", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		source := filepath.Join(tmpDir, "source")
		destination := filepath.Join(tmpDir, "destination")
		require.NoError(t, os.WriteFile(source, []byte("regular"), 0o600))

		err := runNamedPipeOperation(t, source, func() error {
			return copyFileWithOpsAndOpen(
				context.Background(), source, destination, (*os.File).Chmod,
				syncDestinationDir, io.Copy,
				func(path string, followSymlinks bool) (*os.File, os.FileInfo, error) {
					if err := os.Remove(path); err != nil {
						return nil, nil, err
					}
					if err := syscall.Mkfifo(path, 0o600); err != nil {
						return nil, nil, err
					}
					return openRegularSource(path, followSymlinks)
				},
			)
		})

		require.ErrorIs(t, err, ErrUnsupportedFileType)
		assert.ErrorContains(t, err, source)
		assert.NoFileExists(t, destination)
	})

	t.Run("replacement symlink", func(t *testing.T) {
		t.Parallel()

		tmpDir, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)
		source := filepath.Join(tmpDir, "source")
		target := filepath.Join(tmpDir, "target")
		destination := filepath.Join(tmpDir, "destination")
		require.NoError(t, os.WriteFile(source, []byte("original"), 0o600))
		require.NoError(t, os.WriteFile(target, []byte("replacement"), 0o600))

		err = copyFileWithOpsAndOpen(
			context.Background(), source, destination, (*os.File).Chmod,
			syncDestinationDir, io.Copy,
			func(path string, followSymlinks bool) (*os.File, os.FileInfo, error) {
				if removeErr := os.Remove(path); removeErr != nil {
					return nil, nil, removeErr
				}
				if symlinkErr := os.Symlink(target, path); symlinkErr != nil {
					return nil, nil, symlinkErr
				}
				return openRegularSource(path, followSymlinks)
			},
		)

		require.ErrorIs(t, err, ErrSymlinkSkipped)
		assert.NoFileExists(t, destination)
	})
}

func TestFileHashRevalidatesSourceAfterStat(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	source := filepath.Join(tmpDir, "source")
	require.NoError(t, os.WriteFile(source, []byte("regular"), 0o600))

	err := runNamedPipeOperation(t, source, func() error {
		_, hashErr := fileHashContextWithOpen(
			context.Background(), source,
			func(path string, followSymlinks bool) (*os.File, os.FileInfo, error) {
				if err := os.Remove(path); err != nil {
					return nil, nil, err
				}
				if err := syscall.Mkfifo(path, 0o600); err != nil {
					return nil, nil, err
				}
				return openRegularSource(path, followSymlinks)
			},
		)
		return hashErr
	})

	require.ErrorIs(t, err, ErrUnsupportedFileType)
	assert.ErrorContains(t, err, source)
}

func runNamedPipeOperation(t *testing.T, pipePath string, operation func() error) error {
	t.Helper()

	done := make(chan error, 1)
	go func() {
		done <- operation()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		// Unblock a regressed read-only FIFO open before failing so the test does
		// not leak a goroutine or file descriptor into later package tests.
		writer, err := os.OpenFile(pipePath, os.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err == nil {
			_ = writer.Close()
		}
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
		t.Fatal("operation blocked opening a named pipe")
		return nil
	}
}

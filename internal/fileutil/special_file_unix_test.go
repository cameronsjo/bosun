//go:build darwin || linux

package fileutil

import (
	"context"
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
		name      string
		recursive bool
		run       func(context.Context, string, string) error
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
			name: "CopyFileIfChanged",
			run: func(ctx context.Context, source, destination string) error {
				_, err := CopyFileIfChanged(ctx, source, destination)
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
			} else {
				assert.NoFileExists(t, destination)
			}
		})
	}
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

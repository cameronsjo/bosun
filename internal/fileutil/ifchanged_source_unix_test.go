//go:build darwin || linux

package fileutil

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ifChangedEntryPoints() map[string]func(ctx context.Context, src, root, dst string) (bool, error) {
	return map[string]func(ctx context.Context, src, root, dst string) (bool, error){
		"CopyFileIfChanged": func(ctx context.Context, src, _, dst string) (bool, error) {
			return CopyFileIfChanged(ctx, src, dst)
		},
		"CopyFileUnderRootIfChanged": func(ctx context.Context, src, root, dst string) (bool, error) {
			return CopyFileUnderRootIfChanged(ctx, src, root, dst)
		},
	}
}

// TestCopyFileIfChangedSkipsLinkToNamedPipe pins that a symlink whose target
// is a FIFO is skipped like any other symlink source: no follow, no open of
// the pipe, no error (cameronsjo/bosun#802).
func TestCopyFileIfChangedSkipsLinkToNamedPipe(t *testing.T) {
	t.Parallel()

	for name, run := range ifChangedEntryPoints() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir, err := filepath.EvalSymlinks(t.TempDir())
			require.NoError(t, err)
			pipePath := filepath.Join(dir, "pipe")
			require.NoError(t, syscall.Mkfifo(pipePath, 0o600))
			src := filepath.Join(dir, "link")
			require.NoError(t, os.Symlink(pipePath, src))
			root := filepath.Join(dir, "root")
			require.NoError(t, os.Mkdir(root, 0o755))
			dst := filepath.Join(root, "dst.txt")

			var changed bool
			err = runNamedPipeOperation(t, pipePath, func() error {
				var runErr error
				changed, runErr = run(context.Background(), src, root, dst)
				return runErr
			})

			require.NoError(t, err, "a link to a FIFO must be skipped, not followed")
			assert.False(t, changed)
			assert.NoFileExists(t, dst)
		})
	}
}

// TestCopyFileIfChangedReportsSourceInspectionFailure pins the error for a
// source whose Lstat fails for a reason other than absence: both entry points
// report it as a source inspection failure before any hash or destination
// work.
func TestCopyFileIfChangedReportsSourceInspectionFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permission checks")
	}

	for name, run := range ifChangedEntryPoints() {
		t.Run(name, func(t *testing.T) {
			dir, err := filepath.EvalSymlinks(t.TempDir())
			require.NoError(t, err)
			locked := filepath.Join(dir, "locked")
			require.NoError(t, os.Mkdir(locked, 0o755))
			src := filepath.Join(locked, "source.txt")
			require.NoError(t, os.WriteFile(src, []byte("payload"), 0o644))
			require.NoError(t, os.Chmod(locked, 0o000))
			t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
			root := filepath.Join(dir, "root")
			dst := filepath.Join(root, "dst.txt")

			changed, err := run(context.Background(), src, root, dst)

			require.Error(t, err)
			assert.ErrorIs(t, err, os.ErrPermission)
			assert.ErrorContains(t, err, "inspect source")
			assert.False(t, changed)
			_, statErr := os.Lstat(root)
			assert.ErrorIs(t, statErr, os.ErrNotExist, "an unreadable source must not create the destination root")
		})
	}
}

package fileutil

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type cancelOnErrCheckContext struct {
	context.Context
	cancelOn int
	checks   int
}

func (c *cancelOnErrCheckContext) Err() error {
	c.checks++
	if c.checks >= c.cancelOn {
		return context.Canceled
	}
	return nil
}

func TestCopyDirIfChangedStopsBeforeNextMutationAfterCancellation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := filepath.Join(root, "source")
	dst := filepath.Join(root, "destination")
	writeTestFile(t, filepath.Join(src, "a-first.txt"), "first")
	writeTestFile(t, filepath.Join(src, "b-second.txt"), "second")

	ctx, cancel := context.WithCancel(context.Background())
	copyCalls := 0
	written, err := copyDirIfChangedWithOps(
		ctx,
		src,
		dst,
		func(ctx context.Context, src, dst string) (bool, postWriteVerification, error) {
			copyCalls++
			copyErr := copyFileWithoutDirSyncContext(ctx, src, dst)
			cancel()
			return copyErr == nil, nil, copyErr
		},
		syncDestinationDir,
	)

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, copyCalls)
	assert.Equal(t, []string{"a-first.txt"}, written)
	assert.FileExists(t, filepath.Join(dst, "a-first.txt"))
	assert.NoFileExists(t, filepath.Join(dst, "b-second.txt"))
}

func TestCopyDirStopsBeforeNextMutationAfterCancellation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := filepath.Join(root, "source")
	dst := filepath.Join(root, "destination")
	writeTestFile(t, filepath.Join(src, "a-first.txt"), "first")
	writeTestFile(t, filepath.Join(src, "b-second.txt"), "second")

	ctx, cancel := context.WithCancel(context.Background())
	copyCalls := 0
	err := copyDirWithOps(ctx, src, dst, func(ctx context.Context, src, dst string) error {
		copyCalls++
		copyErr := copyFileWithoutDirSyncContext(ctx, src, dst)
		cancel()
		return copyErr
	}, syncDestinationDir)

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, copyCalls)
	assert.FileExists(t, filepath.Join(dst, "a-first.txt"))
	assert.NoFileExists(t, filepath.Join(dst, "b-second.txt"))
}

func TestCopyFileIfChangedCancelledBeforeMutation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := filepath.Join(root, "source.txt")
	dst := filepath.Join(root, "destination.txt")
	require.NoError(t, os.WriteFile(src, []byte("new"), 0o644))
	require.NoError(t, os.WriteFile(dst, []byte("preserve"), 0o644))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	changed, err := CopyFileIfChanged(ctx, src, dst)

	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, changed)
	content, readErr := os.ReadFile(dst)
	require.NoError(t, readErr)
	assert.Equal(t, "preserve", string(content))
}

func TestCopyFileCancelledBeforeMutation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := filepath.Join(root, "source.txt")
	dst := filepath.Join(root, "destination.txt")
	require.NoError(t, os.WriteFile(src, []byte("new"), 0o644))
	require.NoError(t, os.WriteFile(dst, []byte("preserve"), 0o644))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := CopyFile(ctx, src, dst)

	require.ErrorIs(t, err, context.Canceled)
	content, readErr := os.ReadFile(dst)
	require.NoError(t, readErr)
	assert.Equal(t, "preserve", string(content))
}

func TestCopyFileStopsAtEachMutationBoundary(t *testing.T) {
	t.Parallel()

	for _, cancelOn := range []int{2, 3, 4, 5, 6, 7} {
		t.Run(fmt.Sprintf("err_check_%d", cancelOn), func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			src := filepath.Join(root, "source.txt")
			dst := filepath.Join(root, "destination.txt")
			require.NoError(t, os.WriteFile(src, nil, 0o644))
			require.NoError(t, os.WriteFile(dst, []byte("preserve"), 0o644))

			ctx := &cancelOnErrCheckContext{Context: context.Background(), cancelOn: cancelOn}
			err := CopyFile(ctx, src, dst)

			require.ErrorIs(t, err, context.Canceled)
			content, readErr := os.ReadFile(dst)
			require.NoError(t, readErr)
			assert.Equal(t, "preserve", string(content))
		})
	}
}

func TestCopyFileIfChangedCancelledAfterComparison(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := filepath.Join(root, "source.txt")
	dst := filepath.Join(root, "destination.txt")
	require.NoError(t, os.WriteFile(src, []byte("new"), 0o644))
	require.NoError(t, os.WriteFile(dst, []byte("preserve"), 0o644))
	ctx := &cancelOnErrCheckContext{Context: context.Background(), cancelOn: 2}

	changed, err := CopyFileIfChanged(ctx, src, dst)

	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, changed)
	content, readErr := os.ReadFile(dst)
	require.NoError(t, readErr)
	assert.Equal(t, "preserve", string(content))
}

func TestCopyFileCancellationCleansPartiallyWrittenTemp(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := filepath.Join(root, "source.txt")
	dst := filepath.Join(root, "destination.txt")
	require.NoError(t, os.WriteFile(src, []byte("new-payload"), 0o644))
	require.NoError(t, os.WriteFile(dst, []byte("preserve"), 0o644))
	ctx, cancel := context.WithCancel(context.Background())

	err := copyFileWithOps(ctx, src, dst, (*os.File).Chmod, syncDestinationDir, func(writer io.Writer, reader io.Reader) (int64, error) {
		partial := make([]byte, 3)
		n, readErr := reader.Read(partial)
		require.NoError(t, readErr)
		written, writeErr := writer.Write(partial[:n])
		require.NoError(t, writeErr)
		cancel()
		rest, copyErr := io.Copy(writer, reader)
		return int64(written) + rest, copyErr
	})

	require.ErrorIs(t, err, context.Canceled)
	content, readErr := os.ReadFile(dst)
	require.NoError(t, readErr)
	assert.Equal(t, "preserve", string(content))
	entries, readDirErr := os.ReadDir(root)
	require.NoError(t, readDirErr)
	for _, entry := range entries {
		assert.NotContains(t, entry.Name(), ".tmp-", "partial payload temp file must be removed")
	}
}

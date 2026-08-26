package fileutil

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

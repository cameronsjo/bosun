package fileutil

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type cancelOnErrCheckContext struct {
	context.Context
	cancelOn int
	checks   int
}

type cancelAfterFirstRead struct {
	cancel context.CancelFunc
	read   bool
}

func (r *cancelAfterFirstRead) Read(p []byte) (int, error) {
	if r.read {
		return 0, io.EOF
	}
	r.read = true
	p[0] = 'x'
	r.cancel()
	return 1, nil
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

func TestCopyFileIfChangedCancellationDuringReadPhases(t *testing.T) {
	t.Parallel()

	setup := func(t *testing.T, sameContent bool) (string, string) {
		t.Helper()
		root := t.TempDir()
		src := filepath.Join(root, "source.txt")
		dst := filepath.Join(root, "destination.txt")
		require.NoError(t, os.WriteFile(src, []byte("new-content"), 0o644))
		dstContent := []byte("preserve")
		if sameContent {
			dstContent = []byte("new-content")
		}
		require.NoError(t, os.WriteFile(dst, dstContent, 0o644))
		return src, dst
	}

	t.Run("source hash stops before destination read or copy", func(t *testing.T) {
		t.Parallel()
		src, dst := setup(t, false)
		ctx, cancel := context.WithCancel(context.Background())
		hashCalls := 0
		copyCalls := 0
		hashFile := func(hashCtx context.Context, _ string) ([sha256.Size]byte, error) {
			hashCalls++
			return hashReader(hashCtx, &cancelAfterFirstRead{cancel: cancel})
		}

		changed, verify, err := copyFileIfChangedDeferredWithOps(ctx, src, dst, hashFile, filesEqualContext, func(context.Context, string, string) error {
			copyCalls++
			return nil
		})

		require.ErrorIs(t, err, context.Canceled)
		assert.False(t, changed)
		assert.Nil(t, verify)
		assert.Equal(t, 1, hashCalls)
		assert.Equal(t, 0, copyCalls)
		content, readErr := os.ReadFile(dst)
		require.NoError(t, readErr)
		assert.Equal(t, "preserve", string(content))
	})

	t.Run("destination hash stops before readback or copy", func(t *testing.T) {
		t.Parallel()
		src, dst := setup(t, false)
		ctx, cancel := context.WithCancel(context.Background())
		hashCalls := 0
		compareCalls := 0
		copyCalls := 0
		hashFile := func(hashCtx context.Context, path string) ([sha256.Size]byte, error) {
			hashCalls++
			if hashCalls == 2 {
				return hashReader(hashCtx, &cancelAfterFirstRead{cancel: cancel})
			}
			return fileHashContext(hashCtx, path)
		}

		changed, verify, err := copyFileIfChangedDeferredWithOps(ctx, src, dst, hashFile, func(context.Context, string, string) (bool, error) {
			compareCalls++
			return true, nil
		}, func(context.Context, string, string) error {
			copyCalls++
			return nil
		})

		require.ErrorIs(t, err, context.Canceled)
		assert.False(t, changed)
		assert.Nil(t, verify)
		assert.Equal(t, 2, hashCalls)
		assert.Equal(t, 0, compareCalls)
		assert.Equal(t, 0, copyCalls)
		content, readErr := os.ReadFile(dst)
		require.NoError(t, readErr)
		assert.Equal(t, "preserve", string(content))
	})

	t.Run("equal-content readback stops before copy", func(t *testing.T) {
		t.Parallel()
		src, dst := setup(t, true)
		ctx, cancel := context.WithCancel(context.Background())
		compareCalls := 0
		copyCalls := 0
		filesEqual := func(compareCtx context.Context, _, _ string) (bool, error) {
			compareCalls++
			return readersEqualContext(compareCtx, &cancelAfterFirstRead{cancel: cancel}, strings.NewReader("x"))
		}

		changed, verify, err := copyFileIfChangedDeferredWithOps(ctx, src, dst, fileHashContext, filesEqual, func(context.Context, string, string) error {
			copyCalls++
			return nil
		})

		require.ErrorIs(t, err, context.Canceled)
		assert.False(t, changed)
		assert.Nil(t, verify)
		assert.Equal(t, 1, compareCalls)
		assert.Equal(t, 0, copyCalls)
	})

	t.Run("post-write hash reports cancellation after visible write", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		src := filepath.Join(root, "source.txt")
		dst := filepath.Join(root, "destination.txt")
		require.NoError(t, os.WriteFile(src, []byte("new-content"), 0o644))
		ctx, cancel := context.WithCancel(context.Background())
		hashCalls := 0
		copyCalls := 0
		hashFile := func(hashCtx context.Context, path string) ([sha256.Size]byte, error) {
			hashCalls++
			if hashCalls == 3 {
				return hashReader(hashCtx, &cancelAfterFirstRead{cancel: cancel})
			}
			return fileHashContext(hashCtx, path)
		}

		changed, verify, err := copyFileIfChangedDeferredWithOps(ctx, src, dst, hashFile, filesEqualContext, func(copyCtx context.Context, src, dst string) error {
			copyCalls++
			return copyFileWithoutDirSyncContext(copyCtx, src, dst)
		})
		require.NoError(t, err)
		require.NotNil(t, verify)
		err = verify()

		require.ErrorIs(t, err, context.Canceled)
		assert.ErrorIs(t, err, ErrPostWriteVerification)
		assert.True(t, changed)
		assert.Equal(t, 3, hashCalls)
		assert.Equal(t, 1, copyCalls)
		content, readErr := os.ReadFile(dst)
		require.NoError(t, readErr)
		assert.Equal(t, "new-content", string(content))
	})
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

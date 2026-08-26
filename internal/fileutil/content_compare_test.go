package fileutil

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type boundedRepeatingReader struct {
	remaining  int64
	value      byte
	maxRequest int
}

func (r *boundedRepeatingReader) Read(p []byte) (int, error) {
	if len(p) > r.maxRequest {
		r.maxRequest = len(p)
	}
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := int64(len(p))
	if n > r.remaining {
		n = r.remaining
	}
	for i := range p[:n] {
		p[i] = r.value
	}
	r.remaining -= n
	return int(n), nil
}

type chunkedReader struct {
	data  []byte
	chunk int
}

func (r *chunkedReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := min(len(r.data), min(len(p), r.chunk))
	copy(p, r.data[:n])
	r.data = r.data[n:]
	return n, nil
}

type errorReader struct {
	err error
}

func (r errorReader) Read([]byte) (int, error) {
	return 0, r.err
}

type trackingReadCloser struct {
	io.Reader
	closeCalls int
}

func (r *trackingReadCloser) Close() error {
	r.closeCalls++
	return nil
}

func TestReadersEqualContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		a     func() io.Reader
		b     func() io.Reader
		equal bool
	}{
		{
			name:  "empty",
			a:     func() io.Reader { return strings.NewReader("") },
			b:     func() io.Reader { return strings.NewReader("") },
			equal: true,
		},
		{
			name:  "equal across short reads",
			a:     func() io.Reader { return &chunkedReader{data: []byte("same content"), chunk: 1} },
			b:     func() io.Reader { return &chunkedReader{data: []byte("same content"), chunk: 3} },
			equal: true,
		},
		{
			name:  "different content",
			a:     func() io.Reader { return strings.NewReader("content-a") },
			b:     func() io.Reader { return strings.NewReader("content-b") },
			equal: false,
		},
		{
			name:  "first shorter",
			a:     func() io.Reader { return strings.NewReader("short") },
			b:     func() io.Reader { return strings.NewReader("shorter") },
			equal: false,
		},
		{
			name:  "second shorter",
			a:     func() io.Reader { return strings.NewReader("longer") },
			b:     func() io.Reader { return strings.NewReader("long") },
			equal: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			equal, err := readersEqualContext(context.Background(), tt.a(), tt.b())

			require.NoError(t, err)
			assert.Equal(t, tt.equal, equal)
		})
	}
}

func TestReadersEqualContextBoundsMemoryPerRead(t *testing.T) {
	t.Parallel()

	const syntheticSize = 64 * 1024 * 1024
	a := &boundedRepeatingReader{remaining: syntheticSize, value: 'x'}
	b := &boundedRepeatingReader{remaining: syntheticSize, value: 'x'}

	equal, err := readersEqualContext(context.Background(), a, b)

	require.NoError(t, err)
	assert.True(t, equal)
	assert.LessOrEqual(t, a.maxRequest, contentCompareBufferSize)
	assert.LessOrEqual(t, b.maxRequest, contentCompareBufferSize)
}

func TestReadersEqualContextAllocationCountDoesNotScaleWithInput(t *testing.T) {
	allocationsForSize := func(size int64) float64 {
		comparisonFailed := false
		allocations := testing.AllocsPerRun(10, func() {
			a := &boundedRepeatingReader{remaining: size, value: 'x'}
			b := &boundedRepeatingReader{remaining: size, value: 'x'}
			equal, err := readersEqualContext(context.Background(), a, b)
			if err != nil || !equal {
				comparisonFailed = true
			}
		})
		require.False(t, comparisonFailed)
		return allocations
	}

	singleBuffer := allocationsForSize(contentCompareBufferSize)
	manyBuffers := allocationsForSize(1 << 20)

	assert.LessOrEqual(t, manyBuffers, singleBuffer+2,
		"streaming allocations must remain constant as input grows")
}

func TestReadersEqualContextPropagatesReadErrors(t *testing.T) {
	t.Parallel()

	injectedErr := errors.New("injected read failure")
	tests := []struct {
		name string
		a    io.Reader
		b    io.Reader
	}{
		{name: "first reader", a: errorReader{err: injectedErr}, b: strings.NewReader("content")},
		{name: "second reader", a: strings.NewReader("content"), b: errorReader{err: injectedErr}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			equal, err := readersEqualContext(context.Background(), tt.a, tt.b)

			assert.False(t, equal)
			assert.ErrorIs(t, err, injectedErr)
		})
	}
}

func TestReadersEqualContextPropagatesCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	equal, err := readersEqualContext(ctx, &cancelAfterFirstRead{cancel: cancel}, strings.NewReader("x"))

	assert.False(t, equal)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestFilesEqualContextClosesOpenedFiles(t *testing.T) {
	t.Parallel()

	t.Run("pre-cancelled context skips opens", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		openCalls := 0

		equal, err := filesEqualContextWithOpen(ctx, "first", "second", func(string) (io.ReadCloser, error) {
			openCalls++
			return nil, errors.New("must not open")
		})

		assert.False(t, equal)
		assert.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, 0, openCalls)
	})

	t.Run("first open failure preserves cause", func(t *testing.T) {
		t.Parallel()
		injectedErr := errors.New("injected first open failure")

		equal, err := filesEqualContextWithOpen(context.Background(), "first", "second", func(string) (io.ReadCloser, error) {
			return nil, injectedErr
		})

		assert.False(t, equal)
		assert.ErrorIs(t, err, injectedErr)
	})

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		first := &trackingReadCloser{Reader: strings.NewReader("same")}
		second := &trackingReadCloser{Reader: strings.NewReader("same")}
		open := func(path string) (io.ReadCloser, error) {
			if path == "first" {
				return first, nil
			}
			return second, nil
		}

		equal, err := filesEqualContextWithOpen(context.Background(), "first", "second", open)

		require.NoError(t, err)
		assert.True(t, equal)
		assert.Equal(t, 1, first.closeCalls)
		assert.Equal(t, 1, second.closeCalls)
	})

	t.Run("second open failure closes first", func(t *testing.T) {
		t.Parallel()
		first := &trackingReadCloser{Reader: strings.NewReader("same")}
		injectedErr := errors.New("injected open failure")
		open := func(path string) (io.ReadCloser, error) {
			if path == "first" {
				return first, nil
			}
			return nil, injectedErr
		}

		equal, err := filesEqualContextWithOpen(context.Background(), "first", "second", open)

		assert.False(t, equal)
		assert.ErrorIs(t, err, injectedErr)
		assert.Equal(t, 1, first.closeCalls)
	})

	t.Run("cancellation after first open closes first", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		first := &trackingReadCloser{Reader: strings.NewReader("same")}
		openCalls := 0
		open := func(string) (io.ReadCloser, error) {
			openCalls++
			cancel()
			return first, nil
		}

		equal, err := filesEqualContextWithOpen(ctx, "first", "second", open)

		assert.False(t, equal)
		assert.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, 1, openCalls)
		assert.Equal(t, 1, first.closeCalls)
	})

	t.Run("read failure closes both", func(t *testing.T) {
		t.Parallel()
		injectedErr := errors.New("injected read failure")
		first := &trackingReadCloser{Reader: errorReader{err: injectedErr}}
		second := &trackingReadCloser{Reader: strings.NewReader("same")}
		open := func(path string) (io.ReadCloser, error) {
			if path == "first" {
				return first, nil
			}
			return second, nil
		}

		equal, err := filesEqualContextWithOpen(context.Background(), "first", "second", open)

		assert.False(t, equal)
		assert.ErrorIs(t, err, injectedErr)
		assert.Equal(t, 1, first.closeCalls)
		assert.Equal(t, 1, second.closeCalls)
	})
}

func TestCopyFileIfChangedHashCollisionFallsBackToCopy(t *testing.T) {
	t.Parallel()

	injectedErr := errors.New("injected compare failure")
	tests := []struct {
		name         string
		dstContent   string
		compare      func(context.Context, string, string) (bool, error)
		compareCalls int
	}{
		{
			name:       "size mismatch bypasses comparator",
			dstContent: "old-content-is-longer",
			compare: func(context.Context, string, string) (bool, error) {
				return false, errors.New("comparator must not run")
			},
			compareCalls: 0,
		},
		{
			name:       "byte mismatch",
			dstContent: "old",
			compare: func(context.Context, string, string) (bool, error) {
				return false, nil
			},
			compareCalls: 1,
		},
		{
			name:       "comparison error fails safe",
			dstContent: "old",
			compare: func(context.Context, string, string) (bool, error) {
				return false, injectedErr
			},
			compareCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			src := filepath.Join(root, "source")
			dst := filepath.Join(root, "destination")
			require.NoError(t, os.WriteFile(src, []byte("new"), 0o644))
			require.NoError(t, os.WriteFile(dst, []byte(tt.dstContent), 0o644))

			var hash [sha256.Size]byte
			hash[0] = 1
			hashFile := func(context.Context, string) ([sha256.Size]byte, error) { return hash, nil }
			compareCalls := 0
			compare := func(ctx context.Context, src, dst string) (bool, error) {
				compareCalls++
				return tt.compare(ctx, src, dst)
			}

			changed, verify, err := copyFileIfChangedDeferredWithOps(
				context.Background(), src, dst, hashFile, compare, copyFileWithoutDirSyncContext,
			)

			require.NoError(t, err)
			assert.True(t, changed)
			require.NotNil(t, verify)
			require.NoError(t, verify())
			assert.Equal(t, tt.compareCalls, compareCalls)
			contents, readErr := os.ReadFile(dst)
			require.NoError(t, readErr)
			assert.Equal(t, "new", string(contents))
		})
	}
}

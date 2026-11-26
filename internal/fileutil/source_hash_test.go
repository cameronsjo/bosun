package fileutil

import (
	"context"
	"crypto/sha256"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeSourceFixture(t *testing.T, name, content string) (dir, path string) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	path = filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return dir, path
}

func TestSourceHash(t *testing.T) {
	t.Parallel()

	t.Run("regular file hashes its content", func(t *testing.T) {
		t.Parallel()
		_, path := writeSourceFixture(t, "source.txt", "payload")

		got, err := SourceHash(path)

		require.NoError(t, err)
		assert.Equal(t, sha256.Sum256([]byte("payload")), got)
	})

	t.Run("symlink is refused without following", func(t *testing.T) {
		t.Parallel()
		dir, target := writeSourceFixture(t, "target.txt", "payload")
		link := filepath.Join(dir, "link")
		require.NoError(t, os.Symlink(target, link))

		_, err := SourceHash(link)

		require.ErrorIs(t, err, ErrSymlinkSkipped)
	})

	t.Run("directory is an unsupported type", func(t *testing.T) {
		t.Parallel()
		dir, _ := writeSourceFixture(t, "unused", "")

		_, err := SourceHash(dir)

		require.ErrorIs(t, err, ErrUnsupportedFileType)
	})

	t.Run("missing path reports the inspection failure", func(t *testing.T) {
		t.Parallel()
		dir, _ := writeSourceFixture(t, "unused", "")

		_, err := SourceHash(filepath.Join(dir, "missing"))

		require.ErrorIs(t, err, fs.ErrNotExist)
		assert.ErrorContains(t, err, "inspect source")
	})

	t.Run("cancelled context stops before inspection", func(t *testing.T) {
		t.Parallel()
		_, path := writeSourceFixture(t, "source.txt", "payload")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := sourceHashContext(ctx, path)

		require.ErrorIs(t, err, context.Canceled)
	})
}

func TestSourceHashContextWithOps(t *testing.T) {
	t.Parallel()

	t.Run("open error is returned", func(t *testing.T) {
		t.Parallel()
		_, path := writeSourceFixture(t, "source.txt", "payload")
		openErr := errors.New("injected open failure")
		ops := defaultSourceHashOps()
		ops.open = func(string, bool) (*os.File, fs.FileInfo, error) { return nil, nil, openErr }

		_, err := sourceHashContextWithOps(context.Background(), path, ops)

		require.ErrorIs(t, err, openErr)
	})

	t.Run("descriptor for a different file is refused", func(t *testing.T) {
		t.Parallel()
		dir, path := writeSourceFixture(t, "source.txt", "payload")
		other := filepath.Join(dir, "other.txt")
		require.NoError(t, os.WriteFile(other, []byte("other"), 0o644))
		otherInfo, err := os.Lstat(other)
		require.NoError(t, err)
		ops := defaultSourceHashOps()
		// The Lstat reports another file, as if the path was replaced between
		// the Lstat and the open.
		ops.lstat = func(string) (fs.FileInfo, error) { return otherInfo, nil }

		_, err = sourceHashContextWithOps(context.Background(), path, ops)

		require.Error(t, err)
		assert.ErrorContains(t, err, "source changed while hashing")
	})

	t.Run("read error is returned", func(t *testing.T) {
		t.Parallel()
		_, path := writeSourceFixture(t, "source.txt", "payload")
		info, err := os.Lstat(path)
		require.NoError(t, err)
		ops := defaultSourceHashOps()
		ops.open = func(p string, _ bool) (*os.File, fs.FileInfo, error) {
			f, openErr := os.Open(p)
			if openErr != nil {
				return nil, nil, openErr
			}
			// A closed descriptor makes the hash read fail.
			_ = f.Close()
			return f, info, nil
		}

		_, err = sourceHashContextWithOps(context.Background(), path, ops)

		require.ErrorIs(t, err, os.ErrClosed)
		assert.ErrorContains(t, err, "hash file")
	})
}

func TestSourceFilesEqualContext(t *testing.T) {
	t.Parallel()

	t.Run("equal regular files compare equal", func(t *testing.T) {
		t.Parallel()
		dir, src := writeSourceFixture(t, "source.txt", "same")
		dst := filepath.Join(dir, "dst.txt")
		require.NoError(t, os.WriteFile(dst, []byte("same"), 0o644))

		equal, err := sourceFilesEqualContext(context.Background(), src, dst)

		require.NoError(t, err)
		assert.True(t, equal)
	})

	t.Run("source symlink is refused, destination link is followed", func(t *testing.T) {
		t.Parallel()
		dir, target := writeSourceFixture(t, "target.txt", "same")
		link := filepath.Join(dir, "link")
		require.NoError(t, os.Symlink(target, link))

		_, err := sourceFilesEqualContext(context.Background(), link, target)
		require.ErrorIs(t, err, ErrSymlinkSkipped)

		equal, err := sourceFilesEqualContext(context.Background(), target, link)
		require.NoError(t, err)
		assert.True(t, equal)
	})
}

// TestIfChangedSourceSwappedForSymlinkIsSkipped covers the branches that map a
// source replaced by a symlink after the up-front Lstat check to the benign
// skip: the injected source hash reports the skip, and the pinned comparison is
// handed a link directly.
func TestIfChangedSourceSwappedForSymlinkIsSkipped(t *testing.T) {
	t.Parallel()

	t.Run("path destination", func(t *testing.T) {
		t.Parallel()
		dir, src := writeSourceFixture(t, "source.txt", "payload")
		dst := filepath.Join(dir, "dst.txt")
		hashSource := func(context.Context, string) ([sha256.Size]byte, error) {
			return [sha256.Size]byte{}, ErrSymlinkSkipped
		}

		changed, verify, err := copyFileIfChangedDeferredWithOps(context.Background(), src, dst,
			hashSource, fileHashContext, sourceFilesEqualContext,
			func(context.Context, string, string) error {
				t.Fatal("a skipped source reached the copy")
				return nil
			})

		require.NoError(t, err)
		assert.False(t, changed)
		assert.Nil(t, verify)
		assert.NoFileExists(t, dst)
	})

	t.Run("pinned destination", func(t *testing.T) {
		t.Parallel()
		dir, target := writeSourceFixture(t, "target.txt", "payload")
		link := filepath.Join(dir, "link")
		require.NoError(t, os.Symlink(target, link))
		rootPath := filepath.Join(dir, "root")
		require.NoError(t, os.Mkdir(rootPath, 0o755))
		root, err := os.OpenRoot(rootPath)
		require.NoError(t, err)
		defer func() { _ = root.Close() }()

		changed, verify, err := copyFileIfChangedDeferredAtRoot(context.Background(), link, "dst.txt", root, "dst.txt",
			func(context.Context, string, string) error {
				t.Fatal("a skipped source reached the copy")
				return nil
			})

		require.NoError(t, err)
		assert.False(t, changed)
		assert.Nil(t, verify)
		assert.NoFileExists(t, filepath.Join(rootPath, "dst.txt"))
	})
}

// equalPinnedFixture returns a source and a pinned root whose destination holds
// the same content, so the pinned skip decision reaches the byte comparison
// that reopens the source.
func equalPinnedFixture(t *testing.T) (dir, src string, root *os.Root, rootPath string) {
	t.Helper()
	dir, src = writeSourceFixture(t, "source.txt", "same")
	rootPath = filepath.Join(dir, "root")
	require.NoError(t, os.Mkdir(rootPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(rootPath, "dst.txt"), []byte("same"), 0o600))
	root, err := os.OpenRoot(rootPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })
	return dir, src, root, rootPath
}

// secondOpen returns source ops whose first open is the production open and
// whose later opens run replace, modelling a source swapped after it was
// hashed.
func secondOpen(replace func(string) (*os.File, fs.FileInfo, error)) sourceHashOps {
	ops := defaultSourceHashOps()
	opens := 0
	ops.open = func(path string, follow bool) (*os.File, fs.FileInfo, error) {
		opens++
		if opens == 1 {
			return openRegularSource(path, follow)
		}
		return replace(path)
	}
	return ops
}

func refuseCopy(t *testing.T) func(context.Context, string, string) error {
	t.Helper()
	return func(context.Context, string, string) error {
		t.Fatal("the copy ran after the source changed")
		return nil
	}
}

// TestPinnedComparisonReopenOfSwappedSource pins the byte-comparison reopen in
// the pinned skip decision: a source that became a symlink after it was hashed
// is the benign skip, and a source that became a different file is refused.
func TestPinnedComparisonReopenOfSwappedSource(t *testing.T) {
	t.Parallel()

	t.Run("swapped for a symlink is skipped", func(t *testing.T) {
		t.Parallel()
		_, src, root, rootPath := equalPinnedFixture(t)
		ops := secondOpen(func(string) (*os.File, fs.FileInfo, error) {
			return nil, nil, ErrSymlinkSkipped
		})

		changed, verify, err := copyFileIfChangedDeferredAtRootWithSource(context.Background(), src, "dst.txt", root, "dst.txt", refuseCopy(t), ops)

		require.NoError(t, err)
		assert.False(t, changed)
		assert.Nil(t, verify)
		got, readErr := os.ReadFile(filepath.Join(rootPath, "dst.txt"))
		require.NoError(t, readErr)
		assert.Equal(t, "same", string(got))
	})

	t.Run("replaced by a different file is refused", func(t *testing.T) {
		t.Parallel()
		dir, src, root, _ := equalPinnedFixture(t)
		other := filepath.Join(dir, "other.txt")
		require.NoError(t, os.WriteFile(other, []byte("same"), 0o644))
		ops := secondOpen(func(string) (*os.File, fs.FileInfo, error) {
			return openRegularSource(other, false)
		})

		changed, verify, err := copyFileIfChangedDeferredAtRootWithSource(context.Background(), src, "dst.txt", root, "dst.txt", refuseCopy(t), ops)

		require.Error(t, err)
		assert.ErrorContains(t, err, "source changed while comparing")
		assert.False(t, changed)
		assert.Nil(t, verify)
	})
}

// TestPinnedPreRootOpenOfSwappedSourceCreatesNoRoot drives the pinned path's
// pre-root source open with a source that became a symlink after the up-front
// Lstat check: it is the benign skip and the missing root is not created.
func TestPinnedPreRootOpenOfSwappedSourceCreatesNoRoot(t *testing.T) {
	t.Parallel()

	dir, src := writeSourceFixture(t, "source.txt", "payload")
	rootPath := filepath.Join(dir, "absent-root")
	pinned := newPinnedDir(rootPath)
	defer pinned.close()
	ops := defaultSourceHashOps()
	ops.open = func(string, bool) (*os.File, fs.FileInfo, error) { return nil, nil, ErrSymlinkSkipped }

	changed, verify, err := pinned.copyFileIfChangedDeferredWithSource(context.Background(), src, filepath.Join(rootPath, "dst.txt"), refuseCopy(t), ops)

	require.NoError(t, err)
	assert.False(t, changed)
	assert.Nil(t, verify)
	_, statErr := os.Lstat(rootPath)
	assert.ErrorIs(t, statErr, fs.ErrNotExist)
}

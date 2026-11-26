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

package fileutil_test

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/cameronsjo/bosun/internal/fileutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopyFile(t *testing.T) {
	t.Parallel()

	t.Run("copies file content", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		srcPath := filepath.Join(tmpDir, "source.txt")
		dstPath := filepath.Join(tmpDir, "dest.txt")

		content := []byte("hello world")
		require.NoError(t, os.WriteFile(srcPath, content, 0644))

		err := fileutil.CopyFile(srcPath, dstPath)
		require.NoError(t, err)

		got, err := os.ReadFile(dstPath)
		require.NoError(t, err)
		assert.Equal(t, content, got)
	})

	t.Run("creates parent directories", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		srcPath := filepath.Join(tmpDir, "source.txt")
		dstPath := filepath.Join(tmpDir, "nested", "deep", "dest.txt")

		content := []byte("test content")
		require.NoError(t, os.WriteFile(srcPath, content, 0644))

		err := fileutil.CopyFile(srcPath, dstPath)
		require.NoError(t, err)

		got, err := os.ReadFile(dstPath)
		require.NoError(t, err)
		assert.Equal(t, content, got)
	})

	t.Run("preserves file permissions", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		srcPath := filepath.Join(tmpDir, "source.txt")
		dstPath := filepath.Join(tmpDir, "dest.txt")

		require.NoError(t, os.WriteFile(srcPath, []byte("test"), 0755))

		err := fileutil.CopyFile(srcPath, dstPath)
		require.NoError(t, err)

		srcInfo, err := os.Stat(srcPath)
		require.NoError(t, err)
		dstInfo, err := os.Stat(dstPath)
		require.NoError(t, err)

		assert.Equal(t, srcInfo.Mode(), dstInfo.Mode())
	})

	t.Run("returns error for non-existent source", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		srcPath := filepath.Join(tmpDir, "nonexistent.txt")
		dstPath := filepath.Join(tmpDir, "dest.txt")

		err := fileutil.CopyFile(srcPath, dstPath)
		assert.Error(t, err)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("returns ErrSymlinkSkipped for symlink source", func(t *testing.T) {
		t.Parallel()

		tmpDir, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)

		// Create a real file and a symlink pointing to it.
		realFile := filepath.Join(tmpDir, "real.txt")
		require.NoError(t, os.WriteFile(realFile, []byte("real content"), 0644))
		symlinkPath := filepath.Join(tmpDir, "link.txt")
		require.NoError(t, os.Symlink(realFile, symlinkPath))

		dstPath := filepath.Join(tmpDir, "dest.txt")
		err = fileutil.CopyFile(symlinkPath, dstPath)
		assert.ErrorIs(t, err, fileutil.ErrSymlinkSkipped)

		// Destination must not have been created.
		_, statErr := os.Lstat(dstPath)
		assert.True(t, os.IsNotExist(statErr), "symlink target should not be copied")
	})
}

func TestCopyDir(t *testing.T) {
	t.Parallel()

	t.Run("copies directory structure", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		srcDir := filepath.Join(tmpDir, "source")
		dstDir := filepath.Join(tmpDir, "dest")

		// Create source structure
		require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "subdir"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "file1.txt"), []byte("content1"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "subdir", "file2.txt"), []byte("content2"), 0644))

		err := fileutil.CopyDir(srcDir, dstDir)
		require.NoError(t, err)

		// Verify files were copied
		content1, err := os.ReadFile(filepath.Join(dstDir, "file1.txt"))
		require.NoError(t, err)
		assert.Equal(t, "content1", string(content1))

		content2, err := os.ReadFile(filepath.Join(dstDir, "subdir", "file2.txt"))
		require.NoError(t, err)
		assert.Equal(t, "content2", string(content2))
	})

	t.Run("copies empty directories", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		srcDir := filepath.Join(tmpDir, "source")
		dstDir := filepath.Join(tmpDir, "dest")

		require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "emptydir"), 0755))

		err := fileutil.CopyDir(srcDir, dstDir)
		require.NoError(t, err)

		info, err := os.Stat(filepath.Join(dstDir, "emptydir"))
		require.NoError(t, err)
		assert.True(t, info.IsDir())
	})

	t.Run("handles deep nesting", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		srcDir := filepath.Join(tmpDir, "source")
		dstDir := filepath.Join(tmpDir, "dest")

		deepPath := filepath.Join(srcDir, "a", "b", "c", "d", "e")
		require.NoError(t, os.MkdirAll(deepPath, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(deepPath, "deep.txt"), []byte("deep content"), 0644))

		err := fileutil.CopyDir(srcDir, dstDir)
		require.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(dstDir, "a", "b", "c", "d", "e", "deep.txt"))
		require.NoError(t, err)
		assert.Equal(t, "deep content", string(content))
	})

	t.Run("returns error for non-existent source", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		srcDir := filepath.Join(tmpDir, "nonexistent")
		dstDir := filepath.Join(tmpDir, "dest")

		err := fileutil.CopyDir(srcDir, dstDir)
		assert.Error(t, err)
	})

	t.Run("skips symlinks and copies regular files", func(t *testing.T) {
		t.Parallel()

		tmpDir, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)
		srcDir := filepath.Join(tmpDir, "source")
		dstDir := filepath.Join(tmpDir, "dest")

		require.NoError(t, os.MkdirAll(srcDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "regular.txt"), []byte("regular"), 0644))

		// Create a symlink inside the source directory.
		outsideFile := filepath.Join(tmpDir, "outside.txt")
		require.NoError(t, os.WriteFile(outsideFile, []byte("outside"), 0644))
		require.NoError(t, os.Symlink(outsideFile, filepath.Join(srcDir, "link.txt")))

		err = fileutil.CopyDir(srcDir, dstDir)
		require.NoError(t, err)

		// Regular file was copied.
		content, readErr := os.ReadFile(filepath.Join(dstDir, "regular.txt"))
		require.NoError(t, readErr)
		assert.Equal(t, "regular", string(content))

		// Symlink was not copied to destination.
		_, statErr := os.Lstat(filepath.Join(dstDir, "link.txt"))
		assert.True(t, os.IsNotExist(statErr), "symlink should not appear in destination")
	})
}

func TestFileHash(t *testing.T) {
	t.Parallel()

	t.Run("returns consistent hash for same content", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "file.txt")
		require.NoError(t, os.WriteFile(path, []byte("hello"), 0644))

		hash1, err := fileutil.FileHash(path)
		require.NoError(t, err)

		hash2, err := fileutil.FileHash(path)
		require.NoError(t, err)

		assert.Equal(t, hash1, hash2)
	})

	t.Run("returns different hash for different content", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		path1 := filepath.Join(tmpDir, "a.txt")
		path2 := filepath.Join(tmpDir, "b.txt")
		require.NoError(t, os.WriteFile(path1, []byte("hello"), 0644))
		require.NoError(t, os.WriteFile(path2, []byte("world"), 0644))

		hash1, err := fileutil.FileHash(path1)
		require.NoError(t, err)
		hash2, err := fileutil.FileHash(path2)
		require.NoError(t, err)

		assert.NotEqual(t, hash1, hash2)
	})

	t.Run("returns error for non-existent file", func(t *testing.T) {
		t.Parallel()

		_, err := fileutil.FileHash("/nonexistent/file.txt")
		assert.Error(t, err)
	})
}

func TestContentEqual(t *testing.T) {
	t.Parallel()

	t.Run("returns true for identical content", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		content := []byte("same content")
		path := filepath.Join(tmpDir, "file.txt")
		require.NoError(t, os.WriteFile(path, content, 0644))

		srcHash := sha256.Sum256(content)
		equal, err := fileutil.ContentEqual(path, srcHash)
		require.NoError(t, err)
		assert.True(t, equal)
	})

	t.Run("returns false for different content", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("original"), 0644))

		srcHash := sha256.Sum256([]byte("different"))
		equal, err := fileutil.ContentEqual(filepath.Join(tmpDir, "file.txt"), srcHash)
		require.NoError(t, err)
		assert.False(t, equal)
	})

	t.Run("returns false for non-existent file", func(t *testing.T) {
		t.Parallel()

		srcHash := sha256.Sum256([]byte("content"))
		equal, err := fileutil.ContentEqual("/nonexistent/file.txt", srcHash)
		require.NoError(t, err)
		assert.False(t, equal)
	})

	t.Run("returns error when path is unreadable", func(t *testing.T) {
		t.Parallel()

		if os.Getuid() == 0 {
			t.Skip("root bypasses file permission checks")
		}

		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "locked.txt")
		require.NoError(t, os.WriteFile(path, []byte("locked"), 0000))

		srcHash := sha256.Sum256([]byte("locked"))
		equal, err := fileutil.ContentEqual(path, srcHash)
		assert.Error(t, err)
		assert.False(t, equal)

		// Restore permissions so t.TempDir() cleanup succeeds.
		_ = os.Chmod(path, 0644)
	})
}

func TestCopyFileIfChanged(t *testing.T) {
	t.Parallel()

	t.Run("writes new file and reports changed", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		src := filepath.Join(tmpDir, "src.txt")
		dst := filepath.Join(tmpDir, "dst.txt")
		require.NoError(t, os.WriteFile(src, []byte("new content"), 0644))

		changed, err := fileutil.CopyFileIfChanged(src, dst)
		require.NoError(t, err)
		assert.True(t, changed)

		got, err := os.ReadFile(dst)
		require.NoError(t, err)
		assert.Equal(t, "new content", string(got))
	})

	t.Run("skips write when content identical", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		src := filepath.Join(tmpDir, "src.txt")
		dst := filepath.Join(tmpDir, "dst.txt")
		content := []byte("identical content")
		require.NoError(t, os.WriteFile(src, content, 0644))
		require.NoError(t, os.WriteFile(dst, content, 0644))

		// Record dst mod time before call
		infoBefore, err := os.Stat(dst)
		require.NoError(t, err)

		changed, err := fileutil.CopyFileIfChanged(src, dst)
		require.NoError(t, err)
		assert.False(t, changed)

		// File should not have been rewritten
		infoAfter, err := os.Stat(dst)
		require.NoError(t, err)
		assert.Equal(t, infoBefore.ModTime(), infoAfter.ModTime())
	})

	t.Run("writes when content differs", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		src := filepath.Join(tmpDir, "src.txt")
		dst := filepath.Join(tmpDir, "dst.txt")
		require.NoError(t, os.WriteFile(src, []byte("new version"), 0644))
		require.NoError(t, os.WriteFile(dst, []byte("old version"), 0644))

		changed, err := fileutil.CopyFileIfChanged(src, dst)
		require.NoError(t, err)
		assert.True(t, changed)

		got, err := os.ReadFile(dst)
		require.NoError(t, err)
		assert.Equal(t, "new version", string(got))
	})

	t.Run("symlink source skips gracefully", func(t *testing.T) {
		t.Parallel()

		// FileHash follows symlinks (via os.Open), so the src hash is computed
		// from the target file's content. CopyFile then detects the symlink via
		// Lstat and returns ErrSymlinkSkipped. CopyFileIfChanged must surface
		// this as (false, nil) — not written, not an error.
		tmpDir, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)

		realFile := filepath.Join(tmpDir, "real.txt")
		require.NoError(t, os.WriteFile(realFile, []byte("real content"), 0644))
		symlinkPath := filepath.Join(tmpDir, "link.txt")
		require.NoError(t, os.Symlink(realFile, symlinkPath))

		dst := filepath.Join(tmpDir, "dst.txt")
		changed, err := fileutil.CopyFileIfChanged(symlinkPath, dst)
		require.NoError(t, err)
		assert.False(t, changed)

		// Destination must not exist — symlink content was never written.
		_, statErr := os.Lstat(dst)
		assert.True(t, os.IsNotExist(statErr), "symlink content should not be written to destination")
	})
}

func TestCopyDirIfChanged(t *testing.T) {
	t.Parallel()

	t.Run("returns only changed files", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		srcDir := filepath.Join(tmpDir, "src")
		dstDir := filepath.Join(tmpDir, "dst")

		// Create source
		require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "sub"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "unchanged.txt"), []byte("same"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "changed.txt"), []byte("new"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "sub", "new.txt"), []byte("brand new"), 0644))

		// Create pre-existing destination with one matching file
		require.NoError(t, os.MkdirAll(dstDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(dstDir, "unchanged.txt"), []byte("same"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(dstDir, "changed.txt"), []byte("old"), 0644))

		written, err := fileutil.CopyDirIfChanged(srcDir, dstDir)
		require.NoError(t, err)

		sort.Strings(written)
		assert.Equal(t, []string{"changed.txt", filepath.Join("sub", "new.txt")}, written)
	})

	t.Run("returns empty when all files identical", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		srcDir := filepath.Join(tmpDir, "src")
		dstDir := filepath.Join(tmpDir, "dst")

		require.NoError(t, os.MkdirAll(srcDir, 0755))
		require.NoError(t, os.MkdirAll(dstDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("content"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(dstDir, "a.txt"), []byte("content"), 0644))

		written, err := fileutil.CopyDirIfChanged(srcDir, dstDir)
		require.NoError(t, err)
		assert.Empty(t, written)
	})

	t.Run("all files new returns all", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		srcDir := filepath.Join(tmpDir, "src")
		dstDir := filepath.Join(tmpDir, "dst")

		require.NoError(t, os.MkdirAll(srcDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("aaa"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "b.txt"), []byte("bbb"), 0644))

		written, err := fileutil.CopyDirIfChanged(srcDir, dstDir)
		require.NoError(t, err)

		sort.Strings(written)
		assert.Equal(t, []string{"a.txt", "b.txt"}, written)
	})

	t.Run("skips symlinks and reports only regular changed files", func(t *testing.T) {
		t.Parallel()

		tmpDir, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)
		srcDir := filepath.Join(tmpDir, "src")
		dstDir := filepath.Join(tmpDir, "dst")

		require.NoError(t, os.MkdirAll(srcDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "real.txt"), []byte("content"), 0644))

		// Add a symlink alongside the regular file.
		outsideFile := filepath.Join(tmpDir, "outside.txt")
		require.NoError(t, os.WriteFile(outsideFile, []byte("outside"), 0644))
		require.NoError(t, os.Symlink(outsideFile, filepath.Join(srcDir, "link.txt")))

		written, err := fileutil.CopyDirIfChanged(srcDir, dstDir)
		require.NoError(t, err)

		// Only the regular file appears in the written list.
		assert.Equal(t, []string{"real.txt"}, written)

		// Symlink was not copied to destination.
		_, statErr := os.Lstat(filepath.Join(dstDir, "link.txt"))
		assert.True(t, os.IsNotExist(statErr), "symlink should not appear in destination")
	})
}

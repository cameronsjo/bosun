package fileutil_test

import (
	"os"
	"path/filepath"
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
}

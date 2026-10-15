//go:build darwin || linux

package reconcile

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/cameronsjo/bosun/internal/fileutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecuteTemplate_RefusesFIFOWithoutBlocking(t *testing.T) {
	tmpDir := evalSymlinks(t, t.TempDir())
	templateFile := filepath.Join(tmpDir, "pipe.tmpl")
	require.NoError(t, syscall.Mkfifo(templateFile, 0o600))
	outputFile := filepath.Join(tmpDir, "output", "pipe")

	done := make(chan error, 1)
	go func() {
		done <- NewTemplateOps(map[string]any{}).ExecuteTemplate(context.Background(), templateFile, outputFile)
	}()

	select {
	case err := <-done:
		require.ErrorIs(t, err, fileutil.ErrUnsupportedFileType)
		assert.ErrorContains(t, err, templateFile)
		assert.NoFileExists(t, outputFile)
	case <-time.After(2 * time.Second):
		if writer, err := os.OpenFile(templateFile, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			_ = writer.Close()
		}
		t.Fatal("ExecuteTemplate blocked opening a FIFO template")
	}
}

func TestExecuteTemplate_RefusesSwappedSourceAncestor(t *testing.T) {
	tmpDir := evalSymlinks(t, t.TempDir())
	sourceDir := filepath.Join(tmpDir, "source")
	nestedDir := filepath.Join(sourceDir, "nested")
	outsideDir := filepath.Join(tmpDir, "outside")
	require.NoError(t, os.MkdirAll(nestedDir, 0755))
	require.NoError(t, os.MkdirAll(outsideDir, 0755))
	templateName := "template.tmpl"
	require.NoError(t, os.WriteFile(filepath.Join(nestedDir, templateName), []byte("safe"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(outsideDir, templateName), []byte("outside"), 0644))

	root, err := fileutil.OpenPinnedRoot(sourceDir, false)
	require.NoError(t, err)
	defer func() { _ = root.Close() }()
	tmpl := &TemplateOps{
		Data:           map[string]any{},
		sourceRoot:     root,
		sourceRootPath: sourceDir,
	}

	require.NoError(t, os.RemoveAll(nestedDir))
	require.NoError(t, os.Symlink(outsideDir, nestedDir))

	err = tmpl.ExecuteTemplate(context.Background(), filepath.Join(nestedDir, templateName), filepath.Join(tmpDir, "output"))
	require.Error(t, err)
	assert.NoFileExists(t, filepath.Join(tmpDir, "output"))
}

func TestCopyNonTemplateFilesFromRoot_UsesPinnedSourceAfterAncestorSwap(t *testing.T) {
	tmpDir := evalSymlinks(t, t.TempDir())
	sourceDir := filepath.Join(tmpDir, "source")
	outsideDir := filepath.Join(tmpDir, "outside")
	dstDir := filepath.Join(tmpDir, "staging")
	require.NoError(t, os.MkdirAll(sourceDir, 0o755))
	require.NoError(t, os.MkdirAll(outsideDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "config.yml"), []byte("safe"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(outsideDir, "config.yml"), []byte("outside"), 0o644))

	root, err := fileutil.OpenPinnedRoot(sourceDir, false)
	require.NoError(t, err)
	defer func() { _ = root.Close() }()
	require.NoError(t, os.RemoveAll(sourceDir))
	require.NoError(t, os.Symlink(outsideDir, sourceDir))

	require.NoError(t, copyNonTemplateFilesFromRoot(context.Background(), sourceDir, dstDir, root))
	content, err := os.ReadFile(filepath.Join(dstDir, "config.yml"))
	require.NoError(t, err)
	assert.Equal(t, "safe", string(content))
}

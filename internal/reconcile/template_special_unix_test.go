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

// A FIFO in the checkout would block a plain read until a writer appears. The
// template open is nonblocking and checks the opened descriptor, so it refuses
// the entry instead.
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
		// Unblock a regressed blocking open so the goroutine does not leak.
		if writer, err := os.OpenFile(templateFile, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			_ = writer.Close()
		}
		t.Fatal("ExecuteTemplate blocked opening a FIFO template")
	}
}

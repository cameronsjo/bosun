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

// Regression guard: a FIFO in the checkout must be refused without blocking.
// The earlier Lstat check also refused it, so this does not pin the switch to
// reading from the checked descriptor; it keeps the nonblocking open honest.
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

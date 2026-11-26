//go:build !windows

package reconcile

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestWalkStaging_FileSwappedForFIFOFailsWithoutBlocking pins that a staging
// file replaced by a FIFO between its Lstat and its open fails the walk
// instead of blocking in open(2) (cameronsjo/bosun#802). The injected opener
// performs the swap and then delegates to the production default, so the test
// exercises the default opener's flags.
func TestWalkStaging_FileSwappedForFIFOFailsWithoutBlocking(t *testing.T) {
	root := filepath.Join(evalSymlinks(t, t.TempDir()), "staging")
	require.NoError(t, os.MkdirAll(root, 0o700))
	payload := filepath.Join(root, "payload.yml")
	require.NoError(t, os.WriteFile(payload, []byte("rendered"), 0o600))

	defaultOpen := defaultStagingEvidenceOps().openFile
	ops := stagingEvidenceOps{
		openFile: func(namespace *os.Root, name string) (*os.File, error) {
			if name == filepath.Base(payload) {
				if err := os.Remove(payload); err != nil {
					return nil, err
				}
				if err := syscall.Mkfifo(payload, 0o600); err != nil {
					return nil, err
				}
			}
			return defaultOpen(namespace, name)
		},
	}

	done := make(chan error, 1)
	go func() { done <- verifyActiveStaging(root, ops) }()

	select {
	case err := <-done:
		require.Error(t, err)
		require.ErrorContains(t, err, "pin staging file")
	case <-time.After(3 * time.Second):
		// Unblock a regressed blocking open before failing so the goroutine and
		// its descriptor do not leak into later tests.
		if writer, err := os.OpenFile(payload, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			_ = writer.Close()
		}
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
		t.Fatal("staging walk blocked opening a file swapped for a FIFO")
	}
}

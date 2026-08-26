package fileutil

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopyFileIfChanged_PostWriteVerificationFailureReportsWrite(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		postHash func(context.Context, string) ([sha256.Size]byte, error)
	}{
		{
			name: "readback error",
			postHash: func(context.Context, string) ([sha256.Size]byte, error) {
				return [sha256.Size]byte{}, errors.New("injected readback failure")
			},
		},
		{
			name: "hash mismatch",
			postHash: func(context.Context, string) ([sha256.Size]byte, error) {
				return sha256.Sum256([]byte("stale destination")), nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmpDir := t.TempDir()
			src := filepath.Join(tmpDir, "source.yml")
			dst := filepath.Join(tmpDir, "destination.yml")
			require.NoError(t, os.WriteFile(src, []byte("new content"), 0o644))
			hashCalls := 0
			hashFile := func(hashCtx context.Context, path string) ([sha256.Size]byte, error) {
				hashCalls++
				if hashCalls == 3 {
					return tt.postHash(hashCtx, path)
				}
				return fileHashContext(hashCtx, path)
			}

			changed, err := copyFileIfChanged(context.Background(), src, dst, hashFile)

			require.Error(t, err)
			assert.ErrorIs(t, err, ErrPostWriteVerification)
			assert.Equal(t, 3, hashCalls)
			assert.True(t, changed, "the successful rename must remain visible to change tracking")
			content, readErr := os.ReadFile(dst)
			require.NoError(t, readErr)
			assert.Equal(t, "new content", string(content))
		})
	}
}

func TestCopyDirIfChanged_PreservesWrittenPathOnVerificationFailure(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "source")
	dstDir := filepath.Join(tmpDir, "destination")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "config.yml"), []byte("new content"), 0o644))

	verificationErr := fmt.Errorf("%w: injected", ErrPostWriteVerification)
	written, err := copyDirIfChangedWithOps(context.Background(),
		srcDir,
		dstDir,
		func(_ context.Context, src, dst string) (bool, postWriteVerification, error) {
			require.NoError(t, copyFileWithoutDirSyncContext(context.Background(), src, dst))
			return true, func() error { return verificationErr }, nil
		},
		syncDestinationDir,
	)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPostWriteVerification)
	assert.Equal(t, verificationErr, err, "a lone deferred verification error must remain unchanged")
	assert.Equal(t, []string{"config.yml"}, written)
}

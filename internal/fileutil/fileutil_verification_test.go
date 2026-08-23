package fileutil

import (
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
		verifier func(string) ([sha256.Size]byte, error)
	}{
		{
			name: "readback error",
			verifier: func(string) ([sha256.Size]byte, error) {
				return [sha256.Size]byte{}, errors.New("injected readback failure")
			},
		},
		{
			name: "hash mismatch",
			verifier: func(string) ([sha256.Size]byte, error) {
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

			changed, err := copyFileIfChanged(src, dst, tt.verifier)

			require.Error(t, err)
			assert.ErrorIs(t, err, ErrPostWriteVerification)
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

	written, err := copyDirIfChanged(srcDir, dstDir, func(src, dst string) (bool, error) {
		require.NoError(t, CopyFile(src, dst))
		return true, fmt.Errorf("%w: injected", ErrPostWriteVerification)
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPostWriteVerification)
	assert.Equal(t, []string{"config.yml"}, written)
}

package reconcile

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readArchiveMember(t *testing.T, archivePath, sourcePath string) []byte {
	t.Helper()

	f, err := os.Open(archivePath)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()

	gz, err := gzip.NewReader(f)
	require.NoError(t, err)
	defer func() { require.NoError(t, gz.Close()) }()

	wantName := strings.TrimPrefix(filepath.ToSlash(sourcePath), "/")
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		require.NoError(t, err)
		if hdr.Name != wantName {
			continue
		}
		body, err := io.ReadAll(tr)
		require.NoError(t, err)
		require.Len(t, body, int(hdr.Size))
		return body
	}
}

func TestWriteBackupArchive_ChurnTolerance(t *testing.T) {
	original := []byte("abcdefgh")
	tests := []struct {
		name      string
		open      func(t *testing.T, sourcePath, tmpDir string) func(string) (*os.File, error)
		wantBytes []byte
	}{
		{
			name: "replacement after walk is zero-padded",
			open: func(t *testing.T, sourcePath, tmpDir string) func(string) (*os.File, error) {
				replacement := filepath.Join(tmpDir, "replacement")
				require.NoError(t, os.WriteFile(replacement, []byte("intruder"), 0600))
				return func(string) (*os.File, error) {
					if err := os.Remove(sourcePath); err != nil {
						return nil, err
					}
					if err := os.Rename(replacement, sourcePath); err != nil {
						return nil, err
					}
					return os.Open(sourcePath)
				}
			},
			wantBytes: make([]byte, len(original)),
		},
		{
			name: "shrink after walk preserves prefix and zero-pads remainder",
			open: func(t *testing.T, sourcePath, _ string) func(string) (*os.File, error) {
				return func(string) (*os.File, error) {
					f, err := os.Open(sourcePath)
					if err != nil {
						return nil, err
					}
					if err := os.Truncate(sourcePath, 3); err != nil {
						_ = f.Close()
						return nil, err
					}
					return f, nil
				}
			},
			wantBytes: []byte{'a', 'b', 'c', 0, 0, 0, 0, 0},
		},
		{
			name: "vanish after walk is zero-padded",
			open: func(t *testing.T, sourcePath, _ string) func(string) (*os.File, error) {
				return func(string) (*os.File, error) {
					if err := os.Remove(sourcePath); err != nil {
						return nil, err
					}
					return os.Open(sourcePath)
				}
			},
			wantBytes: make([]byte, len(original)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := evalSymlinks(t, t.TempDir())
			sourcePath := filepath.Join(tmpDir, "source")
			require.NoError(t, os.WriteFile(sourcePath, original, 0600))
			archivePath := filepath.Join(tmpDir, "archive.tar.gz")

			d := &DeployOps{backupFS: &backupArchiveFS{open: tt.open(t, sourcePath, tmpDir)}}
			err := d.writeBackupArchive(context.Background(), archivePath, []string{sourcePath}, "")
			require.NoError(t, err)

			assert.Equal(t, tt.wantBytes, readArchiveMember(t, archivePath, sourcePath))
		})
	}
}

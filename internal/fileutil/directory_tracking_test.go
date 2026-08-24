package fileutil

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopyDirIfChanged_DirectoryTrackingCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(t *testing.T, src, dst string)
		repeat  bool
		want    []string
		wantErr bool
	}{
		{
			name: "nested empty directories",
			setup: func(t *testing.T, src, dst string) {
				require.NoError(t, os.MkdirAll(filepath.Join(src, "empty", "nested"), 0o755))
			},
			want: []string{"empty", filepath.Join("empty", "nested")},
		},
		{
			name: "pre-existing directories",
			setup: func(t *testing.T, src, dst string) {
				require.NoError(t, os.MkdirAll(filepath.Join(src, "empty", "nested"), 0o755))
				require.NoError(t, os.MkdirAll(filepath.Join(dst, "empty", "nested"), 0o755))
			},
		},
		{
			name: "repeat is a no-op",
			setup: func(t *testing.T, src, dst string) {
				require.NoError(t, os.MkdirAll(filepath.Join(src, "empty", "nested"), 0o755))
			},
			repeat: true,
		},
		{
			name: "non-directory collision",
			setup: func(t *testing.T, src, dst string) {
				require.NoError(t, os.MkdirAll(filepath.Join(src, "blocked", "nested"), 0o755))
				require.NoError(t, os.MkdirAll(dst, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dst, "blocked"), []byte("keep"), 0o644))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmpDir := t.TempDir()
			src := filepath.Join(tmpDir, "src")
			dst := filepath.Join(tmpDir, "dst")
			tt.setup(t, src, dst)
			if tt.repeat {
				_, err := CopyDirIfChanged(src, dst)
				require.NoError(t, err)
			}

			got, err := CopyDirIfChanged(src, dst)

			if tt.wantErr {
				require.Error(t, err)
				assert.Empty(t, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMkdirIfMissingWithOps_PropagatesErrors(t *testing.T) {
	t.Parallel()

	t.Run("mkdir failure", func(t *testing.T) {
		t.Parallel()

		inspectCalled := false
		created, err := mkdirIfMissingWithOps(
			"destination",
			0755,
			func(string, fs.FileMode) error { return fs.ErrPermission },
			func(string) (fs.FileInfo, error) {
				inspectCalled = true
				return nil, nil
			},
		)

		require.ErrorIs(t, err, fs.ErrPermission)
		assert.False(t, created)
		assert.False(t, inspectCalled, "a non-collision mkdir error must return without inspecting the path")
	})

	t.Run("inspection failure after collision", func(t *testing.T) {
		t.Parallel()

		created, err := mkdirIfMissingWithOps(
			"destination",
			0755,
			func(string, fs.FileMode) error { return fs.ErrExist },
			func(string) (fs.FileInfo, error) { return nil, fs.ErrPermission },
		)

		require.ErrorIs(t, err, fs.ErrPermission)
		assert.False(t, created)
	})

	t.Run("concurrent directory creator is an existing no-op", func(t *testing.T) {
		t.Parallel()

		created, err := mkdirIfMissingWithOps(
			"destination",
			0o755,
			func(string, fs.FileMode) error { return fs.ErrExist },
			func(string) (fs.FileInfo, error) { return fakeFileInfo{mode: fs.ModeDir}, nil },
		)

		require.NoError(t, err)
		assert.False(t, created)
	})
}

type fakeFileInfo struct {
	mode fs.FileMode
}

func (f fakeFileInfo) Name() string       { return "destination" }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() fs.FileMode  { return f.mode }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return f.mode.IsDir() }
func (f fakeFileInfo) Sys() any           { return nil }

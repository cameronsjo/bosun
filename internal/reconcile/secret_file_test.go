package reconcile

import (
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadRegularNonEmptyFile(t *testing.T) {
	info, err := os.Stat(t.TempDir())
	require.NoError(t, err)

	tests := []struct {
		name       string
		stat       func(string) (os.FileInfo, error)
		readFile   func(string) ([]byte, error)
		want       string
		wantReason string
	}{
		{
			name:     "valid",
			stat:     func(string) (os.FileInfo, error) { return fakeRegularFileInfo{FileInfo: info, size: 3}, nil },
			readFile: func(string) ([]byte, error) { return []byte("key"), nil },
			want:     "key",
		},
		{
			name:       "missing",
			stat:       func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
			wantReason: "does not exist",
		},
		{
			name:       "inspect failure",
			stat:       func(string) (os.FileInfo, error) { return nil, errors.New("inspect denied") },
			wantReason: "cannot be inspected",
		},
		{
			name:       "not regular",
			stat:       func(string) (os.FileInfo, error) { return info, nil },
			wantReason: "not a regular file",
		},
		{
			name:       "empty metadata",
			stat:       func(string) (os.FileInfo, error) { return fakeRegularFileInfo{FileInfo: info}, nil },
			wantReason: "is empty",
		},
		{
			name: "read failure",
			stat: func(string) (os.FileInfo, error) {
				return fakeRegularFileInfo{FileInfo: info, size: 3}, nil
			},
			readFile:   func(string) ([]byte, error) { return nil, errors.New("read denied") },
			wantReason: "cannot be read",
		},
		{
			name: "empty read",
			stat: func(string) (os.FileInfo, error) {
				return fakeRegularFileInfo{FileInfo: info, size: 3}, nil
			},
			readFile:   func(string) ([]byte, error) { return nil, nil },
			wantReason: "is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contents, err := readRegularNonEmptyFileWith("/secret", tt.stat, tt.readFile)
			if tt.wantReason == "" {
				require.NoError(t, err)
				assert.Equal(t, tt.want, string(contents))
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantReason)
		})
	}
}

type fakeRegularFileInfo struct {
	os.FileInfo
	size int64
}

func (f fakeRegularFileInfo) Mode() os.FileMode { return 0o600 }
func (f fakeRegularFileInfo) Size() int64       { return f.size }

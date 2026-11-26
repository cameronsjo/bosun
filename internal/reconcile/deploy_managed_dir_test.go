package reconcile

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateManagedDirectory_DescendantCheck pins which directories the
// managed-descendant check accepts and which it names in the error. The check
// only passes when every directory has a managed regular file somewhere beneath
// it, however deep.
func TestValidateManagedDirectory_DescendantCheck(t *testing.T) {
	tests := []struct {
		name      string
		files     []string // regular files, all in the managed manifest
		dirs      []string // extra directories holding no file
		wantFiles []string
		wantErr   string // empty means success
	}{
		{
			name:      "file two levels below intermediate directories",
			files:     []string{"a/b/f"},
			wantFiles: []string{"config/a/b/f"},
		},
		{
			name:      "every directory on a deep chain counts as holding the file",
			files:     []string{"a/b/c/f", "top"},
			wantFiles: []string{"config/a/b/c/f", "config/top"},
		},
		{
			name:    "empty sibling directory is named",
			files:   []string{"a/f"},
			dirs:    []string{"empty"},
			wantErr: "destination directory empty has no managed descendants",
		},
		{
			name:    "empty directory nested inside a populated one is named",
			files:   []string{"a/f"},
			dirs:    []string{"a/sub"},
			wantErr: "destination directory " + filepath.Join("a", "sub") + " has no managed descendants",
		},
		{
			name:    "a directory holding only an empty directory is named",
			files:   []string{"keep"},
			dirs:    []string{"x/y"},
			wantErr: "destination directory x has no managed descendants",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := evalSymlinks(t, t.TempDir())
			managed := make(map[string]bool)
			for _, rel := range tt.files {
				path := filepath.Join(dir, filepath.FromSlash(rel))
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
				require.NoError(t, os.WriteFile(path, []byte("old"), 0o600))
				managed["config/"+rel] = true
			}
			for _, rel := range tt.dirs {
				require.NoError(t, os.MkdirAll(filepath.Join(dir, filepath.FromSlash(rel)), 0o700))
			}

			got, err := validateManagedDirectory(context.Background(), dir, "config", managed)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.ElementsMatch(t, tt.wantFiles, got)
		})
	}
}

package reconcile

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenPinnedPath_AllowsNoReplaceRename(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("renameNoReplace is fail-closed on this platform")
	}
	for _, isDir := range []bool{false, true} {
		name := "file"
		if isDir {
			name = "directory"
		}
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			source := filepath.Join(parent, "source")
			destination := filepath.Join(parent, "destination")
			if isDir {
				require.NoError(t, os.Mkdir(source, 0700))
			} else {
				require.NoError(t, os.WriteFile(source, []byte("payload"), 0600))
			}
			parentHandle, err := openPinnedPath(parent)
			require.NoError(t, err)
			defer func() { _ = parentHandle.Close() }()
			targetHandle, err := openPinnedPath(source)
			require.NoError(t, err)
			defer func() { _ = targetHandle.Close() }()

			require.NoError(t, renameNoReplace(source, destination))
			assert.NoFileExists(t, source)
			if isDir {
				assert.DirExists(t, destination)
			} else {
				assert.FileExists(t, destination)
			}
		})
	}
}

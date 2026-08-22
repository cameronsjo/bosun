package reconcile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenameNoReplace(t *testing.T) {
	tmpDir := t.TempDir()
	t.Run("moves when destination is absent", func(t *testing.T) {
		source, target := filepath.Join(tmpDir, "source"), filepath.Join(tmpDir, "target")
		require.NoError(t, os.WriteFile(source, []byte("source"), 0600))
		require.NoError(t, renameNoReplace(source, target))
		assert.NoFileExists(t, source)
		assert.FileExists(t, target)
	})

	t.Run("preserves both paths when destination exists", func(t *testing.T) {
		source, target := filepath.Join(tmpDir, "source-occupied"), filepath.Join(tmpDir, "target-occupied")
		require.NoError(t, os.WriteFile(source, []byte("source"), 0600))
		require.NoError(t, os.WriteFile(target, []byte("target"), 0600))
		require.Error(t, renameNoReplace(source, target))
		assert.FileExists(t, source)
		content, err := os.ReadFile(target)
		require.NoError(t, err)
		assert.Equal(t, "target", string(content))
	})
}

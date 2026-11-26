package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCountProvisions(t *testing.T) {
	t.Run("counts yml and yaml, a pair once", func(t *testing.T) {
		dir := t.TempDir()
		for _, name := range []string{"a.yml", "b.yaml", "c.yml", "c.yaml", "notes.txt"} {
			require.NoError(t, os.WriteFile(filepath.Join(dir, name), nil, 0o644))
		}
		require.NoError(t, os.Mkdir(filepath.Join(dir, "subdir"), 0o755))

		count, err := countProvisions(dir)

		require.NoError(t, err)
		assert.Equal(t, 3, count)
	})

	t.Run("missing directory is an error", func(t *testing.T) {
		count, err := countProvisions(filepath.Join(t.TempDir(), "missing"))

		require.Error(t, err)
		assert.Zero(t, count)
	})
}

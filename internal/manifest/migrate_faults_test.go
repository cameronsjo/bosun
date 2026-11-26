package manifest

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMigrateFile_FileSystemFailures injects the two failures MigrateFile can
// hit around the migration itself: the file cannot be read, and the migrated
// content cannot be written. Either way the file is reported as an error and its
// bytes on disk are unchanged. Both rely on file modes, which root ignores.
func TestMigrateFile_FileSystemFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes do not restrict access on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores file modes")
	}

	const original = "name: x\nprovisions: [a]\n"

	write := func(t *testing.T, mode os.FileMode) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "service.yml")
		require.NoError(t, os.WriteFile(path, []byte(original), 0o644))
		require.NoError(t, os.Chmod(path, mode))
		// Let the temporary directory be removed.
		t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
		return path
	}

	unchanged := func(t *testing.T, path string) {
		t.Helper()
		require.NoError(t, os.Chmod(path, 0o644))
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, original, string(data))
	}

	t.Run("an unreadable file is reported and left unchanged", func(t *testing.T) {
		path := write(t, 0)

		result, err := MigrateFile(path, MigrateOptions{DryRun: false})

		require.Error(t, err)
		assert.Error(t, result.Error)
		assert.Contains(t, err.Error(), "read file")
		assert.False(t, result.Migrated)
		unchanged(t, path)
	})

	t.Run("a file that cannot be written is reported and left unchanged", func(t *testing.T) {
		path := write(t, 0o444)

		result, err := MigrateFile(path, MigrateOptions{DryRun: false})

		require.Error(t, err)
		assert.Error(t, result.Error)
		assert.Contains(t, err.Error(), "write file")
		unchanged(t, path)
	})

	t.Run("a dry run does not need to write, so it succeeds on the same file", func(t *testing.T) {
		path := write(t, 0o444)

		result, err := MigrateFile(path, MigrateOptions{DryRun: true})

		require.NoError(t, err)
		assert.True(t, result.Migrated)
		unchanged(t, path)
	})
}

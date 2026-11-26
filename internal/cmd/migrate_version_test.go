package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// chdirMigrateProject switches into a minimal project so config.Load succeeds,
// and resets the migrate version flag variables, which cobra leaves at whatever
// the previous Execute call set.
func chdirMigrateProject(t *testing.T) {
	t.Helper()

	projectDir := evalSymlinks(t, t.TempDir())
	require.NoError(t, os.Mkdir(filepath.Join(projectDir, "manifest"), 0o755))

	origDir, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Chdir(origDir)
		migrateWrite = false
		migrateProvDir = ""
		migrateServDir = ""
		migrateStacksDir = ""
	})
	require.NoError(t, os.Chdir(projectDir))
}

// TestMigrateVersionCmd_ExitStatus pins that a file that cannot be migrated
// fails the command after every file has been processed. Before the fix the
// command printed the error count and exited 0.
func TestMigrateVersionCmd_ExitStatus(t *testing.T) {
	const (
		goodService = "name: good\nprovisions: [a]\n"
		multiDoc    = "name: a\nprovisions: [x]\n---\nname: b\nprovisions: [y]\n"
	)

	// layout writes a services directory and returns the flags that point the
	// command at it, with empty provisions and stacks directories.
	layout := func(t *testing.T, services map[string]string) (dir string, args []string) {
		t.Helper()
		dir = t.TempDir()
		servicesDir := filepath.Join(dir, "services")
		for _, sub := range []string{"services", "provisions", "stacks"} {
			require.NoError(t, os.Mkdir(filepath.Join(dir, sub), 0o755))
		}
		for name, content := range services {
			require.NoError(t, os.WriteFile(filepath.Join(servicesDir, name), []byte(content), 0o644))
		}
		return servicesDir, []string{
			"migrate", "version",
			"--services", servicesDir,
			"--provisions", filepath.Join(dir, "provisions"),
			"--stacks", filepath.Join(dir, "stacks"),
		}
	}

	t.Run("a file that cannot be migrated fails the command in dry-run", func(t *testing.T) {
		chdirMigrateProject(t)
		_, args := layout(t, map[string]string{"a-bad.yml": multiDoc, "b-good.yml": goodService})

		_, err := executeCmd(t, args...)

		assert.Error(t, err)
	})

	t.Run("with --write the other files are still migrated and the bad one is untouched", func(t *testing.T) {
		chdirMigrateProject(t)
		servicesDir, args := layout(t, map[string]string{"a-bad.yml": multiDoc, "b-good.yml": goodService})

		_, err := executeCmd(t, append(args, "--write")...)

		assert.Error(t, err)
		bad, readErr := os.ReadFile(filepath.Join(servicesDir, "a-bad.yml"))
		require.NoError(t, readErr)
		assert.Equal(t, multiDoc, string(bad))
		good, readErr := os.ReadFile(filepath.Join(servicesDir, "b-good.yml"))
		require.NoError(t, readErr)
		assert.Contains(t, string(good), "kind: Service", "the valid file is migrated even though another failed")
	})

	t.Run("all files migrating succeeds", func(t *testing.T) {
		chdirMigrateProject(t)
		_, args := layout(t, map[string]string{"b-good.yml": goodService})

		_, err := executeCmd(t, append(args, "--write")...)

		assert.NoError(t, err)
	})
}

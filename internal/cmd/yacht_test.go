package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestYachtCmd_Help(t *testing.T) {
	t.Run("yacht shows help", func(t *testing.T) {
		output, err := executeCmd(t, "yacht")
		assert.NoError(t, err)
		assert.Contains(t, output, "up")
		assert.Contains(t, output, "down")
		assert.Contains(t, output, "restart")
		assert.Contains(t, output, "status")
	})

	t.Run("yacht --help", func(t *testing.T) {
		output, err := executeCmd(t, "yacht", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "Docker Compose")
	})
}

func TestYachtCmd_Aliases(t *testing.T) {
	t.Run("hoist alias works", func(t *testing.T) {
		output, err := executeCmd(t, "hoist", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "Docker Compose")
	})
}

func TestYachtCmd_Subcommands(t *testing.T) {
	t.Run("yacht up help", func(t *testing.T) {
		output, err := executeCmd(t, "yacht", "up", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "services")
	})

	t.Run("yacht down help", func(t *testing.T) {
		output, err := executeCmd(t, "yacht", "down", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "Stops")
	})

	t.Run("yacht restart help", func(t *testing.T) {
		output, err := executeCmd(t, "yacht", "restart", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "Restart")
	})

	t.Run("yacht status help", func(t *testing.T) {
		output, err := executeCmd(t, "yacht", "status", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "status")
	})
}

func TestYachtCmd_Structure(t *testing.T) {
	// Find yacht command
	resetRootCmd(t)
	var yachtCmd *cobra.Command
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "yacht" {
			yachtCmd = cmd
			break
		}
	}

	assert.NotNil(t, yachtCmd, "yacht command should exist")

	t.Run("yacht has subcommands", func(t *testing.T) {
		subcommands := yachtCmd.Commands()
		names := make([]string, 0, len(subcommands))
		for _, cmd := range subcommands {
			names = append(names, cmd.Name())
		}

		assert.Contains(t, names, "up")
		assert.Contains(t, names, "down")
		assert.Contains(t, names, "restart")
		assert.Contains(t, names, "status")
	})
}

// Note: The actual execution of yacht commands requires Docker,
// so we test the command structure and help text rather than execution.
// Integration tests would be needed for full execution testing.

func TestValidateComposeFile(t *testing.T) {
	t.Run("non-existent file", func(t *testing.T) {
		err := validateComposeFile("/non/existent/file.yml")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "compose file not found")
	})

	t.Run("existing valid compose file", func(t *testing.T) {
		tmpDir := t.TempDir()
		composeFile := filepath.Join(tmpDir, "docker-compose.yml")

		content := `services:
  web:
    image: nginx:latest
  db:
    image: postgres:15
`
		require.NoError(t, os.WriteFile(composeFile, []byte(content), 0644))

		// Note: This test will fail if docker compose is not installed
		// In that case, we just check file existence succeeds
		err := validateComposeFile(composeFile)
		// Error could be nil (docker available) or contain "invalid compose file" (docker unavailable)
		if err != nil {
			// If docker compose is not available, the error should be about invalid compose file
			// not about file not found
			assert.NotContains(t, err.Error(), "compose file not found")
		}
	})
}

func TestValidateServiceNames(t *testing.T) {
	t.Run("empty services list", func(t *testing.T) {
		err := validateServiceNames("/any/path.yml", []string{})
		assert.NoError(t, err)
	})

	t.Run("non-existent file", func(t *testing.T) {
		err := validateServiceNames("/non/existent/file.yml", []string{"web"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "read compose file")
	})

	t.Run("valid service names", func(t *testing.T) {
		tmpDir := t.TempDir()
		composeFile := filepath.Join(tmpDir, "docker-compose.yml")

		content := `services:
  web:
    image: nginx:latest
  db:
    image: postgres:15
  redis:
    image: redis:latest
`
		require.NoError(t, os.WriteFile(composeFile, []byte(content), 0644))

		err := validateServiceNames(composeFile, []string{"web", "db"})
		assert.NoError(t, err)
	})

	t.Run("invalid service name", func(t *testing.T) {
		tmpDir := t.TempDir()
		composeFile := filepath.Join(tmpDir, "docker-compose.yml")

		content := `services:
  web:
    image: nginx:latest
  db:
    image: postgres:15
`
		require.NoError(t, os.WriteFile(composeFile, []byte(content), 0644))

		err := validateServiceNames(composeFile, []string{"web", "nonexistent"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown services")
		assert.Contains(t, err.Error(), "nonexistent")
		assert.Contains(t, err.Error(), "Valid services")
	})

	t.Run("multiple invalid service names", func(t *testing.T) {
		tmpDir := t.TempDir()
		composeFile := filepath.Join(tmpDir, "docker-compose.yml")

		content := `services:
  web:
    image: nginx:latest
`
		require.NoError(t, os.WriteFile(composeFile, []byte(content), 0644))

		err := validateServiceNames(composeFile, []string{"foo", "bar"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "foo")
		assert.Contains(t, err.Error(), "bar")
	})

	t.Run("invalid yaml", func(t *testing.T) {
		tmpDir := t.TempDir()
		composeFile := filepath.Join(tmpDir, "docker-compose.yml")

		content := `not: valid: yaml: syntax:
:::
`
		require.NoError(t, os.WriteFile(composeFile, []byte(content), 0644))

		err := validateServiceNames(composeFile, []string{"web"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parse compose file")
	})
}

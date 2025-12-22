package cmd

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
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

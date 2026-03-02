package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTriggerCmd_Registration(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"trigger"})
	require.NoError(t, err)
	assert.Equal(t, "trigger", cmd.Name())
}

func TestTriggerCmd_Help(t *testing.T) {
	t.Run("trigger --help shows description", func(t *testing.T) {
		output, err := executeCmd(t, "trigger", "--help")
		require.NoError(t, err)
		assert.Contains(t, output, "trigger")
		assert.Contains(t, output, "reconciliation")
		assert.Contains(t, output, "daemon")
	})

	t.Run("trigger --help shows connection details", func(t *testing.T) {
		output, err := executeCmd(t, "trigger", "--help")
		require.NoError(t, err)
		assert.Contains(t, output, "Unix socket")
	})

	t.Run("trigger --help shows examples", func(t *testing.T) {
		output, err := executeCmd(t, "trigger", "--help")
		require.NoError(t, err)
		assert.Contains(t, output, "bosun trigger")
		assert.Contains(t, output, "--force")
	})
}

func TestTriggerCmd_NoAliases(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"trigger"})
	require.NoError(t, err)
	assert.Empty(t, cmd.Aliases, "trigger command should have no aliases")
}

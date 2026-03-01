package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatusCmd_Registration(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"status"})
	require.NoError(t, err)
	assert.Equal(t, "status", cmd.Name())
}

func TestStatusCmd_HelpContent(t *testing.T) {
	t.Run("status --help shows description", func(t *testing.T) {
		output, err := executeCmd(t, "status", "--help")
		require.NoError(t, err)
		assert.Contains(t, output, "status")
		assert.Contains(t, output, "health")
	})

	t.Run("status --help shows long description", func(t *testing.T) {
		output, err := executeCmd(t, "status", "--help")
		require.NoError(t, err)
		assert.Contains(t, output, "crew")
		assert.Contains(t, output, "infrastructure")
	})
}

func TestStatusCmd_BridgeAlias(t *testing.T) {
	t.Run("bridge alias resolves to status", func(t *testing.T) {
		cmd, _, err := rootCmd.Find([]string{"bridge"})
		require.NoError(t, err)
		assert.Equal(t, "status", cmd.Name())
	})

	t.Run("bridge --help works", func(t *testing.T) {
		output, err := executeCmd(t, "bridge", "--help")
		require.NoError(t, err)
		assert.Contains(t, output, "health")
	})
}

func TestDaemonStatusCmd_Registration(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"daemon-status"})
	require.NoError(t, err)
	assert.Equal(t, "daemon-status", cmd.Name())
}

func TestDaemonStatusCmd_Help(t *testing.T) {
	t.Run("daemon-status --help shows description", func(t *testing.T) {
		output, err := executeCmd(t, "daemon-status", "--help")
		require.NoError(t, err)
		assert.Contains(t, output, "daemon")
		assert.Contains(t, output, "status")
		assert.Contains(t, output, "Unix socket")
	})

	t.Run("daemon-status --help shows examples", func(t *testing.T) {
		output, err := executeCmd(t, "daemon-status", "--help")
		require.NoError(t, err)
		assert.Contains(t, output, "bosun daemon-status")
		assert.Contains(t, output, "bosun ds")
		assert.Contains(t, output, "--json")
	})
}

func TestDaemonStatusCmd_DsAlias(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"ds"})
	require.NoError(t, err)
	assert.Equal(t, "daemon-status", cmd.Name())
}

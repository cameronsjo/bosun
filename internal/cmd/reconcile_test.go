package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReconcileCmd_Registration(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"reconcile"})
	require.NoError(t, err)
	assert.Equal(t, "reconcile", cmd.Name())
}

func TestReconcileCmd_Help(t *testing.T) {
	t.Run("reconcile --help shows description", func(t *testing.T) {
		output, err := executeCmd(t, "reconcile", "--help")
		require.NoError(t, err)
		assert.Contains(t, output, "reconcile")
		assert.Contains(t, output, "GitOps")
		assert.Contains(t, output, "Clone/pull repository")
	})

	t.Run("reconcile --help shows workflow steps", func(t *testing.T) {
		output, err := executeCmd(t, "reconcile", "--help")
		require.NoError(t, err)
		assert.Contains(t, output, "Acquire lock")
		assert.Contains(t, output, "Decrypt secrets")
		assert.Contains(t, output, "Docker compose up")
	})

	t.Run("reconcile --help shows env vars", func(t *testing.T) {
		output, err := executeCmd(t, "reconcile", "--help")
		require.NoError(t, err)
		assert.Contains(t, output, "REPO_URL")
		assert.Contains(t, output, "REPO_BRANCH")
		assert.Contains(t, output, "DEPLOY_TARGET")
		assert.Contains(t, output, "SECRETS_FILES")
	})
}

func TestReconcileCmd_NoAliases(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"reconcile"})
	require.NoError(t, err)
	assert.Empty(t, cmd.Aliases, "reconcile command should have no aliases")
}

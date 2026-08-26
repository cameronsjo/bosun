package cmd

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/fatih/color"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/daemon"
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

func TestDaemonStatusOutput_UsesStatusForDiagnostics(t *testing.T) {
	status := &daemon.StatusResponse{
		State:     "idle",
		Uptime:    "2m",
		LastError: "sanitized operator diagnostic",
	}
	health := &daemon.HealthResponse{Status: "degraded", Ready: true}

	t.Run("human", func(t *testing.T) {
		var colorOutput bytes.Buffer
		oldOutput := color.Output
		oldNoColor := color.NoColor
		color.Output = &colorOutput
		color.NoColor = true
		t.Cleanup(func() {
			color.Output = oldOutput
			color.NoColor = oldNoColor
		})

		stdout := captureStdout(t, func() { printStatusHuman(status, health) })
		output := stdout + colorOutput.String()
		assert.Contains(t, output, "sanitized operator diagnostic")
		assert.Contains(t, output, "Health: degraded")
		assert.Contains(t, output, "Ready: true")
	})

	t.Run("JSON", func(t *testing.T) {
		output := captureStdout(t, func() { printStatusJSON(status, health) })
		var decoded statusJSONOutput
		require.NoError(t, json.Unmarshal([]byte(output), &decoded))
		require.NotNil(t, decoded.LastError)
		assert.Equal(t, "sanitized operator diagnostic", *decoded.LastError)
		require.NotNil(t, decoded.Health)
		assert.Equal(t, "degraded", *decoded.Health)
		require.NotNil(t, decoded.Ready)
		assert.True(t, *decoded.Ready)
	})
}

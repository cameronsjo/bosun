package cmd

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/fatih/color"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/daemon"
	"github.com/cameronsjo/bosun/internal/reconcile"
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

func TestDaemonStatusOutput_DefaultJSONShapeOmitsTargets(t *testing.T) {
	status := &daemon.StatusResponse{State: "idle", Uptime: "1s"}
	health := &daemon.HealthResponse{Status: "healthy", Ready: true}

	output := captureStdout(t, func() { printStatusJSON(status, health) })
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(output), &fields))
	assert.Len(t, fields, 6)
	for _, key := range []string{"state", "uptime", "last_reconcile", "last_error", "health", "ready"} {
		assert.Contains(t, fields, key)
	}
	assert.Equal(t, "null", string(fields["last_reconcile"]))
	assert.Equal(t, "null", string(fields["last_error"]))
	assert.NotContains(t, output, "targets")
}

func TestPrintDeploymentTargetStatus(t *testing.T) {
	root := t.TempDir()
	cfg := reconcile.DefaultConfig()
	cfg.StateFile = filepath.Join(root, "base.json")
	cfg.StagingDir = filepath.Join(root, "staging")
	cfg.LockFile = filepath.Join(root, "reconcile.lock")
	cfg.Targets = []reconcile.Target{
		{Name: "none", ProjectName: "none", LocalAppdataPath: filepath.Join(root, "appdata-none"), StateFile: filepath.Join(root, "none.json")},
		{Name: "clean", ProjectName: "clean", LocalAppdataPath: filepath.Join(root, "appdata-clean"), StateFile: filepath.Join(root, "clean.json")},
		{Name: "drift", ProjectName: "drift", LocalAppdataPath: filepath.Join(root, "appdata-drift"), StateFile: filepath.Join(root, "drift.json")},
		{Name: "remote", TargetHost: "operator@example.test", ProjectName: "remote", RemoteAppdataPath: "/srv/remote", StateFile: filepath.Join(root, "remote.json")},
	}

	for _, target := range cfg.Targets {
		state := &reconcile.DeployState{}
		switch target.Name {
		case "clean":
			state.LastDeployedCommit = "clean-commit"
		case "drift":
			state.LastDeployedCommit = "drift-commit"
			state.DriftItems = []reconcile.DriftItem{{Service: "web", Type: reconcile.DriftMissing}}
		case "remote":
			state.LastDeployedCommit = "remote-commit"
			state.AttemptCount = reconcile.MaxAttempts
			state.LastAttemptedCommit = "failed-commit"
		}
		require.NoError(t, reconcile.SaveState(target.StateFile, state))
	}

	targets, err := cfg.ResolveTargets()
	require.NoError(t, err)
	output := captureColoredStdout(t, func() { printDeploymentTargetStatus(cfg, targets) })
	assert.Contains(t, output, "none: commit none, drift unknown, breaker closed")
	assert.Contains(t, output, "clean: commit clean-commit, drift clean, breaker closed")
	assert.Contains(t, output, "drift: commit drift-commit, drift drifted, breaker closed")
	assert.Contains(t, output, "remote: commit remote-commit, drift clean, breaker open (remote; cached only)")
}

func TestPrintDaemonTargetsHuman(t *testing.T) {
	output := captureColoredStdout(t, func() {
		printDaemonTargetsHuman([]daemon.TargetHealthStatus{
			{
				Name:               "alpha",
				LastDeployedCommit: "abc123",
				Drift:              daemon.TargetDriftStatus{Status: "drifted"},
				CircuitBreaker:     daemon.SubsystemStatus{Status: daemon.StatusOpen},
			},
			{Name: "beta", LastDeployedCommit: "none", Drift: daemon.TargetDriftStatus{Status: "unknown"}, CircuitBreaker: daemon.SubsystemStatus{Status: daemon.StatusClosed}},
		})
	})
	assert.Contains(t, output, "Deployment Targets:")
	assert.Contains(t, output, "alpha: deploy=abc123 drift=drifted breaker=open")
	assert.Contains(t, output, "beta: deploy=none drift=unknown breaker=closed")
}

func TestRunStatus_AllRemoteUsesCachedTargetState(t *testing.T) {
	root := t.TempDir()
	t.Setenv("BOSUN_STATE_DIR", root)
	targetsJSON, err := json.Marshal([]reconcile.Target{
		{Name: "alpha", TargetHost: "operator@alpha", ProjectName: "alpha", RemoteAppdataPath: "/srv/alpha", StateFile: filepath.Join(root, "alpha.json")},
		{Name: "beta", TargetHost: "operator@beta", ProjectName: "beta", RemoteAppdataPath: "/srv/beta", StateFile: filepath.Join(root, "beta.json")},
	})
	require.NoError(t, err)
	t.Setenv("BOSUN_TARGETS", string(targetsJSON))

	cfg := daemon.ConfigFromEnv()
	targets, err := cfg.ReconcileConfig.ResolveTargets()
	require.NoError(t, err)
	require.Len(t, targets, 2)
	require.NoError(t, reconcile.SaveState(cfg.ReconcileConfig.ConfigForTarget(targets[0]).StateFile, &reconcile.DeployState{LastDeployedCommit: "alpha-commit"}))
	require.NoError(t, reconcile.SaveState(cfg.ReconcileConfig.ConfigForTarget(targets[1]).StateFile, &reconcile.DeployState{LastDeployedCommit: "beta-commit"}))

	output := captureColoredStdout(t, func() { runStatus(nil, nil) })
	assert.Contains(t, output, "--- Deployment Targets ---")
	assert.Contains(t, output, "alpha: commit alpha-commit")
	assert.Contains(t, output, "beta: commit beta-commit")
	assert.Contains(t, output, "All configured targets are remote; local Docker dashboard is unavailable")
	assert.NotContains(t, output, "Crew Status")
}

func TestRunStatus_InvalidTargetLayoutReturnsBeforeDocker(t *testing.T) {
	t.Setenv("BOSUN_STATE_DIR", t.TempDir())
	t.Setenv("BOSUN_TARGETS", `[{"name":"alpha","project_name":"one"},{"name":"alpha","project_name":"two"}]`)
	t.Setenv("DOCKER_HOST", "unix://"+filepath.Join(t.TempDir(), "docker.sock"))

	output := captureColoredStdout(t, func() { runStatus(nil, nil) })
	assert.Contains(t, output, "Failed to resolve deployment targets")
	assert.Contains(t, output, "duplicate names")
	assert.NotContains(t, output, "Docker not available")
}

func TestRunStatus_MixedTargetsReportsDockerUnavailable(t *testing.T) {
	t.Setenv("BOSUN_STATE_DIR", t.TempDir())
	t.Setenv("BOSUN_TARGETS", `[{"name":"local","project_name":"local"},{"name":"remote","target_host":"operator@remote","project_name":"remote","remote_appdata_path":"/srv/remote"}]`)
	t.Setenv("DOCKER_HOST", "unix://"+filepath.Join(t.TempDir(), "missing-docker.sock"))

	output := captureColoredStdout(t, func() { runStatus(nil, nil) })
	assert.Contains(t, output, "--- Deployment Targets ---")
	assert.Contains(t, output, "local:")
	assert.Contains(t, output, "remote:")
	assert.Contains(t, output, "Docker not available")
}

func captureColoredStdout(t *testing.T, fn func()) string {
	t.Helper()
	var colorOutput bytes.Buffer
	oldOutput := color.Output
	oldNoColor := color.NoColor
	color.Output = &colorOutput
	color.NoColor = true
	defer func() {
		color.Output = oldOutput
		color.NoColor = oldNoColor
	}()
	return captureStdout(t, fn) + colorOutput.String()
}

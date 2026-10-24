package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/reconcile"
)

// A malformed or out-of-range value is ignored: the field keeps its default.
func TestApplyReconcileEnvIgnoresInvalidValues(t *testing.T) {
	for name, value := range map[string]string{
		"BOSUN_COMPOSE_UP_TIMEOUT":    "0",
		"BOSUN_BACKUP_TIMEOUT":        "soon",
		"BOSUN_HEALTH_CHECK_TIMEOUT":  "-5s",
		"BOSUN_HEALTH_CHECK_INTERVAL": "0",
		"BOSUN_HEALTH_GATE_TIMEOUT":   "-1",
		"BOSUN_RESTART_WINDOW":        "0",
		"BOSUN_RESTART_THRESHOLD":     "0",
		"BOSUN_HEALTH_GATE_SCOPE":     "sometimes",
		"BOSUN_CRITICAL_CONTAINERS":   "not json",
	} {
		t.Setenv(name, value)
	}
	defaults := reconcile.DefaultConfig()
	got := reconcile.DefaultConfig()

	ApplyReconcileEnv(got)

	require.Equal(t, defaults.ComposeUpTimeout, got.ComposeUpTimeout)
	require.Equal(t, defaults.BackupTimeout, got.BackupTimeout)
	require.Equal(t, defaults.HealthCheckTimeout, got.HealthCheckTimeout)
	require.Equal(t, defaults.HealthCheckInterval, got.HealthCheckInterval)
	require.Equal(t, defaults.HealthGateTimeout, got.HealthGateTimeout)
	require.Equal(t, defaults.RestartWindow, got.RestartWindow)
	require.Equal(t, defaults.RestartThreshold, got.RestartThreshold)
	require.Equal(t, defaults.HealthGateScope, got.HealthGateScope)
	require.False(t, got.CriticalContainers.FromEnv())
}

func TestApplyReconcileEnvZeroDisablesHealthChecks(t *testing.T) {
	t.Setenv("BOSUN_HEALTH_CHECK_TIMEOUT", "0")
	t.Setenv("BOSUN_HEALTH_GATE_TIMEOUT", "0")
	got := reconcile.DefaultConfig()

	ApplyReconcileEnv(got)

	require.Equal(t, time.Duration(0), got.HealthCheckTimeout)
	require.Equal(t, time.Duration(0), got.HealthGateTimeout)
}

func TestApplyReconcileEnvContentHashDefaultsOn(t *testing.T) {
	got := reconcile.DefaultConfig()
	ApplyReconcileEnv(got)
	require.True(t, got.ContentHashSync)
}

func TestApplyProjectOperationalConfigNilIsNoop(t *testing.T) {
	got := reconcile.DefaultConfig()
	ApplyProjectOperationalConfig(nil, got)
	require.Equal(t, reconcile.DefaultConfig(), got)
}

func TestApplyReconcileEnvAppliesValidValues(t *testing.T) {
	for name, value := range map[string]string{
		"BOSUN_DEPLOY_MODE":                "local",
		"BOSUN_COMPOSE_UP_TIMEOUT":         "13m",
		"BOSUN_BACKUP_TIMEOUT":             "7m",
		"BOSUN_HEALTH_CHECK_TIMEOUT":       "23",
		"BOSUN_HEALTH_CHECK_INTERVAL":      "3s",
		"BOSUN_HEALTH_GATE_TIMEOUT":        "17s",
		"BOSUN_HEALTH_GATE_SCOPE":          "declared",
		"BOSUN_RESTART_BREAKER":            "false",
		"BOSUN_RESTART_THRESHOLD":          "9",
		"BOSUN_RESTART_WINDOW":             "21m",
		"BOSUN_DEPLOY_SYNC_PATHS":          `["a/**"]`,
		"BOSUN_DEPLOY_SYNC_EXCLUDE":        `["b/**"]`,
		"BOSUN_CRITICAL_CONTAINERS":        `["c"]`,
		"BOSUN_DRIFT_IGNORE":               `[{"service":"s","type":"missing"}]`,
		"BOSUN_CONTENT_HASH_SYNC":          "false",
		"BOSUN_REMOVE_ORPHANS":             "false",
		"BOSUN_ALLOW_EMPTY_DECLARED_STATE": "true",
		"BOSUN_SKIP_DEPLOY_INVARIANT":      "true",
	} {
		t.Setenv(name, value)
	}
	got := reconcile.DefaultConfig()

	ApplyReconcileEnv(got)

	require.Equal(t, "local", got.DeployMode)
	require.Equal(t, 13*time.Minute, got.ComposeUpTimeout)
	require.Equal(t, 7*time.Minute, got.BackupTimeout)
	require.Equal(t, 23*time.Second, got.HealthCheckTimeout)
	require.Equal(t, 3*time.Second, got.HealthCheckInterval)
	require.Equal(t, 17*time.Second, got.HealthGateTimeout)
	require.Equal(t, "declared", got.HealthGateScope)
	require.False(t, got.RestartBreakerEnabled)
	require.Equal(t, 9, got.RestartThreshold)
	require.Equal(t, 21*time.Minute, got.RestartWindow)
	require.Equal(t, []string{"a/**"}, got.DeploySyncPaths.Value)
	require.Equal(t, []string{"b/**"}, got.DeploySyncExclude.Value)
	require.Equal(t, []string{"c"}, got.CriticalContainers.Value)
	require.Equal(t, []reconcile.DriftIgnoreRule{{Service: "s", Type: "missing"}}, got.DriftIgnore.Value)
	require.False(t, got.ContentHashSync)
	require.False(t, got.RemoveOrphans.Value)
	require.True(t, got.RemoveOrphans.FromEnv())
	require.True(t, got.AllowEmptyDeclaredState)
	require.True(t, got.SkipDeployInvariant)
}

func TestApplyProjectOperationalConfigReadsProjectFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bosun.yaml"), []byte(`
critical_containers: [from-file]
deploy_sync_paths: ["f/**"]
deploy_sync_exclude: ["g/**"]
compose_ignore: [ignored]
health_gate_scope: declared
remove_orphans: false
`), 0o600))
	t.Chdir(dir)
	projectCfg, err := Load()
	require.NoError(t, err)
	got := reconcile.DefaultConfig()

	ApplyProjectOperationalConfig(projectCfg, got)

	require.Equal(t, []string{"from-file"}, got.CriticalContainers.Value)
	require.Equal(t, []string{"f/**"}, got.DeploySyncPaths.Value)
	require.Equal(t, []string{"g/**"}, got.DeploySyncExclude.Value)
	require.Equal(t, []string{"ignored"}, got.ComposeIgnore.Value)
	require.Equal(t, "declared", got.HealthGateScope)
	require.False(t, got.RemoveOrphans.Value)
}

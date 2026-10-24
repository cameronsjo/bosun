package config

import (
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

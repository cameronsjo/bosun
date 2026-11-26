package daemon

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/docker/dockertest"
	"github.com/cameronsjo/bosun/internal/reconcile"
)

// newDriftDaemonNoAlerter mirrors newAlertDaemon but leaves AlertManager nil, so
// d.alerter is nil. It is deliberately not newConcurrencyDaemon: that daemon is
// a dry run, and a dry-run drift tick persists nothing (#792).
func newDriftDaemonNoAlerter(t *testing.T) *Daemon {
	t.Helper()
	tmpDir := evalSymlinks(t, t.TempDir())

	cfg := DefaultConfig()
	cfg.EnableHTTP = false
	cfg.ReconcileConfig = reconcile.DefaultConfig()
	cfg.ReconcileConfig.RepoURL = "https://github.com/test/repo"
	cfg.ReconcileConfig.DryRun = false
	cfg.ReconcileConfig.RepoDir = filepath.Join(tmpDir, "repo")
	cfg.ReconcileConfig.LockFile = privateLockPath(t, tmpDir, "test.lock")
	cfg.ReconcileConfig.StateFile = filepath.Join(tmpDir, "state.json")
	cfg.SocketPath = filepath.Join(tmpDir, "test.sock")

	d, err := New(cfg)
	require.NoError(t, err)
	require.Nil(t, d.alerter, "this helper must build a daemon without an alert manager")
	return d
}

// runningContainers is a docker mock that reports the named services as running
// with the image "<name>:latest".
func runningContainers(names ...string) *dockertest.MockDockerAPI {
	return &dockertest.MockDockerAPI{
		ContainerListFunc: func(_ context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
			items := make([]container.Summary, 0, len(names))
			for _, name := range names {
				items = append(items, container.Summary{
					ID:    name + "123456789",
					Names: []string{"/" + name},
					State: "running",
					Image: name + ":latest",
				})
			}
			return client.ContainerListResult{Items: items}, nil
		},
	}
}

// retiredKeysRows is shared by the two "resolved keys are retired" tests. Both
// rows seed keys inside the 1h cooldown, so no drift alert is sent in either.
var retiredKeysRows = []struct {
	name        string
	seed        []string
	running     []string
	wantRetired []string
	wantKept    []string
}{
	{
		name:        "no drift left: every resolved key is retired",
		seed:        []string{"api:missing"},
		running:     []string{"api", "web"},
		wantRetired: []string{"api:missing"},
	},
	{
		name:        "drift remains elsewhere: only the resolved service is retired",
		seed:        []string{"api:missing", "web:missing"},
		running:     []string{"web"},
		wantRetired: []string{"web:missing"},
		wantKept:    []string{"api:missing"},
	},
}

func seedDriftAlertState(t *testing.T, d *Daemon, alerted ...string) string {
	t.Helper()
	stateFile := d.config.ReconcileConfig.StateFile
	items := make(map[string]time.Time, len(alerted))
	for _, key := range alerted {
		items[key] = time.Now().Add(-10 * time.Minute)
	}
	require.NoError(t, reconcile.SaveState(stateFile, &reconcile.DeployState{
		LastDeployedCommit: "abc123",
		DeclaredServices: []reconcile.DeclaredService{
			{Name: "api", Image: "api:latest"},
			{Name: "web", Image: "web:latest"},
		},
		DriftAlertedItems: items,
	}))
	return stateFile
}

func TestRunDriftCheck_ResolveAlertsDisabled_RetiresResolvedKeys(t *testing.T) {
	for _, tt := range retiredKeysRows {
		t.Run(tt.name, func(t *testing.T) {
			provider := &testAlertProvider{}
			d := newAlertDaemon(t, provider)
			d.config.DriftResolveAlerts = false
			d.config.ReconcileConfig.RestartBreakerEnabled = false
			d.dockerClientOverride = docker.NewClientWithAPI(runningContainers(tt.running...))

			stateFile := seedDriftAlertState(t, d, tt.seed...)

			d.runDriftCheck(context.Background())

			loaded := reconcile.LoadState(stateFile)
			for _, key := range tt.wantRetired {
				assert.NotContains(t, loaded.DriftAlertedItems, key, "a resolved key must be retired even though resolution alerts are disabled")
			}
			for _, key := range tt.wantKept {
				assert.Contains(t, loaded.DriftAlertedItems, key, "a still-drifting service keeps its alerted key")
			}
			assert.Zero(t, provider.attempts, "no resolution alert may be sent, and the drift alert is inside its cooldown")
		})
	}
}

func TestRunDriftCheck_NoAlerter_RetiresResolvedKeys(t *testing.T) {
	for _, tt := range retiredKeysRows {
		t.Run(tt.name, func(t *testing.T) {
			d := newDriftDaemonNoAlerter(t)
			d.config.ReconcileConfig.RestartBreakerEnabled = false
			d.dockerClientOverride = docker.NewClientWithAPI(runningContainers(tt.running...))

			stateFile := seedDriftAlertState(t, d, tt.seed...)

			d.runDriftCheck(context.Background())

			loaded := reconcile.LoadState(stateFile)
			for _, key := range tt.wantRetired {
				assert.NotContains(t, loaded.DriftAlertedItems, key, "a resolved key must be retired when there is no alerter")
			}
			for _, key := range tt.wantKept {
				assert.Contains(t, loaded.DriftAlertedItems, key, "a still-drifting service keeps its alerted key")
			}
		})
	}
}

// TestRunDriftCheck_ResolveDisabled_ReAlertsAfterRecurrence pins the symptom: a
// stale alerted key inside the cooldown silently swallows the next real alert.
// The seed is 10 minutes old against a 1h cooldown on purpose; a seed older than
// the cooldown would let the original code alert anyway.
func TestRunDriftCheck_ResolveDisabled_ReAlertsAfterRecurrence(t *testing.T) {
	provider := &testAlertProvider{}
	d := newAlertDaemon(t, provider)
	d.config.DriftResolveAlerts = false
	d.config.DriftAlertCooldown = time.Hour
	d.config.ReconcileConfig.RestartBreakerEnabled = false

	var apiRunning atomic.Bool
	apiRunning.Store(true)
	mockAPI := &dockertest.MockDockerAPI{
		ContainerListFunc: func(_ context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
			// web is always running, so only api's state drives drift.
			items := []container.Summary{{
				ID:    "web123456789",
				Names: []string{"/web"},
				State: "running",
				Image: "web:latest",
			}}
			if apiRunning.Load() {
				items = append(items, container.Summary{
					ID:    "api123456789",
					Names: []string{"/api"},
					State: "running",
					Image: "api:latest",
				})
			}
			return client.ContainerListResult{Items: items}, nil
		},
	}
	d.dockerClientOverride = docker.NewClientWithAPI(mockAPI)

	stateFile := seedDriftAlertState(t, d, "api:missing")

	// Tick 1: api is running, so its earlier alert has resolved.
	d.runDriftCheck(context.Background())
	loaded := reconcile.LoadState(stateFile)
	require.NotContains(t, loaded.DriftAlertedItems, "api:missing", "the resolved key must be gone after tick 1")
	require.Empty(t, provider.alerts)

	// Tick 2: api disappears again. This is a new incident and must alert.
	apiRunning.Store(false)
	d.runDriftCheck(context.Background())

	require.Len(t, provider.alerts, 1, "a recurrence after resolution must alert; a stale key inside the cooldown suppresses it")
	loaded = reconcile.LoadState(stateFile)
	assert.Contains(t, loaded.DriftAlertedItems, "api:missing", "the new alert is recorded")
}

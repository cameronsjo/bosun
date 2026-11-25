package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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

// TestRunDriftCheck_DryRunObservesOnly pins the dry-run daemon's periodic drift
// tick (#792): it detects and logs drift and updates the in-memory runtime
// status, but writes no state, stops no container, triggers no self-heal, and
// sends no alert. The first row is the control: the same scenario on a daemon
// that is not in dry run must stop the container, alert, trigger self-heal, and
// rewrite the state, so the dry-run row's zeroes cannot come from a scenario
// that never reached those effects.
//
// The dry-run row fails on the original code: the tick had no dry-run check, so
// it saved drift and restart-tracking data into the production state file,
// stopped the restart-looping container, consumed a self-heal attempt and
// triggered a reconcile, and sent the drift and breaker alerts.
func TestRunDriftCheck_DryRunObservesOnly(t *testing.T) {
	tests := []struct {
		name   string
		dryRun bool
	}{
		{name: "a daemon outside dry run acts (control)", dryRun: false},
		{name: "a dry-run daemon observes only", dryRun: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &testAlertProvider{}
			d := newAlertDaemon(t, provider)
			rcfg := d.config.ReconcileConfig
			rcfg.DryRun = tt.dryRun
			rcfg.ProjectName = "homelab"
			rcfg.RestartBreakerEnabled = true
			rcfg.RestartThreshold = 5
			d.config.DriftSelfHeal = reconcile.NewConfigField(true)
			d.config.DriftSelfHealCooldown = reconcile.NewConfigField(time.Nanosecond)
			d.config.MaxSelfHealAttempts = 2
			var triggers atomic.Int32
			d.triggerReconcileFn = func(context.Context, string, bool) error {
				triggers.Add(1)
				return nil
			}

			// "db" is declared but not running (critical drift). "web" is running
			// and has restarted five times since its baseline, which trips the breaker.
			//
			// The persisted state also carries a stale drift record from an earlier
			// real run: two items and an old timestamp, and an alert marker for
			// "cache", which is no longer drifting. A dry run must neither serve
			// those items in its status views nor send a resolution alert for them.
			stateFile := rcfg.StateFile
			staleCheckedAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
			staleItems := []reconcile.DriftItem{
				{Service: "cache", Type: reconcile.DriftMissing, Declared: "redis:7"},
				{Service: "queue", Type: reconcile.DriftMissing, Declared: "nats:2"},
			}
			require.NoError(t, reconcile.SaveState(stateFile, &reconcile.DeployState{
				LastDeployedCommit: "abc123",
				DriftCheckedAt:     staleCheckedAt,
				DriftItems:         staleItems,
				DriftAlertedItems: map[string]time.Time{
					reconcile.DriftAlertKey(staleItems[0]): time.Now().Add(-time.Hour),
				},
				DeclaredServices: []reconcile.DeclaredService{
					{Name: "web", Image: "nginx:latest"},
					{Name: "db", Image: "postgres:16"},
				},
				RestartTracking: map[string]reconcile.RestartTrackingEntry{
					"web": {
						ContainerID:          "homelab-web-",
						RestartCount:         0,
						BaselineRestartCount: 0,
						BaselineAt:           time.Now().Add(-time.Minute),
						CheckedAt:            time.Now().Add(-time.Minute),
					},
				},
			}))
			before, err := os.ReadFile(stateFile)
			require.NoError(t, err)

			mock := dockertest.NewMockDockerAPI()
			mock.ContainerListFunc = func(context.Context, client.ContainerListOptions) (client.ContainerListResult, error) {
				return client.ContainerListResult{Items: []container.Summary{
					dockertest.MakeTestContainer("homelab-web-", "homelab-web-1", "nginx:latest", "running", "homelab", "web"),
				}}, nil
			}
			mock.ContainerInspectFunc = func(_ context.Context, id string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
				return client.ContainerInspectResult{Container: container.InspectResponse{
					ID:              id + "-long-id",
					Name:            "/" + id,
					Config:          &container.Config{Image: "nginx:latest", Labels: breakerTestLabels("homelab", "web", "")},
					State:           &container.State{Status: container.StateRunning},
					RestartCount:    5,
					NetworkSettings: &container.NetworkSettings{},
				}}, nil
			}
			var stopped []string
			mock.ContainerStopFunc = func(_ context.Context, id string, _ client.ContainerStopOptions) (client.ContainerStopResult, error) {
				stopped = append(stopped, id)
				return client.ContainerStopResult{}, nil
			}
			d.dockerClientOverride = docker.NewClientWithAPI(mock)

			d.runDriftCheck(context.Background())
			d.wg.Wait() // no background reconcile may outlive the temp directory

			after, err := os.ReadFile(stateFile)
			require.NoError(t, err)

			// Both status surfaces report this tick's observation, one item (db) with
			// a fresh timestamp, never the two stale persisted items. On the original
			// code the dry-run row served the frozen file: two items, the old time.
			targets, snapErr := d.targetSnapshot()
			require.NoError(t, snapErr)
			statuses := d.targetHealthStatuses(targets)
			require.Len(t, statuses, 1)
			assert.Equal(t, "drifted", statuses[0].Drift.Status)
			assert.Equal(t, 1, statuses[0].Drift.Items, "the per-target record serves the current observation")
			assert.True(t, statuses[0].Drift.CheckedAt.After(staleCheckedAt))

			mux := http.NewServeMux()
			d.RegisterAPIRoutes(mux)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/drift", nil))
			require.Equal(t, http.StatusOK, rec.Code)
			var api APIDriftResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &api))
			assert.Equal(t, "drifted", api.Status)
			assert.Equal(t, 1, api.DriftItemCount)
			require.Len(t, api.Items, 1)
			assert.Equal(t, "db", api.Items[0].Service)
			require.NotNil(t, api.CheckedAt)
			assert.True(t, api.CheckedAt.After(staleCheckedAt))

			if !tt.dryRun {
				assert.Equal(t, []string{"homelab-web-1-long-id"}, stopped, "control: the breaker stops the looping container")
				assert.NotEmpty(t, provider.alerts, "control: drift and breaker alerts are sent")
				var resolved int
				for _, sent := range provider.alerts {
					if strings.Contains(sent.Title, "Drift Resolved") {
						resolved++
					}
				}
				assert.Equal(t, 1, resolved, "control: the now-clean service gets a resolution alert, so the dry-run row's silence is not vacuous")
				assert.Equal(t, int32(1), triggers.Load(), "control: self-heal triggers a reconcile")
				assert.NotEqual(t, string(before), string(after), "control: the tick saves drift state")
				return
			}

			assert.Empty(t, stopped, "a dry-run daemon stops no container")
			assert.Empty(t, provider.alerts, "a dry-run daemon sends no drift or breaker alert")
			assert.Zero(t, triggers.Load(), "a dry-run daemon triggers no self-heal reconcile")
			assert.Equal(t, string(before), string(after), "a dry-run daemon leaves the state file as it found it")

			status, ok := d.targetRuntimeStatus(reconcile.DefaultTargetName)
			require.True(t, ok, "the observed result is still recorded for status views")
			assert.Equal(t, "drifted", status.driftStatus)
		})
	}
}

// TestDryRunStatusViewsBeforeFirstTick pins that a dry-run daemon does not serve
// the frozen persisted drift record before its first tick: the file still holds
// items from an earlier real run, and reporting them (or "clean") would claim a
// current check that never happened. It fails on the code before the observation
// was kept in memory, which served those two stale items.
func TestDryRunStatusViewsBeforeFirstTick(t *testing.T) {
	d := newAlertDaemon(t, &testAlertProvider{})
	d.config.ReconcileConfig.DryRun = true
	require.NoError(t, reconcile.SaveState(d.config.ReconcileConfig.StateFile, &reconcile.DeployState{
		LastDeployedCommit: "abc123",
		DriftCheckedAt:     time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		DriftItems: []reconcile.DriftItem{
			{Service: "cache", Type: reconcile.DriftMissing},
			{Service: "queue", Type: reconcile.DriftMissing},
		},
	}))

	targets, snapErr := d.targetSnapshot()
	require.NoError(t, snapErr)
	statuses := d.targetHealthStatuses(targets)
	require.Len(t, statuses, 1)
	assert.Equal(t, "unknown", statuses[0].Drift.Status)
	assert.Zero(t, statuses[0].Drift.Items)
	assert.True(t, statuses[0].Drift.CheckedAt.IsZero())

	mux := http.NewServeMux()
	d.RegisterAPIRoutes(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/drift", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var api APIDriftResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &api))
	assert.Equal(t, "unknown", api.Status)
	assert.Zero(t, api.DriftItemCount)
	assert.Empty(t, api.Items)
	assert.Nil(t, api.CheckedAt)
}

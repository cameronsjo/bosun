package daemon

import (
	"context"
	"os"
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
			stateFile := rcfg.StateFile
			require.NoError(t, reconcile.SaveState(stateFile, &reconcile.DeployState{
				LastDeployedCommit: "abc123",
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

			if !tt.dryRun {
				assert.Equal(t, []string{"homelab-web-1-long-id"}, stopped, "control: the breaker stops the looping container")
				assert.NotEmpty(t, provider.alerts, "control: drift and breaker alerts are sent")
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

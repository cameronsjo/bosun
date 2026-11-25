package reconcile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// chmodOnRecoveryAlerter runs a callback when the retraction is dispatched. That
// dispatch is the last step before the final success save, so it is where a
// test can make only that save fail.
type chmodOnRecoveryAlerter struct {
	mockAlertSender
	onRecovery func()
}

func (a *chmodOnRecoveryAlerter) SendDeployRecovery(ctx context.Context, commit, target string, priorFailures int) error {
	a.onRecovery()
	return a.mockAlertSender.SendDeployRecovery(ctx, commit, target, priorFailures)
}

// TestRun_FinalStateWriteFailureKeepsRedeployMarker makes the state directory
// read-only after the pre-deploy marker was written and before the final save.
// The run still succeeds and sends its success alert, and the state keeps the
// redeploy marker and the old commit, so the next run retries.
func TestRun_FinalStateWriteFailureKeepsRedeployMarker(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping permission test when running as root")
	}

	h := newRunHarness(t, false)
	h.seed(t, &DeployState{
		SchemaVersion:       2,
		LastDeployedCommit:  "oldcommit",
		LastAttemptedCommit: "bbb222",
		AttemptCount:        2,
		LastAlertedAttempt:  1,
	})
	stateDir := filepath.Dir(h.cfg.StateFile)
	t.Cleanup(func() { _ = os.Chmod(stateDir, 0o755) })

	// Probe that the filesystem enforces directory write permission, then restore it.
	require.NoError(t, os.Chmod(stateDir, 0o555))
	if probe, probeErr := os.CreateTemp(stateDir, ".permission-check-*"); probeErr == nil {
		require.NoError(t, probe.Close())
		require.NoError(t, os.Remove(probe.Name()))
		t.Skip("filesystem does not enforce directory write permission in this test environment")
	}
	require.NoError(t, os.Chmod(stateDir, 0o755))

	alerter := &chmodOnRecoveryAlerter{onRecovery: func() {
		require.NoError(t, os.Chmod(stateDir, 0o555))
	}}
	r := NewReconciler(h.cfg,
		WithGitOperations(gitSyncing("aaa111", "bbb222")),
		WithSecretsDecryptor(&mockSOPS{}),
		WithDeployOps(&DeployOps{DryRun: true}),
		WithAlerter(alerter),
	)

	require.NoError(t, r.Run(context.Background()), "a failed final save is logged, not fatal")

	assert.Equal(t, 1, alerter.deployRecoveryCalls)
	assert.Equal(t, 1, alerter.deploySuccessCalls, "the success alert still sends")
	state := LoadState(h.cfg.StateFile)
	assert.True(t, state.NeedsRedeploy, "the pre-deploy marker survives, so the next run retries")
	assert.Equal(t, "oldcommit", state.LastDeployedCommit)
}

// TestRun_StateWriteFailureIsLoggedNotFatal injects a failed state write (the
// state directory is read-only) into each persisting exit that logs the failure
// and carries on. It pins that a failed save neither aborts the run nor
// suppresses the alert owed at that exit, and it covers the error branch of
// every persistState call these exits make.
func TestRun_StateWriteFailureIsLoggedNotFatal(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping permission test when running as root")
	}

	tests := []struct {
		name              string
		seed              *DeployState
		setup             func(t *testing.T, h *dryRunHarness) (context.Context, []ReconcilerOption)
		wantErr           bool
		wantFailureAlerts int
		wantRecoveryCalls int
	}{
		{
			// Covers the breaker reset save and the interruption marker clear save.
			name: "already-deployed skip with stale counters and a delivered interruption",
			seed: &DeployState{
				LastDeployedCommit:  "aaa111",
				LastAttemptedCommit: "x",
				AttemptCount:        2,
				LastAttemptOutcome: &LastAttemptOutcome{
					Outcome:   attemptOutcomeInterrupted,
					Commit:    "x",
					Timestamp: time.Now().UTC(),
					Alerted:   true,
				},
			},
			setup: func(t *testing.T, h *dryRunHarness) (context.Context, []ReconcilerOption) {
				return context.Background(), []ReconcilerOption{WithGitOperations(gitSyncing("aaa111", "aaa111"))}
			},
			wantRecoveryCalls: 1,
		},
		{
			// Covers the path-aware skip save.
			name: "path-aware skip",
			seed: &DeployState{LastDeployedCommit: "aaa111", DeployCount: 1},
			setup: func(t *testing.T, h *dryRunHarness) (context.Context, []ReconcilerOption) {
				h.cfg.DeployPaths = NewConfigField([]string{"unraid/**"})
				git := &mockGitWithDiff{
					syncChanged: true, syncBefore: "aaa111", syncAfter: "bbb222",
					diffFiles: []string{"docs/README.md"},
				}
				return context.Background(), []ReconcilerOption{WithGitOperations(git)}
			},
		},
		{
			// Covers the sync-failure attempt save and the alert-throttle save.
			name: "sync failure",
			seed: &DeployState{LastDeployedCommit: "aaa111"},
			setup: func(t *testing.T, h *dryRunHarness) (context.Context, []ReconcilerOption) {
				return context.Background(), []ReconcilerOption{
					WithGitOperations(&mockGitOps{syncErr: errors.New("injected sync failure")}),
				}
			},
			wantErr:           true,
			wantFailureAlerts: 1,
		},
		{
			// Covers the interrupted-outcome save and the delivery-recorded save.
			name: "caller cancellation during sync",
			seed: &DeployState{LastDeployedCommit: "aaa111"},
			setup: func(t *testing.T, h *dryRunHarness) (context.Context, []ReconcilerOption) {
				ctx, cancel := context.WithCancel(context.Background())
				t.Cleanup(cancel)
				git := callbackGitOps{sync: func(context.Context) (bool, string, string, error) {
					cancel()
					return false, "", "", context.Canceled
				}}
				return ctx, []ReconcilerOption{WithGitOperations(git)}
			},
			wantErr:           true,
			wantFailureAlerts: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newRunHarness(t, false)
			h.seed(t, tt.seed)
			stateDir := filepath.Dir(h.cfg.StateFile)
			require.NoError(t, os.Chmod(stateDir, 0o555))
			t.Cleanup(func() { _ = os.Chmod(stateDir, 0o755) })
			if probe, probeErr := os.CreateTemp(stateDir, ".permission-check-*"); probeErr == nil {
				require.NoError(t, probe.Close())
				require.NoError(t, os.Remove(probe.Name()))
				t.Skip("filesystem does not enforce directory write permission in this test environment")
			}
			ctx, opts := tt.setup(t, h)

			before := snapshotState(t, h.cfg.StateFile)
			opts = append([]ReconcilerOption{
				WithAlerter(h.alerter),
				WithDeployOps(&DeployOps{DryRun: true}),
			}, opts...)
			err := NewReconciler(h.cfg, opts...).Run(ctx)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err, "a failed state write is logged, not fatal, at this exit")
			}
			assert.Equal(t, tt.wantFailureAlerts, h.alerter.deployFailureCalls, "the alert owed at this exit still sends")
			assert.Equal(t, tt.wantRecoveryCalls, h.alerter.deployRecoveryCalls)
			assert.Equal(t, before, snapshotState(t, h.cfg.StateFile), "the injected failure left the state file as it was")
		})
	}
}

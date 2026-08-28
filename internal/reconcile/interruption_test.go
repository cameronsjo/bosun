package reconcile

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cameronsjo/bosun/internal/docker"
	logpkg "github.com/cameronsjo/bosun/internal/log"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsPropagatedCallerCancellation(t *testing.T) {
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	deadlineCtx, deadlineCancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	t.Cleanup(deadlineCancel)

	tests := []struct {
		name string
		ctx  context.Context
		err  error
		want bool
	}{
		{name: "cancelled caller and direct cancellation", ctx: canceledCtx, err: context.Canceled, want: true},
		{name: "cancelled caller and wrapped cancellation", ctx: canceledCtx, err: fmt.Errorf("stage: %w", context.Canceled), want: true},
		{name: "live caller and cancellation-shaped stage error", ctx: context.Background(), err: context.Canceled},
		{name: "deadline is a failure", ctx: deadlineCtx, err: context.DeadlineExceeded},
		{name: "cancelled caller racing with real error", ctx: canceledCtx, err: errors.New("disk failed")},
		{name: "cancelled caller and nil result", ctx: canceledCtx, err: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isPropagatedCallerCancellation(tt.ctx, tt.err))
		})
	}
}

func TestFinalizeAttempt_InterruptionRestoresBudgetAndPersistsOutcome(t *testing.T) {
	type contextKey string
	const valueKey contextKey = "interruption-value"

	tests := []struct {
		name          string
		seed          DeployState
		commit        string
		force         bool
		providerErr   error
		onFailure     bool
		wantAlerts    int
		wantRedeploy  bool
		wantAttempted string
		wantAttempts  int
		wantAlerted   int
	}{
		{
			name:   "same commit preserves prior real failure and throttle",
			seed:   DeployState{LastAttemptedCommit: "new", AttemptCount: 1, LastAlertedAttempt: 1, NeedsRedeploy: true},
			commit: "new", onFailure: true, wantAlerts: 1, wantRedeploy: true,
			wantAttempted: "new", wantAttempts: 1, wantAlerted: 1,
		},
		{
			name:   "first run of new commit restores prior commit budget",
			seed:   DeployState{LastAttemptedCommit: "old", AttemptCount: 2, LastAlertedAttempt: 1},
			commit: "new", onFailure: true, wantAlerts: 1,
			wantAttempted: "old", wantAttempts: 2, wantAlerted: 1,
		},
		{
			name:   "force mode does not change interruption accounting",
			seed:   DeployState{LastAttemptedCommit: "new", AttemptCount: MaxAttempts, LastAlertedAttempt: 3},
			commit: "new", force: true, onFailure: true, wantAlerts: 1,
			wantAttempted: "new", wantAttempts: MaxAttempts, wantAlerted: 3,
		},
		{
			name:   "provider failure leaves outcome and budget intact",
			seed:   DeployState{LastAttemptedCommit: "new", AttemptCount: 1, LastAlertedAttempt: 1},
			commit: "new", providerErr: errors.New("provider unavailable"), onFailure: true, wantAlerts: 1,
			wantAttempted: "new", wantAttempts: 1, wantAlerted: 1,
		},
		{
			name: "disabled failure alerts still persist outcome",
			seed: DeployState{}, commit: "new", wantAlerts: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stateFile := filepath.Join(t.TempDir(), "state.json")
			state := tt.seed
			require.NoError(t, SaveState(stateFile, &state))
			snapshot := snapshotAttemptBudget(&state, tt.commit)
			state.LastAttemptedCommit, state.AttemptCount = nextAttemptState(state.LastAttemptedCommit, tt.commit, state.AttemptCount)
			state.LastAlertedAttempt = state.AttemptCount

			alerter := &mockAlertSender{lastErr: tt.providerErr}
			r := NewReconciler(&Config{StateFile: stateFile, OnFailure: tt.onFailure, Force: tt.force}, WithAlerter(alerter))
			r.lastCommit = tt.commit
			r.runStartTime = time.Now()

			base := context.WithValue(context.Background(), valueKey, "preserved")
			base = logpkg.WithReconcileID(base, "reconcile-interrupted")
			ctx, cancel := context.WithCancel(base)
			cancel()
			startedAt := time.Now()
			r.finalizeAttempt(ctx, fmt.Errorf("wrapped: %w", context.Canceled), snapshot)
			finishedAt := time.Now()

			saved := LoadState(stateFile)
			assert.Equal(t, tt.wantAttempted, saved.LastAttemptedCommit)
			assert.Equal(t, tt.wantAttempts, saved.AttemptCount)
			assert.Equal(t, tt.wantAlerted, saved.LastAlertedAttempt)
			assert.Equal(t, tt.wantRedeploy, saved.NeedsRedeploy)
			require.NotNil(t, saved.LastAttemptOutcome)
			assert.Equal(t, attemptOutcomeInterrupted, saved.LastAttemptOutcome.Outcome)
			assert.Equal(t, tt.commit, saved.LastAttemptOutcome.Commit)
			assert.False(t, saved.LastAttemptOutcome.Timestamp.IsZero())
			assert.Equal(t, tt.wantAlerts, alerter.deployFailureCalls)

			if tt.wantAlerts == 0 {
				return
			}
			assert.Equal(t, interruptedReconcileReason, alerter.lastFailureReason)
			require.NotNil(t, alerter.lastFailureContext)
			assert.NoError(t, alerter.lastFailureContextErr)
			assert.Equal(t, "preserved", alerter.lastFailureContext.Value(valueKey))
			assert.Equal(t, "reconcile-interrupted", logpkg.ReconcileIDFromContext(alerter.lastFailureContext))
			deadline, ok := alerter.lastFailureContext.Deadline()
			require.True(t, ok)
			assert.WithinRange(t, deadline, startedAt.Add(failureAlertDeliveryTimeout), finishedAt.Add(failureAlertDeliveryTimeout))
		})
	}
}

func TestFinalizeAttempt_TerminalNonInterruptionClearsStaleOutcomeWithoutRestoringBudget(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
		err  error
	}{
		{name: "ordinary failure", ctx: context.Background(), err: errors.New("render failed")},
		{name: "deadline expiry", ctx: expiredDeadlineContext(t), err: fmt.Errorf("stage: %w", context.DeadlineExceeded)},
		{name: "real error races with cancellation", ctx: canceledContext(), err: errors.New("disk failed")},
		{name: "success", ctx: context.Background(), err: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stateFile := filepath.Join(t.TempDir(), "state.json")
			state := &DeployState{
				LastAttemptedCommit: "new", AttemptCount: 2, LastAlertedAttempt: 1,
				LastAttemptOutcome: &LastAttemptOutcome{Outcome: attemptOutcomeInterrupted, Commit: "old", Timestamp: time.Now()},
			}
			snapshot := snapshotAttemptBudget(state, "new")
			state.AttemptCount = 3
			state.LastAlertedAttempt = 3
			require.NoError(t, SaveState(stateFile, state))
			r := NewReconciler(&Config{StateFile: stateFile})
			r.finalizeAttempt(tt.ctx, tt.err, snapshot)

			saved := LoadState(stateFile)
			assert.Equal(t, 3, saved.AttemptCount)
			assert.Equal(t, 3, saved.LastAlertedAttempt)
			assert.Nil(t, saved.LastAttemptOutcome)
		})
	}
}

func TestStageAlertSuppressionLeavesFinalizerAsExclusiveOwner(t *testing.T) {
	ctx := canceledContext()
	stageErr := fmt.Errorf("stage: %w", context.Canceled)
	stateFile := filepath.Join(t.TempDir(), "state.json")
	state := &DeployState{LastAttemptedCommit: "new", AttemptCount: 2, LastAlertedAttempt: 1}
	snapshot := &attemptBudgetSnapshot{state: state, commit: "new", lastAttemptedCommit: "new", attemptCount: 1, lastAlertedAttempt: 1}
	alerter := &mockAlertSender{}
	r := NewReconciler(&Config{StateFile: stateFile, OnFailure: true}, WithAlerter(alerter))
	r.lastCommit = "new"
	r.runStartTime = time.Now()

	r.sendThrottledFailureAlert(ctx, state, "stage failed", stageErr)
	r.sendGateFailureAlerts(ctx, state, stageErr, true, nil)
	assert.Zero(t, alerter.deployFailureCalls)
	assert.Zero(t, alerter.rollbackSuccessCalls+alerter.rollbackFailureCalls)

	r.finalizeAttempt(ctx, stageErr, snapshot)
	assert.Equal(t, 1, alerter.deployFailureCalls)
	assert.Equal(t, interruptedReconcileReason, alerter.lastFailureReason)
	assert.Zero(t, alerter.rollbackSuccessCalls+alerter.rollbackFailureCalls)
}

type callbackGitOps struct {
	sync func(context.Context) (bool, string, string, error)
}

func (g callbackGitOps) Sync(ctx context.Context) (bool, string, string, error) {
	return g.sync(ctx)
}

func (callbackGitOps) IsRepo(context.Context) bool { return true }
func (callbackGitOps) DiffFiles(context.Context, string, string) ([]string, error) {
	return nil, nil
}

type callbackDecryptor struct {
	decrypt func(context.Context) (map[string]any, error)
}

func (d callbackDecryptor) DecryptFiles(ctx context.Context, _ []string) (map[string]any, error) {
	return d.decrypt(ctx)
}

func (callbackDecryptor) CheckAgeKey() error { return nil }

func TestRun_PropagatedCancellationFinalizesAtPipelineBoundary(t *testing.T) {
	tests := []struct {
		name              string
		wantNeedsRedeploy bool
		inject            func(*Reconciler, context.CancelFunc)
	}{
		{
			name: "sync",
			inject: func(r *Reconciler, cancel context.CancelFunc) {
				r.git = callbackGitOps{sync: func(context.Context) (bool, string, string, error) {
					cancel()
					return false, "", "new", context.Canceled
				}}
			},
		},
		{
			name: "decrypt",
			inject: func(r *Reconciler, cancel context.CancelFunc) {
				r.sops = callbackDecryptor{decrypt: func(context.Context) (map[string]any, error) {
					cancel()
					return nil, context.Canceled
				}}
			},
		},
		{
			name: "render",
			inject: func(r *Reconciler, cancel context.CancelFunc) {
				r.stagingOps = defaultStagingEvidenceOps()
				r.stagingOps.mkdirAll = func(context.Context, string, fs.FileMode) error {
					cancel()
					return context.Canceled
				}
			},
		},
		{
			name: "backup footprint",
			inject: func(r *Reconciler, cancel context.CancelFunc) {
				r.backupFilesFromTargetsFn = func(string, []DeployTarget, string) ([]string, error) {
					cancel()
					return nil, context.Canceled
				}
			},
		},
		{
			name:              "deploy",
			wantNeedsRedeploy: true,
			inject: func(r *Reconciler, cancel context.CancelFunc) {
				r.backupFilesFromTargetsFn = func(string, []DeployTarget, string) ([]string, error) {
					return nil, nil
				}
				r.deploy.localFS = &localDeployFS{
					mkdirAll: func(context.Context, string, os.FileMode) error {
						cancel()
						return context.Canceled
					},
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmp := t.TempDir()
			cfg := &Config{
				RepoDir: filepath.Join(tmp, "repo"), StagingDir: filepath.Join(tmp, "staging"),
				BackupDir: filepath.Join(tmp, "backups"), LocalAppdataPath: filepath.Join(tmp, "appdata"),
				LockFile: filepath.Join(tmp, "reconcile.lock"), StateFile: filepath.Join(tmp, "state.json"),
				InfraSubDir: ".", DeployMode: "local", SecretsFiles: []string{"secrets.yaml"}, OnFailure: true,
			}
			require.NoError(t, os.MkdirAll(cfg.RepoDir, 0o755))
			require.NoError(t, os.MkdirAll(cfg.LocalAppdataPath, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(cfg.RepoDir, "secrets.yaml"), []byte("placeholder: true\n"), 0o600))
			seedStubComposeService(t, cfg)
			seed := &DeployState{LastAttemptedCommit: "new", AttemptCount: 1, LastAlertedAttempt: 1}
			require.NoError(t, SaveState(cfg.StateFile, seed))
			alerter := &mockAlertSender{}
			r := NewReconciler(cfg,
				WithGitOperations(&mockGitOps{syncChanged: true, syncBefore: "old", syncAfter: "new"}),
				WithSecretsDecryptor(&mockSecretsDecryptor{decryptResult: map[string]any{}}),
				WithAlerter(alerter),
			)
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			tt.inject(r, cancel)

			err := r.Run(ctx)
			require.ErrorIs(t, err, context.Canceled)
			saved := LoadState(cfg.StateFile)
			assert.Equal(t, "new", saved.LastAttemptedCommit)
			assert.Equal(t, 1, saved.AttemptCount)
			assert.Equal(t, 1, saved.LastAlertedAttempt)
			assert.Equal(t, tt.wantNeedsRedeploy, saved.NeedsRedeploy)
			require.NotNil(t, saved.LastAttemptOutcome)
			assert.Equal(t, attemptOutcomeInterrupted, saved.LastAttemptOutcome.Outcome)
			assert.Equal(t, 1, alerter.deployFailureCalls)
			assert.Equal(t, interruptedReconcileReason, alerter.lastFailureReason)
		})
	}
}

func TestRun_CancelledCriticalHealthGateUsesOnlyFinalizerAlert(t *testing.T) {
	tmp := t.TempDir()
	stateFile := filepath.Join(tmp, "state.json")
	cfg := &Config{
		RepoDir: filepath.Join(tmp, "repo"), StagingDir: filepath.Join(tmp, "staging"),
		BackupDir: filepath.Join(tmp, "backups"), LocalAppdataPath: filepath.Join(tmp, "appdata"),
		LockFile: filepath.Join(tmp, "reconcile.lock"), StateFile: stateFile,
		InfraSubDir: ".", DeployMode: "local", OnFailure: true,
		HealthGateScope:    HealthGateScopeCritical,
		CriticalContainers: NewConfigField([]string{"critical"}),
		HealthGateTimeout:  time.Second,
	}
	require.NoError(t, os.MkdirAll(cfg.RepoDir, 0o755))
	require.NoError(t, os.MkdirAll(cfg.LocalAppdataPath, 0o755))
	seedStubComposeService(t, cfg)
	require.NoError(t, SaveState(stateFile, &DeployState{
		LastAttemptedCommit: "new", AttemptCount: 1, LastAlertedAttempt: 1,
	}))

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	mockAPI := newReconcileMockDockerAPI()
	mockAPI.containerInspectFunc = func(_ context.Context, name string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
		cancel()
		return makeInspectResponse(name, "running", &container.Health{Status: "starting"}), context.Canceled
	}
	dockerClient := docker.NewClientWithAPI(mockAPI)
	deploy := &DeployOps{
		ProjectName: "test",
		composeUpFn: func(context.Context, []string) error { return nil },
	}
	alerter := &mockAlertSender{}
	r := NewReconciler(cfg,
		WithGitOperations(&mockGitOps{syncChanged: true, syncBefore: "old", syncAfter: "new"}),
		WithDeployOps(deploy),
		WithDockerClient(dockerClient),
		WithAlerter(alerter),
	)

	err := r.Run(ctx)
	require.ErrorIs(t, err, context.Canceled)
	saved := LoadState(stateFile)
	assert.Equal(t, 1, saved.AttemptCount)
	assert.Equal(t, 1, saved.LastAlertedAttempt)
	assert.True(t, saved.NeedsRedeploy)
	require.NotNil(t, saved.LastAttemptOutcome)
	assert.Equal(t, attemptOutcomeInterrupted, saved.LastAttemptOutcome.Outcome)
	assert.Equal(t, 1, alerter.deployFailureCalls)
	assert.Equal(t, interruptedReconcileReason, alerter.lastFailureReason)
	assert.Zero(t, alerter.rollbackSuccessCalls+alerter.rollbackFailureCalls)
}

func TestRun_PostSyncHookCancellationRemainsBestEffort(t *testing.T) {
	tmp := t.TempDir()
	stateFile := filepath.Join(tmp, "state.json")
	cfg := &Config{
		RepoDir: filepath.Join(tmp, "repo"), StagingDir: filepath.Join(tmp, "staging"),
		BackupDir: filepath.Join(tmp, "backups"), LocalAppdataPath: filepath.Join(tmp, "appdata"),
		LockFile: filepath.Join(tmp, "reconcile.lock"), StateFile: stateFile,
		InfraSubDir: ".", DeployMode: "local", ContentHashSync: true, OnFailure: true,
		PostSyncHooks: NewConfigField([]PostSyncHook{{
			Paths: []string{"appdata/service/**"}, Container: "service", Action: "restart",
		}}),
	}
	require.NoError(t, os.MkdirAll(filepath.Join(cfg.RepoDir, "appdata", "service"), 0o755))
	require.NoError(t, os.MkdirAll(cfg.LocalAppdataPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cfg.RepoDir, "appdata", "service", "config.yml"), []byte("value: new\n"), 0o644))
	seedStubComposeService(t, cfg)
	require.NoError(t, SaveState(stateFile, &DeployState{
		LastDeployedCommit: "old", LastAttemptedCommit: "old",
		LastAttemptOutcome: &LastAttemptOutcome{Outcome: attemptOutcomeInterrupted, Commit: "old", Timestamp: time.Now()},
	}))

	mockAPI := newReconcileMockDockerAPI()
	mockAPI.containerRestartFunc = func(context.Context, string, client.ContainerRestartOptions) (client.ContainerRestartResult, error) {
		return client.ContainerRestartResult{}, context.Canceled
	}
	dockerClient := docker.NewClientWithAPI(mockAPI)
	deploy := &DeployOps{
		ProjectName:     "test",
		ContentHashSync: true,
		composeUpFn:     func(context.Context, []string) error { return nil },
	}
	alerter := &mockAlertSender{}
	r := NewReconciler(cfg,
		WithGitOperations(&mockGitOps{syncChanged: true, syncBefore: "old", syncAfter: "new"}),
		WithDeployOps(deploy),
		WithDockerClient(dockerClient),
		WithAlerter(alerter),
	)

	require.NoError(t, r.Run(context.Background()))
	saved := LoadState(stateFile)
	assert.Equal(t, "new", saved.LastDeployedCommit)
	assert.Zero(t, saved.AttemptCount)
	assert.Nil(t, saved.LastAttemptOutcome)
	assert.Zero(t, alerter.deployFailureCalls, "swallowed hook cancellation must not become a terminal interruption")
}

func TestRun_PanicAfterSnapshotKeepsFailureAccountingWhileClearingInterruptedOutcome(t *testing.T) {
	tmp := t.TempDir()
	cfg := &Config{
		RepoDir: filepath.Join(tmp, "repo"), LockFile: filepath.Join(tmp, "reconcile.lock"),
		StateFile: filepath.Join(tmp, "state.json"), SecretsFiles: []string{"secrets.yaml"},
	}
	require.NoError(t, os.MkdirAll(cfg.RepoDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cfg.RepoDir, "secrets.yaml"), []byte("placeholder: true\n"), 0o600))
	require.NoError(t, SaveState(cfg.StateFile, &DeployState{
		LastAttemptedCommit: "new", AttemptCount: 1,
		LastAttemptOutcome: &LastAttemptOutcome{Outcome: attemptOutcomeInterrupted, Commit: "new", Timestamp: time.Now()},
	}))
	r := NewReconciler(cfg,
		WithGitOperations(&mockGitOps{syncChanged: true, syncBefore: "old", syncAfter: "new"}),
		WithSecretsDecryptor(callbackDecryptor{decrypt: func(context.Context) (map[string]any, error) {
			panic("decrypt panic")
		}}),
	)

	err := r.Run(context.Background())
	require.ErrorContains(t, err, "panicked")
	saved := LoadState(cfg.StateFile)
	assert.Equal(t, 3, saved.AttemptCount,
		"existing post-tracking panic accounting must not be overwritten while clearing the prior outcome")
	assert.Nil(t, saved.LastAttemptOutcome)
}

func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func expiredDeadlineContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	t.Cleanup(cancel)
	return ctx
}

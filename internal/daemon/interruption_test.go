package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cameronsjo/bosun/internal/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type cycleGitOps struct {
	mu    sync.Mutex
	calls int
	sync  func(context.Context, int) (bool, string, string, error)
}

func (g *cycleGitOps) Sync(ctx context.Context) (bool, string, string, error) {
	g.mu.Lock()
	g.calls++
	call := g.calls
	g.mu.Unlock()
	return g.sync(ctx, call)
}

func (*cycleGitOps) IsRepo(context.Context) bool { return true }
func (*cycleGitOps) DiffFiles(context.Context, string, string) ([]string, error) {
	return nil, nil
}

func (g *cycleGitOps) callCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls
}

type cycleAlertSender struct {
	mu           sync.Mutex
	failures     int
	successes    int
	cancelOnGood context.CancelFunc
}

func (a *cycleAlertSender) SendDeploySuccess(context.Context, string, string, []string, time.Duration) error {
	a.mu.Lock()
	a.successes++
	cancel := a.cancelOnGood
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

func (a *cycleAlertSender) SendDeployFailure(context.Context, string, string, string, []string, time.Duration) error {
	a.mu.Lock()
	a.failures++
	a.mu.Unlock()
	return nil
}

func (*cycleAlertSender) SendDeployRecovery(context.Context, string, string, int) error { return nil }
func (*cycleAlertSender) SendUnhealthyContainers(context.Context, string, []string) error {
	return nil
}
func (*cycleAlertSender) SendRollbackSuccess(context.Context, string, string) error { return nil }
func (*cycleAlertSender) SendRollbackFailure(context.Context, string, string) error { return nil }

func (a *cycleAlertSender) counts() (failures, successes int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.failures, a.successes
}

func TestExecuteReconcile_TerminalCycleContextStopsLaterTargets(t *testing.T) {
	tests := []struct {
		name            string
		newContext      func(*testing.T) (context.Context, context.CancelFunc)
		syncResult      func(context.Context, context.CancelFunc) error
		wantInterrupted bool
	}{
		{
			name: "propagated shutdown cancellation",
			newContext: func(t *testing.T) (context.Context, context.CancelFunc) {
				return context.WithCancel(context.Background())
			},
			syncResult: func(_ context.Context, cancel context.CancelFunc) error {
				cancel()
				return context.Canceled
			},
			wantInterrupted: true,
		},
		{
			name: "real error racing with shutdown",
			newContext: func(t *testing.T) (context.Context, context.CancelFunc) {
				return context.WithCancel(context.Background())
			},
			syncResult: func(_ context.Context, cancel context.CancelFunc) error {
				cancel()
				return errors.New("disk failed")
			},
		},
		{
			name: "shared deadline remains counted failure",
			newContext: func(t *testing.T) (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), 20*time.Millisecond)
			},
			syncResult: func(ctx context.Context, _ context.CancelFunc) error {
				<-ctx.Done()
				return ctx.Err()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, stateDir := newInterruptionCycleDaemon(t)
			ctx, cancel := tt.newContext(t)
			t.Cleanup(cancel)
			git := &cycleGitOps{sync: func(ctx context.Context, _ int) (bool, string, string, error) {
				return false, "old", "new", tt.syncResult(ctx, cancel)
			}}
			alerts := &cycleAlertSender{}
			d.reconcileOpts = append(d.reconcileOpts,
				reconcile.WithGitOperations(git),
				reconcile.WithAlerter(alerts),
			)

			err := d.executeReconcile(ctx, "test", false)
			require.Error(t, err)
			assert.Equal(t, 1, git.callCount(), "later targets must not be constructed or run")

			alpha := reconcile.LoadState(filepath.Join(stateDir, "deploy-state-alpha.json"))
			betaPath := filepath.Join(stateDir, "deploy-state-beta.json")
			_, betaErr := os.Stat(betaPath)
			assert.ErrorIs(t, betaErr, os.ErrNotExist, "untouched target state must not be created")
			failures, _ := alerts.counts()
			assert.Equal(t, 1, failures, "only the active target may attempt a failure lifecycle alert")
			if tt.wantInterrupted {
				require.NotNil(t, alpha.LastAttemptOutcome)
				assert.Equal(t, "interrupted", alpha.LastAttemptOutcome.Outcome)
				assert.Zero(t, alpha.AttemptCount)
			} else {
				assert.Nil(t, alpha.LastAttemptOutcome)
				assert.Equal(t, 1, alpha.AttemptCount)
			}
		})
	}
}

func TestExecuteReconcile_CancellationBetweenTargetsInventsNoAttempt(t *testing.T) {
	d, stateDir := newInterruptionCycleDaemon(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	git := &cycleGitOps{sync: func(context.Context, int) (bool, string, string, error) {
		return true, "old", "new", nil
	}}
	alerts := &cycleAlertSender{cancelOnGood: cancel}
	d.config.ReconcileConfig.OnSuccess = true
	d.reconcileOpts = append(d.reconcileOpts,
		reconcile.WithGitOperations(git),
		reconcile.WithAlerter(alerts),
	)

	err := d.executeReconcile(ctx, "test", false)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, git.callCount())
	_, betaErr := os.Stat(filepath.Join(stateDir, "deploy-state-beta.json"))
	assert.ErrorIs(t, betaErr, os.ErrNotExist)
	failures, successes := alerts.counts()
	assert.Zero(t, failures, "between-target cancellation must invent no interruption alert")
	assert.Equal(t, 1, successes)
}

func TestExecuteReconcile_OrdinaryFailureContinuesWhileContextLive(t *testing.T) {
	d, _ := newInterruptionCycleDaemon(t)
	git := &cycleGitOps{sync: func(context.Context, int) (bool, string, string, error) {
		return false, "old", "new", errors.New("git unavailable")
	}}
	d.reconcileOpts = append(d.reconcileOpts, reconcile.WithGitOperations(git))

	err := d.executeReconcile(context.Background(), "test", false)
	require.Error(t, err)
	assert.Equal(t, 2, git.callCount(), "ordinary target failures retain continue-to-next behavior")
}

func newInterruptionCycleDaemon(t *testing.T) (*Daemon, string) {
	t.Helper()
	d := newConcurrencyDaemon(t)
	base := d.config.ReconcileConfig
	stateDir := filepath.Dir(base.StateFile)
	base.RepoDir = filepath.Join(stateDir, "repo")
	base.StagingDir = filepath.Join(stateDir, "staging")
	base.BackupDir = filepath.Join(stateDir, "backups")
	base.LocalAppdataPath = filepath.Join(stateDir, "appdata")
	base.InfraSubDir = "."
	base.DeployMode = "local"
	base.DryRun = true
	base.OnFailure = true
	base.Targets = []reconcile.Target{
		{Name: "alpha", ProjectName: "alpha", LocalAppdataPath: filepath.Join(stateDir, "appdata-alpha")},
		{Name: "beta", ProjectName: "beta", LocalAppdataPath: filepath.Join(stateDir, "appdata-beta")},
	}
	require.NoError(t, os.MkdirAll(filepath.Join(base.RepoDir, "compose"), 0o755))
	for _, target := range base.Targets {
		require.NoError(t, os.MkdirAll(target.LocalAppdataPath, 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(base.RepoDir, "compose", "stub.yml"),
		[]byte("services:\n  stub:\n    image: alpine:latest\n"), 0o644))
	return d, stateDir
}

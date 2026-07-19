package reconcile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/docker"
)

func TestResolveHealthGateScope(t *testing.T) {
	cases := map[string]string{
		"":         HealthGateScopeCritical,
		"critical": HealthGateScopeCritical,
		"declared": HealthGateScopeDeclared,
		"off":      HealthGateScopeOff,
	}
	for in, want := range cases {
		got, err := ResolveHealthGateScope(in)
		require.NoError(t, err, "scope %q", in)
		assert.Equal(t, want, got, "scope %q", in)
	}

	_, err := ResolveHealthGateScope("bogus")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "critical")
	assert.Contains(t, err.Error(), "declared")
	assert.Contains(t, err.Error(), "off")
}

// gateScopeReconciler builds a Run() harness that reaches the health gate. The
// deploy step runs for real (ContentHashSync) but compose up is stubbed via
// composeUpFn, so r.lastComposeFiles and r.lastBackupPath are populated (the
// gate's rollback preconditions) without a real docker binary. verifyPostDeploy
// is disabled (HealthCheckTimeout=0) so only the GATE can fail — isolating scope
// behavior. containerListFn controls what the pre-deploy #392 snapshot and the
// declared-scope poll see.
func gateScopeReconciler(
	t *testing.T,
	scope string,
	criticalContainers []string,
	containerListFn func(ctx context.Context, opts client.ContainerListOptions) (client.ContainerListResult, error),
) (*Reconciler, *mockAlertSender, string, *bool) {
	t.Helper()
	tmp := t.TempDir()
	stateFile := filepath.Join(tmp, "state.json")
	repoDir := filepath.Join(tmp, "repo")
	appdata := filepath.Join(tmp, "appdata")
	require.NoError(t, os.MkdirAll(repoDir, 0o755))
	require.NoError(t, os.MkdirAll(appdata, 0o755))
	require.NoError(t, SaveState(stateFile, &DeployState{SchemaVersion: 2, LastDeployedCommit: "prevcommit"}))

	restartCalled := false
	mockAPI := newReconcileMockDockerAPI()
	mockAPI.containerListFunc = containerListFn
	mockAPI.containerRestartFunc = func(_ context.Context, _ string, _ client.ContainerRestartOptions) (client.ContainerRestartResult, error) {
		restartCalled = true
		return client.ContainerRestartResult{}, nil
	}
	dockerClient := docker.NewClientWithAPI(mockAPI)

	deploy := &DeployOps{
		DryRun:          false,
		ProjectName:     "test",
		ContentHashSync: true,
		composeUpFn:     func(_ context.Context, _ []string) error { return nil },
	}
	alerter := &mockAlertSender{}
	gitOps := &mockGitOps{syncChanged: true, syncBefore: "prevcommit", syncAfter: "newcommit"}

	cfg := &Config{
		DryRun:              false,
		LockFile:            filepath.Join(tmp, "reconcile.lock"),
		StateFile:           stateFile,
		RepoDir:             repoDir,
		StagingDir:          filepath.Join(tmp, "staging"),
		LocalAppdataPath:    appdata,
		InfraSubDir:         ".",
		HealthGateScope:     scope,
		HealthGateTimeout:   50 * time.Millisecond,
		HealthCheckInterval: 20 * time.Millisecond,
		HealthCheckTimeout:  0, // disable verifyPostDeploy: isolate the gate
		OnFailure:           true,
		OnSuccess:           true,
		PostSyncHooks: NewConfigField([]PostSyncHook{
			{Container: "downstream", Paths: []string{"**"}, Action: "restart"},
		}),
	}
	if criticalContainers != nil {
		cfg.CriticalContainers = NewConfigField(criticalContainers)
	}
	seedStubComposeService(t, cfg)

	r := NewReconciler(cfg, WithGitOperations(gitOps), WithDeployOps(deploy), WithDockerClient(dockerClient), WithAlerter(alerter))
	return r, alerter, stateFile, &restartCalled
}

// dockerListErr fails every ContainerList — the pre-deploy #392 snapshot cannot
// record a baseline (so nothing is exempt) and the declared poll times out with
// an error: a deterministic gate failure this deploy "caused".
func dockerListErr(_ context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
	return client.ContainerListResult{}, fmt.Errorf("docker unavailable")
}

// dockerListEmpty reports zero containers — the declared "stub" service is
// missing both before and after the deploy, so #392 marks it a pre-existing
// casualty (exempt from rollback).
func dockerListEmpty(_ context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
	return client.ContainerListResult{}, nil
}

func TestRunHealthGate_DeclaredScope_RollsBackAndThrottlesAlerts(t *testing.T) {
	r, alerter, stateFile, restartCalled := gateScopeReconciler(t, HealthGateScopeDeclared, nil, dockerListErr)

	// Cycle 1: the declared-scope gate fails, rollback runs, hooks are skipped,
	// and the commit does not advance.
	err := r.Run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "health gate failed")
	assert.False(t, *restartCalled, "post-sync hooks must be skipped when a rollback ran")
	saved := LoadState(stateFile)
	assert.True(t, saved.NeedsRedeploy, "failed gate leaves the deploy incomplete for retry")
	assert.Equal(t, "prevcommit", saved.LastDeployedCommit, "commit must not advance past a failed gate")

	// Cycles 2-4: keep failing until the breaker trips at MaxAttempts.
	for i := 0; i < 3; i++ {
		_ = r.Run(context.Background())
	}

	// Failure and rollback alerts fire TOGETHER on the 1/3/10/30 ShouldAlert
	// window — attempt 1 and attempt 3 — not once per cycle (#339). The rollback
	// outcome (restore vs failed restore) varies with the docker binary, so the
	// invariant is the paired count on the alert windows.
	assert.Equal(t, 2, alerter.deployFailureCalls, "gate failure alert throttled to attempts 1 and 3")
	assert.Equal(t, 2, alerter.rollbackSuccessCalls+alerter.rollbackFailureCalls,
		"a rollback alert fires on the SAME window as each failure alert")
	assert.Equal(t, 0, alerter.deploySuccessCalls)
}

func TestRunHealthGate_DeclaredScope_AllHealthySucceeds(t *testing.T) {
	// Every declared service is healthy → the declared gate passes and the deploy
	// records success.
	r, alerter, stateFile, restartCalled := gateScopeReconciler(t, HealthGateScopeDeclared, nil, dockerListHealthyStub)

	require.NoError(t, r.Run(context.Background()))
	saved := LoadState(stateFile)
	assert.Equal(t, "newcommit", saved.LastDeployedCommit)
	assert.False(t, saved.NeedsRedeploy)
	assert.True(t, *restartCalled, "hooks run on a clean deploy")
	assert.Equal(t, 0, alerter.deployFailureCalls)
	assert.Equal(t, 0, alerter.rollbackSuccessCalls+alerter.rollbackFailureCalls)
}

func TestRunHealthGate_DeclaredScope_PreExistingUnhealthyExempt(t *testing.T) {
	// The stub service is missing both before and after the deploy, so #392
	// treats it as a pre-existing casualty: the declared gate does NOT roll back.
	r, alerter, stateFile, restartCalled := gateScopeReconciler(t, HealthGateScopeDeclared, nil, dockerListEmpty)

	require.NoError(t, r.Run(context.Background()), "a pre-existing-unhealthy declared service must not fail the gate")
	saved := LoadState(stateFile)
	assert.Equal(t, "newcommit", saved.LastDeployedCommit, "deploy records success")
	assert.False(t, saved.NeedsRedeploy)
	assert.True(t, *restartCalled, "hooks run when no rollback occurred")
	assert.Equal(t, 0, alerter.deployFailureCalls)
	assert.Equal(t, 0, alerter.rollbackSuccessCalls+alerter.rollbackFailureCalls, "no rollback, no rollback alert")
}

func TestRunHealthGate_CriticalScope_IgnoresDeclaredUnhealthy(t *testing.T) {
	// Regression pin: with the default "critical" scope and NO critical
	// containers configured, an unhealthy declared-but-non-critical service must
	// NOT trigger rollback — the default path is byte-for-byte unchanged.
	r, alerter, stateFile, _ := gateScopeReconciler(t, HealthGateScopeCritical, nil, dockerListErr)

	require.NoError(t, r.Run(context.Background()), "critical scope ignores declared services")
	saved := LoadState(stateFile)
	assert.Equal(t, "newcommit", saved.LastDeployedCommit)
	assert.False(t, saved.NeedsRedeploy)
	assert.Equal(t, 0, alerter.rollbackSuccessCalls+alerter.rollbackFailureCalls, "critical scope did not roll back")
}

func TestRunHealthGate_SkipGuards(t *testing.T) {
	ctx := context.Background()

	t.Run("off scope skips before touching docker", func(t *testing.T) {
		r := NewReconciler(&Config{HealthGateScope: HealthGateScopeOff, CriticalContainers: NewConfigField([]string{"x"})})
		rb, err := r.runHealthGate(ctx, &DeployState{}, true)
		assert.False(t, rb)
		assert.NoError(t, err)
	})

	t.Run("remote deploy skips (docker API is local-only)", func(t *testing.T) {
		r := NewReconciler(&Config{HealthGateScope: HealthGateScopeCritical, CriticalContainers: NewConfigField([]string{"x"})})
		rb, err := r.runHealthGate(ctx, &DeployState{}, false)
		assert.False(t, rb)
		assert.NoError(t, err)
	})

	t.Run("no docker client skips", func(t *testing.T) {
		r := NewReconciler(&Config{HealthGateScope: HealthGateScopeCritical, CriticalContainers: NewConfigField([]string{"x"})})
		rb, err := r.runHealthGate(ctx, &DeployState{}, true)
		assert.False(t, rb)
		assert.NoError(t, err)
	})

	t.Run("invalid scope falls back to critical (no-op with no critical containers)", func(t *testing.T) {
		r := NewReconciler(&Config{HealthGateScope: "bogus"})
		rb, err := r.runHealthGate(ctx, &DeployState{}, true)
		assert.False(t, rb)
		assert.NoError(t, err)
	})

	t.Run("declared scope with no declared services is a no-op", func(t *testing.T) {
		r := NewReconciler(&Config{HealthGateScope: HealthGateScopeDeclared})
		rb, err := r.runHealthGate(ctx, &DeployState{}, true)
		assert.False(t, rb)
		assert.NoError(t, err)
	})
}

func TestSendGateFailureAlerts(t *testing.T) {
	ctx := context.Background()
	gateErr := fmt.Errorf("gate boom")

	t.Run("no alerter is a no-op", func(t *testing.T) {
		r := NewReconciler(&Config{OnFailure: true})
		r.sendGateFailureAlerts(ctx, &DeployState{AttemptCount: 1}, gateErr, true, nil)
	})

	t.Run("OnFailure=false suppresses", func(t *testing.T) {
		a := &mockAlertSender{}
		r := NewReconciler(&Config{OnFailure: false}, WithAlerter(a))
		r.sendGateFailureAlerts(ctx, &DeployState{AttemptCount: 1}, gateErr, true, nil)
		assert.Equal(t, 0, a.deployFailureCalls)
	})

	t.Run("off-window (throttled) sends nothing", func(t *testing.T) {
		a := &mockAlertSender{}
		r := NewReconciler(&Config{OnFailure: true, StateFile: filepath.Join(t.TempDir(), "s.json")}, WithAlerter(a))
		// AttemptCount 2 is below the next threshold (3) and already past LastAlertedAttempt 1.
		r.sendGateFailureAlerts(ctx, &DeployState{AttemptCount: 2, LastAlertedAttempt: 1}, gateErr, true, nil)
		assert.Equal(t, 0, a.deployFailureCalls)
		assert.Equal(t, 0, a.rollbackSuccessCalls+a.rollbackFailureCalls)
	})

	t.Run("on-window with successful rollback emits failure + rollback-success", func(t *testing.T) {
		a := &mockAlertSender{}
		r := NewReconciler(&Config{OnFailure: true, StateFile: filepath.Join(t.TempDir(), "s.json")}, WithAlerter(a))
		r.lastBackupPath = "/backups/backup-20260101-000000"
		st := &DeployState{AttemptCount: 1}
		r.sendGateFailureAlerts(ctx, st, gateErr, true, nil)
		assert.Equal(t, 1, a.deployFailureCalls)
		assert.Equal(t, 1, a.rollbackSuccessCalls)
		assert.Equal(t, 0, a.rollbackFailureCalls)
		assert.Equal(t, 1, st.LastAlertedAttempt, "throttle window is consumed")
	})

	t.Run("on-window with failed rollback emits failure + rollback-failure", func(t *testing.T) {
		a := &mockAlertSender{}
		r := NewReconciler(&Config{OnFailure: true, StateFile: filepath.Join(t.TempDir(), "s.json")}, WithAlerter(a))
		st := &DeployState{AttemptCount: 1}
		r.sendGateFailureAlerts(ctx, st, gateErr, true, fmt.Errorf("restore boom"))
		assert.Equal(t, 1, a.deployFailureCalls)
		assert.Equal(t, 1, a.rollbackFailureCalls)
	})

	t.Run("no rollback emits only the failure alert", func(t *testing.T) {
		a := &mockAlertSender{}
		r := NewReconciler(&Config{OnFailure: true, StateFile: filepath.Join(t.TempDir(), "s.json")}, WithAlerter(a))
		r.sendGateFailureAlerts(ctx, &DeployState{AttemptCount: 1}, gateErr, false, nil)
		assert.Equal(t, 1, a.deployFailureCalls)
		assert.Equal(t, 0, a.rollbackSuccessCalls+a.rollbackFailureCalls)
	})
}

func TestRunHealthGate_OffScope_NoGate(t *testing.T) {
	// off: no poll, no gate — the deploy records success even though the declared
	// service is unhealthy (would fail under declared scope).
	r, alerter, stateFile, _ := gateScopeReconciler(t, HealthGateScopeOff, nil, dockerListErr)

	require.NoError(t, r.Run(context.Background()), "off scope runs no health gate")
	saved := LoadState(stateFile)
	assert.Equal(t, "newcommit", saved.LastDeployedCommit)
	assert.False(t, saved.NeedsRedeploy)
	assert.Equal(t, 0, alerter.deployFailureCalls)
	assert.Equal(t, 0, alerter.rollbackSuccessCalls+alerter.rollbackFailureCalls)
}

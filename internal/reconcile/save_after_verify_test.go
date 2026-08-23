package reconcile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/docker"
)

// buildVerifyGateReconciler wires a full Run() harness that reaches
// verifyPostDeploy: a seeded stub compose service (so declaredServices is
// non-empty), a dry-run DeployOps (so no real docker/compose is needed for the
// deploy itself), and an injected docker client whose ContainerList behavior the
// caller controls. deployMode is "local" or "remote".
func buildVerifyGateReconciler(
	t *testing.T,
	listFn func(ctx context.Context, opts client.ContainerListOptions) (client.ContainerListResult, error),
	deployMode string,
) (*Reconciler, *Config, *mockAlertSender, string) {
	t.Helper()
	tmp := t.TempDir()
	repoDir := filepath.Join(tmp, "repo")
	appdata := filepath.Join(tmp, "appdata")
	stateFile := filepath.Join(tmp, "state.json")
	require.NoError(t, os.MkdirAll(appdata, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(repoDir, "unraid"), 0o755))

	// Remote backup (BackupRemote) tars real paths through the ssh shim and, unlike
	// local Backup, does not filter absent paths — so give RemoteAppdataPath a real,
	// populated compose dir for the pre-deploy backup to archive.
	remoteAppdata := filepath.Join(tmp, "remote-appdata")
	require.NoError(t, os.MkdirAll(filepath.Join(remoteAppdata, "compose"), 0o755))
	// Mirror the staged file name (seedStubComposeService writes compose/stub.yml)
	// so the pre-deploy backup's per-file path resolves to a real file.
	require.NoError(t, os.WriteFile(filepath.Join(remoteAppdata, "compose", "stub.yml"), []byte("services: {}\n"), 0o644))

	cfg := &Config{
		DeployMode:          deployMode,
		ProjectName:         "test",
		LockFile:            filepath.Join(tmp, "reconcile.lock"),
		StateFile:           stateFile,
		RepoDir:             repoDir,
		StagingDir:          filepath.Join(tmp, "staging"),
		BackupDir:           filepath.Join(tmp, "backups"),
		LocalAppdataPath:    appdata,
		RemoteAppdataPath:   remoteAppdata,
		TargetHost:          "user@testhost",
		InfraSubDir:         "unraid",
		SkipDeployInvariant: true,
		HealthCheckTimeout:  200 * time.Millisecond,
		HealthCheckInterval: 40 * time.Millisecond,
		OnFailure:           true,
		OnSuccess:           true,
	}
	seedStubComposeService(t, cfg)

	mockAPI := newReconcileMockDockerAPI()
	mockAPI.containerListFunc = listFn
	dc := docker.NewClientWithAPI(mockAPI)
	alerter := &mockAlertSender{}
	gitOps := &mockGitOps{syncChanged: true, syncBefore: "old-commit", syncAfter: "new-commit"}

	r := NewReconciler(cfg,
		WithGitOperations(gitOps),
		WithSecretsDecryptor(&mockSOPS{}),
		WithDeployOps(&DeployOps{DryRun: true}),
		WithDockerClient(dc),
		WithAlerter(alerter),
	)
	return r, cfg, alerter, stateFile
}

// dockerListError always fails, so the pre-deploy health snapshot cannot
// populate preDeployUnhealthy (disabling the #392 exemption) and the post-deploy
// poll times out with an error — a deterministic verify failure every cycle.
func dockerListError(_ context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
	return client.ContainerListResult{}, fmt.Errorf("docker unavailable")
}

// dockerListHealthyStub reports the seeded "stub" service as a running container
// so verifyPostDeploy passes.
func dockerListHealthyStub(_ context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
	return client.ContainerListResult{Items: []container.Summary{
		{
			ID:    "abcdef123456abcdef",
			Names: []string{"/test-stub-1"},
			Image: "alpine:latest",
			State: "running",
			Labels: map[string]string{
				"com.docker.compose.project": "test",
				"com.docker.compose.service": "stub",
			},
		},
	}}, nil
}

func TestRun_LocalVerifyFailure_BreakerAndAlert(t *testing.T) {
	r, cfg, alerter, stateFile := buildVerifyGateReconciler(t, dockerListError, "local")

	var sawVerifyFailure, sawBreaker bool
	for cycle := 1; cycle <= 4; cycle++ {
		err := r.Run(context.Background())
		require.Error(t, err, "cycle %d must fail: verification cannot pass", cycle)
		switch {
		case strings.Contains(err.Error(), "health verification failed"):
			sawVerifyFailure = true
		case strings.Contains(err.Error(), "circuit breaker"):
			sawBreaker = true
		}

		state := LoadState(stateFile)
		assert.NotEqual(t, "new-commit", state.LastDeployedCommit,
			"a failed verification must never record the commit as deployed (cycle %d)", cycle)
		assert.Equal(t, 0, state.DeployCount, "no successful deploy may be counted (cycle %d)", cycle)
		assert.True(t, state.NeedsRedeploy, "NeedsRedeploy must stay set so the next cycle retries (cycle %d)", cycle)
		if cycle == 1 {
			assertPrivateStagingTree(t, cfg.StagingDir)
		}
	}

	assert.True(t, sawVerifyFailure, "early cycles must fail with a health-verification error")
	assert.True(t, sawBreaker, "the circuit breaker must trip after MaxAttempts consecutive verify failures")
	// Exact alert count pins the #336 fix: alert timing, not just breaker state.
	// The failure-alert schedule is thresholds {1,3,10,30}, plus a MaxAttempts
	// activation rule. Across the four cycles:
	//   cycle 1 (attempt 1) → alerts (threshold 1). This is the fix — the old
	//     ordering reset AttemptCount to 0 before the alert, so ShouldAlert(0,…)
	//     suppressed it and this cycle-1 alert never fired.
	//   cycle 2 (attempt 2) → suppressed (below the next threshold, 3).
	//   cycle 3 (attempt 3) → alerts (attempt == MaxAttempts activation).
	//   cycle 4 → breaker trips; sendThrottledFailureAlert sees ShouldAlert(3,3)
	//     == false (attempt 3 already alerted), so it re-alerts nothing.
	// Exactly two failure alerts. A regression to the old save-before-verify
	// ordering fires zero (AttemptCount reset masks every alert AND the deploy is
	// skipped as already-deployed next cycle); an alert mis-timed to only the
	// breaker trip fires one. Equality catches both.
	assert.Equal(t, 2, alerter.deployFailureCalls, "failure alerts must fire on attempt 1 and attempt 3 (== MaxAttempts)")
	assert.Equal(t, 0, alerter.deploySuccessCalls, "no success alert may fire while verification keeps failing")

	// Final on-disk state is a clean failure state, not a masked success.
	final := LoadState(stateFile)
	assert.Equal(t, MaxAttempts, final.AttemptCount, "the breaker counter reflects MaxAttempts consecutive failures")
}

func TestRun_LocalVerifySuccess_RecordsSuccess(t *testing.T) {
	r, _, alerter, stateFile := buildVerifyGateReconciler(t, dockerListHealthyStub, "local")

	require.NoError(t, r.Run(context.Background()))

	state := LoadState(stateFile)
	assert.Equal(t, "new-commit", state.LastDeployedCommit, "a passing verification records the deployed commit")
	assert.False(t, state.NeedsRedeploy, "success clears the redeploy marker")
	assert.Equal(t, 1, state.DeployCount, "exactly one successful deploy is counted")
	assert.Equal(t, 0, state.AttemptCount, "success resets the breaker counter")
	assert.True(t, state.HealthVerificationPassed, "the health verdict is recorded as passed")
	assert.Equal(t, 1, alerter.deploySuccessCalls, "a success alert fires once")
	assert.Equal(t, 0, alerter.deployFailureCalls, "no failure alert on the healthy path")
}

func TestRun_RemoteSkipsVerify_RecordsSuccess(t *testing.T) {
	// A hermetic ssh shim so the once-per-deploy sha256sum probe runs locally.
	setupSSHShim(t)

	// The docker mock ERRORS: if verification ran, the deploy would fail. A
	// remote deploy must skip verification (it polls the local daemon, which
	// cannot see remote containers) and still record success.
	r, _, alerter, stateFile := buildVerifyGateReconciler(t, dockerListError, "remote")

	require.NoError(t, r.Run(context.Background()), "remote deploy must not run local health verification")

	state := LoadState(stateFile)
	assert.Equal(t, "new-commit", state.LastDeployedCommit, "remote deploy records success unchanged")
	assert.False(t, state.NeedsRedeploy)
	assert.Equal(t, 1, state.DeployCount)
	assert.Equal(t, 1, alerter.deploySuccessCalls)
	assert.Equal(t, 0, alerter.deployFailureCalls)
}

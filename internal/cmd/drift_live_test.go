package cmd

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/docker/dockertest"
	"github.com/cameronsjo/bosun/internal/reconcile"
	"github.com/cameronsjo/bosun/internal/ui"
)

// liveSessionHarness builds a liveDriftSession over a mock Docker API and counts
// how often the session connects and loads ignore rules.
type liveSessionHarness struct {
	session     *liveDriftSession
	mock        *dockertest.MockDockerAPI
	connects    int
	ruleLoads   int
	connectErr  error
	ignoreRules []reconcile.DriftIgnoreRule
}

func newLiveSessionHarness() *liveSessionHarness {
	h := &liveSessionHarness{mock: dockertest.NewMockDockerAPI()}
	h.session = &liveDriftSession{
		newClient: func() (*docker.Client, error) {
			h.connects++
			if h.connectErr != nil {
				return nil, h.connectErr
			}
			return docker.NewClientWithAPI(h.mock), nil
		},
		loadRules: func() []reconcile.DriftIgnoreRule {
			h.ruleLoads++
			return h.ignoreRules
		},
	}
	return h
}

func liveDriftState() *reconcile.DeployState {
	return &reconcile.DeployState{
		LastDeployedCommit: "abc123",
		DeclaredServices:   []reconcile.DeclaredService{{Name: "web", Image: "nginx"}},
	}
}

func TestLiveDriftSession_SharesClientAndRulesAcrossTargets(t *testing.T) {
	h := newLiveSessionHarness()
	dir := t.TempDir()

	for _, target := range []string{"a", "b", "c"} {
		state, err := h.session.collect(liveDriftState(), filepath.Join(dir, target+".json"), "proj-"+target)
		require.NoError(t, err)
		require.Len(t, state.DriftItems, 1, "no containers are running, so web is missing")
		assert.Equal(t, reconcile.DriftMissing, state.DriftItems[0].Type)
	}

	assert.Equal(t, 1, h.connects, "one Docker connection for every target")
	assert.Equal(t, 1, h.ruleLoads, "ignore rules are read once")
	assert.Equal(t, 3, h.mock.ContainerListCalls, "each target still lists its own containers")
	assert.Equal(t, 0, h.mock.CloseCalls, "the client stays open until the session closes")

	h.session.close()
	assert.Equal(t, 1, h.mock.CloseCalls)
	h.session.close()
	assert.Equal(t, 1, h.mock.CloseCalls, "closing twice closes the client once")
}

func TestLiveDriftSession_AppliesIgnoreRules(t *testing.T) {
	tests := []struct {
		name      string
		rules     []reconcile.DriftIgnoreRule
		wantItems int
	}{
		{name: "no rules keeps the drift", rules: nil, wantItems: 1},
		{
			name:      "matching rule drops the drift",
			rules:     []reconcile.DriftIgnoreRule{{Service: "web", Type: "missing"}},
			wantItems: 0,
		},
		{
			name:      "non-matching rule keeps the drift",
			rules:     []reconcile.DriftIgnoreRule{{Service: "other", Type: "*"}},
			wantItems: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newLiveSessionHarness()
			h.ignoreRules = tt.rules
			stateFile := filepath.Join(t.TempDir(), "state.json")

			state, err := h.session.collect(liveDriftState(), stateFile, "")
			require.NoError(t, err)

			assert.Len(t, state.DriftItems, tt.wantItems)
			saved := reconcile.LoadState(stateFile)
			assert.Len(t, saved.DriftItems, tt.wantItems, "the filtered result is what gets saved")
			assert.False(t, saved.DriftCheckedAt.IsZero())
		})
	}
}

func TestLiveDriftSession_CollectErrors(t *testing.T) {
	t.Run("connect failure names the connection and saves nothing", func(t *testing.T) {
		h := newLiveSessionHarness()
		h.connectErr = errors.New("no daemon")
		stateFile := filepath.Join(t.TempDir(), "state.json")

		state, err := h.session.collect(liveDriftState(), stateFile, "")

		require.Error(t, err)
		assert.Nil(t, state)
		assert.ErrorContains(t, err, "connect to Docker")
		assert.ErrorIs(t, err, h.connectErr)
		assert.NoFileExists(t, stateFile)
		assert.Equal(t, 0, h.ruleLoads, "rules are not read when Docker is unreachable")
	})

	t.Run("a failed connect is retried by the next target", func(t *testing.T) {
		h := newLiveSessionHarness()
		h.connectErr = errors.New("no daemon")
		_, err := h.session.collect(liveDriftState(), filepath.Join(t.TempDir(), "a.json"), "")
		require.Error(t, err)

		h.connectErr = nil
		_, err = h.session.collect(liveDriftState(), filepath.Join(t.TempDir(), "b.json"), "")
		require.NoError(t, err)
		assert.Equal(t, 2, h.connects)
	})

	t.Run("container list failure is reported as a collect failure", func(t *testing.T) {
		h := newLiveSessionHarness()
		h.mock.ContainerListFunc = func(context.Context, client.ContainerListOptions) (client.ContainerListResult, error) {
			return client.ContainerListResult{}, errors.New("mock: container list failed")
		}
		stateFile := filepath.Join(t.TempDir(), "state.json")

		state, err := h.session.collect(liveDriftState(), stateFile, "")

		require.Error(t, err)
		assert.Nil(t, state)
		assert.ErrorContains(t, err, "collect actual state")
		assert.NoFileExists(t, stateFile)
	})
}

func TestLiveDriftSession_MustCollectEndsRunOnFailure(t *testing.T) {
	exitCalls := 0
	old := ui.SetExitFn(func(int) { exitCalls++ })
	t.Cleanup(func() { ui.SetExitFn(old) })

	h := newLiveSessionHarness()
	h.connectErr = errors.New("no daemon")
	original := liveDriftState()

	got := h.session.mustCollect(original, filepath.Join(t.TempDir(), "state.json"), "")

	assert.Equal(t, 1, exitCalls)
	assert.Same(t, original, got, "with the exit intercepted the caller keeps its state")
}

func TestLiveDriftSession_CloseWithoutUseIsNoop(t *testing.T) {
	h := newLiveSessionHarness()
	h.session.close()
	assert.Equal(t, 0, h.connects)
	assert.Equal(t, 0, h.ruleLoads)
	assert.Equal(t, 0, h.mock.CloseCalls)
}

// TestRunMultiTargetDrift_LiveWithoutEligibleTargetNeverConnects pins that a
// --live run where no target has a deployment (or declared services) still
// ends with the unavailable error, as before, and never touches Docker. The
// Docker host is unreachable, so any connect attempt would end the run.
func TestRunMultiTargetDrift_LiveWithoutEligibleTargetNeverConnects(t *testing.T) {
	targets := []reconcile.Target{{Name: "alpha"}, {Name: "beta"}}

	setup := func(t *testing.T) (exitCalls *int) {
		t.Helper()
		saveDriftFlags(t)
		t.Setenv("DOCKER_HOST", "unix://"+filepath.Join(t.TempDir(), "absent.sock"))
		tmpDir := evalSymlinks(t, t.TempDir())
		driftStateFile = filepath.Join(tmpDir, reconcile.DefaultStateFile)
		driftLive = true
		// alpha has no state file; beta has a deployment but no declared services.
		makeState(t, tmpDir, targets[1], &reconcile.DeployState{LastDeployedCommit: "abc123"})

		calls := 0
		old := ui.SetExitFn(func(int) { calls++ })
		t.Cleanup(func() { ui.SetExitFn(old) })
		return &calls
	}

	t.Run("human output", func(t *testing.T) {
		exitCalls := setup(t)
		driftJSON = false

		var runErr error
		captureStdout(t, func() { runErr = runMultiTargetDrift(targets) })

		require.ErrorIs(t, runErr, errDriftStateUnavailable)
		assert.Zero(t, *exitCalls, "no Docker connection was attempted")
	})

	t.Run("json output", func(t *testing.T) {
		exitCalls := setup(t)
		driftJSON = true

		var runErr error
		captureStdout(t, func() { runErr = runMultiTargetDriftJSON(targets) })

		require.ErrorIs(t, runErr, errDriftStateUnavailable)
		assert.Zero(t, *exitCalls, "no Docker connection was attempted")
	})
}

package daemon

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/docker/dockertest"
	"github.com/cameronsjo/bosun/internal/reconcile"
)

// Tests for #793: drift admission is a flag under reconcileMu, never the
// mutex held across drift-check I/O.

const driftAdmissionWait = 2 * time.Second

var errDriftOrderStop = errors.New("drift order probe stop")

// driftOrderGitOps is a reconcile.GitOperations fake that records what the
// daemon looked like when a reconcile reached git sync, then stops the
// reconcile with errDriftOrderStop.
type driftOrderGitOps struct {
	d         *Daemon
	stateFile string
	listCalls *atomic.Int32 // optional: drift Docker list calls seen so far

	once   sync.Once
	synced chan struct{}

	mu             sync.Mutex
	driftCheckedAt time.Time
	drifting       bool
	selfHeal       *reconcile.DriftSelfHealTracking
	listCount      int32
}

func newDriftOrderGitOps(d *Daemon) *driftOrderGitOps {
	return &driftOrderGitOps{
		d:         d,
		stateFile: d.config.ReconcileConfig.StateFile,
		synced:    make(chan struct{}),
	}
}

func (g *driftOrderGitOps) Sync(context.Context) (bool, string, string, error) {
	g.once.Do(func() {
		state := reconcile.LoadState(g.stateFile)
		g.d.reconcileMu.Lock()
		drifting := g.d.drifting
		g.d.reconcileMu.Unlock()

		g.mu.Lock()
		g.driftCheckedAt = state.DriftCheckedAt
		g.drifting = drifting
		g.selfHeal = state.DriftSelfHeal
		if g.listCalls != nil {
			g.listCount = g.listCalls.Load()
		}
		g.mu.Unlock()
		close(g.synced)
	})
	return false, "", "", errDriftOrderStop
}

func (*driftOrderGitOps) IsRepo(context.Context) bool { return true }

func (*driftOrderGitOps) DiffFiles(context.Context, string, string) ([]string, error) {
	return nil, nil
}

func (g *driftOrderGitOps) waitSynced(t *testing.T) {
	t.Helper()
	select {
	case <-g.synced:
	case <-time.After(driftAdmissionWait):
		t.Fatal("reconcile never reached git sync")
	}
}

// blockingFirstList blocks the first Docker container list until release is
// closed; later calls return an empty list at once. calls counts every call
// without touching the mock's unsynchronized counters.
type blockingFirstList struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	calls   atomic.Int32
}

func newBlockingFirstList() *blockingFirstList {
	return &blockingFirstList{entered: make(chan struct{}), release: make(chan struct{})}
}

func (b *blockingFirstList) list(ctx context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
	b.calls.Add(1)
	first := false
	b.once.Do(func() { first = true })
	if first {
		close(b.entered)
		select {
		case <-b.release:
		case <-ctx.Done():
			return client.ContainerListResult{}, ctx.Err()
		}
	}
	return client.ContainerListResult{Items: []container.Summary{}}, nil
}

func (b *blockingFirstList) waitEntered(t *testing.T) {
	t.Helper()
	select {
	case <-b.entered:
	case <-time.After(driftAdmissionWait):
		t.Fatal("drift check never reached Docker")
	}
}

func seedDeclaredAPI(t *testing.T, d *Daemon) {
	t.Helper()
	require.NoError(t, reconcile.SaveState(d.config.ReconcileConfig.StateFile, &reconcile.DeployState{
		LastDeployedCommit: "abc123",
		DeclaredServices: []reconcile.DeclaredService{
			{Name: "api", Image: "api:latest"},
		},
	}))
}

// startBlockedDrift runs one drift check in the background and returns once
// it is inside its first Docker list call. The returned func releases Docker
// and waits for the check to finish.
func startBlockedDrift(t *testing.T, d *Daemon, blocker *blockingFirstList) func() {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		d.runDriftCheck(context.Background())
	}()
	blocker.waitEntered(t)
	var releaseOnce sync.Once
	finish := func() {
		releaseOnce.Do(func() { close(blocker.release) })
		select {
		case <-done:
		case <-time.After(driftAdmissionWait):
			t.Fatal("drift check did not finish after Docker was released")
		}
	}
	t.Cleanup(finish)
	return finish
}

func newBlockedDriftDaemon(t *testing.T) (*Daemon, *blockingFirstList) {
	t.Helper()
	blocker := newBlockingFirstList()
	mock := dockertest.NewMockDockerAPI()
	mock.ContainerListFunc = blocker.list
	d := newDockerDaemon(t, mock)
	d.config.ReconcileConfig.RestartBreakerEnabled = false
	seedDeclaredAPI(t, d)
	return d, blocker
}

// waiterCount reads reconcileWaiters without blocking if reconcileMu is held.
func waiterCount(d *Daemon) (int, bool) {
	if !d.reconcileMu.TryLock() {
		return 0, false
	}
	defer d.reconcileMu.Unlock()
	return d.reconcileWaiters, true
}

func TestDaemonStatusHandlers_RespondDuringDriftIO(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		handler func(d *Daemon) http.HandlerFunc
	}{
		{"socket", "/status", func(d *Daemon) http.HandlerFunc { return (&SocketServer{daemon: d}).handleStatus }},
		{"tcp", "/status", func(d *Daemon) http.HandlerFunc { return (&TCPServer{daemon: d}).handleStatus }},
		{"api", "/api/status", func(d *Daemon) http.HandlerFunc { return d.handleAPIStatus }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, blocker := newBlockedDriftDaemon(t)
			finish := startBlockedDrift(t, d, blocker)

			handler := tt.handler(d)
			w := httptest.NewRecorder()
			served := make(chan struct{})
			go func() {
				defer close(served)
				handler(w, httptest.NewRequest(http.MethodGet, tt.target, nil))
			}()

			select {
			case <-served:
			case <-time.After(driftAdmissionWait):
				finish()
				<-served
				t.Fatalf("%s status handler blocked on drift-check I/O", tt.name)
			}
			assert.Equal(t, http.StatusOK, w.Code)
			finish()
		})
	}
}

func TestTriggerReconcile_WaitsForDriftCycleThenRuns(t *testing.T) {
	d, blocker := newBlockedDriftDaemon(t)
	git := newDriftOrderGitOps(d)
	d.reconcileOpts = append(d.reconcileOpts, reconcile.WithGitOperations(git))
	finish := startBlockedDrift(t, d, blocker)

	triggerErr := make(chan error, 1)
	go func() { triggerErr <- d.TriggerReconcile(context.Background(), "test", false) }()

	require.Eventually(t, func() bool {
		n, ok := waiterCount(d)
		return ok && n == 1
	}, driftAdmissionWait, 5*time.Millisecond, "trigger must wait as a counted waiter without reconcileMu held")
	select {
	case <-git.synced:
		t.Fatal("reconcile started while the drift cycle owned the state file")
	default:
	}

	finish()
	git.waitSynced(t)

	git.mu.Lock()
	assert.False(t, git.driftCheckedAt.IsZero(), "drift must save its state before the reconcile starts")
	assert.False(t, git.drifting, "drift admission must be released before the reconcile starts")
	git.mu.Unlock()

	select {
	case err := <-triggerErr:
		require.Error(t, err)
		assert.ErrorContains(t, err, errDriftOrderStop.Error())
	case <-time.After(driftAdmissionWait):
		t.Fatal("TriggerReconcile did not return")
	}
	n, ok := waiterCount(d)
	require.True(t, ok)
	assert.Zero(t, n)
}

func TestTriggerReconcile_ReturnsContextErrorWhileWaitingForDrift(t *testing.T) {
	d, blocker := newBlockedDriftDaemon(t)
	git := newDriftOrderGitOps(d)
	d.reconcileOpts = append(d.reconcileOpts, reconcile.WithGitOperations(git))
	finish := startBlockedDrift(t, d, blocker)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	triggerErr := make(chan error, 1)
	go func() { triggerErr <- d.TriggerReconcile(ctx, "test", false) }()

	select {
	case err := <-triggerErr:
		require.Error(t, err)
		assert.True(t, errors.Is(err, context.DeadlineExceeded), "got %v", err)
	case <-time.After(driftAdmissionWait):
		finish()
		t.Fatal("TriggerReconcile ignored its context while waiting for drift")
	}

	n, ok := waiterCount(d)
	require.True(t, ok)
	assert.Zero(t, n, "an abandoned waiter must not keep drift out")
	finish()
	select {
	case <-git.synced:
		t.Fatal("an abandoned trigger must not reconcile")
	default:
	}
	d.reconcileMu.Lock()
	assert.False(t, d.reconciling)
	d.reconcileMu.Unlock()
}

func TestDaemonRunDriftCheck_SkipsWhileTriggerWaiting(t *testing.T) {
	mock := dockertest.NewMockDockerAPI()
	d := newDockerDaemon(t, mock)
	d.config.ReconcileConfig.RestartBreakerEnabled = false
	seedDeclaredAPI(t, d)

	d.reconcileMu.Lock()
	d.reconcileWaiters = 1
	d.reconcileMu.Unlock()

	d.runDriftCheck(context.Background())

	assert.Equal(t, 0, mock.ContainerListCalls, "drift must not start while a trigger waits for admission")
	d.reconcileMu.Lock()
	assert.False(t, d.drifting)
	d.reconcileWaiters = 0
	d.reconcileMu.Unlock()
}

func TestDriftCheckLoop_SlowCycleAdmitsWaitingTriggerBeforeNextCycle(t *testing.T) {
	d, blocker := newBlockedDriftDaemon(t)
	d.config.DriftInterval = 10 * time.Millisecond
	git := newDriftOrderGitOps(d)
	git.listCalls = &blocker.calls
	d.reconcileOpts = append(d.reconcileOpts, reconcile.WithGitOperations(git))

	ctx, cancel := context.WithCancel(context.Background())
	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		d.driftCheckLoop(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-loopDone:
		case <-time.After(driftAdmissionWait):
			t.Error("drift loop did not stop")
		}
	})

	blocker.waitEntered(t)
	triggerErr := make(chan error, 1)
	go func() { triggerErr <- d.TriggerReconcile(context.Background(), "test", false) }()
	require.Eventually(t, func() bool {
		n, ok := waiterCount(d)
		return ok && n == 1
	}, driftAdmissionWait, 5*time.Millisecond)

	// Let ticks pile up behind the slow cycle, then release it.
	time.Sleep(50 * time.Millisecond)
	close(blocker.release)

	git.waitSynced(t)
	git.mu.Lock()
	assert.Equal(t, int32(1), git.listCount, "a buffered drift tick ran ahead of the waiting trigger")
	git.mu.Unlock()

	select {
	case err := <-triggerErr:
		assert.ErrorContains(t, err, errDriftOrderStop.Error())
	case <-time.After(driftAdmissionWait):
		t.Fatal("TriggerReconcile did not return")
	}
}

func TestDaemonRunDriftCheck_SelfHealUsesRealAdmission(t *testing.T) {
	mock := dockertest.NewMockDockerAPI()
	d := newDockerDaemon(t, mock)
	d.config.ReconcileConfig.RestartBreakerEnabled = false
	d.config.DriftSelfHeal = reconcile.NewConfigField(true)
	d.config.DriftSelfHealCooldown = reconcile.NewConfigField(time.Nanosecond)
	d.config.MaxSelfHealAttempts = 3
	seedDeclaredAPI(t, d) // declared api, no containers listed: missing drift
	git := newDriftOrderGitOps(d)
	d.reconcileOpts = append(d.reconcileOpts, reconcile.WithGitOperations(git))

	d.runDriftCheck(context.Background())
	git.waitSynced(t)
	waitForDaemonTasks(t, d, driftAdmissionWait)

	git.mu.Lock()
	assert.False(t, git.drifting, "self-heal reconcile must start only after drift admission is released")
	require.NotNil(t, git.selfHeal, "self-heal attempt must be persisted before its reconcile starts")
	assert.Equal(t, 1, git.selfHeal.Attempts)
	git.mu.Unlock()

	d.reconcileMu.Lock()
	assert.Zero(t, d.reconcileWaiters)
	assert.False(t, d.reconciling)
	d.reconcileMu.Unlock()
}

func TestDaemonRunDriftCheck_PanicClearsDriftAdmission(t *testing.T) {
	mock := dockertest.NewMockDockerAPI()
	mock.ContainerListFunc = func(context.Context, client.ContainerListOptions) (client.ContainerListResult, error) {
		panic("simulated drift panic")
	}
	d := newDockerDaemon(t, mock)
	d.config.ReconcileConfig.RestartBreakerEnabled = false
	seedDeclaredAPI(t, d)
	git := newDriftOrderGitOps(d)
	d.reconcileOpts = append(d.reconcileOpts, reconcile.WithGitOperations(git))

	func() {
		defer func() { _ = recover() }()
		d.runDriftCheck(context.Background())
	}()

	d.reconcileMu.Lock()
	assert.False(t, d.drifting, "a panic must not leave drift admission claimed")
	assert.Nil(t, d.driftDone)
	d.reconcileMu.Unlock()

	triggerErr := make(chan error, 1)
	go func() { triggerErr <- d.TriggerReconcile(context.Background(), "test", false) }()
	git.waitSynced(t)
	select {
	case err := <-triggerErr:
		assert.ErrorContains(t, err, errDriftOrderStop.Error())
	case <-time.After(driftAdmissionWait):
		t.Fatal("TriggerReconcile did not return after a drift panic")
	}
}

package reconcile

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/docker/dockertest"
)

// replicaSummary builds a labeled container of project "core", service "web".
// ListContainers keeps the first 12 characters of the ID, so ids carry a
// 16-character suffix. Every fixture sets Created explicitly: selection orders
// by it and most shared fixtures leave it zero.
func replicaSummary(id, name, state, status string, created int64) container.Summary {
	return container.Summary{
		ID:      id + "0000000000000000",
		Names:   []string{"/" + name},
		Image:   "nginx:latest",
		State:   container.ContainerState(state),
		Status:  status,
		Created: created,
		Labels:  composeLabels("core", "web"),
	}
}

// reversed returns a copy of items in the opposite list order.
func reversed(items []container.Summary) []container.Summary {
	out := make([]container.Summary, len(items))
	for i, item := range items {
		out[len(items)-1-i] = item
	}
	return out
}

// listOrder is one way Docker may list a fixture's containers.
type listOrder struct {
	name  string
	items []container.Summary
}

// listOrders returns the fixture in both list orders, in a fixed sequence.
func listOrders(items []container.Summary) []listOrder {
	return []listOrder{
		{name: "listed in order", items: items},
		{name: "listed reversed", items: reversed(items)},
	}
}

func collectReplicas(t *testing.T, items []container.Summary) []ActualService {
	t.Helper()
	mock := dockertest.NewMockDockerAPI()
	mock.ContainerListFunc = func(_ context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
		return client.ContainerListResult{Items: items}, nil
	}
	actual, err := CollectActualState(context.Background(), docker.NewClientWithAPI(mock), "core")
	require.NoError(t, err)
	return actual
}

func TestPreferContainer(t *testing.T) {
	info := func(id, state string, created int64) docker.ContainerInfo {
		return docker.ContainerInfo{ID: id, State: state, Created: time.Unix(created, 0)}
	}

	tests := []struct {
		name string
		cur  docker.ContainerInfo
		cand docker.ContainerInfo
		want bool
	}{
		{"running candidate beats exited current", info("aaa", "exited", 1000), info("bbb", "running", 2000), true},
		{"exited candidate loses to running current", info("aaa", "running", 2000), info("bbb", "exited", 1000), false},
		{"restarting candidate beats exited current", info("aaa", "exited", 1000), info("bbb", "restarting", 2000), true},
		{"paused counts as stopped", info("aaa", "paused", 1000), info("bbb", "running", 2000), true},
		{"older running candidate beats newer running current", info("aaa", "running", 2000), info("bbb", "running", 1000), true},
		{"newer running candidate loses to older running current", info("aaa", "running", 1000), info("bbb", "running", 2000), false},
		{"restarting versus running does not change the pick (older current)", info("aaa", "restarting", 1000), info("bbb", "running", 2000), false},
		{"restarting versus running does not change the pick (older candidate)", info("aaa", "running", 2000), info("bbb", "restarting", 1000), true},
		{"older stopped candidate beats newer stopped current", info("aaa", "exited", 2000), info("bbb", "created", 1000), true},
		{"same age: smaller ID candidate wins", info("bbb", "running", 1000), info("aaa", "running", 1000), true},
		{"same age: larger ID candidate loses", info("aaa", "running", 1000), info("bbb", "running", 1000), false},
		{"identical container is not preferred over itself", info("aaa", "running", 1000), info("aaa", "running", 1000), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, preferContainer(tt.cur, tt.cand))
		})
	}
}

// TestIsLiveState pins the live set against the skip check's presence rule for
// an untripped service. They agree today; this fails if either changes alone,
// so the change is made on purpose.
func TestIsLiveState(t *testing.T) {
	for _, state := range []string{"running", "restarting", "exited", "created", "dead", "paused", "removing", ""} {
		assert.Equal(t, ServiceIsPresent(state, false), isLiveState(state), state)
	}
}

// TestCollectActualState_ReplicaSelection runs every case in both list orders:
// the original kept whichever matching container Docker listed first, so a
// case fails on it in exactly the order that lists the wrong container first.
func TestCollectActualState_ReplicaSelection(t *testing.T) {
	healthy := "Up 10 minutes (healthy)"
	unhealthy := "Up 10 minutes (unhealthy)"
	up := "Up 10 minutes"
	exited := "Exited (1) 2 minutes ago"

	tests := []struct {
		name       string
		items      []container.Summary
		wantName   string
		wantState  string
		wantHealth string
	}{
		{
			name: "exited leftover beside a running replica",
			items: []container.Summary{
				replicaSummary("aaa", "core-web-1", "exited", exited, 1000),
				replicaSummary("bbb", "core-web-2", "running", up, 2000),
			},
			wantName: "core-web-2", wantState: "running",
		},
		{
			name: "newer exited container loses to an older running one",
			items: []container.Summary{
				replicaSummary("aaa", "core-web-1", "exited", exited, 2000),
				replicaSummary("bbb", "core-web-2", "running", up, 1000),
			},
			wantName: "core-web-2", wantState: "running",
		},
		{
			name: "every container stopped: the oldest is observed",
			items: []container.Summary{
				replicaSummary("aaa", "core-web-1", "exited", exited, 1000),
				replicaSummary("bbb", "core-web-2", "created", "Created", 2000),
			},
			wantName: "core-web-1", wantState: "exited",
		},
		{
			name: "every container stopped with the same age: the smaller ID is observed",
			items: []container.Summary{
				replicaSummary("aaa", "core-web-1", "exited", exited, 1000),
				replicaSummary("bbb", "core-web-2", "exited", exited, 1000),
			},
			wantName: "core-web-1", wantState: "exited",
		},
		{
			name: "oldest wins among running replicas",
			items: []container.Summary{
				replicaSummary("aaa", "core-web-1", "running", up, 2000),
				replicaSummary("bbb", "core-web-2", "running", up, 1000),
			},
			wantName: "core-web-2", wantState: "running",
		},
		{
			name: "restarting versus running does not change the pick",
			items: []container.Summary{
				replicaSummary("aaa", "core-web-1", "restarting", "Restarting (1) 5 seconds ago", 1000),
				replicaSummary("bbb", "core-web-2", "running", up, 2000),
			},
			wantName: "core-web-1", wantState: "restarting",
		},
		{
			name: "health is ignored: the older healthy replica beats a newer unhealthy one",
			items: []container.Summary{
				replicaSummary("aaa", "core-web-1", "running", healthy, 1000),
				replicaSummary("bbb", "core-web-2", "running", unhealthy, 2000),
			},
			wantName: "core-web-1", wantState: "running", wantHealth: "healthy",
		},
	}

	for _, tt := range tests {
		for _, order := range listOrders(tt.items) {
			t.Run(tt.name+"/"+order.name, func(t *testing.T) {
				actual := collectReplicas(t, order.items)
				require.Len(t, actual, 1)
				assert.Equal(t, "web", actual[0].Name)
				assert.Equal(t, tt.wantName, actual[0].ContainerName)
				assert.Equal(t, tt.wantState, actual[0].State)
				assert.Equal(t, tt.wantHealth, actual[0].Health)
			})
		}
	}
}

// TestCollectActualState_ListsEveryContainer pins what the restart breaker
// reads: all containers of the service, oldest first, whatever the list order.
func TestCollectActualState_ListsEveryContainer(t *testing.T) {
	items := []container.Summary{
		replicaSummary("bbb", "core-web-2", "running", "Up 10 minutes", 2000),
		replicaSummary("aaa", "core-web-1", "exited", "Exited (1) 2 minutes ago", 1000),
	}
	for _, order := range listOrders(items) {
		t.Run(order.name, func(t *testing.T) {
			actual := collectReplicas(t, order.items)
			require.Len(t, actual, 1)
			assert.Equal(t, []ServiceContainer{
				{Name: "core-web-1", ID: "aaa000000000", State: "exited"},
				{Name: "core-web-2", ID: "bbb000000000", State: "running"},
			}, actual[0].Containers)
		})
	}
}

// TestCollectActualState_PausedOnlyIsStopped is a control: with one container
// the original agrees. It pins that a paused container is observed, and is not
// present for the skip check, matching the Service State Selection scenario.
func TestCollectActualState_PausedOnlyIsStopped(t *testing.T) {
	actual := collectReplicas(t, []container.Summary{
		replicaSummary("aaa", "core-web-1", "paused", "Up 10 minutes (Paused)", 1000),
	})
	require.Len(t, actual, 1)
	assert.Equal(t, "paused", actual[0].State)

	declared := []DeclaredService{{Name: "web", Image: "nginx:latest"}}
	assert.True(t, hasMissingDeclaredServices(declared, actual, nil))

	report := CompareDrift(declared, actual)
	require.Len(t, report.Items, 1)
	assert.Equal(t, DriftMissing, report.Items[0].Type)
	assert.Equal(t, "state=paused", report.Items[0].Actual)
}

// TestReplicaSelection_Consumers drives an exited leftover plus a running
// replica through the readers of CollectActualState that raised a false alarm
// on the original: drift, the commit-already-deployed skip check, and the
// post-deploy health poll's classifier.
func TestReplicaSelection_Consumers(t *testing.T) {
	items := []container.Summary{
		replicaSummary("aaa", "core-web-1", "exited", "Exited (1) 2 minutes ago", 1000),
		replicaSummary("bbb", "core-web-2", "running", "Up 10 minutes", 2000),
	}
	declared := []DeclaredService{{Name: "web", Image: "nginx:latest"}}

	for _, order := range listOrders(items) {
		t.Run(order.name, func(t *testing.T) {
			actual := collectReplicas(t, order.items)

			assert.Empty(t, CompareDrift(declared, actual).Items, "no missing item for a stopped leftover")
			assert.False(t, hasMissingDeclaredServices(declared, actual, nil), "skip check counts the service as present")
			assert.Empty(t, classifyHealth(declared, actual), "health poll counts the service as healthy")
		})
	}
}

// TestReplicaSelection_HealthIgnoredByDrift: an older healthy replica beside a
// newer unhealthy one yields no unhealthy drift item, in either list order. The
// unhealthy replica is not reported until per-replica drift (#853).
func TestReplicaSelection_HealthIgnoredByDrift(t *testing.T) {
	items := []container.Summary{
		replicaSummary("aaa", "core-web-1", "running", "Up 10 minutes (healthy)", 1000),
		replicaSummary("bbb", "core-web-2", "running", "Up 10 minutes (unhealthy)", 2000),
	}
	declared := []DeclaredService{{Name: "web", Image: "nginx:latest"}}

	for _, order := range listOrders(items) {
		t.Run(order.name, func(t *testing.T) {
			actual := collectReplicas(t, order.items)
			assert.Empty(t, CompareDrift(declared, actual).Items)
		})
	}
}

// TestReplicaSelection_ImageDriftReadsSelectedReplica: replicas on different
// images are compared through the selected (oldest) replica only.
func TestReplicaSelection_ImageDriftReadsSelectedReplica(t *testing.T) {
	older := replicaSummary("aaa", "core-web-1", "running", "Up 10 minutes", 1000)
	older.Image = "nginx:1.25"
	newer := replicaSummary("bbb", "core-web-2", "running", "Up 10 minutes", 2000)
	newer.Image = "nginx:1.24"
	items := []container.Summary{older, newer}

	for _, order := range listOrders(items) {
		t.Run(order.name, func(t *testing.T) {
			actual := collectReplicas(t, order.items)

			assert.Empty(t, CompareDrift([]DeclaredService{{Name: "web", Image: "nginx:1.25"}}, actual).Items,
				"declared image matches the selected replica")

			report := CompareDrift([]DeclaredService{{Name: "web", Image: "nginx:1.24"}}, actual)
			require.Len(t, report.Items, 1)
			assert.Equal(t, DriftImageMismatch, report.Items[0].Type)
			assert.Equal(t, "nginx:1.25", report.Items[0].Actual)
		})
	}
}

// breakerCounts is the restart count each container reports, by container name.
// A test sets it before every cycle; the inspect mock reads it.
type breakerCounts map[string]int

// replicaBreakerInspect serves restart-looping containers: each container
// reports the count in counts (0 when absent). IDs follow the
// "<name>-0123456789abcdef" shape of breakerInspectByName.
func replicaBreakerInspect(counts breakerCounts, labels map[string]string) func(context.Context, string, client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	return func(_ context.Context, name string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
		return client.ContainerInspectResult{
			Container: container.InspectResponse{
				ID:              name + "-0123456789abcdef",
				Name:            "/" + name,
				RestartCount:    counts[name],
				State:           &container.State{Status: container.StateRunning},
				Config:          &container.Config{Image: "nginx", Labels: labels},
				NetworkSettings: &container.NetworkSettings{Networks: map[string]*network.EndpointSettings{}},
			},
		}, nil
	}
}

// breakerShortID is the container ID the breaker tracks: the first 12
// characters of the inspected ID.
func breakerShortID(name string) string {
	return (name + "-0123456789abcdef")[:12]
}

// breakerFullID is the ID the breaker stops a container by.
func breakerFullID(name string) string {
	return name + "-0123456789abcdef"
}

// testBreakerReplica builds a container of project "test", service "web" whose
// listed ID matches what inspect reports, as on a real daemon.
func testBreakerReplica(name, state string, created int64) container.Summary {
	s := replicaSummary(breakerShortID(name), name, state, "Up 10 minutes", created)
	s.Labels = composeLabels("test", "web")
	return s
}

// breakerHarness runs the drift-time flow the daemon runs each tick: list,
// collect the actual state, run the breaker, keep its tracking.
type breakerHarness struct {
	t       *testing.T
	items   []container.Summary
	counts  breakerCounts
	stopped []string
	client  *docker.Client
	state   *DeployState
}

func newBreakerHarness(t *testing.T) *breakerHarness {
	t.Helper()
	h := &breakerHarness{t: t, counts: breakerCounts{}, state: &DeployState{}}
	mock := dockertest.NewMockDockerAPI()
	mock.ContainerListFunc = func(_ context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
		return client.ContainerListResult{Items: h.items}, nil
	}
	mock.ContainerInspectFunc = replicaBreakerInspect(h.counts, composeLabels("test", "web"))
	mock.ContainerStopFunc = func(_ context.Context, id string, _ client.ContainerStopOptions) (client.ContainerStopResult, error) {
		h.stopped = append(h.stopped, id)
		return client.ContainerStopResult{}, nil
	}
	h.client = docker.NewClientWithAPI(mock)
	return h
}

// cycle collects the actual state and runs the breaker once.
func (h *breakerHarness) cycle() (*RestartBreakerResult, []ActualService) {
	h.t.Helper()
	actual, err := CollectActualState(context.Background(), h.client, "test")
	require.NoError(h.t, err)
	result, err := RunRestartBreaker(context.Background(), h.client, RestartBreakerScope{ProjectName: "test"}, actual, h.state, 5, 10*time.Minute)
	require.NoError(h.t, err)
	h.state.RestartTracking = result.Updated
	return result, actual
}

// TestRunRestartBreaker_AlternatingReplicaStillTrips: the older replica of a
// scaled service crash-loops, alternating between restarting and running, and
// Docker lists the two containers in a different order on different samples.
// Drift keeps observing the looping replica (a stable selection), and the
// breaker accumulates its baseline and trips it. On 467a93e the breaker
// watched only the selected container, and first-listed-wins selection moved
// to the healthy sibling on the second sample.
func TestRunRestartBreaker_AlternatingReplicaStillTrips(t *testing.T) {
	h := newBreakerHarness(t)

	cycles := []struct {
		state       string
		count       int
		newestFirst bool
	}{
		{"restarting", 2, false},
		{"running", 5, true},
		{"restarting", 8, false},
	}

	var last *RestartBreakerResult
	for _, c := range cycles {
		older := testBreakerReplica("test-web-1", c.state, 1000)
		newer := testBreakerReplica("test-web-2", "running", 2000)
		h.items = []container.Summary{older, newer}
		if c.newestFirst {
			h.items = []container.Summary{newer, older}
		}
		h.counts["test-web-1"] = c.count

		var actual []ActualService
		last, actual = h.cycle()
		require.Len(t, actual, 1)
		require.Equal(t, "test-web-1", actual[0].ContainerName, "the looping replica stays observed")
	}

	assert.Equal(t, []string{"web"}, last.Tripped)
	assert.Equal(t, []string{breakerFullID("test-web-1")}, h.stopped, "only the looping replica is stopped")
}

// TestRunRestartBreaker_EveryReplicaCrashLoops: when every replica of a service
// crash-loops, each is tripped and stopped by its own ID. On 467a93e only the
// selected replica was watched, so the other kept looping.
func TestRunRestartBreaker_EveryReplicaCrashLoops(t *testing.T) {
	h := newBreakerHarness(t)

	var last *RestartBreakerResult
	for _, count := range []int{2, 5, 8} {
		h.items = []container.Summary{
			testBreakerReplica("test-web-1", "restarting", 1000),
			testBreakerReplica("test-web-2", "restarting", 2000),
		}
		h.counts["test-web-1"] = count
		h.counts["test-web-2"] = count
		last, _ = h.cycle()
	}

	assert.Equal(t, []string{"web"}, last.Tripped)
	sort.Strings(h.stopped)
	assert.Equal(t, []string{breakerFullID("test-web-1"), breakerFullID("test-web-2")}, h.stopped)
}

// TestRunRestartBreaker_NewerReplicaCrashLoopsBesideHealthyOne: a crash-looping
// replica that drift does not select (it is newer) is still watched and
// stopped, and the healthy older replica keeps its own untripped baseline. On
// 467a93e the breaker never inspected the newer replica.
func TestRunRestartBreaker_NewerReplicaCrashLoopsBesideHealthyOne(t *testing.T) {
	h := newBreakerHarness(t)

	var last *RestartBreakerResult
	for _, count := range []int{2, 5, 8} {
		h.items = []container.Summary{
			testBreakerReplica("test-web-1", "running", 1000),
			testBreakerReplica("test-web-2", "restarting", 2000),
		}
		h.counts["test-web-2"] = count
		last, _ = h.cycle()
	}

	assert.Equal(t, []string{"web"}, last.Tripped)
	assert.Equal(t, []string{breakerFullID("test-web-2")}, h.stopped, "only the looping replica is stopped")
	assert.False(t, last.Updated["web"].Tripped, "the healthy older replica is not tripped")
	assert.True(t, last.Updated["web#"+breakerShortID("test-web-2")].Tripped)
}

// TestRunRestartBreaker_StoppedReplicaStaysTrippedWhileSiblingRuns: after the
// breaker stops a replica, a running sibling does not take over its entry. The
// stopped replica stays tripped with no resolved alert, and the sibling gets
// its own baseline. On 467a93e the entry moved to the sibling and resolved on
// the second sample while the stopped replica stayed stopped.
func TestRunRestartBreaker_StoppedReplicaStaysTrippedWhileSiblingRuns(t *testing.T) {
	h := newBreakerHarness(t)
	h.items = []container.Summary{
		testBreakerReplica("test-web-1", "exited", 1000),
		testBreakerReplica("test-web-2", "running", 2000),
	}

	now := time.Now()
	h.state.RestartTracking = map[string]RestartTrackingEntry{
		"web": {
			RestartCount:         9,
			CheckedAt:            now.Add(-3 * time.Minute),
			BaselineRestartCount: 2,
			BaselineAt:           now.Add(-8 * time.Minute),
			ContainerID:          breakerShortID("test-web-1"),
			Tripped:              true,
			TrippedAt:            now.Add(-3 * time.Minute),
		},
	}

	for i := 1; i <= 2; i++ {
		result, _ := h.cycle()

		assert.Empty(t, result.Resolved, "sample %d: no resolved alert while the stopped replica stays stopped", i)
		stopped := result.Updated["web"]
		assert.True(t, stopped.Tripped, "sample %d", i)
		assert.Equal(t, breakerShortID("test-web-1"), stopped.ContainerID, "sample %d: the entry stays tied to the container that tripped", i)
		sibling := result.Updated["web#"+breakerShortID("test-web-2")]
		assert.False(t, sibling.Tripped, "sample %d", i)
		assert.Equal(t, breakerShortID("test-web-2"), sibling.ContainerID, "sample %d", i)
	}
	assert.Empty(t, h.stopped)
}

// TestRunRestartBreaker_ReplacementContainerResolvesAfterGraceSample: a deploy
// replaces the tripped container. The entry stays tripped for the first sample
// on the replacement and resolves on the next sample with an unchanged count,
// as Restart Breaker Resolution Attribution requires.
func TestRunRestartBreaker_ReplacementContainerResolvesAfterGraceSample(t *testing.T) {
	h := newBreakerHarness(t)
	h.items = []container.Summary{testBreakerReplica("test-web-3", "running", 3000)}

	now := time.Now()
	h.state.RestartTracking = map[string]RestartTrackingEntry{
		"web": {
			RestartCount:         9,
			CheckedAt:            now.Add(-3 * time.Minute),
			BaselineRestartCount: 2,
			BaselineAt:           now.Add(-8 * time.Minute),
			ContainerID:          breakerShortID("test-web-1"),
			Tripped:              true,
			TrippedAt:            now.Add(-3 * time.Minute),
		},
	}

	first, _ := h.cycle()
	assert.Empty(t, first.Resolved, "the first sample on the replacement only starts the grace interval")
	entry := first.Updated["web"]
	assert.True(t, entry.Tripped)
	assert.True(t, entry.StabilityPending)
	assert.Equal(t, breakerShortID("test-web-3"), entry.ContainerID)

	second, _ := h.cycle()
	assert.Equal(t, []string{"web"}, second.Resolved)
	assert.False(t, second.Updated["web"].Tripped)
}

// TestRunRestartBreaker_ExitedBlipRestartsBaseline pins existing behavior: a
// container observed "exited" for one sample is not evaluated that cycle, and
// its untripped entry is dropped, so its baseline restarts when it is running
// again. This was true of a one-container service before replicas were
// tracked. Docker normally reports "restarting" throughout the backoff, so
// this is rare.
func TestRunRestartBreaker_ExitedBlipRestartsBaseline(t *testing.T) {
	h := newBreakerHarness(t)

	var last *RestartBreakerResult
	for _, c := range []struct {
		state string
		count int
	}{
		{"restarting", 2},
		{"exited", 5},
		{"restarting", 8},
	} {
		h.items = []container.Summary{
			testBreakerReplica("test-web-1", c.state, 1000),
			testBreakerReplica("test-web-2", "running", 2000),
		}
		h.counts["test-web-1"] = c.count
		last, _ = h.cycle()
	}

	assert.Empty(t, last.Tripped, "the baseline restarted after the blip")
}

func TestAssignTrackKeys(t *testing.T) {
	bc := func(name, id string) breakerContainer {
		return breakerContainer{Service: "web", ContainerName: name, Obs: restartObservation{ContainerID: id}}
	}
	tests := []struct {
		name       string
		containers []breakerContainer
		tracked    map[string]RestartTrackingEntry
		existing   map[string]map[string]bool
		want       map[string]string // tracking key to container name
	}{
		{
			name:       "first sight of one container takes the service key",
			containers: []breakerContainer{bc("web-1", "aaa")},
			want:       map[string]string{"web": "web-1"},
		},
		{
			name:       "a second container gets its own key",
			containers: []breakerContainer{bc("web-2", "bbb"), bc("web-1", "aaa")},
			want:       map[string]string{"web": "web-1", "web#bbb": "web-2"},
		},
		{
			name:       "a container keeps the entry that holds its ID",
			containers: []breakerContainer{bc("web-1", "aaa"), bc("web-2", "bbb")},
			tracked:    map[string]RestartTrackingEntry{"web": {ContainerID: "bbb"}},
			want:       map[string]string{"web": "web-2", "web#aaa": "web-1"},
		},
		{
			name:       "a replacement takes over the entry of a container that is gone",
			containers: []breakerContainer{bc("web-3", "ccc")},
			tracked:    map[string]RestartTrackingEntry{"web": {ContainerID: "aaa", Tripped: true}},
			existing:   map[string]map[string]bool{"web": {"ccc": true}},
			want:       map[string]string{"web": "web-3"},
		},
		{
			name:       "a container does not take over the entry of one that still exists",
			containers: []breakerContainer{bc("web-2", "bbb")},
			tracked:    map[string]RestartTrackingEntry{"web": {ContainerID: "aaa", Tripped: true}},
			existing:   map[string]map[string]bool{"web": {"aaa": true, "bbb": true}},
			want:       map[string]string{"web#bbb": "web-2"},
		},
		{
			name:       "an unknown container list lets a new ID take the service entry",
			containers: []breakerContainer{bc("web-3", "ccc")},
			tracked:    map[string]RestartTrackingEntry{"web": {ContainerID: "aaa"}},
			want:       map[string]string{"web": "web-3"},
		},
		{
			name:       "an entry without an ID is taken over",
			containers: []breakerContainer{bc("web-1", "aaa")},
			tracked:    map[string]RestartTrackingEntry{"web": {RestartCount: 3}},
			existing:   map[string]map[string]bool{"web": {"aaa": true}},
			want:       map[string]string{"web": "web-1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := make(map[string]string)
			for key, c := range assignTrackKeys(tt.containers, tt.tracked, tt.existing) {
				got[key] = c.ContainerName
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestServiceTripped(t *testing.T) {
	tracking := map[string]RestartTrackingEntry{
		"web":        {Tripped: false},
		"web#abc":    {Tripped: true},
		"webby":      {Tripped: true},
		"api":        {Tripped: false},
		"api#def":    {Tripped: false},
		"worker#xyz": {Tripped: true},
	}

	assert.True(t, serviceTripped(tracking, "web"), "a tripped second container counts for the service")
	assert.True(t, serviceTripped(tracking, "worker"))
	assert.False(t, serviceTripped(tracking, "api"))
	assert.False(t, serviceTripped(tracking, "wed"), "a name that is only a prefix does not match")
	assert.False(t, serviceTripped(nil, "web"))
}

// TestSkipCheck_TrippedReplicaCountsServiceAsPresent: when the breaker stopped
// the only live replica, the service is present for the skip check, so compose
// up does not restart it. The tripped entry may belong to a second container.
func TestSkipCheck_TrippedReplicaCountsServiceAsPresent(t *testing.T) {
	declared := []DeclaredService{{Name: "web", Image: "nginx:latest"}}
	actual := collectReplicas(t, []container.Summary{
		replicaSummary("aaa", "core-web-1", "exited", "Exited (1) 2 minutes ago", 1000),
		replicaSummary("bbb", "core-web-2", "exited", "Exited (1) 2 minutes ago", 2000),
	})

	assert.True(t, hasMissingDeclaredServices(declared, actual, nil))
	tracking := map[string]RestartTrackingEntry{"web#bbb000000000": {Tripped: true}}
	assert.False(t, hasMissingDeclaredServices(declared, actual, tracking))
	assert.True(t, presentServices(actual, tracking)["web"])
}

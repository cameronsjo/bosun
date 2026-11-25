package reconcile

import (
	"context"
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
		for orderName, items := range map[string][]container.Summary{
			"listed in order": tt.items,
			"listed reversed": reversed(tt.items),
		} {
			t.Run(tt.name+"/"+orderName, func(t *testing.T) {
				actual := collectReplicas(t, items)
				require.Len(t, actual, 1)
				assert.Equal(t, "web", actual[0].Name)
				assert.Equal(t, tt.wantName, actual[0].ContainerName)
				assert.Equal(t, tt.wantState, actual[0].State)
				assert.Equal(t, tt.wantHealth, actual[0].Health)
			})
		}
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

	for orderName, listed := range map[string][]container.Summary{
		"listed in order": items,
		"listed reversed": reversed(items),
	} {
		t.Run(orderName, func(t *testing.T) {
			actual := collectReplicas(t, listed)

			assert.Empty(t, CompareDrift(declared, actual).Items, "no missing item for a stopped leftover")
			assert.False(t, hasMissingDeclaredServices(declared, actual, nil), "skip check counts the service as present")
			assert.Empty(t, classifyHealth(declared, actual), "health poll counts the service as healthy")
		})
	}
}

// replicaBreakerInspect serves restart-looping containers: only the named
// looping replica carries the current restart count; every other container
// reads 0. IDs follow the "<name>-0123456789abcdef" shape of breakerInspectByName.
func replicaBreakerInspect(loopingName string, restartCount *int, labels map[string]string) func(context.Context, string, client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	return func(_ context.Context, name string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
		count := 0
		if name == loopingName {
			count = *restartCount
		}
		return client.ContainerInspectResult{
			Container: container.InspectResponse{
				ID:              name + "-0123456789abcdef",
				Name:            "/" + name,
				RestartCount:    count,
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

func testBreakerReplica(id, name, state string, created int64) container.Summary {
	s := replicaSummary(id, name, state, "Up 10 minutes", created)
	s.Labels = composeLabels("test", "web")
	return s
}

// TestRunRestartBreaker_AlternatingReplicaStillTrips: the older replica of a
// scaled service crash-loops, alternating between restarting and running, and
// Docker lists the two containers in a different order on different samples.
// Selection must keep observing the looping replica, so its restart baseline
// accumulates and the breaker trips. On the original, first-listed-wins picks
// the healthy sibling on the second sample, the observed container ID changes,
// and the baseline resets on every change, so the loop is never tripped.
func TestRunRestartBreaker_AlternatingReplicaStillTrips(t *testing.T) {
	restartCount := 0
	var items []container.Summary
	var stopped []string

	mock := dockertest.NewMockDockerAPI()
	mock.ContainerListFunc = func(_ context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
		return client.ContainerListResult{Items: items}, nil
	}
	mock.ContainerInspectFunc = replicaBreakerInspect("test-web-1", &restartCount, composeLabels("test", "web"))
	mock.ContainerStopFunc = func(_ context.Context, id string, _ client.ContainerStopOptions) (client.ContainerStopResult, error) {
		stopped = append(stopped, id)
		return client.ContainerStopResult{}, nil
	}
	dockerClient := docker.NewClientWithAPI(mock)

	cycles := []struct {
		state       string
		count       int
		newestFirst bool
	}{
		{"restarting", 2, false},
		{"running", 5, true},
		{"restarting", 8, false},
	}

	state := &DeployState{}
	var last *RestartBreakerResult
	for _, c := range cycles {
		older := testBreakerReplica("aaa", "test-web-1", c.state, 1000)
		newer := testBreakerReplica("bbb", "test-web-2", "running", 2000)
		items = []container.Summary{older, newer}
		if c.newestFirst {
			items = []container.Summary{newer, older}
		}
		restartCount = c.count

		actual, err := CollectActualState(context.Background(), dockerClient, "test")
		require.NoError(t, err)
		require.Len(t, actual, 1)
		require.Equal(t, "test-web-1", actual[0].ContainerName, "the looping replica stays observed")

		last, err = RunRestartBreaker(context.Background(), dockerClient, RestartBreakerScope{ProjectName: "test"}, actual, state, 5, 10*time.Minute)
		require.NoError(t, err)
		state.RestartTracking = last.Updated
	}

	assert.Equal(t, []string{"web"}, last.Tripped)
	assert.Equal(t, []string{"test-web-1-0123456789abcdef"}, stopped, "only the looping replica is stopped")
}

// TestRunRestartBreaker_StoppedReplicaHandsOverToLiveSibling: after the breaker
// stops the selected replica, the running sibling becomes the observed
// container. The tripped entry stays tripped for the first sample on the new
// container and resolves on the next sample with an unchanged restart count,
// while the stopped replica stays stopped. On the original, an exited replica
// listed first is the observed container, the breaker skips it as not running,
// and the entry never resolves.
func TestRunRestartBreaker_StoppedReplicaHandsOverToLiveSibling(t *testing.T) {
	restartCount := 0
	items := []container.Summary{
		testBreakerReplica("aaa", "test-web-1", "exited", 1000),
		testBreakerReplica("bbb", "test-web-2", "running", 2000),
	}

	mock := dockertest.NewMockDockerAPI()
	mock.ContainerListFunc = func(_ context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
		return client.ContainerListResult{Items: items}, nil
	}
	mock.ContainerInspectFunc = replicaBreakerInspect("test-web-2", &restartCount, composeLabels("test", "web"))
	dockerClient := docker.NewClientWithAPI(mock)

	now := time.Now()
	state := &DeployState{RestartTracking: map[string]RestartTrackingEntry{
		"web": {
			RestartCount:         9,
			CheckedAt:            now.Add(-3 * time.Minute),
			BaselineRestartCount: 2,
			BaselineAt:           now.Add(-8 * time.Minute),
			ContainerID:          breakerShortID("test-web-1"),
			Tripped:              true,
			TrippedAt:            now.Add(-3 * time.Minute),
		},
	}}
	scope := RestartBreakerScope{ProjectName: "test"}

	run := func() *RestartBreakerResult {
		actual, err := CollectActualState(context.Background(), dockerClient, "test")
		require.NoError(t, err)
		require.Len(t, actual, 1)
		require.Equal(t, "test-web-2", actual[0].ContainerName, "the running sibling is observed")
		result, err := RunRestartBreaker(context.Background(), dockerClient, scope, actual, state, 5, 10*time.Minute)
		require.NoError(t, err)
		state.RestartTracking = result.Updated
		return result
	}

	first := run()
	assert.Empty(t, first.Resolved, "first sample on the sibling only starts the grace interval")
	entry := first.Updated["web"]
	assert.True(t, entry.Tripped)
	assert.True(t, entry.StabilityPending)
	assert.Equal(t, breakerShortID("test-web-2"), entry.ContainerID)

	second := run()
	assert.Equal(t, []string{"web"}, second.Resolved, "an unchanged restart count on the sibling resolves the service")
	assert.False(t, second.Updated["web"].Tripped)
}

// TestRunRestartBreaker_ExitedBlipHandsPickToSibling pins a known limit
// recorded in the change proposal: a crash-looping replica caught "exited" for
// one sample hands the selection to its running sibling for that sample, and
// the baseline restarts when the observed container ID changes. Docker normally
// reports "restarting" throughout the backoff, so this is rare. If selection
// later keys on something that survives the blip, this test should flip to
// asserting a trip.
func TestRunRestartBreaker_ExitedBlipHandsPickToSibling(t *testing.T) {
	restartCount := 0
	var items []container.Summary

	mock := dockertest.NewMockDockerAPI()
	mock.ContainerListFunc = func(_ context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
		return client.ContainerListResult{Items: items}, nil
	}
	mock.ContainerInspectFunc = replicaBreakerInspect("test-web-1", &restartCount, composeLabels("test", "web"))
	dockerClient := docker.NewClientWithAPI(mock)

	cycles := []struct {
		state string
		count int
	}{
		{"restarting", 2},
		{"exited", 5},
		{"restarting", 8},
	}

	state := &DeployState{}
	var last *RestartBreakerResult
	for _, c := range cycles {
		items = []container.Summary{
			testBreakerReplica("aaa", "test-web-1", c.state, 1000),
			testBreakerReplica("bbb", "test-web-2", "running", 2000),
		}
		restartCount = c.count

		actual, err := CollectActualState(context.Background(), dockerClient, "test")
		require.NoError(t, err)
		last, err = RunRestartBreaker(context.Background(), dockerClient, RestartBreakerScope{ProjectName: "test"}, actual, state, 5, 10*time.Minute)
		require.NoError(t, err)
		state.RestartTracking = last.Updated
	}

	assert.Empty(t, last.Tripped, "the baseline restarted when the observed container changed")
}

package reconcile

import (
	"context"
	"sort"
	"strings"
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

// breakerCounts is the restart count each container reports, by container name.
// A test sets it before every cycle; the inspect mock reads it.
type breakerCounts map[string]int

// composeNumber is the Compose container number of a "<project>-<service>-<n>"
// container name.
func composeNumber(name string) string {
	return name[strings.LastIndex(name, "-")+1:]
}

// breakerHarness runs the drift-time flow the daemon runs each tick: list,
// collect the actual state, run the breaker, keep its tracking. Containers are
// project "test", service "web"; the listed ID matches what inspect reports, as
// on a real daemon, and ids overrides the ID of a name to model a recreation.
type breakerHarness struct {
	t       *testing.T
	items   []container.Summary
	counts  breakerCounts
	ids     map[string]string
	stopped []string
	client  *docker.Client
	state   *DeployState
}

func newBreakerHarness(t *testing.T) *breakerHarness {
	t.Helper()
	h := &breakerHarness{t: t, counts: breakerCounts{}, ids: map[string]string{}, state: &DeployState{}}
	mock := dockertest.NewMockDockerAPI()
	mock.ContainerListFunc = func(_ context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
		return client.ContainerListResult{Items: h.items}, nil
	}
	mock.ContainerInspectFunc = func(_ context.Context, name string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
		return client.ContainerInspectResult{
			Container: container.InspectResponse{
				ID:              h.fullID(name),
				Name:            "/" + name,
				RestartCount:    h.counts[name],
				State:           &container.State{Status: container.StateRunning},
				Config:          &container.Config{Image: "nginx", Labels: h.labels(name)},
				NetworkSettings: &container.NetworkSettings{Networks: map[string]*network.EndpointSettings{}},
			},
		}, nil
	}
	mock.ContainerStopFunc = func(_ context.Context, id string, _ client.ContainerStopOptions) (client.ContainerStopResult, error) {
		h.stopped = append(h.stopped, id)
		return client.ContainerStopResult{}, nil
	}
	h.client = docker.NewClientWithAPI(mock)
	return h
}

// shortID is the container ID the breaker tracks: the first 12 characters of
// the inspected ID.
func (h *breakerHarness) shortID(name string) string {
	if id, ok := h.ids[name]; ok {
		return id
	}
	return (name + "-0123456789abcdef")[:12]
}

// fullID is the ID the breaker stops a container by.
func (h *breakerHarness) fullID(name string) string {
	return h.shortID(name) + "-0123456789abcdef"
}

func (h *breakerHarness) labels(name string) map[string]string {
	labels := composeLabels("test", "web")
	labels[ComposeContainerNumberLabel] = composeNumber(name)
	return labels
}

// replica builds the listed container for name.
func (h *breakerHarness) replica(name, state string, created int64) container.Summary {
	s := replicaSummary(h.shortID(name), name, state, "Up 10 minutes", created)
	s.Labels = h.labels(name)
	return s
}

// tripped builds the persisted entry of a container the breaker tripped.
func (h *breakerHarness) tripped(name string, count int, pending bool) RestartTrackingEntry {
	now := time.Now()
	return RestartTrackingEntry{
		RestartCount:         count,
		CheckedAt:            now.Add(-3 * time.Minute),
		BaselineRestartCount: 2,
		BaselineAt:           now.Add(-8 * time.Minute),
		ContainerID:          h.shortID(name),
		ContainerNumber:      composeNumber(name),
		Tripped:              true,
		TrippedAt:            now.Add(-3 * time.Minute),
		StabilityPending:     pending,
	}
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
		older := h.replica("test-web-1", c.state, 1000)
		newer := h.replica("test-web-2", "running", 2000)
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
	assert.Equal(t, []string{h.fullID("test-web-1")}, h.stopped, "only the looping replica is stopped")
}

// TestRunRestartBreaker_EveryReplicaCrashLoops: when every replica of a service
// crash-loops, each is tripped and stopped by its own ID. On 467a93e only the
// selected replica was watched, so the other kept looping.
func TestRunRestartBreaker_EveryReplicaCrashLoops(t *testing.T) {
	h := newBreakerHarness(t)

	var last *RestartBreakerResult
	for _, count := range []int{2, 5, 8} {
		h.items = []container.Summary{
			h.replica("test-web-1", "restarting", 1000),
			h.replica("test-web-2", "restarting", 2000),
		}
		h.counts["test-web-1"] = count
		h.counts["test-web-2"] = count
		last, _ = h.cycle()
	}

	assert.Equal(t, []string{"web"}, last.Tripped)
	sort.Strings(h.stopped)
	assert.Equal(t, []string{h.fullID("test-web-1"), h.fullID("test-web-2")}, h.stopped)
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
			h.replica("test-web-1", "running", 1000),
			h.replica("test-web-2", "restarting", 2000),
		}
		h.counts["test-web-2"] = count
		last, _ = h.cycle()
	}

	assert.Equal(t, []string{"web"}, last.Tripped)
	assert.Equal(t, []string{h.fullID("test-web-2")}, h.stopped, "only the looping replica is stopped")
	assert.False(t, last.Updated["web"].Tripped, "the healthy older replica is not tripped")
	assert.True(t, last.Updated["web#"+h.shortID("test-web-2")].Tripped)
}

// TestRunRestartBreaker_StoppedReplicaStaysTrippedWhileSiblingRuns: after the
// breaker stops a replica, a running sibling does not take over its entry. The
// stopped replica stays tripped with no resolved alert, and the sibling gets
// its own baseline. On 467a93e the entry moved to the sibling and resolved on
// the second sample while the stopped replica stayed stopped.
func TestRunRestartBreaker_StoppedReplicaStaysTrippedWhileSiblingRuns(t *testing.T) {
	h := newBreakerHarness(t)
	h.items = []container.Summary{
		h.replica("test-web-1", "exited", 1000),
		h.replica("test-web-2", "running", 2000),
	}
	h.state.RestartTracking = map[string]RestartTrackingEntry{"web": h.tripped("test-web-1", 9, false)}

	for i := 1; i <= 2; i++ {
		result, _ := h.cycle()

		assert.Empty(t, result.Resolved, "sample %d: no resolved alert while the stopped replica stays stopped", i)
		stopped := result.Updated["web"]
		assert.True(t, stopped.Tripped, "sample %d", i)
		assert.Equal(t, h.shortID("test-web-1"), stopped.ContainerID, "sample %d: the entry stays tied to the container that tripped", i)
		sibling := result.Updated["web#"+h.shortID("test-web-2")]
		assert.False(t, sibling.Tripped, "sample %d", i)
		assert.Equal(t, h.shortID("test-web-2"), sibling.ContainerID, "sample %d", i)
	}
	assert.Empty(t, h.stopped)
}

// TestRunRestartBreaker_ResolvedOnlyWhenEveryTrippedContainerRecovers: two
// replicas are tripped. The first is stable and resolves on the first sample;
// the second keeps looping, so the service is not reported resolved until the
// second recovers too. On 27c19c1 the service was reported resolved on the
// first sample while the second replica was still tripped.
func TestRunRestartBreaker_ResolvedOnlyWhenEveryTrippedContainerRecovers(t *testing.T) {
	h := newBreakerHarness(t)
	h.items = []container.Summary{
		h.replica("test-web-1", "running", 1000),
		h.replica("test-web-2", "running", 2000),
	}
	h.state.RestartTracking = map[string]RestartTrackingEntry{
		"web":                            h.tripped("test-web-1", 9, true),
		"web#" + h.shortID("test-web-2"): h.tripped("test-web-2", 9, true),
	}
	h.counts["test-web-1"] = 9  // unchanged: stable
	h.counts["test-web-2"] = 12 // still looping

	first, _ := h.cycle()
	assert.Empty(t, first.Resolved, "one replica is still tripped, so the service is not resolved")
	assert.False(t, first.Updated["web"].Tripped, "the stable replica's own entry resolved")
	assert.True(t, first.Updated["web#"+h.shortID("test-web-2")].Tripped)

	second, _ := h.cycle() // the second replica's count is unchanged now
	assert.Equal(t, []string{"web"}, second.Resolved, "resolved once the last tripped replica recovers")
	assert.False(t, serviceTripped(second.Updated, "web"))
}

// TestRunRestartBreaker_GoneTrippedContainerClearsWithoutAlert: a tripped
// container is removed and only a sibling with another container number
// remains. The tripped entry clears with no resolved alert, and the sibling
// does not inherit it. On 27c19c1 the sibling took over the tripped entry (and
// resolved it after a grace sample), and the entry was never cleared.
func TestRunRestartBreaker_GoneTrippedContainerClearsWithoutAlert(t *testing.T) {
	h := newBreakerHarness(t)
	h.items = []container.Summary{h.replica("test-web-2", "running", 2000)}
	h.state.RestartTracking = map[string]RestartTrackingEntry{"web": h.tripped("test-web-1", 9, false)}

	for i := 1; i <= 2; i++ {
		result, _ := h.cycle()

		assert.Empty(t, result.Resolved, "sample %d: clearing is not a resolution", i)
		assert.False(t, serviceTripped(result.Updated, "web"), "sample %d: nothing stays tripped", i)
		assert.NotContains(t, result.Updated, "web", "sample %d: the gone container's entry is cleared", i)
		sibling := result.Updated["web#"+h.shortID("test-web-2")]
		assert.Equal(t, h.shortID("test-web-2"), sibling.ContainerID, "sample %d: the sibling has its own entry", i)
		assert.False(t, sibling.Tripped, "sample %d", i)
	}
}

// TestRunRestartBreaker_ServiceRemovedClearsTrippedEntry: with every container
// of a service gone, its tripped entry clears.
func TestRunRestartBreaker_ServiceRemovedClearsTrippedEntry(t *testing.T) {
	h := newBreakerHarness(t)
	h.items = nil
	h.state.RestartTracking = map[string]RestartTrackingEntry{"web": h.tripped("test-web-1", 9, false)}

	result, _ := h.cycle()

	assert.Empty(t, result.Resolved)
	assert.NotContains(t, result.Updated, "web")
}

// TestRunRestartBreaker_ReplacementContainerResolvesAfterGraceSample: a deploy
// recreates the tripped container (same container number, new ID). The entry
// stays tripped for the first sample on the replacement and resolves on the
// next sample with an unchanged count, as Restart Breaker Resolution
// Attribution requires.
func TestRunRestartBreaker_ReplacementContainerResolvesAfterGraceSample(t *testing.T) {
	h := newBreakerHarness(t)
	h.state.RestartTracking = map[string]RestartTrackingEntry{"web": h.tripped("test-web-1", 9, false)}
	h.ids["test-web-1"] = "replacement1" // same name and number, new container
	h.items = []container.Summary{h.replica("test-web-1", "running", 3000)}

	first, _ := h.cycle()
	assert.Empty(t, first.Resolved, "the first sample on the replacement only starts the grace interval")
	entry := first.Updated["web"]
	assert.True(t, entry.Tripped)
	assert.True(t, entry.StabilityPending)
	assert.Equal(t, "replacement1", entry.ContainerID)
	assert.Equal(t, "1", entry.ContainerNumber)

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
			h.replica("test-web-1", c.state, 1000),
			h.replica("test-web-2", "running", 2000),
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
	bcn := func(name, id, number string) breakerContainer {
		c := bc(name, id)
		c.Number = number
		return c
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
			name:       "a replacement with the same container number takes over the entry",
			containers: []breakerContainer{bcn("web-1", "ccc", "1")},
			tracked:    map[string]RestartTrackingEntry{"web": {ContainerID: "aaa", ContainerNumber: "1", Tripped: true}},
			existing:   map[string]map[string]bool{"web": {"ccc": true}},
			want:       map[string]string{"web": "web-1"},
		},
		{
			name:       "a new sibling with another container number does not inherit the entry",
			containers: []breakerContainer{bcn("web-2", "ccc", "2")},
			tracked:    map[string]RestartTrackingEntry{"web": {ContainerID: "aaa", ContainerNumber: "1", Tripped: true}},
			existing:   map[string]map[string]bool{"web": {"ccc": true}},
			want:       map[string]string{"web#ccc": "web-2"},
		},
		{
			name:       "an entry without a number is taken over by a numbered replacement",
			containers: []breakerContainer{bcn("web-1", "ccc", "1")},
			tracked:    map[string]RestartTrackingEntry{"web": {ContainerID: "aaa", Tripped: true}},
			existing:   map[string]map[string]bool{"web": {"ccc": true}},
			want:       map[string]string{"web": "web-1"},
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

func TestServiceOfKey(t *testing.T) {
	assert.Equal(t, "web", serviceOfKey("web"))
	assert.Equal(t, "web", serviceOfKey("web#abc123"))
	assert.Equal(t, "", serviceOfKey(""))
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

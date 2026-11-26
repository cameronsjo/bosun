package reconcile

import (
	"context"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
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

package docker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// statsConcurrencyCap is the worker bound GetAllContainerStats enforces.
const statsConcurrencyCap = maxConcurrentStats

// statsListFunc returns n running containers named c0..c(n-1).
func statsListFunc(n int) func(context.Context, client.ContainerListOptions) (client.ContainerListResult, error) {
	return func(context.Context, client.ContainerListOptions) (client.ContainerListResult, error) {
		items := make([]container.Summary, n)
		for i := range items {
			items[i] = makeTestContainer(fmt.Sprintf("id%010d", i), fmt.Sprintf("c%d", i), "img:latest", "running")
		}
		return client.ContainerListResult{Items: items}, nil
	}
}

func okStatsResult() client.ContainerStatsResult {
	stats := makeStatsJSON(100000000, 1000000000, 50000000, 500000000, 100000000, 200000000, 2)
	return client.ContainerStatsResult{Body: io.NopCloser(bytes.NewReader(stats))}
}

// trackPeak records the highest number of calls in flight at once.
func trackPeak(inFlight, peak *atomic.Int32) func() {
	cur := inFlight.Add(1)
	for {
		p := peak.Load()
		if cur <= p || peak.CompareAndSwap(p, cur) {
			break
		}
	}
	return func() { inFlight.Add(-1) }
}

func TestClient_GetAllContainerStats_RunsConcurrently(t *testing.T) {
	const n = 4

	mock := NewMockDockerAPI()
	mock.ContainerListFunc = statsListFunc(n)

	var inFlight, peak atomic.Int32
	allArrived := make(chan struct{})
	var once sync.Once
	mock.ContainerStatsFunc = func(ctx context.Context, _ string, _ client.ContainerStatsOptions) (client.ContainerStatsResult, error) {
		done := trackPeak(&inFlight, &peak)
		defer done()
		if inFlight.Load() >= n {
			once.Do(func() { close(allArrived) })
		}
		// Hold every call until all n are in flight. A serial loop never gets
		// there and falls through on the timeout with peak == 1.
		select {
		case <-allArrived:
		case <-time.After(500 * time.Millisecond):
		}
		return okStatsResult(), nil
	}

	got, err := NewClientWithAPI(mock).GetAllContainerStats(context.Background())
	require.NoError(t, err)

	assert.Equal(t, int32(n), peak.Load(), "all stats calls should overlap")
	require.Len(t, got, n)
	for i, s := range got {
		assert.Equal(t, fmt.Sprintf("c%d", i), s.Name, "results keep list order")
	}
}

func TestClient_GetAllContainerStats_BoundsConcurrency(t *testing.T) {
	const n = statsConcurrencyCap + 4

	mock := NewMockDockerAPI()
	mock.ContainerListFunc = statsListFunc(n)

	var inFlight, peak atomic.Int32
	mock.ContainerStatsFunc = func(ctx context.Context, _ string, _ client.ContainerStatsOptions) (client.ContainerStatsResult, error) {
		done := trackPeak(&inFlight, &peak)
		defer done()
		time.Sleep(30 * time.Millisecond) // let overlapping calls pile up
		return okStatsResult(), nil
	}

	got, err := NewClientWithAPI(mock).GetAllContainerStats(context.Background())
	require.NoError(t, err)

	assert.Len(t, got, n)
	assert.LessOrEqual(t, peak.Load(), int32(statsConcurrencyCap))
	assert.Greater(t, peak.Load(), int32(1), "calls should overlap")
}

func TestClient_GetAllContainerStats_CancelledContext(t *testing.T) {
	mock := NewMockDockerAPI()
	mock.ContainerListFunc = statsListFunc(statsConcurrencyCap + 2)
	mock.ContainerStatsFunc = func(ctx context.Context, _ string, _ client.ContainerStatsOptions) (client.ContainerStatsResult, error) {
		return client.ContainerStatsResult{}, ctx.Err()
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := NewClientWithAPI(mock).GetAllContainerStats(ctx)
	require.NoError(t, err)
	assert.Empty(t, got, "a cancelled context yields no stats and no error")
}

// nameLookupCases drives the by-name lookups: the daemon-side name filter is a
// regex, so the client sends an anchored, escaped pattern and still verifies
// the exact name on what comes back (#803).
var nameLookupCases = []struct {
	name        string
	lookup      string
	wantName    string
	wantPattern string
}{
	{name: "plain", lookup: "web", wantName: "web", wantPattern: "^/web$"},
	{name: "leading slash", lookup: "/web", wantName: "web", wantPattern: "^/web$"},
	{name: "dot is escaped", lookup: "my.app", wantName: "my.app", wantPattern: `^/my\.app$`},
	{name: "regex metacharacter is escaped", lookup: "a(b", wantName: "a(b", wantPattern: `^/a\(b$`},
}

// nameLookupMock returns a decoy container with a longer name plus the exact
// one, and records the options of the last ContainerList call.
func nameLookupMock(wantName string, captured *client.ContainerListOptions) *MockDockerAPI {
	mock := NewMockDockerAPI()
	mock.ContainerListFunc = func(_ context.Context, options client.ContainerListOptions) (client.ContainerListResult, error) {
		*captured = options
		return client.ContainerListResult{Items: []container.Summary{
			makeTestContainer("dec123456789", wantName+"-2", "decoy:latest", "running"),
			makeTestContainer("abc123456789", wantName, "nginx:latest", "running"),
		}}, nil
	}
	return mock
}

func TestClient_GetContainerByName_SendsAnchoredNameFilter(t *testing.T) {
	for _, tt := range nameLookupCases {
		t.Run(tt.name, func(t *testing.T) {
			var captured client.ContainerListOptions
			mock := nameLookupMock(tt.wantName, &captured)

			got, err := NewClientWithAPI(mock).GetContainerByName(context.Background(), tt.lookup)
			require.NoError(t, err)

			assert.Equal(t, tt.wantName, got.Name, "exact name wins over the longer decoy")
			assert.Equal(t, "nginx:latest", got.Image)
			assert.Equal(t, []string{"8080:80/tcp"}, got.Ports, "lookup keeps the ports ListContainers reports")
			assert.True(t, captured.All, "stopped containers stay visible")
			assert.Equal(t, map[string]bool{tt.wantPattern: true}, captured.Filters["name"])
			assert.Equal(t, 1, mock.ContainerListCalls)
		})
	}
}

func TestClient_GetContainerByName_NotFoundAmongDecoys(t *testing.T) {
	mock := NewMockDockerAPI()
	mock.ContainerListFunc = func(context.Context, client.ContainerListOptions) (client.ContainerListResult, error) {
		return client.ContainerListResult{Items: []container.Summary{
			makeTestContainer("dec123456789", "web-2", "decoy:latest", "running"),
		}}, nil
	}

	_, err := NewClientWithAPI(mock).GetContainerByName(context.Background(), "web")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "container not found: web")
}

func TestClient_Exists_SendsAnchoredNameFilter(t *testing.T) {
	for _, tt := range nameLookupCases {
		t.Run(tt.name, func(t *testing.T) {
			var captured client.ContainerListOptions
			mock := nameLookupMock(tt.wantName, &captured)

			got, err := NewClientWithAPI(mock).Exists(context.Background(), tt.lookup)
			require.NoError(t, err)

			assert.True(t, got)
			assert.True(t, captured.All, "stopped containers stay visible")
			assert.Equal(t, map[string]bool{tt.wantPattern: true}, captured.Filters["name"])
		})
	}
}

func TestClient_Exists_FalseAmongDecoys(t *testing.T) {
	mock := NewMockDockerAPI()
	mock.ContainerListFunc = func(context.Context, client.ContainerListOptions) (client.ContainerListResult, error) {
		return client.ContainerListResult{Items: []container.Summary{
			makeTestContainer("dec123456789", "web-2", "decoy:latest", "running"),
		}}, nil
	}

	got, err := NewClientWithAPI(mock).Exists(context.Background(), "web")
	require.NoError(t, err)
	assert.False(t, got, "a substring match from the daemon is not an exact match")
}

package reconcile

import (
	"context"
	"testing"
	"time"

	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/docker/dockertest"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvaluateRestartBreaker(t *testing.T) {
	now := time.Date(2026, 3, 13, 12, 0, 0, 0, time.UTC)
	threshold := 5
	window := 10 * time.Minute

	t.Run("first observation records baseline", func(t *testing.T) {
		current := map[string]int{"web": 3}
		tracked := map[string]RestartTrackingEntry{}

		result := evaluateRestartBreaker(current, tracked, threshold, window, now)

		assert.Empty(t, result.Tripped)
		assert.Empty(t, result.Resolved)
		assert.Equal(t, 3, result.Updated["web"].RestartCount)
		assert.Equal(t, now, result.Updated["web"].CheckedAt)
	})

	t.Run("stable container below threshold", func(t *testing.T) {
		current := map[string]int{"web": 5}
		tracked := map[string]RestartTrackingEntry{
			"web": {RestartCount: 3, CheckedAt: now.Add(-5 * time.Minute)},
		}

		result := evaluateRestartBreaker(current, tracked, threshold, window, now)

		assert.Empty(t, result.Tripped)
		assert.Empty(t, result.Resolved)
		// Delta is 2 (below 5), keeps original baseline
		assert.Equal(t, 3, result.Updated["web"].RestartCount)
	})

	t.Run("trips when delta exceeds threshold within window", func(t *testing.T) {
		current := map[string]int{"web": 10}
		tracked := map[string]RestartTrackingEntry{
			"web": {RestartCount: 4, CheckedAt: now.Add(-5 * time.Minute)},
		}

		result := evaluateRestartBreaker(current, tracked, threshold, window, now)

		assert.Equal(t, []string{"web"}, result.Tripped)
		assert.Empty(t, result.Resolved)
		assert.True(t, result.Updated["web"].Tripped)
		assert.Equal(t, now, result.Updated["web"].TrippedAt)
	})

	t.Run("does not trip when outside window", func(t *testing.T) {
		current := map[string]int{"web": 20}
		tracked := map[string]RestartTrackingEntry{
			"web": {RestartCount: 4, CheckedAt: now.Add(-15 * time.Minute)},
		}

		result := evaluateRestartBreaker(current, tracked, threshold, window, now)

		assert.Empty(t, result.Tripped)
		// Baseline resets when outside window
		assert.Equal(t, 20, result.Updated["web"].RestartCount)
		assert.Equal(t, now, result.Updated["web"].CheckedAt)
	})

	t.Run("no restarts resets baseline", func(t *testing.T) {
		current := map[string]int{"web": 10}
		tracked := map[string]RestartTrackingEntry{
			"web": {RestartCount: 10, CheckedAt: now.Add(-5 * time.Minute)},
		}

		result := evaluateRestartBreaker(current, tracked, threshold, window, now)

		assert.Empty(t, result.Tripped)
		assert.Equal(t, 10, result.Updated["web"].RestartCount)
		assert.Equal(t, now, result.Updated["web"].CheckedAt)
	})

	t.Run("resolved when restart count stabilizes after trip", func(t *testing.T) {
		current := map[string]int{"web": 15}
		tracked := map[string]RestartTrackingEntry{
			"web": {RestartCount: 15, CheckedAt: now.Add(-5 * time.Minute), Tripped: true, TrippedAt: now.Add(-5 * time.Minute)},
		}

		result := evaluateRestartBreaker(current, tracked, threshold, window, now)

		assert.Empty(t, result.Tripped)
		assert.Equal(t, []string{"web"}, result.Resolved)
		assert.False(t, result.Updated["web"].Tripped)
	})

	t.Run("stays tripped when restarts continue", func(t *testing.T) {
		current := map[string]int{"web": 20}
		tracked := map[string]RestartTrackingEntry{
			"web": {RestartCount: 15, CheckedAt: now.Add(-5 * time.Minute), Tripped: true, TrippedAt: now.Add(-5 * time.Minute)},
		}

		result := evaluateRestartBreaker(current, tracked, threshold, window, now)

		assert.Empty(t, result.Tripped)  // Already tripped, not re-tripped
		assert.Empty(t, result.Resolved) // Still accumulating
		assert.True(t, result.Updated["web"].Tripped)
	})

	t.Run("removed container cleaned up", func(t *testing.T) {
		current := map[string]int{} // web is gone
		tracked := map[string]RestartTrackingEntry{
			"web": {RestartCount: 10, CheckedAt: now.Add(-5 * time.Minute)},
		}

		result := evaluateRestartBreaker(current, tracked, threshold, window, now)

		assert.Empty(t, result.Tripped)
		assert.Empty(t, result.Resolved)
		assert.NotContains(t, result.Updated, "web")
	})

	// Regression test for #348 (defect 1): tripping the breaker stops the
	// container, so it drops out of collectRestartCounts (running/restarting
	// only) and out of `current` on the very next check. Without preserving
	// it here, the tripped record is silently pruned -- no resolved alert
	// ever fires, and a later restart is treated as a brand-new baseline.
	t.Run("tripped entry survives when the stopped container disappears from current", func(t *testing.T) {
		current := map[string]int{} // breaker stopped "web"; it's no longer running
		tracked := map[string]RestartTrackingEntry{
			"web": {RestartCount: 15, CheckedAt: now.Add(-5 * time.Minute), Tripped: true, TrippedAt: now.Add(-5 * time.Minute)},
		}

		result := evaluateRestartBreaker(current, tracked, threshold, window, now)

		assert.Empty(t, result.Tripped)
		assert.Empty(t, result.Resolved, "no new observation, so no resolution should fire yet")
		require.Contains(t, result.Updated, "web", "tripped entry must not be dropped just because the container isn't currently running")
		assert.True(t, result.Updated["web"].Tripped, "trip state must survive until the container is observed again")
		assert.Equal(t, 15, result.Updated["web"].RestartCount)
	})

	// Regression test for #348 (defect 2): resolution used to compare the
	// current count against the frozen trip-time baseline. RestartCount is
	// monotonic for the life of a container, so once any restart happens
	// after tripping, the count permanently exceeds that frozen baseline and
	// the breaker could never resolve again -- even after the container
	// fully stabilizes.
	t.Run("resolves after restarts stop, even though count climbed past the trip-time baseline", func(t *testing.T) {
		tracked := map[string]RestartTrackingEntry{
			"web": {RestartCount: 15, CheckedAt: now.Add(-10 * time.Minute), Tripped: true, TrippedAt: now.Add(-10 * time.Minute)},
		}

		// Cycle 1: still crash-looping after the trip; count climbs past 15.
		result1 := evaluateRestartBreaker(map[string]int{"web": 20}, tracked, threshold, window, now.Add(-5*time.Minute))
		assert.Empty(t, result1.Resolved, "still accumulating restarts, must not resolve yet")
		require.True(t, result1.Updated["web"].Tripped)
		assert.Equal(t, 20, result1.Updated["web"].RestartCount, "rolling baseline must advance to the latest observed count")

		// Cycle 2: no further restarts since cycle 1 -- container has stabilized.
		result2 := evaluateRestartBreaker(map[string]int{"web": 20}, result1.Updated, threshold, window, now)
		assert.Equal(t, []string{"web"}, result2.Resolved, "must resolve once restarts stop, regardless of the pre-trip baseline")
		assert.False(t, result2.Updated["web"].Tripped)
	})

	t.Run("multiple containers mixed state", func(t *testing.T) {
		current := map[string]int{"web": 20, "db": 2, "cache": 8}
		tracked := map[string]RestartTrackingEntry{
			"web":   {RestartCount: 14, CheckedAt: now.Add(-3 * time.Minute)},
			"db":    {RestartCount: 1, CheckedAt: now.Add(-5 * time.Minute)},
			"cache": {RestartCount: 8, CheckedAt: now.Add(-5 * time.Minute)},
		}

		result := evaluateRestartBreaker(current, tracked, threshold, window, now)

		assert.Equal(t, []string{"web"}, result.Tripped) // delta=6 >= 5
		assert.Empty(t, result.Resolved)
		assert.True(t, result.Updated["web"].Tripped)
		assert.False(t, result.Updated["db"].Tripped)   // delta=1 < 5
		assert.False(t, result.Updated["cache"].Tripped) // delta=0
	})
}

func TestRunRestartBreaker(t *testing.T) {
	fullID := "abc123def456abc123def456abc123def456abc123def456abc123def456abcd"

	t.Run("stops container when tripped", func(t *testing.T) {
		var stoppedContainer string
		mockAPI := &dockertest.MockDockerAPI{
			ContainerInspectFunc: func(_ context.Context, id string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
				return client.ContainerInspectResult{
					Container: container.InspectResponse{
						ID:           fullID,
						Name:         "/test-web-1",
						RestartCount: 12,
						State: &container.State{
							Status:    "running",
							StartedAt: "2026-03-13T10:00:00Z",
						},
						Config: &container.Config{
							Image:  "nginx",
							Labels: map[string]string{},
							Env:    []string{},
						},
						NetworkSettings: &container.NetworkSettings{
							Networks: map[string]*network.EndpointSettings{},
						},
					},
				}, nil
			},
			ContainerStopFunc: func(_ context.Context, id string, _ client.ContainerStopOptions) (client.ContainerStopResult, error) {
				stoppedContainer = id
				return client.ContainerStopResult{}, nil
			},
		}
		client := docker.NewClientWithAPI(mockAPI)

		actual := []ActualService{
			{Name: "web", ContainerName: "test-web-1", State: "running"},
		}
		state := &DeployState{
			RestartTracking: map[string]RestartTrackingEntry{
				"web": {RestartCount: 5, CheckedAt: time.Now().Add(-3 * time.Minute)},
			},
		}

		result, err := RunRestartBreaker(
			context.Background(), client, actual, state, 5, 10*time.Minute,
		)
		require.NoError(t, err)
		assert.Equal(t, []string{"web"}, result.Tripped)
		assert.Equal(t, "test-web-1", stoppedContainer)
	})

	t.Run("no action when below threshold", func(t *testing.T) {
		mockAPI := &dockertest.MockDockerAPI{
			ContainerInspectFunc: func(_ context.Context, id string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
				return client.ContainerInspectResult{
					Container: container.InspectResponse{
						ID:           fullID,
						Name:         "/test-web-1",
						RestartCount: 3,
						State: &container.State{
							Status:    "running",
							StartedAt: "2026-03-13T10:00:00Z",
						},
						Config: &container.Config{
							Image:  "nginx",
							Labels: map[string]string{},
							Env:    []string{},
						},
						NetworkSettings: &container.NetworkSettings{
							Networks: map[string]*network.EndpointSettings{},
						},
					},
				}, nil
			},
		}
		client := docker.NewClientWithAPI(mockAPI)

		actual := []ActualService{
			{Name: "web", ContainerName: "test-web-1", State: "running"},
		}
		state := &DeployState{
			RestartTracking: map[string]RestartTrackingEntry{
				"web": {RestartCount: 1, CheckedAt: time.Now().Add(-3 * time.Minute)},
			},
		}

		result, err := RunRestartBreaker(
			context.Background(), client, actual, state, 5, 10*time.Minute,
		)
		require.NoError(t, err)
		assert.Empty(t, result.Tripped)
		assert.Equal(t, 0, mockAPI.ContainerStopCalls)
	})

	t.Run("skips non-running containers", func(t *testing.T) {
		mockAPI := &dockertest.MockDockerAPI{}
		client := docker.NewClientWithAPI(mockAPI)

		actual := []ActualService{
			{Name: "web", ContainerName: "test-web-1", State: "exited"},
		}
		state := &DeployState{}

		result, err := RunRestartBreaker(
			context.Background(), client, actual, state, 5, 10*time.Minute,
		)
		require.NoError(t, err)
		assert.Empty(t, result.Tripped)
		assert.Equal(t, 0, mockAPI.ContainerInspectCalls)
	})
}

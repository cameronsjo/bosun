package reconcile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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

func evaluateRestartBreakerCounts(
	current map[string]int,
	tracked map[string]RestartTrackingEntry,
	threshold int,
	window time.Duration,
	now time.Time,
) *RestartBreakerResult {
	observations := make(map[string]restartObservation, len(current))
	for service, count := range current {
		observations[service] = restartObservation{RestartCount: count}
	}
	return evaluateRestartBreaker(observations, tracked, threshold, window, now)
}

func TestEvaluateRestartBreaker(t *testing.T) {
	now := time.Date(2026, 3, 13, 12, 0, 0, 0, time.UTC)
	threshold := 5
	window := 10 * time.Minute

	t.Run("first observation records baseline", func(t *testing.T) {
		current := map[string]int{"web": 3}
		tracked := map[string]RestartTrackingEntry{}

		result := evaluateRestartBreakerCounts(current, tracked, threshold, window, now)

		assert.Empty(t, result.Tripped)
		assert.Empty(t, result.Resolved)
		assert.Equal(t, 3, result.Updated["web"].RestartCount)
		assert.Equal(t, now, result.Updated["web"].CheckedAt)
		assert.Equal(t, 3, result.Updated["web"].BaselineRestartCount)
		assert.Equal(t, now, result.Updated["web"].BaselineAt)
	})

	t.Run("stable container below threshold", func(t *testing.T) {
		current := map[string]int{"web": 5}
		tracked := map[string]RestartTrackingEntry{
			"web": {RestartCount: 3, CheckedAt: now.Add(-5 * time.Minute)},
		}

		result := evaluateRestartBreakerCounts(current, tracked, threshold, window, now)

		assert.Empty(t, result.Tripped)
		assert.Empty(t, result.Resolved)
		// The latest observation advances while the accumulating baseline stays put.
		assert.Equal(t, 5, result.Updated["web"].RestartCount)
		assert.Equal(t, 3, result.Updated["web"].BaselineRestartCount)
		assert.Equal(t, now.Add(-5*time.Minute), result.Updated["web"].BaselineAt)
	})

	t.Run("trips when delta exceeds threshold within window", func(t *testing.T) {
		current := map[string]int{"web": 10}
		tracked := map[string]RestartTrackingEntry{
			"web": {RestartCount: 4, CheckedAt: now.Add(-5 * time.Minute)},
		}

		result := evaluateRestartBreakerCounts(current, tracked, threshold, window, now)

		assert.Equal(t, []string{"web"}, result.Tripped)
		assert.Empty(t, result.Resolved)
		assert.True(t, result.Updated["web"].Tripped)
		assert.False(t, result.Updated["web"].StabilityPending,
			"the pre-stop trip sample is not a post-recovery stability candidate")
		assert.Equal(t, now, result.Updated["web"].TrippedAt)
	})

	t.Run("slow loop trips across samples farther apart than window", func(t *testing.T) {
		tracked := map[string]RestartTrackingEntry{
			"web": {RestartCount: 0, CheckedAt: now},
		}

		first := evaluateRestartBreakerCounts(map[string]int{"web": 2}, tracked, threshold, window, now.Add(15*time.Minute))
		assert.Empty(t, first.Tripped)
		assert.Equal(t, 2, first.Updated["web"].RestartCount)
		assert.Equal(t, 0, first.Updated["web"].BaselineRestartCount)
		assert.Equal(t, now, first.Updated["web"].BaselineAt)

		second := evaluateRestartBreakerCounts(map[string]int{"web": 4}, first.Updated, threshold, window, now.Add(30*time.Minute))
		assert.Empty(t, second.Tripped)
		assert.Equal(t, 4, second.Updated["web"].RestartCount)
		assert.Equal(t, 0, second.Updated["web"].BaselineRestartCount)
		assert.Equal(t, now, second.Updated["web"].BaselineAt)

		third := evaluateRestartBreakerCounts(map[string]int{"web": 5}, second.Updated, threshold, window, now.Add(45*time.Minute))
		assert.Equal(t, []string{"web"}, third.Tripped)
		assert.True(t, third.Updated["web"].Tripped)
	})

	t.Run("no restarts resets baseline", func(t *testing.T) {
		current := map[string]int{"web": 10}
		tracked := map[string]RestartTrackingEntry{
			"web": {RestartCount: 10, CheckedAt: now.Add(-5 * time.Minute)},
		}

		result := evaluateRestartBreakerCounts(current, tracked, threshold, window, now)

		assert.Empty(t, result.Tripped)
		assert.Equal(t, 10, result.Updated["web"].RestartCount)
		assert.Equal(t, now, result.Updated["web"].CheckedAt)
		assert.Equal(t, 10, result.Updated["web"].BaselineRestartCount)
		assert.Equal(t, now, result.Updated["web"].BaselineAt)
	})

	t.Run("clean sample resets a preserved slow-loop baseline", func(t *testing.T) {
		tracked := map[string]RestartTrackingEntry{
			"web": {
				RestartCount:         2,
				CheckedAt:            now.Add(-15 * time.Minute),
				BaselineRestartCount: 0,
				BaselineAt:           now.Add(-30 * time.Minute),
			},
		}

		result := evaluateRestartBreakerCounts(map[string]int{"web": 2}, tracked, threshold, window, now)

		assert.Empty(t, result.Tripped)
		assert.Equal(t, 2, result.Updated["web"].BaselineRestartCount)
		assert.Equal(t, now, result.Updated["web"].BaselineAt)
	})

	t.Run("resolved after a full stable check cycle", func(t *testing.T) {
		current := map[string]restartObservation{"web": {ContainerID: "container-1", RestartCount: 15}}
		tracked := map[string]RestartTrackingEntry{
			"web": {ContainerID: "container-1", RestartCount: 15, CheckedAt: now.Add(-5 * time.Minute), Tripped: true, TrippedAt: now.Add(-5 * time.Minute), StabilityPending: true},
		}

		result := evaluateRestartBreaker(current, tracked, threshold, window, now)
		assert.Empty(t, result.Tripped)
		assert.Equal(t, []string{"web"}, result.Resolved)
		assert.False(t, result.Updated["web"].Tripped)
	})

	t.Run("same-container count decrease starts a new stability interval", func(t *testing.T) {
		tracked := map[string]RestartTrackingEntry{
			"web": {ContainerID: "container-1", RestartCount: 15, CheckedAt: now.Add(-5 * time.Minute), Tripped: true, StabilityPending: true},
		}
		lower := map[string]restartObservation{"web": {ContainerID: "container-1", RestartCount: 0}}

		first := evaluateRestartBreaker(lower, tracked, threshold, window, now)
		second := evaluateRestartBreaker(lower, first.Updated, threshold, window, now.Add(time.Minute))

		assert.Empty(t, first.Resolved, "a counter reset is not itself a clean interval")
		assert.Equal(t, []string{"web"}, second.Resolved)
	})

	t.Run("recreation resets count without resolving", func(t *testing.T) {
		tracked := map[string]RestartTrackingEntry{
			"web": {ContainerID: "old-container", RestartCount: 15, CheckedAt: now.Add(-5 * time.Minute), Tripped: true, TrippedAt: now.Add(-5 * time.Minute)},
		}

		recreated := evaluateRestartBreaker(map[string]restartObservation{
			"web": {ContainerID: "new-container", RestartCount: 0},
		}, tracked, threshold, window, now)
		assert.Empty(t, recreated.Resolved)
		assert.True(t, recreated.Updated["web"].Tripped)
		assert.Equal(t, "new-container", recreated.Updated["web"].ContainerID)

		looping := evaluateRestartBreaker(map[string]restartObservation{
			"web": {ContainerID: "new-container", RestartCount: 2},
		}, recreated.Updated, threshold, window, now.Add(time.Minute))
		assert.Empty(t, looping.Resolved)
		assert.True(t, looping.Updated["web"].Tripped)

		stable := evaluateRestartBreaker(map[string]restartObservation{
			"web": {ContainerID: "new-container", RestartCount: 2},
		}, looping.Updated, threshold, window, now.Add(2*time.Minute))
		assert.Equal(t, []string{"web"}, stable.Resolved)
	})

	t.Run("successive recreations restart the stability grace", func(t *testing.T) {
		tracked := map[string]RestartTrackingEntry{
			"web": {ContainerID: "container-1", RestartCount: 15, CheckedAt: now.Add(-5 * time.Minute), Tripped: true, StabilityPending: true},
		}

		first := evaluateRestartBreaker(map[string]restartObservation{
			"web": {ContainerID: "container-2", RestartCount: 0},
		}, tracked, threshold, window, now)
		second := evaluateRestartBreaker(map[string]restartObservation{
			"web": {ContainerID: "container-3", RestartCount: 0},
		}, first.Updated, threshold, window, now.Add(time.Minute))

		assert.Empty(t, first.Resolved)
		assert.Empty(t, second.Resolved)
		assert.True(t, second.Updated["web"].Tripped)
		assert.Equal(t, "container-3", second.Updated["web"].ContainerID)
	})

	t.Run("legacy tripped entry requires two observations of the current identity", func(t *testing.T) {
		tracked := map[string]RestartTrackingEntry{
			"web": {RestartCount: 15, CheckedAt: now.Add(-5 * time.Minute), Tripped: true},
		}
		current := map[string]restartObservation{"web": {ContainerID: "container-1", RestartCount: 0}}

		first := evaluateRestartBreaker(current, tracked, threshold, window, now)
		second := evaluateRestartBreaker(current, first.Updated, threshold, window, now.Add(time.Minute))

		assert.Empty(t, first.Resolved)
		assert.Equal(t, []string{"web"}, second.Resolved)
	})

	t.Run("untripped recreation starts a fresh restart baseline", func(t *testing.T) {
		tracked := map[string]RestartTrackingEntry{
			"web": {
				ContainerID:          "container-1",
				RestartCount:         4,
				CheckedAt:            now.Add(-5 * time.Minute),
				BaselineRestartCount: 0,
				BaselineAt:           now.Add(-10 * time.Minute),
			},
		}

		result := evaluateRestartBreaker(map[string]restartObservation{
			"web": {ContainerID: "container-2", RestartCount: 6},
		}, tracked, threshold, window, now)

		assert.Empty(t, result.Tripped, "counts from different container identities must not accumulate")
		assert.Equal(t, 6, result.Updated["web"].BaselineRestartCount)
		assert.Equal(t, "container-2", result.Updated["web"].ContainerID)
	})

	t.Run("stays tripped when restarts continue", func(t *testing.T) {
		current := map[string]int{"web": 20}
		tracked := map[string]RestartTrackingEntry{
			"web": {RestartCount: 15, CheckedAt: now.Add(-5 * time.Minute), Tripped: true, TrippedAt: now.Add(-5 * time.Minute)},
		}

		result := evaluateRestartBreakerCounts(current, tracked, threshold, window, now)

		assert.Empty(t, result.Tripped)  // Already tripped, not re-tripped
		assert.Empty(t, result.Resolved) // Still accumulating
		assert.True(t, result.Updated["web"].Tripped)
	})

	t.Run("removed container cleaned up", func(t *testing.T) {
		current := map[string]int{} // web is gone
		tracked := map[string]RestartTrackingEntry{
			"web": {RestartCount: 10, CheckedAt: now.Add(-5 * time.Minute)},
		}

		result := evaluateRestartBreakerCounts(current, tracked, threshold, window, now)

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
			"web": {
				ContainerID:      "container-1",
				RestartCount:     15,
				CheckedAt:        now.Add(-5 * time.Minute),
				Tripped:          true,
				TrippedAt:        now.Add(-5 * time.Minute),
				StabilityPending: true,
			},
		}

		result := evaluateRestartBreakerCounts(current, tracked, threshold, window, now)

		assert.Empty(t, result.Tripped)
		assert.Empty(t, result.Resolved, "no new observation, so no resolution should fire yet")
		require.Contains(t, result.Updated, "web", "tripped entry must not be dropped just because the container isn't currently running")
		assert.True(t, result.Updated["web"].Tripped, "trip state must survive until the container is observed again")
		assert.Equal(t, 15, result.Updated["web"].RestartCount)
		assert.False(t, result.Updated["web"].StabilityPending,
			"an absent container must invalidate any pre-stop stability candidate")

		firstReturn := evaluateRestartBreaker(map[string]restartObservation{
			"web": {ContainerID: "container-1", RestartCount: 15},
		}, result.Updated, threshold, window, now.Add(time.Minute))
		assert.Empty(t, firstReturn.Resolved,
			"the first post-recovery observation only starts the grace interval")
		assert.True(t, firstReturn.Updated["web"].StabilityPending)

		confirmed := evaluateRestartBreaker(map[string]restartObservation{
			"web": {ContainerID: "container-1", RestartCount: 15},
		}, firstReturn.Updated, threshold, window, now.Add(2*time.Minute))
		assert.Equal(t, []string{"web"}, confirmed.Resolved)
	})

	// Regression test for #348 (defect 2): resolution used to compare the
	// current count against the frozen trip-time baseline. RestartCount is
	// monotonic for the life of a container, so once any restart happens
	// after tripping, the count permanently exceeds that frozen baseline and
	// the breaker could never resolve again -- even after the container
	// fully stabilizes.
	t.Run("resolves after restarts stop, even though count climbed past the trip-time baseline", func(t *testing.T) {
		tracked := map[string]RestartTrackingEntry{
			"web": {ContainerID: "container-1", RestartCount: 15, CheckedAt: now.Add(-10 * time.Minute), Tripped: true, TrippedAt: now.Add(-10 * time.Minute)},
		}

		// Cycle 1: still crash-looping after the trip; count climbs past 15.
		result1 := evaluateRestartBreaker(map[string]restartObservation{
			"web": {ContainerID: "container-1", RestartCount: 20},
		}, tracked, threshold, window, now.Add(-5*time.Minute))
		assert.Empty(t, result1.Resolved, "still accumulating restarts, must not resolve yet")
		require.True(t, result1.Updated["web"].Tripped)
		assert.Equal(t, 20, result1.Updated["web"].RestartCount, "rolling baseline must advance to the latest observed count")

		// Cycle 2: no further restarts since cycle 1 -- container has stabilized.
		result2 := evaluateRestartBreaker(map[string]restartObservation{
			"web": {ContainerID: "container-1", RestartCount: 20},
		}, result1.Updated, threshold, window, now)
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

		result := evaluateRestartBreakerCounts(current, tracked, threshold, window, now)

		assert.Equal(t, []string{"web"}, result.Tripped) // delta=6 >= 5
		assert.Empty(t, result.Resolved)
		assert.True(t, result.Updated["web"].Tripped)
		assert.False(t, result.Updated["db"].Tripped)    // delta=1 < 5
		assert.False(t, result.Updated["cache"].Tripped) // delta=0
	})
}

func TestRestartBreakerSamplingMismatch(t *testing.T) {
	tests := []struct {
		name          string
		driftInterval time.Duration
		restartWindow time.Duration
		want          bool
	}{
		{name: "sparse sampling", driftInterval: 15 * time.Minute, restartWindow: 10 * time.Minute, want: true},
		{name: "equal cadence", driftInterval: 10 * time.Minute, restartWindow: 10 * time.Minute, want: false},
		{name: "faster sampling", driftInterval: 5 * time.Minute, restartWindow: 10 * time.Minute, want: false},
		{name: "disabled drift checks", driftInterval: 0, restartWindow: 10 * time.Minute, want: false},
		{name: "invalid restart window", driftInterval: 5 * time.Minute, restartWindow: 0, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, RestartBreakerSamplingMismatch(tt.driftInterval, tt.restartWindow))
		})
	}
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
							Labels: composeLabels("test", "web"),
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
			context.Background(), client, RestartBreakerScope{ProjectName: "test"}, actual, state, 5, 10*time.Minute,
		)
		require.NoError(t, err)
		assert.Equal(t, []string{"web"}, result.Tripped)
		assert.Equal(t, fullID, stoppedContainer, "the stop must target the inspected container ID, not its name")
		assert.Equal(t, fullID[:12], result.Updated["web"].ContainerID)
	})

	t.Run("empty project scope stops no containers or advances tracking", func(t *testing.T) {
		mockAPI := &dockertest.MockDockerAPI{
			ContainerInspectFunc: func(_ context.Context, _ string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
				t.Fatal("an unresolved scope must not inspect containers")
				return client.ContainerInspectResult{}, nil
			},
		}
		dockerClient := docker.NewClientWithAPI(mockAPI)
		tracking := map[string]RestartTrackingEntry{
			"web": {RestartCount: 4, CheckedAt: time.Now().Add(-3 * time.Minute)},
		}
		state := &DeployState{RestartTracking: tracking}
		actual := []ActualService{{Name: "web", ContainerName: "foreign-web-1", State: "running"}}

		result, err := RunRestartBreaker(
			context.Background(), dockerClient, RestartBreakerScope{}, actual, state, 5, 10*time.Minute,
		)

		require.NoError(t, err)
		assert.Empty(t, result.Tripped)
		assert.Empty(t, result.Resolved)
		assert.Equal(t, tracking, result.Updated, "unresolved scope must preserve existing tracking")
		assert.Equal(t, 0, mockAPI.ContainerInspectCalls)
		assert.Equal(t, 0, mockAPI.ContainerStopCalls)
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
							Labels: composeLabels("test", "web"),
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
			context.Background(), client, RestartBreakerScope{ProjectName: "test"}, actual, state, 5, 10*time.Minute,
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
			context.Background(), client, RestartBreakerScope{ProjectName: "test"}, actual, state, 5, 10*time.Minute,
		)
		require.NoError(t, err)
		assert.Empty(t, result.Tripped)
		assert.Equal(t, 0, mockAPI.ContainerInspectCalls)
	})

	t.Run("stopped tripped container invalidates a pending stability candidate", func(t *testing.T) {
		mockAPI := &dockertest.MockDockerAPI{}
		client := docker.NewClientWithAPI(mockAPI)
		actual := []ActualService{{Name: "web", ContainerName: "test-web-1", State: "exited"}}
		state := &DeployState{RestartTracking: map[string]RestartTrackingEntry{
			"web": {
				ContainerID:      "abc123def456",
				RestartCount:     12,
				Tripped:          true,
				StabilityPending: true,
			},
		}}

		result, err := RunRestartBreaker(context.Background(), client, RestartBreakerScope{ProjectName: "test"}, actual, state, 5, 10*time.Minute)

		require.NoError(t, err)
		require.Contains(t, result.Updated, "web")
		assert.True(t, result.Updated["web"].Tripped)
		assert.False(t, result.Updated["web"].StabilityPending)
		assert.Empty(t, result.Resolved)
		assert.Equal(t, 0, mockAPI.ContainerInspectCalls)
	})

	t.Run("partial inspect failure preserves the unobserved baseline", func(t *testing.T) {
		inspectErr := errors.New("temporary inspect failure")
		mockAPI := &dockertest.MockDockerAPI{
			ContainerInspectFunc: func(_ context.Context, id string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
				if id == "test-web-1" {
					return client.ContainerInspectResult{}, inspectErr
				}
				return client.ContainerInspectResult{
					Container: container.InspectResponse{
						ID:           fullID,
						Name:         "/test-db-1",
						RestartCount: 2,
						State:        &container.State{Status: "running"},
						Config:       &container.Config{Image: "postgres", Labels: composeLabels("test", "db")},
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
			{Name: "db", ContainerName: "test-db-1", State: "running"},
		}
		webEntry := RestartTrackingEntry{
			RestartCount:         3,
			CheckedAt:            time.Now().Add(-15 * time.Minute),
			BaselineRestartCount: 1,
			BaselineAt:           time.Now().Add(-30 * time.Minute),
			ContainerID:          "persisted-web-id",
			Tripped:              true,
			StabilityPending:     true,
		}
		state := &DeployState{
			RestartTracking: map[string]RestartTrackingEntry{
				"web": webEntry,
				"db":  {RestartCount: 2, CheckedAt: time.Now().Add(-5 * time.Minute)},
			},
		}

		result, err := RunRestartBreaker(context.Background(), client, RestartBreakerScope{ProjectName: "test"}, actual, state, 5, 10*time.Minute)

		require.NoError(t, err)
		assert.Equal(t, webEntry, result.Updated["web"])
		assert.Contains(t, result.Updated, "db")
	})

	t.Run("total inspect failure preserves pending stability state", func(t *testing.T) {
		mockAPI := &dockertest.MockDockerAPI{
			ContainerInspectFunc: func(_ context.Context, _ string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
				return client.ContainerInspectResult{}, errors.New("temporary inspect failure")
			},
		}
		client := docker.NewClientWithAPI(mockAPI)
		actual := []ActualService{{Name: "web", ContainerName: "test-web-1", State: "running"}}
		entry := RestartTrackingEntry{
			ContainerID:      "persisted-web-id",
			RestartCount:     0,
			CheckedAt:        time.Now().Add(-5 * time.Minute),
			Tripped:          true,
			StabilityPending: true,
		}
		state := &DeployState{RestartTracking: map[string]RestartTrackingEntry{"web": entry}}

		result, err := RunRestartBreaker(context.Background(), client, RestartBreakerScope{ProjectName: "test"}, actual, state, 5, 10*time.Minute)

		require.NoError(t, err)
		assert.Equal(t, entry, result.Updated["web"])
		assert.Empty(t, result.Resolved, "an inspect error is not a clean stability observation")
	})
}

// composeLabels returns the Compose ownership labels for a project service.
func composeLabels(project, service string) map[string]string {
	return map[string]string{
		ComposeProjectLabel: project,
		ComposeServiceLabel: service,
	}
}

// composeLabelsAt adds the Compose working_dir label to composeLabels.
func composeLabelsAt(project, service, workingDir string) map[string]string {
	labels := composeLabels(project, service)
	labels[ComposeWorkingDirLabel] = workingDir
	return labels
}

// breakerInspectByName returns an inspect func that serves a restart-looping
// container per name with the given labels; names absent from labelsByName
// fail the test so an unexpected inspect is visible.
func breakerInspectByName(t *testing.T, restartCount int, labelsByName map[string]map[string]string) func(context.Context, string, client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	t.Helper()
	return func(_ context.Context, name string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
		labels, ok := labelsByName[name]
		if !ok {
			t.Errorf("unexpected inspect of %q", name)
		}
		return client.ContainerInspectResult{
			Container: container.InspectResponse{
				ID:              name + "-0123456789abcdef",
				Name:            "/" + name,
				RestartCount:    restartCount,
				State:           &container.State{Status: container.StateRunning},
				Config:          &container.Config{Image: "nginx", Labels: labels},
				NetworkSettings: &container.NetworkSettings{Networks: map[string]*network.EndpointSettings{}},
			},
		}, nil
	}
}

func TestRestartBreakerScopeOwnsContainer(t *testing.T) {
	ownedDir := filepath.Join(string(filepath.Separator), "srv", "appdata", "compose")
	project := RestartBreakerScope{ProjectName: "homelab"}
	fallback := RestartBreakerScope{ProjectName: ComposeFallbackProjectName, RequireWorkingDir: true, ComposeWorkingDir: ownedDir}

	tests := []struct {
		name    string
		scope   RestartBreakerScope
		labels  map[string]string
		service string
		want    bool
	}{
		{"labeled project service", project, composeLabels("homelab", "web"), "web", true},
		{"no labels (name-prefix match only)", project, nil, "web", false},
		{"empty labels", project, map[string]string{}, "web", false},
		{"project label without service label", project, map[string]string{ComposeProjectLabel: "homelab"}, "web", false},
		{"other project", project, composeLabels("other", "web"), "web", false},
		{"service label disagrees with matched service", project, composeLabels("homelab", "api"), "web", false},
		{"empty service", project, composeLabels("homelab", ""), "", false},
		{"unresolved scope", RestartBreakerScope{}, composeLabels("", "web"), "web", false},
		{"fallback with matching working dir", fallback, composeLabelsAt("compose", "web", ownedDir), "web", true},
		{"fallback with uncleaned matching working dir", fallback, composeLabelsAt("compose", "web", ownedDir+string(filepath.Separator)), "web", true},
		{"fallback without working dir label", fallback, composeLabels("compose", "web"), "web", false},
		{"fallback from a foreign compose directory", fallback, composeLabelsAt("compose", "web", filepath.Join(string(filepath.Separator), "home", "someone", "compose")), "web", false},
		{"fallback with relative working dir", fallback, composeLabelsAt("compose", "web", "compose"), "web", false},
		{"fallback with no directory to verify", RestartBreakerScope{ProjectName: ComposeFallbackProjectName, RequireWorkingDir: true}, composeLabelsAt("compose", "web", ownedDir), "web", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.scope.ownsContainer(tt.labels, tt.service))
		})
	}
}

func TestRestartBreakerScopeFor(t *testing.T) {
	appdata := filepath.Join(string(filepath.Separator), "srv", "appdata")

	t.Run("explicit project needs no extra marker", func(t *testing.T) {
		scope := RestartBreakerScopeFor(&Config{ProjectName: "homelab", LocalAppdataPath: appdata}, "homelab")
		assert.Equal(t, RestartBreakerScope{ProjectName: "homelab"}, scope)
		assert.Empty(t, scope.unresolvedReason())
	})

	t.Run("explicitly configured compose project is trusted as configured", func(t *testing.T) {
		scope := RestartBreakerScopeFor(&Config{ProjectName: ComposeFallbackProjectName}, ComposeFallbackProjectName)
		assert.False(t, scope.RequireWorkingDir)
		assert.Empty(t, scope.unresolvedReason())
	})

	t.Run("fallback project requires the local compose directory", func(t *testing.T) {
		scope := RestartBreakerScopeFor(&Config{LocalAppdataPath: appdata}, ComposeFallbackProjectName)
		assert.True(t, scope.RequireWorkingDir)
		assert.Equal(t, filepath.Join(appdata, "compose"), scope.ComposeWorkingDir)
		assert.Empty(t, scope.unresolvedReason())
	})

	t.Run("fallback project without appdata path is unresolved", func(t *testing.T) {
		scope := RestartBreakerScopeFor(&Config{}, ComposeFallbackProjectName)
		assert.True(t, scope.RequireWorkingDir)
		assert.NotEmpty(t, scope.unresolvedReason())
	})

	t.Run("nil config with fallback project is unresolved", func(t *testing.T) {
		assert.NotEmpty(t, RestartBreakerScopeFor(nil, ComposeFallbackProjectName).unresolvedReason())
	})

	t.Run("empty project stays unresolved", func(t *testing.T) {
		assert.NotEmpty(t, RestartBreakerScopeFor(&Config{LocalAppdataPath: appdata}, "").unresolvedReason())
	})
}

// TestRunRestartBreaker_UnlabeledPrefixContainerSurvives pins #717: an
// unlabeled container whose name starts with "<project>-" reaches the
// breaker's candidate list through CollectActualState's name fallback, but its
// labels do not prove ownership, so it must never be stopped. The labeled
// sibling in the same sample proves the breaker still acts on real members.
func TestRunRestartBreaker_UnlabeledPrefixContainerSurvives(t *testing.T) {
	var stopped []string
	mockAPI := &dockertest.MockDockerAPI{
		ContainerListFunc: func(_ context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
			return client.ContainerListResult{Items: []container.Summary{
				dockertest.MakeTestContainer("foreign", "test-web-1", "nginx", "running", "", ""),
				dockertest.MakeTestContainer("owned", "test-api-1", "api", "running", "test", "api"),
			}}, nil
		},
		ContainerInspectFunc: breakerInspectByName(t, 12, map[string]map[string]string{
			"test-web-1": nil,
			"test-api-1": composeLabels("test", "api"),
		}),
		ContainerStopFunc: func(_ context.Context, id string, _ client.ContainerStopOptions) (client.ContainerStopResult, error) {
			stopped = append(stopped, id)
			return client.ContainerStopResult{}, nil
		},
	}
	dockerClient := docker.NewClientWithAPI(mockAPI)

	actual, err := CollectActualState(context.Background(), dockerClient, "test")
	require.NoError(t, err)
	require.Len(t, actual, 2, "fixture must reach the breaker through the name-prefix fallback")

	webEntry := RestartTrackingEntry{RestartCount: 5, CheckedAt: time.Now().Add(-3 * time.Minute)}
	state := &DeployState{RestartTracking: map[string]RestartTrackingEntry{
		"web": webEntry,
		"api": {RestartCount: 5, CheckedAt: time.Now().Add(-3 * time.Minute)},
	}}

	result, err := RunRestartBreaker(context.Background(), dockerClient, RestartBreakerScope{ProjectName: "test"}, actual, state, 5, 10*time.Minute)

	require.NoError(t, err)
	assert.Equal(t, []string{"api"}, result.Tripped)
	assert.Equal(t, []string{"test-api-1-0123456789abcdef"}, stopped, "only the labeled project member may be stopped")
	assert.Equal(t, webEntry, result.Updated["web"], "an unproven container must not advance or reset tracking")
}

// TestRunRestartBreaker_ForeignComposeStackSurvives pins #717's second half: a
// stack Compose itself named "compose" (started from some other directory
// called compose) shares the fallback project name, so without a working_dir
// matching this target's compose directory it must not be stopped.
func TestRunRestartBreaker_ForeignComposeStackSurvives(t *testing.T) {
	appdata := t.TempDir()
	scope := RestartBreakerScopeFor(&Config{LocalAppdataPath: appdata}, ComposeFallbackProjectName)
	require.Empty(t, scope.unresolvedReason())

	var stopped []string
	mockAPI := &dockertest.MockDockerAPI{
		ContainerInspectFunc: breakerInspectByName(t, 12, map[string]map[string]string{
			"compose-web-1": composeLabelsAt("compose", "web", filepath.Join(t.TempDir(), "compose")),
			"compose-api-1": composeLabelsAt("compose", "api", filepath.Join(appdata, "compose")),
		}),
		ContainerStopFunc: func(_ context.Context, id string, _ client.ContainerStopOptions) (client.ContainerStopResult, error) {
			stopped = append(stopped, id)
			return client.ContainerStopResult{}, nil
		},
	}
	actual := []ActualService{
		{Name: "web", ContainerName: "compose-web-1", State: "running"},
		{Name: "api", ContainerName: "compose-api-1", State: "running"},
	}
	state := &DeployState{RestartTracking: map[string]RestartTrackingEntry{
		"web": {RestartCount: 5, CheckedAt: time.Now().Add(-3 * time.Minute)},
		"api": {RestartCount: 5, CheckedAt: time.Now().Add(-3 * time.Minute)},
	}}

	result, err := RunRestartBreaker(context.Background(), docker.NewClientWithAPI(mockAPI), scope, actual, state, 5, 10*time.Minute)

	require.NoError(t, err)
	assert.Equal(t, []string{"api"}, result.Tripped)
	assert.Equal(t, []string{"compose-api-1-0123456789abcdef"}, stopped, "only the stack started from this target's compose directory may be stopped")
}

func TestRunRestartBreaker_FallbackWithoutMarkerStopsNothing(t *testing.T) {
	mockAPI := &dockertest.MockDockerAPI{
		ContainerInspectFunc: func(_ context.Context, _ string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
			t.Fatal("a fallback scope with no compose directory must not inspect containers")
			return client.ContainerInspectResult{}, nil
		},
	}
	tracking := map[string]RestartTrackingEntry{"web": {RestartCount: 4, CheckedAt: time.Now().Add(-3 * time.Minute)}}
	state := &DeployState{RestartTracking: tracking}
	actual := []ActualService{{Name: "web", ContainerName: "compose-web-1", State: "running"}}

	result, err := RunRestartBreaker(context.Background(), docker.NewClientWithAPI(mockAPI),
		RestartBreakerScopeFor(&Config{}, ComposeFallbackProjectName), actual, state, 5, 10*time.Minute)

	require.NoError(t, err)
	assert.Empty(t, result.Tripped)
	assert.Equal(t, tracking, result.Updated)
	assert.Equal(t, 0, mockAPI.ContainerStopCalls)
}

// TestRunRestartBreaker_StopsInspectedContainerID pins that a trip stops the
// exact container that was inspected and ownership-checked. If another
// container took over the name between inspect and stop, a name-based stop
// would hit it instead.
func TestRunRestartBreaker_StopsInspectedContainerID(t *testing.T) {
	inspectedID := "feedfacecafe0000111122223333444455556666777788889999aaaabbbbcccc"
	var stopped []string
	mockAPI := &dockertest.MockDockerAPI{
		ContainerInspectFunc: func(_ context.Context, _ string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
			return client.ContainerInspectResult{Container: container.InspectResponse{
				ID:              inspectedID,
				Name:            "/test-web-1",
				RestartCount:    12,
				State:           &container.State{Status: container.StateRunning},
				Config:          &container.Config{Image: "nginx", Labels: composeLabels("test", "web")},
				NetworkSettings: &container.NetworkSettings{Networks: map[string]*network.EndpointSettings{}},
			}}, nil
		},
		ContainerStopFunc: func(_ context.Context, id string, _ client.ContainerStopOptions) (client.ContainerStopResult, error) {
			stopped = append(stopped, id)
			return client.ContainerStopResult{}, nil
		},
	}
	actual := []ActualService{{Name: "web", ContainerName: "test-web-1", State: "running"}}
	state := &DeployState{RestartTracking: map[string]RestartTrackingEntry{
		"web": {RestartCount: 5, CheckedAt: time.Now().Add(-3 * time.Minute)},
	}}

	result, err := RunRestartBreaker(context.Background(), docker.NewClientWithAPI(mockAPI), RestartBreakerScope{ProjectName: "test"}, actual, state, 5, 10*time.Minute)

	require.NoError(t, err)
	assert.Equal(t, []string{"web"}, result.Tripped)
	assert.Equal(t, []string{inspectedID}, stopped, "stop must use the full inspected ID, never the reusable name")
}

// TestRestartBreakerScopeOwnsContainer_SymlinkedWorkingDir pins that the
// compose-fallback working_dir check compares symlink-resolved paths: either
// spelling of a symlinked appdata directory matches, while a symlink that
// points at some other directory does not.
func TestRestartBreakerScopeOwnsContainer_SymlinkedWorkingDir(t *testing.T) {
	base := t.TempDir()
	realAppdata := filepath.Join(base, "real-appdata")
	otherAppdata := filepath.Join(base, "other-appdata")
	require.NoError(t, os.MkdirAll(filepath.Join(realAppdata, "compose"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(otherAppdata, "compose"), 0o755))
	linkToReal := filepath.Join(base, "appdata-link")
	linkToOther := filepath.Join(base, "elsewhere-link")
	require.NoError(t, os.Symlink(realAppdata, linkToReal))
	require.NoError(t, os.Symlink(otherAppdata, linkToOther))

	tests := []struct {
		name       string
		configured string
		workingDir string
		want       bool
	}{
		{"configured symlink, compose recorded resolved path", linkToReal, filepath.Join(realAppdata, "compose"), true},
		{"configured real path, compose recorded symlink path", realAppdata, filepath.Join(linkToReal, "compose"), true},
		{"configured symlink, compose recorded symlink path", linkToReal, filepath.Join(linkToReal, "compose"), true},
		{"symlink pointing elsewhere", realAppdata, filepath.Join(linkToOther, "compose"), false},
		{"configured symlink, foreign symlink elsewhere", linkToReal, filepath.Join(linkToOther, "compose"), false},
		{"configured symlink, foreign real directory", linkToReal, filepath.Join(otherAppdata, "compose"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scope := RestartBreakerScopeFor(&Config{LocalAppdataPath: tt.configured}, ComposeFallbackProjectName)
			require.Empty(t, scope.unresolvedReason())
			assert.Equal(t, tt.want, scope.ownsContainer(composeLabelsAt("compose", "web", tt.workingDir), "web"))
		})
	}
}

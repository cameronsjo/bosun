package reconcile

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/log"
)

// RestartBreakerResult holds the outcome of a restart circuit breaker evaluation.
type RestartBreakerResult struct {
	// Tripped contains service names that exceeded the restart threshold.
	Tripped []string
	// Resolved contains service names that stabilized after being tripped.
	Resolved []string
	// Updated is the new tracking state to persist.
	Updated map[string]RestartTrackingEntry
}

// evaluateRestartBreaker is the pure function that computes restart circuit breaker
// state transitions. It compares current restart counts against tracked state to
// detect a sustained restart run reaching the configured threshold. The window
// remains part of the caller contract and configuration warning, but it is not a
// destructive reset boundary while restarts continue (#265).
//
// Returns which containers should be tripped and which have resolved.
func evaluateRestartBreaker(
	current map[string]int, // service name → current restart count
	tracked map[string]RestartTrackingEntry,
	threshold int,
	_ time.Duration,
	now time.Time,
) *RestartBreakerResult {
	result := &RestartBreakerResult{
		Updated: make(map[string]RestartTrackingEntry, len(current)),
	}

	for service, count := range current {
		prev, exists := tracked[service]

		if !exists {
			// First observation: record baseline, no action.
			result.Updated[service] = RestartTrackingEntry{
				RestartCount:         count,
				CheckedAt:            now,
				BaselineRestartCount: count,
				BaselineAt:           now,
			}
			continue
		}

		// Already tripped: keep tripped state, check for resolution.
		if prev.Tripped {
			// Resolve against the count observed on the *previous* check, not
			// the frozen count from the moment it tripped (#348). RestartCount
			// is monotonic for the life of the container, so once any restart
			// occurs after tripping, the count permanently exceeds the
			// trip-time baseline and a fixed comparison could never resolve
			// again even after the container stabilizes. Advancing the
			// baseline every cycle means "no restarts since last check"
			// still resolves it correctly.
			if count <= prev.RestartCount {
				result.Resolved = append(result.Resolved, service)
				result.Updated[service] = RestartTrackingEntry{
					RestartCount:         count,
					CheckedAt:            now,
					BaselineRestartCount: count,
					BaselineAt:           now,
				}
			} else {
				// Still accumulating restarts after trip — keep tripped, but
				// advance the rolling baseline so a later stable cycle can resolve.
				result.Updated[service] = RestartTrackingEntry{
					RestartCount:         count,
					CheckedAt:            now,
					BaselineRestartCount: prev.BaselineRestartCount,
					BaselineAt:           prev.BaselineAt,
					Tripped:              true,
					TrippedAt:            prev.TrippedAt,
				}
			}
			continue
		}

		// Legacy state has no explicit baseline. Its latest observation is the
		// conservative starting point, preserving backward-compatible decode.
		baselineCount := prev.BaselineRestartCount
		baselineAt := prev.BaselineAt
		if baselineAt.IsZero() {
			baselineCount = prev.RestartCount
			baselineAt = prev.CheckedAt
		}

		deltaSinceLastCheck := count - prev.RestartCount

		// No restarts since last check: update baseline.
		if deltaSinceLastCheck <= 0 {
			result.Updated[service] = RestartTrackingEntry{
				RestartCount:         count,
				CheckedAt:            now,
				BaselineRestartCount: count,
				BaselineAt:           now,
			}
			continue
		}

		// Restart observations are discrete samples. If restarts continue on each
		// sample, preserve their earliest baseline even when the sampling interval
		// exceeds window; otherwise a sparse cadence makes the breaker impossible
		// to trip (#265). A clean sample above resets the run.
		accumulatedDelta := count - baselineCount
		if accumulatedDelta >= threshold {
			result.Tripped = append(result.Tripped, service)
			result.Updated[service] = RestartTrackingEntry{
				RestartCount:         count,
				CheckedAt:            now,
				BaselineRestartCount: baselineCount,
				BaselineAt:           baselineAt,
				Tripped:              true,
				TrippedAt:            now,
			}
			continue
		}

		// Below threshold but still accumulating: advance the latest observation
		// while retaining the earliest unresolved baseline.
		result.Updated[service] = RestartTrackingEntry{
			RestartCount:         count,
			CheckedAt:            now,
			BaselineRestartCount: baselineCount,
			BaselineAt:           baselineAt,
		}
	}

	// Clean up tracking for containers no longer present -- except a tripped
	// entry for a container the breaker itself stopped (#348). Stopping the
	// container removes it from collectRestartCounts (running/restarting
	// only), so without this a stopped-and-tripped container would drop out
	// of `current` and be silently pruned here: the trip record is lost,
	// no resolved alert ever fires, and a later restart starts from a fresh
	// baseline that can immediately re-trip.
	for service, prev := range tracked {
		if _, present := current[service]; present {
			continue
		}
		if prev.Tripped {
			result.Updated[service] = prev
		}
	}

	return result
}

// RestartBreakerSamplingMismatch reports whether restart counts are sampled
// less frequently than the configured evaluation window. Runtime accumulation
// remains safe in this configuration, but the mismatch is operationally
// surprising and should be surfaced by config-load and doctor (#265).
func RestartBreakerSamplingMismatch(driftInterval, restartWindow time.Duration) bool {
	return driftInterval > 0 && restartWindow > 0 && driftInterval > restartWindow
}

// collectRestartCounts inspects running containers to get their restart counts.
// Only inspects containers in the declared services list to minimize API calls.
func collectRestartCounts(
	ctx context.Context,
	client *docker.Client,
	actual []ActualService,
) map[string]int {
	logger := log.ComponentCtx(ctx, log.ComponentReconcile)
	counts := make(map[string]int, len(actual))

	for _, svc := range actual {
		if svc.State != "running" && svc.State != "restarting" {
			continue
		}

		details, err := client.Inspect(ctx, svc.ContainerName)
		if err != nil {
			logger.Warn().
				Str(log.FieldContainer, svc.ContainerName).
				Err(err).
				Msg("Restart breaker: failed to inspect container")
			continue
		}

		counts[svc.Name] = details.RestartCount
	}

	return counts
}

// RunRestartBreaker performs restart circuit breaker evaluation and takes action
// on containers that exceed the restart threshold. Returns the result for alerting.
func RunRestartBreaker(
	ctx context.Context,
	client *docker.Client,
	actual []ActualService,
	state *DeployState,
	threshold int,
	window time.Duration,
) (*RestartBreakerResult, error) {
	logger := log.ComponentCtx(ctx, log.ComponentReconcile)

	counts := collectRestartCounts(ctx, client, actual)
	if len(counts) == 0 {
		return &RestartBreakerResult{Updated: state.RestartTracking}, nil
	}

	if state.RestartTracking == nil {
		state.RestartTracking = make(map[string]RestartTrackingEntry)
	}

	result := evaluateRestartBreaker(counts, state.RestartTracking, threshold, window, time.Now())

	// Stop tripped containers (best-effort: attempt all, collect errors).
	var stopErrs []error
	for _, service := range result.Tripped {
		containerName := containerNameForService(actual, service)
		if containerName == "" {
			continue
		}

		logger.Warn().
			Str(log.FieldContainer, containerName).
			Str("service", service).
			Int("threshold", threshold).
			Msg("Restart circuit breaker tripped: stopping container")

		if err := client.StopContainer(ctx, containerName, 10); err != nil {
			logger.Error().
				Str(log.FieldContainer, containerName).
				Err(err).
				Msg("Failed to stop restart-looping container")
			stopErrs = append(stopErrs, fmt.Errorf("stop container %s: %w", containerName, err))
		}
	}

	if len(stopErrs) > 0 {
		return result, errors.Join(stopErrs...)
	}
	return result, nil
}

// containerNameForService finds the container name for a service from actual state.
func containerNameForService(actual []ActualService, service string) string {
	for _, a := range actual {
		if a.Name == service {
			return a.ContainerName
		}
	}
	return ""
}

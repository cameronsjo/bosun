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
// detect restart velocity exceeding the threshold within the window.
//
// Returns which containers should be tripped and which have resolved.
func evaluateRestartBreaker(
	current map[string]int, // service name → current restart count
	tracked map[string]RestartTrackingEntry,
	threshold int,
	window time.Duration,
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
				RestartCount: count,
				CheckedAt:    now,
			}
			continue
		}

		// Already tripped: keep tripped state, check for resolution.
		if prev.Tripped {
			// Container is stopped (we stopped it). Keep the entry until
			// the container is restarted by an operator or new deploy.
			// If the container is running again with a lower/same restart count,
			// it was manually restarted — mark resolved.
			if count <= prev.RestartCount {
				result.Resolved = append(result.Resolved, service)
				// Reset tracking with current count.
				result.Updated[service] = RestartTrackingEntry{
					RestartCount: count,
					CheckedAt:    now,
				}
			} else {
				// Still accumulating restarts after trip — keep tripped.
				result.Updated[service] = prev
			}
			continue
		}

		delta := count - prev.RestartCount
		elapsed := now.Sub(prev.CheckedAt)

		// No restarts since last check: update baseline.
		if delta <= 0 {
			result.Updated[service] = RestartTrackingEntry{
				RestartCount: count,
				CheckedAt:    now,
			}
			continue
		}

		// Within window and exceeds threshold: trip.
		if elapsed <= window && delta >= threshold {
			result.Tripped = append(result.Tripped, service)
			result.Updated[service] = RestartTrackingEntry{
				RestartCount: count,
				CheckedAt:    now,
				Tripped:      true,
				TrippedAt:    now,
			}
			continue
		}

		// Restarts detected but below threshold or outside window.
		// If outside window, reset baseline to current count.
		if elapsed > window {
			result.Updated[service] = RestartTrackingEntry{
				RestartCount: count,
				CheckedAt:    now,
			}
		} else {
			// Within window, delta below threshold: keep original baseline.
			result.Updated[service] = RestartTrackingEntry{
				RestartCount: prev.RestartCount,
				CheckedAt:    prev.CheckedAt,
			}
		}
	}

	// Clean up tracking for containers no longer present.
	// (Handled implicitly — only current containers are in Updated.)

	return result
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

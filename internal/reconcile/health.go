package reconcile

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/log"
)

// HealthCheckResult holds the outcome of a post-deploy health poll.
type HealthCheckResult struct {
	Passed     bool
	Unhealthy  []string
	Duration   time.Duration
	Iterations int
}

// pollContainerHealth polls container health until all declared services are
// healthy or the timeout expires. Containers without a HEALTHCHECK directive
// are considered healthy once running.
//
// Returns a HealthCheckResult and nil error on success (all healthy).
// Returns a HealthCheckResult and an error when the timeout expires with
// unhealthy containers remaining.
func pollContainerHealth(
	ctx context.Context,
	client *docker.Client,
	declared []DeclaredService,
	projectName string,
	timeout time.Duration,
	interval time.Duration,
) (*HealthCheckResult, error) {
	logger := log.ComponentCtx(ctx, log.ComponentReconcile)
	start := time.Now()
	deadline := start.Add(timeout)
	iteration := 0

	for {
		iteration++

		var unhealthy []string
		actual, err := CollectActualState(ctx, client, projectName)
		if err != nil {
			logger.Warn().
				Err(err).
				Int("iteration", iteration).
				Msg("Health poll: failed to collect container state")
			// Continue polling — transient Docker API errors shouldn't abort
			// verification, but the deadline check below still applies so a
			// persistent Docker API failure can't poll forever.
		} else {
			unhealthy = classifyHealth(declared, actual)

			logger.Debug().
				Int("iteration", iteration).
				Int("unhealthy_count", len(unhealthy)).
				Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
				Msg("Health poll iteration")

			if len(unhealthy) == 0 {
				result := &HealthCheckResult{
					Passed:     true,
					Duration:   time.Since(start),
					Iterations: iteration,
				}
				return result, nil
			}
		}

		// Check if we've exceeded the timeout. Evaluated every iteration —
		// success or error — so a persistent CollectActualState error still
		// respects the deadline instead of polling indefinitely.
		if time.Now().After(deadline) {
			result := &HealthCheckResult{
				Passed:     false,
				Unhealthy:  unhealthy,
				Duration:   time.Since(start),
				Iterations: iteration,
			}
			return result, fmt.Errorf("health verification timed out after %v: unhealthy containers: %s",
				timeout, strings.Join(unhealthy, ", "))
		}

		// Wait for the next poll interval, the deadline, or context
		// cancellation — whichever comes first. Without the deadline arm, an
		// interval longer than the remaining time would overshoot the
		// timeout by up to a full interval before the next check.
		select {
		case <-ctx.Done():
			return &HealthCheckResult{
				Passed:     false,
				Duration:   time.Since(start),
				Iterations: iteration,
			}, fmt.Errorf("health verification cancelled: %w", ctx.Err())
		case <-time.After(time.Until(deadline)):
		case <-time.After(interval):
		}
	}
}

// classifyHealth returns the names of declared services that are not yet healthy.
// A container is healthy if:
//   - It has health status "healthy", OR
//   - It has no healthcheck (health == "") and its state is "running"
//
// A container is unhealthy if:
//   - It has health status "unhealthy", OR
//   - Its state is not "running" (exited, restarting, dead, or missing)
func classifyHealth(declared []DeclaredService, actual []ActualService) []string {
	actualByName := make(map[string]ActualService, len(actual))
	for _, a := range actual {
		actualByName[a.Name] = a
	}

	var unhealthy []string
	for _, d := range declared {
		a, found := actualByName[d.Name]
		if !found {
			unhealthy = append(unhealthy, d.Name)
			continue
		}

		if a.State != "running" {
			unhealthy = append(unhealthy, d.Name)
			continue
		}

		// Running + no healthcheck = healthy.
		if a.Health == "" {
			continue
		}

		// Running + healthy = healthy.
		if a.Health == "healthy" {
			continue
		}

		// Running + starting/unhealthy = not yet healthy.
		unhealthy = append(unhealthy, d.Name)
	}

	return unhealthy
}

// blockingUnhealthy filters an unhealthy-container list down to the subset
// that should actually fail the post-deploy health gate: names absent from
// preExisting. A container already unhealthy before this reconcile's deploy
// touched anything is a pre-existing casualty from some unrelated cause —
// failing the whole reconcile (and skipping post-sync hooks) over it makes
// every future deploy fail identically until someone fixes the unrelated
// container (#392). A container that transitions from healthy/missing into
// unhealthy during this reconcile always counts, regardless of preExisting.
func blockingUnhealthy(unhealthy []string, preExisting map[string]bool) []string {
	if len(preExisting) == 0 {
		return unhealthy
	}
	blocking := make([]string, 0, len(unhealthy))
	for _, name := range unhealthy {
		if !preExisting[name] {
			blocking = append(blocking, name)
		}
	}
	return blocking
}

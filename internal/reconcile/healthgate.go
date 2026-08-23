package reconcile

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/log"
)

// HealthGatePollInterval is the default interval for polling container status.
const HealthGatePollInterval = 5 * time.Second

// ContainerHealthResult describes the health gate outcome for a single container.
type ContainerHealthResult struct {
	Name   string
	Status string // "healthy", "unhealthy", "starting", "no_healthcheck", "missing", "not_running"
	Detail string // Human-readable detail (e.g. health log output)
}

// CheckCriticalContainerHealth polls critical containers via Docker API until all are
// healthy or the timeout expires. Returns nil if all containers pass, or an error
// listing which containers failed and why.
//
// Health classification:
//   - "healthy": pass
//   - no healthcheck defined (empty Health field + running): pass
//   - "unhealthy": fail
//   - "starting" at timeout: fail
//   - missing or not running: fail
func CheckCriticalContainerHealth(
	ctx context.Context,
	client *docker.Client,
	containers []string,
	timeout time.Duration,
	interval time.Duration,
) error {
	if len(containers) == 0 || timeout <= 0 {
		return nil
	}
	if interval <= 0 {
		interval = HealthGatePollInterval
	}

	logger := log.ComponentCtx(ctx, log.ComponentReconcile)
	logger.Info().
		Strs("containers", containers).
		Dur("timeout", timeout).
		Dur("interval", interval).
		Msg("Starting critical container health gate")

	deadline := time.After(timeout)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		results := inspectCriticalContainers(ctx, client, containers)

		var failed []ContainerHealthResult
		for _, r := range results {
			if r.Status != "healthy" && r.Status != "no_healthcheck" {
				failed = append(failed, r)
			}
		}

		if len(failed) == 0 {
			logger.Info().
				Strs("containers", containers).
				Msg("All critical containers healthy")
			return nil
		}

		select {
		case <-deadline:
			// Final check: a container may have become healthy since the last poll.
			finalResults := inspectCriticalContainers(ctx, client, containers)
			var finalFailed []ContainerHealthResult
			for _, r := range finalResults {
				if r.Status != "healthy" && r.Status != "no_healthcheck" {
					finalFailed = append(finalFailed, r)
				}
			}
			if len(finalFailed) == 0 {
				logger.Info().
					Strs("containers", containers).
					Msg("All critical containers healthy (final check)")
				return nil
			}
			return formatHealthGateError(finalFailed)
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			logger.Debug().
				Int("pending", len(failed)).
				Msg("Health gate polling. Some containers not yet healthy")
		}
	}
}

// inspectCriticalContainers checks each container's health via Docker API.
func inspectCriticalContainers(
	ctx context.Context,
	client *docker.Client,
	containers []string,
) []ContainerHealthResult {
	results := make([]ContainerHealthResult, 0, len(containers))

	for _, name := range containers {
		details, err := client.Inspect(ctx, name)
		if err != nil {
			results = append(results, ContainerHealthResult{
				Name:   name,
				Status: "missing",
				Detail: fmt.Sprintf("inspect failed: %v", err),
			})
			continue
		}

		if details.State != "running" {
			results = append(results, ContainerHealthResult{
				Name:   name,
				Status: "not_running",
				Detail: fmt.Sprintf("state: %s", details.State),
			})
			continue
		}

		switch details.Health {
		case "healthy":
			results = append(results, ContainerHealthResult{
				Name:   name,
				Status: "healthy",
			})
		case "unhealthy":
			detail := "unhealthy"
			if details.HealthLog != nil {
				detail = formatHealthDetail(details)
			}
			results = append(results, ContainerHealthResult{
				Name:   name,
				Status: "unhealthy",
				Detail: detail,
			})
		case "starting":
			results = append(results, ContainerHealthResult{
				Name:   name,
				Status: "starting",
				Detail: "container still starting",
			})
		default:
			// Empty health field = no healthcheck defined, which passes the gate.
			results = append(results, ContainerHealthResult{
				Name:   name,
				Status: "no_healthcheck",
			})
		}
	}

	return results
}

// formatHealthGateError builds an error message listing all failed containers.
func formatHealthGateError(failed []ContainerHealthResult) error {
	var parts []string
	for _, f := range failed {
		if f.Detail != "" {
			parts = append(parts, fmt.Sprintf("%s (%s: %s)", f.Name, f.Status, f.Detail))
		} else {
			parts = append(parts, fmt.Sprintf("%s (%s)", f.Name, f.Status))
		}
	}
	return fmt.Errorf("critical container health gate failed: %s", strings.Join(parts, ", "))
}

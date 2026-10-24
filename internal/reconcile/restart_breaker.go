package reconcile

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
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

// restartObservation is the Docker state needed to distinguish a stable
// container from a replacement whose RestartCount was reset by recreation.
type restartObservation struct {
	ContainerID  string
	RestartCount int
	// FullID is the full Docker ID of the inspected container. A trip stops
	// this exact container, never whatever currently holds its name.
	FullID string
}

// evaluateRestartBreaker is the pure function that computes restart circuit breaker
// state transitions. It compares current restart counts against tracked state to
// detect a sustained restart run reaching the configured threshold. The window
// remains part of the caller contract and configuration warning, but it is not a
// destructive reset boundary while restarts continue (#265).
//
// Returns which containers should be tripped and which have resolved.
func evaluateRestartBreaker(
	current map[string]restartObservation,
	tracked map[string]RestartTrackingEntry,
	threshold int,
	_ time.Duration,
	now time.Time,
) *RestartBreakerResult {
	result := &RestartBreakerResult{
		Updated: make(map[string]RestartTrackingEntry, len(current)),
	}

	for service, observation := range current {
		count := observation.RestartCount
		prev, exists := tracked[service]
		containerID := observation.ContainerID
		if containerID == "" {
			// Legacy callers and state written before identity tracking have no ID.
			// Preserve any known identity rather than erasing migration progress.
			containerID = prev.ContainerID
		}

		if !exists {
			// First observation: record baseline, no action.
			result.Updated[service] = RestartTrackingEntry{
				RestartCount:         count,
				CheckedAt:            now,
				BaselineRestartCount: count,
				BaselineAt:           now,
				ContainerID:          containerID,
			}
			continue
		}

		// Already tripped: keep tripped state, check for resolution.
		if prev.Tripped {
			// Docker resets RestartCount when it recreates a container. Only an
			// unchanged observation across two checks of the same known identity
			// proves stability; the first post-recreate sample merely starts the
			// grace cycle (#266). This also keeps legacy unknown-identity state
			// tripped until the current identity has been observed twice.
			sameIdentity := prev.ContainerID != "" && containerID != "" && prev.ContainerID == containerID
			if sameIdentity && count == prev.RestartCount && prev.StabilityPending {
				result.Resolved = append(result.Resolved, service)
				result.Updated[service] = RestartTrackingEntry{
					RestartCount:         count,
					CheckedAt:            now,
					BaselineRestartCount: count,
					BaselineAt:           now,
					ContainerID:          containerID,
				}
			} else {
				// Every observed sample starts a new candidate stability interval.
				// A restart-count increase or another recreation before the next
				// check prevents resolution and re-arms the interval from here.
				result.Updated[service] = RestartTrackingEntry{
					RestartCount:         count,
					CheckedAt:            now,
					BaselineRestartCount: prev.BaselineRestartCount,
					BaselineAt:           prev.BaselineAt,
					ContainerID:          containerID,
					Tripped:              true,
					TrippedAt:            prev.TrippedAt,
					StabilityPending:     true,
				}
			}
			continue
		}

		// Counts from different Docker container identities are not comparable:
		// recreation resets RestartCount. Start a fresh baseline instead of
		// inheriting restart signal from the replaced container. An empty prior
		// identity is legacy state, so it retains #265's baseline migration until
		// a concrete identity has been persisted.
		if prev.ContainerID != "" && containerID != "" && prev.ContainerID != containerID {
			result.Updated[service] = RestartTrackingEntry{
				RestartCount:         count,
				CheckedAt:            now,
				BaselineRestartCount: count,
				BaselineAt:           now,
				ContainerID:          containerID,
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
				ContainerID:          containerID,
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
				ContainerID:          containerID,
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
			ContainerID:          containerID,
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
			// A missing container is not a stability observation. In particular,
			// the breaker normally stops a service immediately after tripping it;
			// its first later observation must start a new grace interval rather
			// than resolving from a candidate recorded before the stop.
			prev.StabilityPending = false
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

// ComposeFallbackProjectName is the project name Docker Compose derives when
// no project_name is configured and the rendered compose files live in a
// directory named "compose". Any Compose stack started from such a directory
// gets the same name, so the name alone does not prove Bosun owns a stack.
const ComposeFallbackProjectName = "compose"

// ComposeWorkingDirLabel is the label Docker Compose v2 sets to the absolute
// project directory a container was created from.
const ComposeWorkingDirLabel = "com.docker.compose.project.working_dir"

// RestartBreakerScope names the containers the restart breaker may stop. The
// breaker is destructive, so every field narrows the scope; none widens it.
type RestartBreakerScope struct {
	// ProjectName is the Compose project a container's
	// com.docker.compose.project label must equal. Empty means the scope is
	// unresolved and the breaker acts on nothing.
	ProjectName string
	// RequireWorkingDir additionally demands that a container's
	// com.docker.compose.project.working_dir label equal ComposeWorkingDir.
	// It is set when ProjectName is only Compose's derived fallback name.
	RequireWorkingDir bool
	// ComposeWorkingDir is the absolute, cleaned directory Bosun runs
	// docker compose from for this target. Empty with RequireWorkingDir set
	// means ownership cannot be proven and the breaker acts on nothing.
	// Matching also compares symlink-resolved paths, so a symlinked appdata
	// path keeps protection whichever spelling Compose recorded.
	ComposeWorkingDir string
}

// resolvePathForMatch returns path with symlinks resolved, or the cleaned
// literal path when resolution fails (for example, the path does not exist).
func resolvePathForMatch(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}

// RestartBreakerScopeFor derives the breaker scope for a target config and the
// Compose project name the caller resolved for it. When that name is only
// Compose's derived fallback (cfg sets no project_name), a foreign stack
// started from any other directory named "compose" shares it, so the scope
// also requires the working_dir label to point at this target's local compose
// directory (#717).
func RestartBreakerScopeFor(cfg *Config, projectName string) RestartBreakerScope {
	scope := RestartBreakerScope{ProjectName: projectName}
	if projectName != ComposeFallbackProjectName || (cfg != nil && cfg.ProjectName != "") {
		return scope
	}
	scope.RequireWorkingDir = true
	if cfg == nil || cfg.LocalAppdataPath == "" {
		return scope
	}
	dir, err := filepath.Abs(filepath.Join(cfg.LocalAppdataPath, "compose"))
	if err != nil {
		return scope
	}
	scope.ComposeWorkingDir = dir
	return scope
}

// unresolvedReason returns why the scope cannot prove ownership of any
// container, or "" when it can.
func (s RestartBreakerScope) unresolvedReason() string {
	if s.ProjectName == "" {
		return "Compose project scope is unresolved"
	}
	if s.RequireWorkingDir && s.ComposeWorkingDir == "" {
		return "fallback Compose project name has no local compose directory to verify ownership against"
	}
	return ""
}

// ownsContainer reports whether a container's labels prove it is the given
// service of the scoped Compose project. It reads labels only: a container
// name is chosen by whoever creates the container, so a name prefix is never
// evidence of ownership (#717). Missing labels fail closed.
func (s RestartBreakerScope) ownsContainer(labels map[string]string, service string) bool {
	if s.unresolvedReason() != "" || service == "" {
		return false
	}
	if labels[ComposeProjectLabel] != s.ProjectName || labels[ComposeServiceLabel] != service {
		return false
	}
	if !s.RequireWorkingDir {
		return true
	}
	workingDir := labels[ComposeWorkingDirLabel]
	if workingDir == "" || !filepath.IsAbs(workingDir) {
		return false
	}
	if filepath.Clean(workingDir) == s.ComposeWorkingDir {
		return true
	}
	// Compose records the path it was given; Bosun's configured path may be
	// the other spelling of the same directory through a symlink.
	return resolvePathForMatch(workingDir) == resolvePathForMatch(s.ComposeWorkingDir)
}

// collectRestartCounts inspects running containers to get their restart counts.
// Only inspects containers in the declared services list to minimize API calls.
// A container whose inspected labels do not prove scope ownership is not
// observed: it can never trip, and any prior tracking for its service is kept
// as if the inspect had failed.
func collectRestartCounts(
	ctx context.Context,
	client *docker.Client,
	actual []ActualService,
	scope RestartBreakerScope,
) (map[string]restartObservation, map[string]struct{}) {
	logger := log.ComponentCtx(ctx, log.ComponentReconcile)
	observations := make(map[string]restartObservation, len(actual))
	unobserved := make(map[string]struct{})

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
			unobserved[svc.Name] = struct{}{}
			continue
		}

		if !scope.ownsContainer(details.Labels, svc.Name) {
			// Info, not Warn: a foreign container that persists would repeat
			// this line on every drift cycle.
			logger.Info().
				Str(log.FieldContainer, svc.ContainerName).
				Str("service", svc.Name).
				Str("expected_project", scope.ProjectName).
				Str("observed_project", details.Labels[ComposeProjectLabel]).
				Str("observed_service", details.Labels[ComposeServiceLabel]).
				Bool("working_dir_required", scope.RequireWorkingDir).
				Msg("Restart breaker: skipping container. Reason: its Compose labels do not prove it belongs to this target; it will not be stopped")
			unobserved[svc.Name] = struct{}{}
			continue
		}

		observations[svc.Name] = restartObservation{
			ContainerID:  details.ID,
			RestartCount: details.RestartCount,
			FullID:       details.FullID,
		}
	}

	return observations, unobserved
}

// RunRestartBreaker performs restart circuit breaker evaluation and takes action
// on containers that exceed the restart threshold. Returns the result for alerting.
// An unresolved scope (no project name, or a fallback project name with no
// directory to verify against) returns the existing tracking unchanged without
// inspecting or stopping any container. Only containers whose inspected
// Compose labels match the scope are evaluated or stopped.
func RunRestartBreaker(
	ctx context.Context,
	client *docker.Client,
	scope RestartBreakerScope,
	actual []ActualService,
	state *DeployState,
	threshold int,
	window time.Duration,
) (*RestartBreakerResult, error) {
	logger := log.ComponentCtx(ctx, log.ComponentReconcile)

	// CollectActualState treats an empty project name as match-everything and
	// falls back to name-prefix matching for unlabeled containers. The
	// restart breaker is destructive, so an unresolved target scope must not
	// inspect or stop a container that Bosun cannot prove it owns. Keep the
	// tracking state unchanged as well: advancing it would trust the same
	// unscoped observation this guard refuses.
	if reason := scope.unresolvedReason(); reason != "" {
		logger.Debug().
			Int("candidate_containers", len(actual)).
			Str("project", scope.ProjectName).
			Str("reason", reason).
			Msg("Restart circuit breaker skipped: ownership cannot be proven; no containers will be inspected or stopped")
		return &RestartBreakerResult{Updated: state.RestartTracking}, nil
	}

	if state.RestartTracking == nil {
		state.RestartTracking = make(map[string]RestartTrackingEntry)
	}

	observations, unobserved := collectRestartCounts(ctx, client, actual, scope)
	result := evaluateRestartBreaker(observations, state.RestartTracking, threshold, window, time.Now())

	// A partial Docker sample is not evidence that an unobserved service was
	// removed. Retain its last known entry so a transient inspect failure cannot
	// erase an accumulating restart baseline while other services are evaluated.
	for service := range unobserved {
		if prev, exists := state.RestartTracking[service]; exists {
			result.Updated[service] = prev
		}
	}

	// Stop tripped containers (best-effort: attempt all, collect errors). Stop
	// by the full ID that was inspected and ownership-checked: a name can be
	// reused by a different container between the inspect and the stop.
	var stopErrs []error
	for _, service := range result.Tripped {
		containerName := containerNameForService(actual, service)
		containerID := observations[service].FullID
		if containerID == "" {
			logger.Error().
				Str(log.FieldContainer, containerName).
				Str("service", service).
				Msg("Restart circuit breaker tripped but not stopping container. Reason: inspect returned no container ID to target")
			stopErrs = append(stopErrs, fmt.Errorf("stop container for service %s: inspect returned no container ID", service))
			continue
		}

		logger.Warn().
			Str(log.FieldContainer, containerName).
			Str("container_id", containerID).
			Str("service", service).
			Int("threshold", threshold).
			Msg("Restart circuit breaker tripped: stopping container")

		if err := client.StopContainer(ctx, containerID, 10); err != nil {
			logger.Error().
				Str(log.FieldContainer, containerName).
				Str("container_id", containerID).
				Err(err).
				Msg("Failed to stop restart-looping container")
			stopErrs = append(stopErrs, fmt.Errorf("stop container %s (%s): %w", containerName, containerID, err))
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

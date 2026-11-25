package reconcile

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
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
					RecoveredSinceAlert:  prev.RecoveredSinceAlert,
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

// breakerContainer is one inspected, ownership-checked container of a service.
type breakerContainer struct {
	Service       string
	ContainerName string
	// Number is the container's Compose container number, "" when unlabeled.
	Number string
	Obs    restartObservation
}

// unobservedContainer marks a container whose inspect failed or whose labels
// did not prove ownership. ID is empty when the container list carries none.
type unobservedContainer struct {
	Service string
	ID      string
}

// replicasOf returns every container of a service. A hand-built ActualService
// without a container list stands for its one selected container.
func replicasOf(svc ActualService) []ServiceContainer {
	if len(svc.Containers) > 0 {
		return svc.Containers
	}
	return []ServiceContainer{{Name: svc.ContainerName, State: svc.State}}
}

// collectRestartCounts inspects every running or restarting container of each
// service to get its restart count, not only the one drift selected: a
// crash-looping replica must be watched whichever replica drift reports.
// A container whose inspected labels do not prove scope ownership is not
// observed: it can never trip, and any prior tracking for it is kept as if the
// inspect had failed.
func collectRestartCounts(
	ctx context.Context,
	client *docker.Client,
	actual []ActualService,
	scope RestartBreakerScope,
) ([]breakerContainer, []unobservedContainer) {
	logger := log.ComponentCtx(ctx, log.ComponentReconcile)
	var observed []breakerContainer
	var unobserved []unobservedContainer

	for _, svc := range actual {
		for _, replica := range replicasOf(svc) {
			if replica.State != "running" && replica.State != "restarting" {
				continue
			}

			details, err := client.Inspect(ctx, replica.Name)
			if err != nil {
				logger.Warn().
					Str(log.FieldContainer, replica.Name).
					Err(err).
					Msg("Restart breaker: failed to inspect container")
				unobserved = append(unobserved, unobservedContainer{Service: svc.Name, ID: replica.ID})
				continue
			}

			if !scope.ownsContainer(details.Labels, svc.Name) {
				// Info, not Warn: a foreign container that persists would repeat
				// this line on every drift cycle.
				logger.Info().
					Str(log.FieldContainer, replica.Name).
					Str("service", svc.Name).
					Str("expected_project", scope.ProjectName).
					Str("observed_project", details.Labels[ComposeProjectLabel]).
					Str("observed_service", details.Labels[ComposeServiceLabel]).
					Bool("working_dir_required", scope.RequireWorkingDir).
					Msg("Restart breaker: skipping container. Reason: its Compose labels do not prove it belongs to this target; it will not be stopped")
				unobserved = append(unobserved, unobservedContainer{Service: svc.Name, ID: replica.ID})
				continue
			}

			observed = append(observed, breakerContainer{
				Service:       svc.Name,
				ContainerName: replica.Name,
				Number:        details.Labels[ComposeContainerNumberLabel],
				Obs: restartObservation{
					ContainerID:  details.ID,
					RestartCount: details.RestartCount,
					FullID:       details.FullID,
				},
			})
		}
	}

	return observed, unobserved
}

// belongsToService reports whether a tracking key names a container of service.
// The first or only container of a service is keyed by the service name;
// further containers by "<service>#<container ID>". Compose service names
// cannot contain "#".
func belongsToService(key, service string) bool {
	return key == service || strings.HasPrefix(key, service+"#")
}

// serviceOfKey returns the service a tracking key belongs to.
func serviceOfKey(key string) string {
	service, _, _ := strings.Cut(key, "#")
	return service
}

// sortedTrackingKeys returns every key of the tracking map in sorted order.
func sortedTrackingKeys(tracked map[string]RestartTrackingEntry) []string {
	keys := make([]string, 0, len(tracked))
	for key := range tracked {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// trackedKeysFor returns the tracking keys of a service in sorted order.
func trackedKeysFor(tracked map[string]RestartTrackingEntry, service string) []string {
	var keys []string
	for key := range tracked {
		if belongsToService(key, service) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

// serviceTripped reports whether the breaker has tripped any container of the
// service. A container the breaker stopped stays tripped, so the skip check
// reads this instead of one per-service entry.
func serviceTripped(tracking map[string]RestartTrackingEntry, service string) bool {
	for key, entry := range tracking {
		if entry.Tripped && belongsToService(key, service) {
			return true
		}
	}
	return false
}

// setRecoveredSinceAlert sets the recovered-since-alert flag on every tripped
// entry of the service. The flag describes the service, so all its tripped
// entries carry the same value and any one of them can be cleared first.
func setRecoveredSinceAlert(tracking map[string]RestartTrackingEntry, service string, recovered bool) {
	for key, entry := range tracking {
		if entry.Tripped && belongsToService(key, service) {
			entry.RecoveredSinceAlert = recovered
			tracking[key] = entry
		}
	}
}

// assignTrackKeys gives every observed container the key of its tracking entry,
// so each container has its own baseline.
//
// A container keeps the entry whose ContainerID it matches. A container with no
// entry of its own takes over the entry of a replaced container of its service
// (one that no longer exists, or a legacy entry without an ID), so recreation by
// a deploy keeps its grace-period handling. Any other container gets a new key:
// the service name when no entry holds it, else "<service>#<container ID>".
// existing lists the IDs of every container of a service in any state; a nil
// entry means the list is unknown, and only an entry keyed by the service name
// can be taken over.
func assignTrackKeys(
	containers []breakerContainer,
	tracked map[string]RestartTrackingEntry,
	existing map[string]map[string]bool,
) map[string]breakerContainer {
	byService := make(map[string][]breakerContainer)
	var services []string
	for _, c := range containers {
		if _, seen := byService[c.Service]; !seen {
			services = append(services, c.Service)
		}
		byService[c.Service] = append(byService[c.Service], c)
	}
	sort.Strings(services)

	keyed := make(map[string]breakerContainer, len(containers))
	for _, service := range services {
		group := byService[service]
		sort.Slice(group, func(i, j int) bool {
			if group[i].Obs.ContainerID != group[j].Obs.ContainerID {
				return group[i].Obs.ContainerID < group[j].Obs.ContainerID
			}
			return group[i].ContainerName < group[j].ContainerName
		})
		entries := trackedKeysFor(tracked, service)
		claimed := make(map[string]bool, len(group))
		assigned := make([]string, len(group))

		for i, c := range group {
			if c.Obs.ContainerID == "" {
				continue
			}
			for _, key := range entries {
				if !claimed[key] && tracked[key].ContainerID == c.Obs.ContainerID {
					assigned[i], claimed[key] = key, true
					break
				}
			}
		}

		for i, c := range group {
			if assigned[i] != "" {
				continue
			}
			if key := successorKey(service, c.Number, entries, claimed, tracked, existing[service]); key != "" {
				assigned[i], claimed[key] = key, true
			}
		}

		for i, c := range group {
			if assigned[i] != "" {
				continue
			}
			key := service
			if _, held := tracked[key]; held || claimed[key] {
				suffix := c.Obs.ContainerID
				if suffix == "" {
					suffix = c.ContainerName
				}
				key = service + "#" + suffix
			}
			assigned[i], claimed[key] = key, true
		}

		for i, c := range group {
			keyed[assigned[i]] = c
		}
	}
	return keyed
}

// successorKey picks the unclaimed entry of a service that a new container with
// the given Compose container number replaces, preferring a tripped one, or ""
// when there is none. A container replaces an entry only when the entry's
// container no longer exists and the container numbers agree; a new sibling
// with another number never inherits an entry. A number that is unknown on
// either side (state from before it was recorded, or an unlabeled container)
// does not block the match.
func successorKey(
	service, number string,
	entries []string,
	claimed map[string]bool,
	tracked map[string]RestartTrackingEntry,
	ids map[string]bool,
) string {
	replaced := func(key string) bool {
		if n := tracked[key].ContainerNumber; n != "" && number != "" && n != number {
			return false
		}
		if ids == nil {
			return key == service
		}
		id := tracked[key].ContainerID
		return id == "" || !ids[id]
	}
	candidate := ""
	for _, key := range entries {
		if claimed[key] || !replaced(key) {
			continue
		}
		if tracked[key].Tripped {
			return key
		}
		if candidate == "" {
			candidate = key
		}
	}
	return candidate
}

// containerIDsByService lists the IDs of every container of each service, in
// any state. A service without a container list is left out, which marks its
// list as unknown.
func containerIDsByService(actual []ActualService) map[string]map[string]bool {
	ids := make(map[string]map[string]bool, len(actual))
	for _, svc := range actual {
		if len(svc.Containers) == 0 {
			continue
		}
		set := make(map[string]bool, len(svc.Containers))
		for _, c := range svc.Containers {
			set[c.ID] = true
		}
		ids[svc.Name] = set
	}
	return ids
}

// serviceNames returns the sorted, unique services of the given tracking keys.
func serviceNames(keyed map[string]breakerContainer, keys []string) []string {
	seen := make(map[string]bool, len(keys))
	var names []string
	for _, key := range keys {
		service := keyed[key].Service
		if service == "" || seen[service] {
			continue
		}
		seen[service] = true
		names = append(names, service)
	}
	sort.Strings(names)
	return names
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

	// Every running or restarting container of a service is evaluated with its
	// own baseline, keyed by its tracking entry, so a crash-looping replica is
	// watched whichever replica drift reports and a replica the breaker stopped
	// stays tripped while a sibling runs.
	containers, unobserved := collectRestartCounts(ctx, client, actual, scope)
	keyed := assignTrackKeys(containers, state.RestartTracking, containerIDsByService(actual))
	current := make(map[string]restartObservation, len(keyed))
	for key, c := range keyed {
		current[key] = c.Obs
	}
	result := evaluateRestartBreaker(current, state.RestartTracking, threshold, window, time.Now())

	// A partial Docker sample is not evidence that an unobserved container was
	// removed. Retain its last known entry so a transient inspect failure cannot
	// erase an accumulating restart baseline while other containers are
	// evaluated. A container list without IDs retains every entry of the service.
	for _, u := range unobserved {
		for _, key := range trackedKeysFor(state.RestartTracking, u.Service) {
			prev := state.RestartTracking[key]
			if u.ID == "" || prev.ContainerID == "" || prev.ContainerID == u.ID {
				result.Updated[key] = prev
			}
		}
	}

	// Record each observed container's Compose number so a replacement can be
	// matched to the entry it takes over.
	for key, c := range keyed {
		if entry, ok := result.Updated[key]; ok {
			entry.ContainerNumber = c.Number
			result.Updated[key] = entry
		}
	}

	// A tripped container that no longer exists, and that no new container with
	// its Compose number took over, has nothing left to recover: clear its entry
	// without a resolved alert, so the next deploy starts from a fresh baseline.
	// An unknown container list (a hand-built ActualService) never counts as gone.
	listed := containerIDsByService(actual)
	present := make(map[string]bool, len(actual))
	for _, svc := range actual {
		present[svc.Name] = true
	}
	//
	// A service that had a container recover since its trip alert (the flag on
	// its tripped entries) is reported resolved once its last tripped entry goes
	// away this way; clearedRecovered remembers which services qualify.
	clearedRecovered := make(map[string]bool)
	for _, key := range sortedTrackingKeys(state.RestartTracking) {
		prev := state.RestartTracking[key]
		if !prev.Tripped || prev.ContainerID == "" {
			continue
		}
		if _, observed := keyed[key]; observed {
			continue
		}
		service := serviceOfKey(key)
		ids, known := listed[service]
		if (known && ids[prev.ContainerID]) || (!known && present[service]) {
			continue
		}
		delete(result.Updated, key)
		clearedRecovered[service] = clearedRecovered[service] || prev.RecoveredSinceAlert
		logger.Info().
			Str("service", service).
			Str("container_id", prev.ContainerID).
			Str("tracking_key", key).
			Msg("Restart breaker: tripped container no longer exists and no container replaced it; clearing its entry without a resolved alert")
	}

	trippedKeys := result.Tripped
	result.Tripped = serviceNames(keyed, trippedKeys)

	// A service is resolved only when none of its containers is still tripped:
	// one replica recovering must not report a service resolved while another
	// replica is still tripped. A later resolution of the last one reports it.
	// The suppressed recovery is remembered on the service's still-tripped
	// entries, so the resolved alert is not lost if the last of them is cleared
	// quietly in a later tick (recover, then clear).
	var resolved []string
	for _, service := range serviceNames(keyed, result.Resolved) {
		if serviceTripped(result.Updated, service) {
			setRecoveredSinceAlert(result.Updated, service, true)
			logger.Debug().
				Str("service", service).
				Msg("Restart breaker: a container recovered but another container of the service is still tripped; not reporting the service resolved")
			continue
		}
		resolved = append(resolved, service)
	}

	// A service that trips again starts a new alert cycle: a recovery before this
	// trip alert no longer counts.
	for _, service := range result.Tripped {
		setRecoveredSinceAlert(result.Updated, service, false)
	}

	// The last tripped entry of a service went away without recovering. Report
	// the service resolved only when a container recovered since its trip alert;
	// when every entry was cleared quietly there is nothing to report.
	clearedServices := make([]string, 0, len(clearedRecovered))
	for service := range clearedRecovered {
		clearedServices = append(clearedServices, service)
	}
	sort.Strings(clearedServices)
	for _, service := range clearedServices {
		if !clearedRecovered[service] || serviceTripped(result.Updated, service) || slices.Contains(resolved, service) {
			continue
		}
		logger.Info().
			Str("service", service).
			Msg("Restart breaker: last tripped container is gone and a container of the service recovered since the trip alert; reporting the service resolved")
		resolved = append(resolved, service)
	}
	sort.Strings(resolved)
	result.Resolved = resolved

	// Stop tripped containers (best-effort: attempt all, collect errors). Stop
	// by the full ID that was inspected and ownership-checked: a name can be
	// reused by a different container between the inspect and the stop.
	sort.Strings(trippedKeys)
	var stopErrs []error
	for _, key := range trippedKeys {
		service := keyed[key].Service
		containerName := keyed[key].ContainerName
		containerID := keyed[key].Obs.FullID
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

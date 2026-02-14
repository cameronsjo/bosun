## Context

Bosun's reconciliation is currently open-loop: push commit, deploy, record success. It never checks whether the declared services are actually running and healthy. This is the gap between a deployment tool and a GitOps controller. Flux and ArgoCD solve this in Kubernetes with continuous drift detection; bosun needs an equivalent for Docker Compose.

The existing `DeployState` and `docker.Client` provide the foundation. The design extends both without breaking the current pipeline.

## Goals / Non-Goals

- Goals:
  - Detect drift between manifests and running containers (missing, crashed, wrong image, unhealthy)
  - Surface drift through CLI, daemon API, logs, and alerts
  - Verify state immediately after deployment
  - Run periodic drift checks independent of reconciliation
- Non-Goals:
  - Auto-remediation (triggering reconcile on drift) - future work, requires careful design
  - Config-level drift detection (environment variables, volumes, networks) - too noisy for v1
  - Image digest comparison (`:latest` tag resolution) - requires registry access

## Decisions

### Declared state derived from rendered compose, not raw manifests

**Decision**: Extract declared state from the compose YAML output (the artifact fed to `docker compose up`), not from `ServiceManifest` types.

**Rationale**: The compose output is the single source of truth for what Docker will create. Parsing manifests would require duplicating the render pipeline's logic for provisions, needs, sidecars, and overrides. The compose output already has all of this resolved.

**Implementation**: After `renderTemplates()` in the pipeline, parse the `services` key from each rendered compose file. Extract `{name, image}` pairs.

### Drift stored in deploy state file (not a separate file)

**Decision**: Extend `DeployState` with drift fields rather than creating a separate drift state file.

**Rationale**: Single atomic file is simpler to manage. Drift is directly related to a deployment. The state file is already atomically written and read by CLI, daemon, and reconciler.

**Trade-off**: State file grows slightly. Acceptable since drift items are small (service name + type + detail string) and bounded by the number of services (typically <50 in a homelab).

### Container filtering by compose project label

**Decision**: Filter containers using `com.docker.compose.project` label matching bosun's `ProjectName` config.

**Rationale**: Docker Compose v2 sets this label on all managed containers. Filtering by it ensures we only compare against bosun-managed services, not system containers, monitoring stacks, or other compose projects.

**Alternative considered**: Filter by container name prefix. Rejected because name patterns are fragile and not guaranteed by Docker Compose.

### Post-deploy verification warns but doesn't fail

**Decision**: Drift detected immediately after `compose up` is logged as a warning, not treated as a deployment failure.

**Rationale**: `docker compose up` may return before health checks complete. Failing the pipeline would cause false negatives and circuit breaker trips. The grace period mitigates this, but startup timing is unpredictable.

### Periodic drift checks are observation-only

**Decision**: Periodic drift checks update state and send alerts but never trigger reconciliation.

**Rationale**: Auto-remediation requires solving hard problems: should a crashed container be restarted, or does it indicate a bad config that needs a rollback? Starting with observation gives operators visibility without risk. Auto-remediation can be added later behind a flag.

## Risks / Trade-offs

- **Docker API overhead**: `ListContainers()` on every drift interval adds API calls. Mitigated by the default 5-minute interval and the fact that the call is lightweight (single API call, no inspect per container).
- **State file size**: Adding declared services and drift items increases file size. Bounded by service count, negligible in practice.
- **Image tag comparison is approximate**: Comparing `image` strings doesn't resolve tags to digests. `:latest` on the manifest matches `:latest` on the container even if the underlying image changed. This is a known limitation documented as a non-goal for v1.
- **Schema version bump**: Adding fields to `DeployState` requires bumping `schema_version` to 2. Old bosun binaries reading v2 state files will ignore unknown fields (Go's JSON unmarshaling is permissive), so this is backwards-compatible.

## Migration Plan

1. Bump `DeployState.SchemaVersion` to 2
2. Add new fields with `omitempty` JSON tags for backwards compatibility
3. Old state files (v1) load fine - new fields default to zero values
4. No data migration needed - drift fields populate on first drift check

## Open Questions

- Should `bosun drift --fix` be added in v1 to trigger a reconciliation when drift is detected, or save it for a follow-up?
- Should drift status feed into the daemon's `/health` endpoint (degraded when drift detected)?

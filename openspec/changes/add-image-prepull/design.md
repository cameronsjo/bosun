# Image Pre-Pull Design

## Context

The reconcile pipeline currently runs `docker compose up -d --remove-orphans` as a single step with a 10-minute timeout covering both image pulls and container startup. On first deploy or image tag change, the pull phase can consume most of that budget, leaving insufficient time for startup, or timing out entirely on slow registries (GHCR, self-hosted registries over WAN).

This is a cross-cutting change: it adds a new pipeline stage, introduces new configuration surface (env vars and timeouts), and touches structured logging conventions.

## Goals / Non-Goals

- Goals:
  - Separate image pull from container startup with independent timeouts
  - Provide clear, actionable errors distinguishing pull failures from startup failures
  - Log pull progress with structured fields for observability
  - Maintain backward compatibility (no config changes required to keep current behavior)
- Non-Goals:
  - Per-image pull concurrency control (Docker Compose handles this)
  - Image caching or registry mirroring
  - Pull policy configuration (`--pull always` vs `--pull missing`) - compose defaults are sufficient
  - Remote deploy pre-pull (remote deploys use SSH-piped compose commands with different constraints)

## Decisions

### Decision: `docker compose pull` before `docker compose up`

Run `docker compose pull` as an explicit pipeline stage before compose up. This leverages Docker Compose's built-in parallel pull and progress reporting.

**Alternatives considered:**
- Docker SDK `ImagePull` per image: More control but requires parsing compose files ourselves, loses Compose's auth config resolution, and duplicates Compose's parallel pull logic. Rejected for complexity.
- `docker compose up --pull always`: Combines pull and up in a single command but provides no way to set a separate timeout for the pull phase. Rejected because it doesn't solve the core problem.
- `docker compose create --pull always` then `docker compose start`: Separates pull but uses non-standard Compose workflow. Rejected for fragility.

### Decision: Pipeline stage placement

The pre-pull stage runs after "Extract declared state" and before "Create backup." This ordering ensures:
1. Compose files are rendered and available (templates executed)
2. Declared state is already extracted (we know what services exist)
3. Backup hasn't been created yet (no wasted backup if pull fails)
4. Rollback remains meaningful (backup + compose up still work as a unit)

### Decision: Independent configurable timeouts

Introduce `BOSUN_IMAGE_PULL_TIMEOUT` (default 15m) and `BOSUN_COMPOSE_UP_TIMEOUT` (default 5m) to replace the single `ComposeUpTimeout` constant. The defaults sum to 20m, slightly more than the old 10m, reflecting the reality that multi-image pulls can be slow. A new `ComposeUpTimeoutDefault` constant (5m) provides the default for `BOSUN_COMPOSE_UP_TIMEOUT`, replacing the previous single 10m timeout.

**Alternatives considered:**
- Single shared timeout pool: Simpler but doesn't solve the "pull ate all the timeout budget" problem. Rejected.
- Hardcoded timeouts with no env vars: Simpler but violates the project convention of environment-based configuration. Rejected.

### Decision: Pull failure does not trigger rollback

A pre-pull failure means no images were started with the new config, so the system is still in its previous state. Rollback (running compose up with backup files) would be a no-op at best and destructive at worst (pulling the backup's images could also fail). Pre-pull failures abort the pipeline without rollback, similar to how template rendering failures abort without rollback.

### Decision: Skip pre-pull for remote deploys

Remote deploys execute compose commands over SSH. Adding a separate `docker compose pull` SSH command adds complexity and doubles SSH round-trips. The remote host already pulls images as part of `docker compose up`. This can be revisited if remote deploy timeouts become a problem.

## Risks / Trade-offs

- **Extra command execution**: Adds one `docker compose pull` invocation per reconcile. Trade-off: ~1-2s overhead when images are cached vs. minutes of clarity when they're not.
- **Timeout sum exceeds old single timeout**: The combined default (15m + 5m = 20m) is longer than the old 10m. This is intentional — the old timeout was often too short for first-time pulls. Operators who want the old ceiling can set `BOSUN_IMAGE_PULL_TIMEOUT=5m`.
- **Pull succeeds but compose up still fails**: Possible if a pulled image has a broken entrypoint. The existing rollback mechanism handles this case unchanged.

## Open Questions

- None. The design is straightforward and follows established patterns in the codebase.

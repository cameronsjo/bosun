## Context

Bosun's reconcile pipeline has 6 stages: git sync, decrypt secrets, render
templates, backup, deploy files, compose up. Currently the only gate is a boolean
from `git.Sync()` — "did HEAD change?" This conflates "git is current" with
"deployment is current." When the pipeline is interrupted at any stage after git
sync, subsequent runs skip everything because git already shows the latest commit.

### Stakeholders

- Bosun daemon (runs the reconcile loop)
- Webhook triggers (GitHub, manual, socket)
- CLI users (`bosun trigger`, `bosun reconcile`)

### Constraints

- State must survive container restarts (persistent volume)
- State file must not interfere with the git repo
- Solution must handle bosun restarting itself during compose up
- `docker compose up` is idempotent — re-running with same config is safe
- Lock file already prevents concurrent reconcile runs

## Goals / Non-Goals

**Goals:**

- Detect interrupted deployments and automatically retry on next trigger
- Provide a manual force flag for all trigger paths
- Handle the self-restart scenario (bosun in its own compose stack)
- Zero-config for existing users (sensible defaults)

**Non-Goals:**

- Content-addressed hashing of rendered output (optimization, not needed for correctness)
- Stage-level checkpointing / resume-from-where-you-stopped
- Drift detection (comparing deployed files to expected state)
- Tracking deployment state per-service (only per-reconcile)

## Decisions

### Decision: Simple state file over stage-level checkpointing

A single JSON file recording last successfully deployed commit is sufficient
because every stage in the pipeline is idempotent:

- Git sync: already at latest → no-op
- Decrypt: deterministic from same input
- Render: clears staging dir first, re-renders everything
- Backup: creates new timestamped backup (harmless duplicate)
- Deploy: atomic copy/rsync (overwrites target, same result)
- Compose up: idempotent (no-op if containers already match config)

Since re-running the full pipeline from scratch produces the same result, there's
no need to track which stage completed. Just track whether the whole thing succeeded.

**Alternatives considered:**

- Stage-level checkpointing: More complex, no correctness benefit given idempotency
- Content hash of rendered output: Useful optimization for skipping deploy when
  templates produce identical output, but not needed to fix the core bug
- Always-deploy (remove skip entirely): Wasteful on every poll, especially for
  remote deployments over SSH. The state file preserves the optimization while
  fixing the bug

### Decision: State file in `/var/lib/bosun/` (persistent application state)

Use `/var/lib/bosun/` as the default state directory, configurable via `StateDir`
in config. This follows FHS conventions for persistent mutable application state.

**Why not `/var/run/bosun/` (alongside the lock file)?**
`/var/run/` is conventionally tmpfs on Linux, and Docker container recreation
(not just restart) wipes the filesystem layer. Since the whole point of the state
file is surviving process death and container recreation, placing it on tmpfs
would defeat the purpose. The daemon MUST log a startup warning if the state
directory appears to be on a tmpfs mount.

In containerized deployments, `/var/lib/bosun/` **must** be on a persistent
volume. This should be documented prominently, not buried in a footnote.

**Graceful fallback:** If the state file is missing (tmpfs, fresh install, lost
volume), bosun treats the system as "never deployed" and runs the full pipeline.
This is correct fail-open behavior — slightly wasteful but never wrong.

### Decision: Force flag in TriggerRequest, not reconcile Config

The `Force` field belongs in `TriggerRequest` (per-invocation) rather than
`reconcile.Config` (per-daemon-lifetime). This allows individual triggers to
override the skip logic without requiring a daemon restart.

The `reconcile.Config.Force` field already exists for the CLI command. For daemon
mode, we thread force from the API request to the reconciler by setting
`Config.Force` per-invocation (the reconciler is recreated or the field is set
before each run).

### Decision: Attempt tracking to prevent bad-commit crash loops

If a commit breaks the pipeline (bad template, invalid compose file), the
sequence without protection is: pull → fail → no state written → next poll →
pull (same commit) → fail → repeat hourly forever.

Track `last_attempted_commit` and `attempt_count` in the state file. After 3
consecutive failures on the same commit, stop retrying and require either a new
commit or `--force` to resume. Surface this through health checks as "degraded."

### Decision: Atomic writes with fsync

State file writes use the pattern: write temp → fsync temp → rename → fsync
directory. The temp file MUST be created in the same directory as the target
(`os.CreateTemp(filepath.Dir(stateFile), ...)`) to avoid EXDEV errors from
cross-filesystem rename. This matches the existing pattern in `template.go:55`
and `deploy.go:406`.

The fsync before rename adds two lines but survives power loss, not just process
kills. Worth the minimal cost for a bare-metal homelab tool.

### Decision: Schema versioning in state file

Include `"schema_version": 1` in the state file. When fields are added later
(rendered output hash, per-service tracking), old-format state files can be
detected and handled gracefully instead of crashing on missing fields.

### Decision: Defer rendered output hash to future iteration

Directory content hashing requires stable sort order, metadata decisions
(permissions? timestamps? symlinks?), and determinism testing. The
implementation cost is 30-50 careful lines for a field that is purely
informational in v1 (forensic, not gating). Defer to a future change when
there is a concrete use case for content-addressed skip logic.

## Risks / Trade-offs

- **State file corruption:** If the file is malformed, treat as "never deployed"
  and run the full pipeline. Fail-open is correct here.
- **Clock skew:** Not relevant — we compare commit hashes, not timestamps.
- **Multiple daemon instances:** The lock file prevents concurrent runs, and the
  state file is read/written under the lock. Safe.
- **State file on non-persistent volume:** Lost on container recreation → full
  pipeline runs. Correct but wasteful. Log warning on startup if state dir
  appears to be tmpfs.
- **State write failure after successful deploy:** Pipeline re-runs on every
  trigger. Backup accumulation risk (new tarball each time). Log at ERROR level.
  After N consecutive write failures, surface through health check as degraded.
- **Cross-filesystem rename (EXDEV):** Temp file must be in same directory as
  target. Enforced by implementation pattern, not left to caller.
- **Bad commit crash loop:** Attempt tracking with circuit breaker after 3
  consecutive failures on the same commit.

## Migration Plan

1. Existing deployments have no state file → first run after upgrade does a full
   deploy. This is the correct behavior (safe, idempotent).
2. No breaking changes to existing config or environment variables.
3. New env vars (`BOSUN_STATE_DIR`) are optional with sensible defaults.
4. `TriggerRequest` gains optional `Force` field — backwards compatible (zero value
   is false, existing clients don't send it).

## Open Questions

None — the design is straightforward given the idempotency of every pipeline stage.

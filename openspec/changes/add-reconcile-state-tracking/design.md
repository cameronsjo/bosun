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

### Decision: State file location alongside lock file directory

Use the same parent directory as the lock file (`/var/run/bosun/` by default),
configurable via `StateDir` in config. This directory is already expected to exist
and be writable by the daemon.

**Risk:** `/var/run/` may be tmpfs on some systems, meaning state is lost on host
reboot. This is acceptable — losing state just means the next reconcile does a
full run, which is correct behavior. The state file is an optimization, not a
correctness requirement. Without it, bosun falls back to "always deploy" behavior.

### Decision: Force flag in TriggerRequest, not reconcile Config

The `Force` field belongs in `TriggerRequest` (per-invocation) rather than
`reconcile.Config` (per-daemon-lifetime). This allows individual triggers to
override the skip logic without requiring a daemon restart.

The `reconcile.Config.Force` field already exists for the CLI command. For daemon
mode, we thread force from the API request to the reconciler by setting
`Config.Force` per-invocation (the reconciler is recreated or the field is set
before each run).

## Risks / Trade-offs

- **State file corruption:** If the file is malformed, treat as "never deployed"
  and run the full pipeline. Fail-open is correct here.
- **Clock skew:** Not relevant — we compare commit hashes, not timestamps.
- **Multiple daemon instances:** The lock file prevents concurrent runs, and the
  state file is read/written under the lock. Safe.
- **State file on tmpfs:** Lost on reboot → full pipeline runs. Correct.

## Migration Plan

1. Existing deployments have no state file → first run after upgrade does a full
   deploy. This is the correct behavior (safe, idempotent).
2. No breaking changes to existing config or environment variables.
3. New env vars (`BOSUN_STATE_DIR`) are optional with sensible defaults.
4. `TriggerRequest` gains optional `Force` field — backwards compatible (zero value
   is false, existing clients don't send it).

## Open Questions

None — the design is straightforward given the idempotency of every pipeline stage.

## Context

Bosun's reconciler was designed around "one instance = one server." The daemon creates one `Reconciler` with one `Config`, and every field in `Config` is singular: `TargetHost`, `StateFile`, `StagingDir`, `ProjectName`. Users wanting multi-server deployments must run multiple daemon containers, each with independent env vars, no shared visibility, and duplicated git polling.

The "one yacht, many ports" principle requires the daemon to reconcile a single repo to multiple targets from a single process.

### Prerequisites

- `data-driven-deploy-paths` (bosun-qjs) SHOULD land first — it removes hardcoded deploy targets, reducing the surface area this change touches in the deploy pipeline
- `refactor-dynamic-targets` (bosun-qjs) SHOULD land first — data-driven manifest output types reduce rigid struct coupling

### Constraints

- Single binary deployment — no orchestration layer, no database
- Must work behind reverse proxy on each target
- Daemon runs as a container — one process, one git clone
- State must survive restarts (file-based persistence)
- Backwards compatible — single-target configs work unchanged

## Goals / Non-Goals

- **Goals:**
  - Deploy to N targets from one daemon instance with one git repo
  - Per-target state tracking (independent deploy commits, circuit breakers, drift)
  - Per-target alerting context (operators know which server failed)
  - Isolated staging directories (no file collisions between targets)
  - Backwards compatible (zero-config migration for single-target users)
  - Per-target secrets scoping (shared secrets + target-specific overrides)

- **Non-Goals:**
  - Parallel target reconciliation (sequential is simpler, avoids resource contention; parallelism is a future optimization)
  - Cross-target dependency ordering (targets are independent; service mesh coordination is out of scope)
  - Per-target git branches (all targets deploy from the same branch and commit)
  - Remote Docker API for health checks (health gate remains local-only; remote targets skip it)
  - Web UI for multi-target status (CLI and alerts are sufficient for v1)

## Decisions

### Decision: Target as a named descriptor within Config

Each target is a named struct with its own host, paths, project name, secrets scope, and overrides. The reconciler iterates the targets list and runs the full pipeline for each.

```go
type Target struct {
    Name              string
    TargetHost        string        // empty = local
    LocalAppdataPath  string
    RemoteAppdataPath string
    ProjectName       string
    StateFile         string        // derived from Name if not set
    StagingDir        string        // derived from Name if not set
    SecretsScope      string        // key prefix for per-target secrets
    CriticalContainers []string
    PostSyncHooks     []PostSyncHook
    DeploySyncPaths   []string
    DeploySyncExclude []string
}
```

**Alternatives considered:**
- **Separate Config per target**: More isolation but duplicates shared fields (repo URL, branch, secrets files, backup config). Rejected — too much boilerplate for users.
- **Config inheritance / overlay**: Target overrides base config. Elegant but complex to implement and reason about. Deferred — can be added later if targets share many fields.
- **External orchestration**: Run N daemons via docker-compose replicas with different env vars. Already possible today but loses coordination and visibility. Not a solution — it's the problem statement.

### Decision: Sequential target reconciliation

The daemon reconciles targets one at a time in list order. A failure on target A logs an error and alert, then target B proceeds while the shared cycle context remains live. If that context is canceled or its deadline expires, iteration stops as required by the canonical `Non-Live Cycle Context Stops Target Iteration` requirement.

**Why not parallel:**
- Git operations: single repo clone, single working tree. Parallel targets would need per-target clones or git worktrees, adding significant complexity.
- Resource contention: template rendering, SSH connections, Docker API calls. Sequential avoids thundering herd.
- Debugging: sequential execution produces linear, readable logs.
- YAGNI: most users have 2-3 targets. Sequential latency is acceptable.

**Future path to parallel:** Per-target repo directories + goroutine pool. The Target struct is designed to support this without schema changes.

### Decision: Per-target state files with naming convention

State files are named `deploy-state-<target-name>.json` under `StateDir`. The default target (backwards compat) uses `deploy-state.json`.

**Why not one aggregated state file:**
- Atomic writes: each target saves independently without read-modify-write races
- Failure isolation: corrupt state for target A doesn't affect target B
- Simplicity: `LoadState` / `SaveState` don't change signature, just path

### Decision: Per-target staging directories

Each target gets `<StagingDir>/<target-name>/` as its staging root. Template rendering writes to the target's staging dir. Deploy reads from it. Cleanup removes it after deploy.

**Why not shared staging with namespaced subdirs:**
- Risk of cross-contamination if cleanup fails mid-pipeline
- Simpler mental model: each target has a clean workspace

### Decision: Secrets scoping via key prefix

Secrets are still flat-merged from SOPS files. Per-target secrets use a naming convention:

```yaml
# Shared (all targets)
db_host: shared-db.local

# Target-specific
targets:
  unraid:
    db_password: secret1
  pi:
    db_password: secret2
```

The reconciler merges `targets.<name>.*` over the top-level keys when rendering templates for a given target. Templates don't change — `{{ .db_password }}` resolves to the correct value per target.

**Alternatives considered:**
- **Per-target secret files**: `BOSUN_SECRETS_FILES_<TARGET>`. More isolation but multiplies SOPS key management. Supported as an optional override but not the primary mechanism.
- **Dot-notation flattening**: `unraid.db_password` at top level. Ambiguous — is `unraid` a target name or a data key? Rejected.

### Decision: Backwards-compatible config migration

When `targets:` is absent from `bosun.yaml` (or empty), the reconciler creates a single implicit target from the flat config fields. This target has `Name: "default"` and uses the existing `StateFile`, `StagingDir`, etc. No user action required.

When `targets:` is present, flat target fields (`target_host`, `project_name`, etc.) are ignored with a deprecation warning.

### Decision: Two-layer locking for multi-target

Reconciliation uses a two-layer locking model:

1. **Process-wide single-flight gate** — an in-memory mutex that serializes all reconciliation entry points (daemon loop, CLI `bosun reconcile`, webhook triggers). Only one reconciliation cycle runs at a time within a process. Incoming triggers while a cycle is running set a dirty flag to coalesce a follow-up run after the current cycle completes.

2. **Per-target file locks** — each target gets `<LockDir>/reconcile-<target-name>.lock` (the implicit default target uses `reconcile.lock`). These prevent the same target from being reconciled by two separate processes (e.g., daemon on host A and CLI on host B sharing a lock directory via NFS). Per-target file locks are acquired inside the single-flight gate.

**Why two layers:**
- The single-flight gate protects the shared git worktree. Without it, a CLI `bosun reconcile --target=pi` could race with the daemon mid-cycle on `unraid`, both touching the same git clone.
- Per-target file locks protect cross-process overlap. The in-memory gate only serializes within one process; file locks extend protection across processes.
- Dirty-flag coalescing prevents trigger storms from queuing N redundant cycles.

**Why not per-target locks alone:**
- Per-target locks would allow concurrent reconciliation of different targets, but all targets share one git working tree. Concurrent git operations on the same worktree are unsafe.

**Future path to parallel:** Replace the single-flight gate with a per-target gate + per-target git clones. The file lock layer is already per-target and needs no changes.

## Risks / Trade-offs

- **Increased config complexity** → Mitigated by backwards compatibility. Single-target users see no change. Multi-target users opt in via `targets:` section.
- **Sequential latency** → For 3 targets at ~30s each, total cycle is ~90s. Acceptable for GitOps polling (typical interval 5–60 min). Webhook-triggered deploys may feel slower.
- **Secrets merge precedence** → Target-scoped secrets override shared secrets. If a user accidentally scopes a secret, the shared value is silently shadowed. Mitigated by logging which secrets are overridden at debug level.
- **State file proliferation** → N targets = N state files. Manageable for typical counts (2-5). `bosun status` aggregates all targets.

## Migration Plan

1. **Phase 0 (this proposal):** Spec + design only. No code changes.
2. **Phase 1:** Implement `Target` struct, config parsing, implicit default target. All tests pass with no config changes (backwards compat).
3. **Phase 2:** Per-target state files, per-target staging dirs, per-target lock files. Daemon iterates targets.
4. **Phase 3:** Secrets scoping, per-target alerting context, CLI `--target` flag.
5. **Phase 4:** Deprecation warnings for flat target fields when `targets:` is present.

**Rollback:** Remove `targets:` from `bosun.yaml`. Daemon falls back to implicit default target. No data migration — state files are additive.

## Open Questions

- Should `bosun doctor` validate all targets' connectivity (SSH reachability, Docker availability)?
- Should the daemon support per-target polling intervals, or is one interval for all targets sufficient?
- Should `bosun trigger` accept a `--target` flag to trigger a single target, or always trigger all?
- How should `bosun drift` display multi-target results — tabular summary or per-target sections?

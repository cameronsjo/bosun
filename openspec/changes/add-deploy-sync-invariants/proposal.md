# Change: Add deploy-sync invariants and per-file write observability

## Why

The local-deploy pipeline has a silent-success failure mode (GitHub #214, reproduced 2026-05-16). Templates render correctly into `/app/staging`, but the staging-to-appdata sync silently no-ops — the rendered files never overwrite the destination at `/mnt/user/appdata/<path>`. Compose-up runs against the stale on-disk file and exits 0 because nothing on disk changed. Every diagnostic surface reports success:

- HTTP webhook → 200
- Daemon log → `success: true`
- `docker compose up` → exit 0
- Drift check → `no declared services` (logged at `warn`, easily missed in a daemon stream)
- Post-sync hooks → only the catch-all fires (1 of 7 expected) because `WrittenFiles` is empty

The only way operators currently detect this is by checking host-side file mtimes after each deploy — which nobody automates. During the FreshRSS migration, the reproduction reporter applied a four-step manual workaround (render locally, scp, ssh into compose-up) four separate times before realizing the daemon was lying about success.

Two reinforcing signals from the reproduction logs:

- `declared_services: 0` is logged when `ExtractDeclaredState` (drift.go:79) finds no matching files. Today this is a `warn` and the pipeline continues. The current `Post-Deploy Verification` requirement explicitly says verification "SHALL only run when... declared services were extracted" — so zero-declared-services silently skips verification entirely.
- `CopyDirIfChanged` has zero per-file observability. The only log it emits is per-directory ("Syncing X..."), which means an operator can't distinguish "wrote 12 files" from "wrote 0 files" from the log stream.

## What Changes

- **`declared_services: 0` becomes a hard error.** When `ExtractDeclaredState` returns zero services, reconcile SHALL fail unless the operator opts in via `BOSUN_ALLOW_EMPTY_DECLARED_STATE=true` (escape hatch for genuinely empty repos, e.g. early scaffolding). `ExtractDeclaredState` SHALL distinguish "compose directory does not exist" (error) from "compose directory exists but contains no parseable services" (clearly logged warning).
- **Per-file write log lines** in `CopyDirIfChanged` and `CopyFileIfChanged`. Each file write emits `Debug` with `src`, `dst`, `bytes`. Each skip emits `Debug` with `reason=hash_match`. Today there is no way to observe per-file behavior from the log stream.
- **Post-deploy mtime invariant.** After every local deploy, the reconciler SHALL assert that each path in `WrittenFiles` exists at its destination with `mtime >= reconcileStartTime`. If a target's source directory is non-empty but its `WrittenFiles` is empty, the reconciler SHALL fail before compose-up runs. Escape hatch: `BOSUN_SKIP_DEPLOY_INVARIANT=true` for diagnostic/development scenarios.
- **New env-var configuration surface:**
  - `BOSUN_ALLOW_EMPTY_DECLARED_STATE` (default `false`)
  - `BOSUN_SKIP_DEPLOY_INVARIANT` (default `false`)
- **Pipeline order change:** the new mtime invariant runs after deploy sync completes and BEFORE compose-up. A successful compose-up against a stale on-disk file is the worst possible outcome of this bug, so blocking compose-up is the right place for the gate.

## Impact

- **Affected specs:** reconcile (modified — Pipeline Orchestration; added — Deploy Sync Invariants)
- **Affected code:**
  - `internal/reconcile/reconcile.go` — hard error on `declared_services: 0`; call new `verifyDeploy` between sync and compose-up
  - `internal/reconcile/drift.go` — `ExtractDeclaredState` returns distinct errors for missing-dir vs empty-dir
  - `internal/reconcile/deploy.go` — new `verifyDeploy` helper; uses `WrittenFiles` and `reconcileStartTime`
  - `internal/fileutil/fileutil.go` — per-file write/skip log lines in `CopyDirIfChanged` and `CopyFileIfChanged`
  - `internal/daemon/daemon.go` — `ConfigFromEnv()` parses two new env vars
  - `internal/log/` — no new components; reuse `log.Component("fileutil")` and existing reconcile component
- **Operator-visible changes:**
  - A reconcile that previously silently no-op'd now fails with a clear error pointing at the specific target whose `WrittenFiles` was empty.
  - Operators on intentionally-empty repos (early scaffolding, archive branches) MUST set `BOSUN_ALLOW_EMPTY_DECLARED_STATE=true` once.
- **CHANGELOG:** behavior change worth a top-line note; not a breaking change for normal operation (the silent-success case was never intended behavior).
- **Out of scope:**
  - Targeted root-cause fix (which staging-vs-discovery path mismatch is producing the empty `WrittenFiles`). The invariants must land first to confirm the root cause via a diagnostic run; the fix follows in a separate change.
  - The stale-config-after-daemon-restart issue (hooks not reloading from `bosun.yaml` until restart). Tracked separately.
  - A `bosun adopt` command for project-name relabeling. That belongs in #219 follow-up if at all.

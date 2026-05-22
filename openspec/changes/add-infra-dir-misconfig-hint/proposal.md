# Change: Suggest the correct BOSUN_INFRA_DIR when the compose directory is missing

## Why

GitHub #214's root cause was confirmed on 2026-05-19: the silent-success deploy
was caused by `BOSUN_INFRA_DIR="."` while the homelab's infrastructure
(`compose/` and `appdata/`) is nested under `unraid/`. With `InfraSubDir="."`
the entire repo became the infra root, so `ExtractDeclaredState` globbed a
non-existent `staging/compose`, `discoverDeployTargets` treated repo-metadata
dirs (`.beads`, `.claude`, `unraid`) as deploy targets, and the sync mapped
`staging/unraid → /mnt/appdata/unraid` — landing rendered output at a path
nothing reads.

The `add-deploy-sync-invariants` change (Layer 1) already converts this into a
loud failure: `ExtractDeclaredState` now returns `ErrComposeDirMissing` and the
reconcile aborts. But the error only names the *missing* path
(`<staging>/compose`); it does not tell the operator what to set instead. The
operator's own config comment — `# . = repo root, templates in unraid/` —
shows the mental-model gap that produced the misconfiguration: they expected
Bosun to "find templates under unraid/" when `InfraSubDir` *is* the infra root.

Bosun already has enough on disk to diagnose this precisely. When the configured
compose directory is missing, a sibling directory almost always contains the
real `compose/`. Naming it turns a dead-end error ("compose dir missing") into a
one-line fix ("did you mean `BOSUN_INFRA_DIR=unraid`?").

## What Changes

- **Missing-compose-dir errors gain a diagnostic hint.** When the configured
  infra/staging directory has no `compose/` child, the reconciler SHALL scan the
  directory's immediate children for any that themselves contain a `compose/`
  subdirectory. When one or more candidates are found, the failure SHALL name
  them and suggest the `BOSUN_INFRA_DIR` value that would point at the infra
  root.
- **The hint is diagnostic only.** It does NOT auto-correct `InfraSubDir` or
  change any deploy behavior. `ErrComposeDirMissing` still fails the reconcile
  unconditionally (per the existing Deploy Sync Invariants requirement). The
  hint only enriches the error message and log.
- **No new configuration surface.** No new env vars, no new config fields. The
  scan is bounded (one level of children, checking for a single subdirectory)
  so it adds negligible latency to an already-failing path.

## Impact

- **Affected specs:** reconcile (added — Infra Directory Misconfiguration Hint)
- **Affected code:**
  - `internal/reconcile/drift.go` — `ExtractDeclaredState` (or a sibling helper)
    scans for candidate infra dirs when `composeDir` is absent; returns the
    candidates alongside `ErrComposeDirMissing`.
  - `internal/reconcile/reconcile.go` — the stage-6 call site, which knows the
    current `InfraSubDir`, composes the suggested `BOSUN_INFRA_DIR` value
    (`filepath.Join(currentInfraSubDir, candidate)`) into the surfaced error.
- **Operator-visible change:** a reconcile that previously failed with
  `compose dir missing: /app/staging/compose` now fails with an actionable
  message naming the candidate and the env var to set.
- **CHANGELOG:** patch-level improvement to error messaging; not a behavior or
  contract change (the failure still happens; only the message improves).
- **Out of scope:**
  - Auto-correcting or inferring `InfraSubDir` at runtime — surfacing a
    suggestion is safe; silently changing where Bosun deploys is not.
  - The homelab config fix itself (`BOSUN_INFRA_DIR=unraid` + hook globs) — that
    lives in the `cameronsjo/homelab` repo, not Bosun.
  - A `bosun doctor` mirror of this check — a reasonable follow-up, but the
    reconcile-time error is where operators actually hit the failure.

# Change: Path-Aware Deploy Skipping

## Why

Bosun runs the full reconciliation pipeline (decrypt, render, sync, compose up, verify) for every commit — even docs-only or beads-tracking commits. On Unraid's FUSE filesystem, unnecessary file rewrites cause stale file handles (#40). A `deploy_paths` allowlist lets the reconciler skip the pipeline when no infrastructure files changed, saving ~80s per skipped commit and eliminating false FUSE invalidation.

## What Changes

- Add `deploy_paths` field to `bosun.yaml` config — a list of glob patterns defining which paths are deploy-relevant.
- Add `BOSUN_DEPLOY_PATHS` env var override (JSON array, fully replaces config file value).
- After git sync and config reload (but before circuit breaker), diff changed files against the allowlist.
- If no changed files match any `deploy_paths` pattern, record the commit as deployed and skip the pipeline.
- If `DiffFiles` fails (shallow clone), fall through to full deploy (safe fallback, same pattern as post-sync hooks).
- `--force` bypasses the path check.
- Reuse the existing `matchGlob()` function from post-sync hooks for pattern matching.

## Impact

- Affected specs: `reconcile`
- Affected code:
  - `internal/config/config.go` — add `deploy_paths` to YAML DTO, Config struct, extract helper, Load/LoadFrom
  - `internal/reconcile/reconcile.go` — add to Config, ReloadedConfig, reloadProjectConfig, Run pipeline
  - `internal/reconcile/hooks.go` — add `MatchAnyPath()` utility
  - `internal/daemon/daemon.go` — wire in ConfigFromEnv + ConfigReloader
  - `internal/cmd/reconcile.go` — wire in runReconcile + ConfigReloader
- All consumers:
  - `config.DeployPaths()` — called by daemon ConfigFromEnv, CLI runReconcile, and ConfigReloader closures
  - `reconcile.MatchAnyPath()` — called by `Run()` pipeline skip logic
  - `reconcile.Config.DeployPaths` — read by `Run()` and updated by `reloadProjectConfig()`
  - `reconcile.ReloadedConfig.DeployPaths` — returned by ConfigReloader closures in daemon and CLI

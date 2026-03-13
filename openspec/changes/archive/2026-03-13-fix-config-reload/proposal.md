# Change: Reload bosun.yaml from repo after git pull

## Why

`bosun.yaml` is loaded once at daemon startup and cached for the daemon's lifetime. When hook config or settle delay changes are pushed to the repo, the daemon pulls the new commit but ignores the updated `bosun.yaml`. This requires a manual daemon restart to pick up config changes — defeating the purpose of GitOps.

## What Changes

- Add `config.LoadFrom(dir string)` — loads project config from a specific directory instead of walking up from CWD
- Add a "reload project config" step to the reconcile pipeline between git sync and template rendering
- When the repo contains a `bosun.yaml`, re-parse it and update `PostSyncHooks` and `HookSettleDelay` on the reconciler's config before proceeding
- Log when config changes are detected (action-oriented: "Reloaded project config from repo. Hooks: N, SettleDelay: Xs")
- Env var overrides (`BOSUN_POST_SYNC_HOOKS`, `BOSUN_HOOK_SETTLE_DELAY`) still take precedence — if set, the repo config is ignored for those fields (preserving existing precedence semantics)

## Impact

- Affected specs: `reconcile`
- Affected code: `internal/config/config.go`, `internal/reconcile/reconcile.go`
- All consumers:
  - `config.Load()` — called by daemon `ConfigFromEnv()`, CLI `runReconcile()`, and many CLI commands (unchanged)
  - `config.LoadFrom()` — new function, called by reconciler after git sync
  - `r.config.PostSyncHooks` — read by `executePostSyncHooks()` in reconcile.go
  - `r.config.HookSettleDelay` — read by `ExecutePostSyncHooks()` in hooks.go
  - `ConfigFromEnv()` env var overrides — NOT affected (still take precedence)

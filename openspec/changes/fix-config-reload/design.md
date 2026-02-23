## Context

The daemon loads `bosun.yaml` once at startup via `config.Load()`, which walks up from CWD to find the project root. During reconciliation, the repo is cloned/pulled to a separate directory (`r.config.RepoDir`, default `/app/repo`). The updated `bosun.yaml` in the repo is never re-read.

Only two fields come from `bosun.yaml` into the reconcile config: `PostSyncHooks` and `HookSettleDelay`. All other reconcile fields come from environment variables.

## Goals / Non-Goals

- **Goals:**
  - Reload `PostSyncHooks` and `HookSettleDelay` from the repo's `bosun.yaml` after each git pull
  - Preserve env var override precedence (env vars still win over file config)
  - Log when reloaded config differs from current

- **Non-Goals:**
  - Full daemon hot-reload (env vars, repo URL, polling intervals) — that's `bosun-8ci`
  - Reloading config fields beyond hooks (project name, deploy targets, etc.)
  - Watching for file changes outside of reconciliation cycles

## Decisions

- **New `config.LoadFrom(dir)` function** — `Load()` uses `FindRoot()` which walks up from CWD. The repo directory is known (`r.config.RepoDir`), so we need to load from a specific path. `LoadFrom` calls `loadConfigFile(dir)` directly, skipping `FindRoot()`.

- **Reload happens after git sync, before template rendering** — this is step 2.5 in the pipeline. The repo has been pulled but templates haven't rendered yet. If hooks change, the new hooks apply to this deploy cycle.

- **Env var precedence preserved** — the reconciler needs to know whether hooks/settle were set by env var or by file. We add two bool flags to `reconcile.Config`: `PostSyncHooksFromEnv` and `HookSettleDelayFromEnv`. When true, reload skips those fields.

- **Scope: hooks and settle delay only** — these are the only `bosun.yaml` fields consumed by the reconciler. Other fields (project name, infra containers, tunnel config) are used by CLI commands, not the reconcile pipeline.

## Risks / Trade-offs

- **Config parse failure mid-reconcile** — if the pushed `bosun.yaml` has a syntax error, the parse fails. We log a warning and keep the existing config. This matches the graceful degradation pattern already used at startup.

- **Per-cycle overhead** — parsing a small YAML file adds ~1ms per reconcile. Negligible compared to git pull, template rendering, and docker compose up.

## Open Questions

None.

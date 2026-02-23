# Config Reload — Breaking an Import Cycle with Function Injection

**Date:** 2026-02-23
**Type:** architecture
**Project:** bosun

## Goal

Fix a bug where `bosun.yaml` changes pushed to the repo were ignored until the daemon restarted. The daemon loaded config once at startup and cached it. After each `git pull`, the reconciler needed to re-read the config file and update post-sync hooks and settle delay — without requiring a restart.

## Architecture

The reconciliation pipeline gained a new step between git sync and secrets decryption:

```
1. Lock
2. Git clone/pull
3. **Reload project config from repo** (NEW)
4. Decrypt secrets (SOPS)
5. Render templates
...
```

The reload is surgical — only `PostSyncHooks` and `HookSettleDelay` are refreshed. These are the fields most likely to change between deploys (adding a new service that needs a hook, tuning settle timing). Other config sections (repo URL, paths, etc.) are immutable for the lifetime of a daemon process.

### The Import Cycle Problem

The naive implementation — calling `config.LoadFrom(dir)` directly from `reconcile.go` — creates a circular dependency:

```
config imports reconcile (for PostSyncHook type)
    └─ reconcile imports config (for LoadFrom)
        └─ cycle detected
```

Go's `PostSyncHook` struct lives in `reconcile` because that's where it's consumed (pattern matching, container restarts). The `config` package already imports `reconcile` to return hooks from its `extractPostSyncHooks()` helper. Adding the reverse import is impossible.

### Solution: Function Injection

Rather than restructuring packages or introducing a shared types package, the reconciler declares what it needs and lets callers provide it:

```go
// In reconcile package — defines the contract
type ReloadedConfig struct {
    PostSyncHooks   []PostSyncHook
    HookSettleDelay time.Duration
}

type ConfigReloaderFunc func(dir string) (*ReloadedConfig, error)
```

Both the daemon and CLI inject a closure that bridges `config.LoadFrom()` to `ReloadedConfig`:

```go
// In daemon.go and cmd/reconcile.go — provides the implementation
rcfg.ConfigReloader = func(dir string) (*reconcile.ReloadedConfig, error) {
    cfg, err := config.LoadFrom(dir)
    if err != nil {
        return nil, err
    }
    return &reconcile.ReloadedConfig{
        PostSyncHooks:   cfg.PostSyncHooks(),
        HookSettleDelay: cfg.HookSettleDelay(),
    }, nil
}
```

This keeps `reconcile` ignorant of `config` while giving it access to the parsed result. The pattern is Go's standard answer to circular dependencies: inversion of control through function types.

## Decisions Made

| Decision | Rationale |
|----------|-----------|
| Function injection over shared types package | Avoids a new package for two fields. The closure is 6 lines at each call site. A `types` package would add file, import, and maintenance overhead for minimal gain. |
| `LoadFrom(dir)` over extending `Load()` | `Load()` uses `FindRoot()` which walks up from CWD — wrong when the repo dir is known. `LoadFrom` takes an explicit path and skips discovery entirely. |
| Env var tracking via bool flags | `PostSyncHooksFromEnv` and `HookSettleDelayFromEnv` prevent repo config from overriding explicit env vars. Simple and explicit — no precedence matrix to reason about. |
| Graceful degradation on all failure modes | Missing config file, parse errors, nil reloader — all result in keeping the existing config. A daemon that silently keeps working is better than one that crashes on a YAML typo. |

## Gotchas

- **The import cycle wasn't obvious from reading the code.** `config` importing `reconcile` for `PostSyncHook` is a transitive dependency through the extract helpers. The cycle only surfaces when you try to add the reverse import.

- **`config.Load()` vs `config.LoadFrom()` matters.** The CLI's reloader initially used `Load()`, which calls `FindRoot()` from CWD. Inside a reconciliation, CWD is wherever the daemon started — not the repo directory. `LoadFrom(dir)` takes the explicit path the reconciler already knows (`r.config.RepoDir`).

- **Empty `ReloadedConfig` is a no-op signal.** When the repo has no `bosun.yaml`, the reloader returns `&ReloadedConfig{}` (zero-value fields). The reload method checks `len(hooks) == 0 && delay == 0` and skips the update. This prevents wiping existing config when the repo simply doesn't have a config file.

## Key Takeaways

- **Function injection is Go's escape hatch for import cycles.** Before reaching for a shared types package or restructuring, check if a `func` type on the consumer side breaks the cycle cleanly. It often does, especially when only one or two values cross the boundary.
- **Track the source of config values, not just the values.** The `FromEnv` bool flags are trivial to add but critical for correct precedence. Without them, every reload would silently overwrite intentional env var overrides.
- **Graceful degradation chains compound.** `loadConfigFile()` returns zero-value on missing file. `LoadFrom()` passes through. `reloadProjectConfig()` no-ops on zero-value or error. Each layer handles its failure mode independently, and the chain "just works" without coordinated error handling.
- **Test the reloader in isolation.** All 7 `reloadProjectConfig` tests inject mock `ConfigReloaderFunc` closures — no file I/O, no config parsing. This makes the tests fast, deterministic, and focused on the reload logic itself.
- **The plan's assumption was wrong, and that's fine.** The original plan assumed `reconcile` could import `config` directly. The import cycle was a deviation that required replanning mid-implementation. The function injection pattern was the fix — a better design than the naive approach would have been.

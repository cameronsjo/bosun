# ConfigField[T] Generic Refactor — Field Report

**Date:** 2026-03-24
**Type:** architecture
**Project:** bosun

## Goal

Replace 11 paired `*FromEnv` boolean fields across `reconcile.Config` and `daemon.Config` with a single `ConfigField[T]` generic type that tracks value source internally. Eliminate the pattern where every reloadable config field requires a companion boolean wired in 3 places.

## Architecture

### The Problem: Paired Shadow Fields

Every hot-reloadable config field had a companion boolean:

```go
PostSyncHooks        []PostSyncHook
PostSyncHooksFromEnv bool  // guards reload
```

This pattern repeated 11 times across 2 packages, requiring coordination in 3 locations:
1. **Struct definition** — declare the field AND the boolean
2. **ConfigFromEnv()** — parse the env var AND set the boolean
3. **reloadProjectConfig()** — check the boolean AND conditionally update

Missing any one of these three wiring points caused silent bugs (env vars overwritten by config reload, or config reload skipped for non-env values).

### The Solution: ConfigField[T]

```go
type ConfigSource int
const (
    SourceDefault ConfigSource = iota
    SourceFile
    SourceEnv
)

type ConfigField[T any] struct {
    Value  T
    Source ConfigSource
}
```

Key design choices:
- **Enum over boolean.** `ConfigSource` has three values (Default, File, Env) instead of two (fromEnv true/false). This distinguishes "never set" from "set from file" — useful for debugging and future extensibility (CLI flags, per-target overrides).
- **Exported Value field.** Callers access `.Value` directly rather than through a getter. This makes the migration mechanical (append `.Value` everywhere) and keeps the API familiar.
- **Unexported reloadField helper.** The generic helper stays in the `reconcile` package since it encodes reload semantics (nil-check for slices, zero-check for durations) that are specific to the hot-reload pattern.

### Consumer Impact

The `ConfigField[T]` type is consumed across three struct contexts that share field names:

| Struct | Package | Field Type | ConfigField? |
|--------|---------|-----------|-------------|
| `Config` | reconcile | `ConfigField[[]string]` | Yes |
| `ReloadedConfig` | reconcile | `[]string` | No — DTO from parser |
| `Target` | reconcile | `[]string` | No — per-target override |

This three-struct disambiguation was the primary source of migration difficulty.

## Decisions Made

**Accept ~195 callsite churn.** Every read of a ConfigField value changes from `r.config.PostSyncHooks` to `r.config.PostSyncHooks.Value`. The compiler catches every missed site — zero runtime risk. The alternative (keeping paired fields with renamed booleans) preserved the very pattern that causes bugs.

**3 incremental PRs, not big-bang.** Each PR migrates a subset of fields and is independently buildable/testable:
- PR #207: Foundation type + 2 fields (PostSyncHooks, HookSettleDelay)
- PR #208: 6 remaining reconcile fields
- PR #209: 3 daemon-only fields

**ConfigField lives in reconcile, not a shared package.** `daemon` already imports `reconcile`, so placing the type there adds zero new coupling. A separate `internal/configfield` package would add an import to both packages for no practical benefit.

**ReloadedConfig stays as plain types.** The DTO from the config parser has no concept of "source" — it's always from a file. The reload logic checks `field.FromEnv()` on the target Config, not the source ReloadedConfig.

## What Worked

**Compiler-driven migration.** Change the struct field type, then `go build ./...` produces every callsite that needs updating. No grep heuristics, no missed references. This is the ideal property for a large refactor.

**reloadField[T] helper.** The 8 nearly-identical 4-line reload blocks in `config_reload.go` collapsed to 8 one-liners:

```go
changed = reloadField(&r.config.PostSyncHooks, reloaded.PostSyncHooks,
    func(v []PostSyncHook) bool { return v != nil }) || changed
```

**Terse constructors for tests.** `NewConfigField(value)`, `EnvConfigField(value)`, `FileConfigField(value)` kept test struct literals readable without verbose `ConfigField[[]string]{Value: v, Source: SourceEnv}`.

## What Didn't Work

**Regex/script-based migration.** Python scripts to wrap struct literal fields couldn't distinguish Config from ReloadedConfig from Target contexts — all three have fields named `CriticalContainers`, `DeploySyncPaths`, etc. Every script over-wrapped ReloadedConfig and Target fields, requiring manual correction.

**The fix cycle:** script wraps 80% correctly → compiler catches remaining 20% → half of the 20% are over-wraps in wrong struct contexts → manual unwrap → repeat. Go AST tools (gorename, gopls rename) would have been context-aware and avoided this entirely.

## Gotchas

- **`replace_all` catches substrings.** `replace_all` on `PostSyncHooks` also matches `PostSyncHooksFromEnv`, turning it into `PostSyncHooks.ValueFromEnv`. Always check for substring matches before using `replace_all`.
- **Shallow copy of ConfigField is correct for value types** but slices still need deep-copy of the backing array. `ConfigForTarget` must clone `.Value` (the slice), not the ConfigField itself.
- **Go generics don't support `[]T` constraints.** A method like `DeepCopy[T any]()` on `ConfigField[[]T]` isn't expressible — you'd need `ConfigField[S ~[]T, T any]` which Go doesn't support. Keep the explicit `append([]string(nil), ...)` pattern.

## Key Takeaways

- When the same field name appears in multiple structs with different types, automated refactoring needs AST awareness — regex can't distinguish struct contexts
- `ConfigField[T]` makes it impossible to add a reloadable field without source tracking — the compiler enforces what convention couldn't
- Three-value enums (Default/File/Env) beat booleans for source tracking — the zero value is meaningful and the enum is extensible
- Stacked PRs work well for incremental refactors — each PR is independently reviewable and the compiler guarantees each step is complete
- The `reloadField[T]` helper reduced config_reload.go from 40+ lines to 8, making the reload logic scannable at a glance

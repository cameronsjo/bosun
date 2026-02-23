# Post-Sync Hook Delay Controls — Field Report

**Date:** 2026-02-22
**Type:** architecture
**Project:** bosun

## Goal

Add two orthogonal timing knobs to post-sync hooks so that container restarts don't race against FUSE filesystem propagation on Unraid's shfs. A global `hook_settle_delay` pauses after deploy before any hooks run (filesystem concern), and a per-hook `delay` pauses before restarting a specific container (container-specific concern).

## Architecture

### The Problem

Post-sync hooks (shipped in the deploy-resilience feature) restart containers immediately after `docker compose up` writes config files. On FUSE filesystems like Unraid's shfs, there's a propagation delay — a freshly-restarted container process may read stale config because the VFS page cache hasn't settled. The symptom: Traefik restarts but loads the *previous* config.

### Two-Level Design

The solution uses two independent delays that compose naturally:

| Knob | Scope | Purpose | Default |
|------|-------|---------|---------|
| `hook_settle_delay` | Global (config) | Pause after deploy, before any hooks run | `0` (disabled) |
| `delay` | Per-hook | Pause before restarting this specific container | `0` (disabled) |

These are orthogonal: the settle delay handles filesystem-level propagation (affects all hooks equally), while the per-hook delay handles container-specific needs (e.g., Traefik needs 5s but Gatus is fine immediately).

```yaml
# bosun.yaml
hook_settle_delay: "2s"
post_sync_hooks:
  - paths: ["traefik/conf.d/**"]
    action: restart
    container: traefik
    delay: "5s"
  - paths: ["gatus/config.yaml"]
    action: restart
    container: gatus
```

### Custom Duration Type

Go's `time.Duration` doesn't unmarshal from YAML/JSON strings like `"5s"`. Rather than scattering parse logic across consumers, a `reconcile.Duration` wrapper was created with:

- `UnmarshalJSON` — handles both string `"5s"` and number `5` (as seconds)
- `UnmarshalYAML` — handles string `"5s"` or bare seconds `"5"`
- `MarshalJSON` / `MarshalYAML` — round-trip as Go duration strings
- `IsZero()` — supports `omitempty` in YAML/JSON tags

The internal `parse()` method tries `time.ParseDuration(s)` first, then falls back to `time.ParseDuration(s + "s")` for bare seconds. This mirrors the existing `parseDurationOrSeconds` pattern in `daemon.go`.

### Config Flow

The delay values flow through three layers:

```
bosun.yaml → config.Load() → reconcile.Config → ExecutePostSyncHooks()
     ↑                                ↑
  YAML tags                     env var overrides
```

Each consumer (CLI `reconcile.go`, daemon `daemon.go`) independently loads from config and allows env var override via `BOSUN_HOOK_SETTLE_DELAY`. The CLI inlines a 4-line duration parser; the daemon reuses its existing `parseDurationOrSeconds` helper. A shared utility was considered and rejected — two call sites doesn't justify a new package.

## Decisions Made

1. **Wrapper type vs inline parsing**: Created `reconcile.Duration` instead of parsing at each call site. The type carries through the entire config pipeline cleanly and provides consistent error messages. Cost: one new file. Benefit: zero parse logic in config, CLI, or daemon code.

2. **Inline parser in CLI vs shared utility**: The daemon already had `parseDurationOrSeconds`. The CLI needed the same logic but lives in a different package. Rather than creating a shared `internal/duration` package for two call sites, the 4-line logic was inlined. YAGNI — if a third consumer appears, extract then.

3. **Context-cancellable waits**: Both delays use `select { case <-time.After(d): case <-ctx.Done(): }` instead of bare `time.Sleep()`. This follows the existing pattern at `reconcile.go:469-476` (startup grace period) and ensures clean shutdown during long delays.

4. **Env var completely replaces config**: `BOSUN_HOOK_SETTLE_DELAY` overrides (not merges with) the YAML value. This matches the existing precedent where `BOSUN_POST_SYNC_HOOKS` completely replaces hooks from `bosun.yaml`.

## Gotchas

- **Go's `time.Duration` is not a text marshaler**: This is a well-known Go pain point. The stdlib type marshals as nanosecond integers in JSON, which is unusable for human-authored config. Every Go project that accepts duration strings from config files ends up writing a wrapper.

- **YAML `omitempty` and custom types**: Go's `encoding/json` checks `IsZero()` for `omitempty`, but `gopkg.in/yaml.v3` has its own rules. The `MarshalYAML` method returns `nil` for zero values to suppress output — this is the idiomatic way to get `omitempty` behavior in YAML.

- **macOS test paths**: `/var` symlinks to `/private/var` on macOS. Tests using `t.TempDir()` need `filepath.EvalSymlinks()` to avoid path comparison failures. This was already documented in CLAUDE.md but worth noting as an active gotcha.

## Key Takeaways

- Two orthogonal knobs (global settle + per-hook delay) compose better than a single "delay" field that tries to serve both filesystem and container concerns
- Custom marshal/unmarshal types in Go pay for themselves quickly — the Duration wrapper eliminated parse logic from 4 different files
- Plan-driven execution across 13 files with interdependencies worked cleanly as sequential steps; parallelization wasn't worth the coordination cost for this shape of work
- The existing `parseDurationOrSeconds` pattern in the daemon served as a design reference — new code should match existing project patterns before inventing its own
- All 15 test packages passed on first run with zero errors, validating that the plan's understanding of the codebase was accurate

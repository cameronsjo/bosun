# Change: Add post-sync hook configuration to bosun.yaml

## Why

Post-sync container restart hooks are fully implemented at the runtime level (glob matching, deduplication, Docker restart) but users cannot configure them declaratively. The only config path is a `BOSUN_POST_SYNC_HOOKS` JSON environment variable in the daemon, which is ergonomically poor for a YAML-native tool. The CLI `bosun reconcile` command has no hook loading at all.

This blocks issue #38 (Restart Traefik after config file sync) — the hook code works, but there's no user-facing way to declare hooks.

Closes #38

## What Changes

- Add `post_sync_hooks` field to `bosun.yaml` config schema
- Expose hooks from `config.Config` for both daemon and CLI consumption
- Wire `bosun reconcile` CLI command to load hooks from project config
- Preserve `BOSUN_POST_SYNC_HOOKS` env var as an override (same precedence as other env var overrides)

## Impact

- Affected specs: `reconcile` (modify existing Post-Sync Container Restart Hooks requirement)
- Affected code: `internal/config/config.go`, `internal/cmd/reconcile.go`, `internal/config/config_test.go`
- No breaking changes — env var path continues to work, YAML is additive

# Change: FUSE-safe post-deploy hooks and glob correctness

## Why

The April 2026 reconcile-path bug hunt (Cluster D) found that the post-sync hook
pipeline — the exact mechanism bosun ships to work around Unraid's FUSE config
propagation — has multiple silent-failure modes that make a hook report success
while never actually firing or never being seen by the target service.

The glob matcher does a prefix-only check and discards everything after `**`, so
suffix filters (`appdata/**/dynamic.yml`) silently degrade and any `**/foo`
pattern matches every changed file (#232, P0). Because the same matcher backs
`deploy_paths` / `deploy_sync_paths` / `deploy_sync_exclude`, this corrupts
path-aware deploy skipping too. Hooks fire synchronously after a write returns,
but `HookSettleDelay` defaults to `0` and the destination directory is never
fsynced — on FUSE the renamed config is invisible when the hook restarts the
container, so Traefik re-reads stale config and the "Restarted traefik" log lies
(#233, P0). Deletion-only commits remove files via `removeStaleFiles` but never
record them in `WrittenFiles`, so the hook matcher sees zero changes and never
fires — stale prod routes persist (#234, P0). A typo'd glob that matches nothing
returns silently and no-ops forever (#269, P1). Hot-reload of `bosun.yaml`
silently zeroes `HookSettleDelay` and drops `post_sync_hooks` when the keys are
absent, regressing an operator's FUSE workaround weeks later (#267 / #268, P1).
An `exec` hook with an empty command is silently skipped (#283, P2), and a
post-write content-verification failure makes a file silently miss future hooks
(#282, P2).

There is no spec describing FUSE-safe timing or glob semantics, so these are
implementation gaps with no authoritative requirement to regress against. This
proposal adds a `reconcile-fuse-hooks` capability for glob/timing/observability
correctness and modifies the existing `reconcile` "Post-Sync Container Restart
Hooks" requirement to cover deletion-aware change tracking and empty-command
rejection.

## What Changes

- **Glob matching correctness (#232)** — recursive `**` matching SHALL honor the
  suffix after `**`; suffix and infix filters work, and `**/foo` SHALL NOT match
  unrelated files. **BREAKING**: patterns previously matching by prefix-only luck
  may stop matching; this corrects silently-wrong semantics shared with
  `deploy_paths`.
- **FUSE-safe hook timing (#233)** — the reconciler SHALL fsync each destination
  directory after writing files and SHALL apply a non-zero settle delay before
  running post-sync hooks, with a safe default rather than `0`, and `doctor`
  SHALL warn when a FUSE-like target runs with a zero delay.
- **Deletion-aware hooks (#234)** — deletion-only commits SHALL record removed
  paths in the deploy change set so matching hooks fire. **MODIFIED** reconcile
  requirement.
- **Hook match observability (#269)** — when configured hooks evaluate changed
  files and nothing matches, the reconciler SHALL emit a discoverable warning
  naming the patterns and sample files.
- **Empty hook command rejection (#283)** — a hook whose action requires a
  command (`exec`) with an empty command SHALL be a config-load error, not a
  silent skip. **MODIFIED** reconcile requirement.
- **Post-write verification propagation (#282)** — a successful write whose
  post-write content verification fails SHALL still be recorded as written (so
  the hook fires) or SHALL surface as an error; it SHALL NOT silently vanish from
  the change set.
- **Hot-reload removal semantics (#267 / #268)** — absent `post_sync_hooks` /
  `hook_settle_delay` keys on hot-reload SHALL retain the prior in-memory value
  (DTO pointer fields distinguish "absent" from "explicitly set"); operators
  clear hooks with `post_sync_hooks: []` and reset the delay with an explicit
  `hook_settle_delay: 0s`.

## Impact

- Affected specs: NEW capability `reconcile-fuse-hooks`; MODIFIED requirement
  `Post-Sync Container Restart Hooks` in `reconcile`.
- Affected code:
  - `internal/reconcile/hooks.go` — `matchGlob` (`:105-122`), hook execution
    loop and empty-command skip (`:139-155`, `:188-194`), settle-delay
    application
  - `internal/reconcile/reconcile.go` — `executePostSyncHooks` empty-match early
    return (`:769-786`)
  - `internal/reconcile/deploy.go` — `removeStaleFiles` (`:155-158`, `:251-299`),
    `WrittenFiles` accumulation, post-write verification handling (`:314-321`)
  - `internal/fileutil/fileutil.go` — `CopyFile` (no dir fsync after rename,
    `:60-110`), `CopyFileIfChanged` post-write verification (`:217-221`)
  - `internal/reconcile/config_reload.go` — reload predicates (`:36`, `:38-41`)
  - `internal/reconcile/configfield.go` — `PostSyncHook` reload field (`:59-66`)
  - `internal/daemon/daemon.go` — `ReloadedConfig{HookSettleDelay}` build
    (`:1621-1624`)
  - `internal/config` — `PostSyncHook` DTO + validation, `HookSettleDelay`
    parsing
  - `internal/preflight` — `doctor` FUSE/zero-delay check
- All consumers of the glob matcher and hook change set (each needs its own
  scenario + task):
  - `matchGlob` callers: post-sync hook path matching (`hooks.go`), `matchAnyPath`
    for `deploy_paths` / `deploy_sync_paths` / `deploy_sync_exclude`
    (`deploy.go` path-aware skip)
  - hook change-set producers: `CopyDirIfChanged` (added/changed),
    `removeStaleFiles` (deleted), git-diff fallback
  - hot-reload consumers: daemon reloader (`daemon.go`), `config_reload.go`
    appliers for `PostSyncHooks` and `HookSettleDelay`
- New / changed config + env vars:
  - `BOSUN_HOOK_SETTLE_DELAY` default becomes a non-zero safe value (documented
    breaking default change)
  - `post_sync_hooks` / `hook_settle_delay` DTO fields become pointer-typed for
    absent-vs-set distinction (internal; affects reload semantics)
- Docs: `skills/onboard/resources/gitops.md` (hook timing, FUSE), `docs/gitops.md`
  / `docs/troubleshooting.md`, `CLAUDE.md` env-var table (settle-delay default,
  reload semantics).

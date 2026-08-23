## 1. Glob matching correctness (#232)

- [ ] 1.1 Replace `matchGlob` (`hooks.go:105-122`) prefix-only `**` handling with full doublestar semantics (suffix/infix honored, `**/foo` does not match unrelated files)
- [ ] 1.2 Confirm `matchAnyPath` (`deploy.go`) uses the corrected matcher for `deploy_paths` / `deploy_sync_paths` / `deploy_sync_exclude`
- [ ] 1.3 Tests: `matchGlob("**/foo.yml", "unrelated/bar.yml")==false`; `matchGlob("appdata/**/dynamic.yml", "appdata/traefik/dynamic.yml")==true`; deploy-path family regression cases

## 2. FUSE-safe hook timing (#233)

- [ ] 2.1 fsync the destination directory after rename in `fileutil.CopyFile` (`:60-110`)
- [ ] 2.2 Change `HookSettleDelay` default from `0` to a safe non-zero value; apply the delay before running hooks in `hooks.go`
- [ ] 2.3 Add a `doctor`/preflight check warning when a FUSE-like target runs with `hook_settle_delay: 0s`
- [ ] 2.4 Tests: dir fsync invoked after write; non-zero default applied; doctor warns on zero-delay FUSE target

## 3. Deletion-aware hooks (#234)

- [ ] 3.1 Append `removeStaleFiles` deletions (`deploy.go:155-158`, `:251-299`) to the deploy change set (`WrittenFiles` or parallel `DeletedFiles`), tagged op=remove
- [ ] 3.2 Ensure `executePostSyncHooks` (`reconcile.go:769-786`) evaluates the merged add+remove set (move empty-match return below the merge)
- [ ] 3.3 Tests: deletion-only commit matching a hook pattern fires the hook

## 4. Hook match observability (#269)

- [ ] 4.1 In `executePostSyncHooks`, emit a warn-level log when hooks are configured and changed files were evaluated but nothing matched, naming patterns and sample files
- [ ] 4.2 Distinguish "no files changed" from "files changed, none matched" in the log message
- [ ] 4.3 Tests: typo'd pattern over a non-empty change set produces a discoverable warning

## 5. Empty hook command rejection (#283)

- [x] 5.1 Validate at config load that an `exec` hook has a non-empty command; reject with a clear error
- [x] 5.2 Remove (or make unreachable) the silent warn-and-continue skip in `hooks.go:188-194`
- [x] 5.3 Tests: config with `action: exec` and empty/absent command fails to load

## 6. Post-write verification propagation (#282)

- [x] 6.1 In `CopyFileIfChanged` (`fileutil.go:217-221`) / `deploy.go:314-321`, ensure a successful rename whose post-write verification fails still records the path in the change set (or surfaces a hard error) — never silently omits it
- [x] 6.2 Tests: simulated verification failure still results in the path being hook-eligible (not skipped on retry)

## 7. Hot-reload removal semantics (#267 / #268)

- [ ] 7.1 Make the reload DTO field for `hook_settle_delay` a `*time.Duration` (`config_reload.go:38-41`, `daemon.go:1621-1624`); reloader overwrites only when non-nil
- [ ] 7.2 Make the reload DTO field for `post_sync_hooks` a `*[]PostSyncHook` (`config_reload.go:36`, `configfield.go:59-66`); absent key ⇒ nil ⇒ retained
- [ ] 7.3 Tests: absent `hook_settle_delay` retains prior value; explicit `0s` zeroes it; absent `post_sync_hooks` retains hooks; `post_sync_hooks: []` clears them

## 8. Documentation

- [ ] 8.1 Update `skills/onboard/resources/gitops.md` (hook timing, FUSE, deletion-aware hooks, glob semantics)
- [ ] 8.2 Update `docs/gitops.md` and `docs/troubleshooting.md`
- [ ] 8.3 Update the `CLAUDE.md` env-var table (settle-delay default change, reload removal semantics)

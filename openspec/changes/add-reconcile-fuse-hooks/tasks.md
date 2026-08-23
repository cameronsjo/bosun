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

## 7. Hot-reload presence semantics (#267 / #268)

- [ ] 7.1 Preserve raw `hook_settle_delay` key presence through `internal/config`, daemon and CLI `ConfigReloader` closures, and `ReloadedConfig`; absent key retains the effective delay while explicit `0s` sets zero
- [ ] 7.2 Treat a successfully loaded root hook slice as authoritative in `reloadProjectConfig`: absent `post_sync_hooks` and explicit `[]` both clear file-sourced hooks, while `BOSUN_POST_SYNC_HOOKS` remains a replacement override
- [ ] 7.3 Preserve graceful degradation: a missing config file or read/parse error retains existing hooks and delay; keep invalid executable hooks on the existing fail-closed path
- [ ] 7.4 Apply root hook state before target overrides: absent target hooks inherit root, explicit target `[]` clears inheritance, and removal of target-specific hooks discards stale target state; preserve slice cloning and target isolation
- [ ] 7.5 Add table-driven config/reload tests covering initial load, absent vs explicit zero/empty, env replacement precedence, config deletion/error, and root/target combinations for both daemon and CLI reload closures
- [ ] 7.6 Run focused tests repeatedly and under `-race`, plus relevant full tests, vet, changed-code lint, build, and strict OpenSpec validation

## 8. Documentation

- [ ] 8.1 Update `skills/onboard/resources/gitops.md` (hook timing, FUSE, deletion-aware hooks, glob semantics)
- [ ] 8.2 Update `docs/gitops.md` and `docs/troubleshooting.md`
- [ ] 8.3 Update the `CLAUDE.md` env-var table (settle-delay default change, reload removal semantics)

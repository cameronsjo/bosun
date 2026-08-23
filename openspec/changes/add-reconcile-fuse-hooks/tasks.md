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

- [x] 4.1 In `executePostSyncHooks`, emit a warn-level log when hooks are configured and changed files were evaluated but nothing matched, with bounded pattern and staging-relative file samples plus complete counts
- [x] 4.2 Distinguish "no files changed" from "files changed, none matched" in the log message
- [x] 4.3 Tests: typo'd pattern over a non-empty change set produces a discoverable warning

## 5. Empty hook command rejection (#283)

- [x] 5.1 Validate at config load that an `exec` hook has a non-empty command; reject with a clear error
- [x] 5.2 Remove (or make unreachable) the silent warn-and-continue skip in `hooks.go:188-194`
- [x] 5.3 Tests: config with `action: exec` and empty/absent command fails to load

## 6. Post-write verification propagation (#282)

- [x] 6.1 In `CopyFileIfChanged` (`fileutil.go:217-221`) / `deploy.go:314-321`, ensure a successful rename whose post-write verification fails still records the path in the change set (or surfaces a hard error) — never silently omits it
- [x] 6.2 Tests: simulated verification failure still results in the path being hook-eligible (not skipped on retry)

## 7. Hot-reload presence semantics (#267 / #268)

- [x] 7.1 Preserve config-file-found metadata and raw `hook_settle_delay` key presence through `internal/config`, daemon and CLI `ConfigReloader` closures, and `ReloadedConfig`; distinguish a present empty file from no file, retain the effective delay when the key is absent, and honor explicit `0s`
- [x] 7.2 Treat a successfully loaded root hook slice as authoritative in `reloadProjectConfig`: absent `post_sync_hooks`, explicit `[]`, and a present empty config all clear file-sourced hooks, while missing/read/parse/unknown-field errors produce no snapshot and retain prior state
- [x] 7.3 Validate all root and target hooks before applying any hook-related field; invalid executable hooks abort the current reconciliation before deployment and retain prior hooks/delay, with source-aware redacted logs for apply/clear/retain/reject outcomes
- [x] 7.4 Apply root hook state before target overrides: absent target hooks inherit root, explicit target `[]` clears inheritance, removing a target hook key or target descriptor discards stale operational hooks and falls back to root, and structural target removal remains restart-gated
- [x] 7.5 Preserve replacement precedence for `BOSUN_POST_SYNC_HOOKS`, `BOSUN_HOOK_SETTLE_DELAY`, and authoritative `BOSUN_TARGETS` hook overrides; deep-clone hooks plus nested `Paths`/`Command` slices at loader, root, and target boundaries
- [x] 7.6 Add table-driven tests for initial load and both daemon/CLI reload closures covering absent vs explicit zero/empty, present-empty vs missing/malformed config, environment precedence, atomic invalid-hook rejection, root/target inheritance, explicit-empty, key/descriptor removal, slice mutation, redacted logs, and concurrent target reload under `-race`
- [x] 7.7 Add a failed-pipeline regression proving a template failure at commit B does not advance `DeployState.LastDeployedCommit` from A and the next hook fallback diff uses A; verify repo-relative fallback paths are normalized to the same staging-relative namespace as written/deleted paths
- [x] 7.8 Run focused tests repeatedly and under `-race`, plus relevant full tests, vet, changed-code lint, build, and strict OpenSpec validation

## 8. Documentation

- [x] 8.1 Update `skills/onboard/resources/configuration.md` (root/target presence, successful-snapshot, and environment precedence semantics)
- [x] 8.2 Update `skills/onboard/resources/gitops.md` (hook timing, FUSE, deletion-aware hooks, glob and reload semantics)
- [x] 8.3 Update `docs/gitops.md` and `docs/troubleshooting.md`
- [x] 8.4 Update the `AGENTS.md` env-var table (settle-delay default change and authoritative environment replacement semantics)

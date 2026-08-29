## Grounding Summary

- Original task count: 30
- Verified complete: 30
- Partial: 0
- Known runtime defects: 0
- Archive prerequisite: satisfied by PR #634 (`aaa182a`), released in v0.42.1
  (`ca655b5`), for tasks 1.3, 2.3, 8.2, and 8.4

## 1. Glob matching correctness (#232)

- [x] 1.1 Replace prefix-only `**` handling with `doublestar.Match` — PR #401 (`e2670e6`); `internal/reconcile/hooks.go` `matchGlob`
- [x] 1.2 Route hooks, `deploy_paths`, `deploy_sync_paths`, and `deploy_sync_exclude` through the same matcher — PR #401; `hooks.go`, `reconcile.go`, and `discovery.go`
- [x] 1.3 Cover recursive suffix/infix behavior through both `deploy_sync_paths` and `deploy_sync_exclude` with reachable one-level `discoverDeployTargets` candidates — PR #634 (`aaa182a`); `internal/reconcile/discovery_test.go`

## 2. FUSE-safe hook timing (#233)

- [x] 2.1 Sync destination parent directories after atomic rename and before post-write verification/hook execution — PR #402 (`5d7902f`), retained as deterministic unique-parent batching by PR #558 (`41a06ee`); `internal/fileutil/fileutil.go`
- [x] 2.2 Apply a 2-second fallback only when the delay is unconfigured and the deploy path is `/mnt/user` or a descendant; honor explicit zero and retain zero elsewhere — PRs #402 and #544 (`24d511b`); `hooks.go`
- [x] 2.3 Make `bosun doctor` distinguish omitted delay from explicit zero using runtime-equivalent file/environment precedence: omitted `/mnt/user` values pass through the safe fallback, explicit non-positive values warn, positive values pass, invalid environment input falls back to the file/default source, and non-FUSE/lookalike paths remain excluded — PR #634 (`aaa182a`); `internal/cmd/diagnostics_doctor.go` and `diagnostics_doctor_test.go`
- [x] 2.4 Cover directory sync seams, FUSE/non-FUSE/default/explicit-zero runtime resolution, and doctor path classification/warning behavior — PRs #402, #544, and #558 plus the source-aware task 2.3 regression; `directory_sync_test.go`, `hooks_test.go`, and `diagnostics_doctor_test.go`

## 3. Deletion-aware hooks (#234)

- [x] 3.1 Record removals separately in `DeployResult.DeletedFiles` with `AddDeleted` and staging-relative prefix helpers — PR #405 (`8cade1c`); `internal/reconcile/deploy.go`
- [x] 3.2 Union written and deleted paths before local hook matching, including mixed write/delete deploys — PR #405; `internal/reconcile/reconcile.go`
- [x] 3.3 Cover deletion-only and mixed write/delete matching plus deletion prefixing — PR #405 (`8cade1c`) covers mixed writes/deletions; PR #551 (`f6ed4a3`) adds direct deletion-only change-source and hook-selection regressions; reconcile/deploy tests

## 4. Hook match observability (#269)

- [x] 4.1 Warn when a non-empty evaluated change set matches no configured hook, with complete counts and bounded/redacted samples — PR #546 (`323f0aa`); `reconcile.go`
- [x] 4.2 Distinguish authoritative no-change from changed-but-unmatched input — PR #546
- [x] 4.3 Cover typoed patterns, bounds, duplicate/empty counts, redaction, and no-change logging — PR #546; reconcile tests

## 5. Empty hook command rejection (#283)

- [x] 5.1 Reject `exec` hooks with empty commands before deployment — PR #520 (`c56905e`); `ValidatePostSyncHooks`
- [x] 5.2 Remove the silent execution-time empty-command skip by making invalid hooks unreachable and validating again at execution — PR #520
- [x] 5.3 Cover root, target, environment, programmatic, startup, and reload invalid-command paths — PR #520; config/daemon/cmd/reconcile tests

## 6. Post-write verification propagation (#282)

- [x] 6.1 Preserve a successfully renamed path in the deploy result and return typed `fileutil.ErrPostWriteVerification`; run eligible remediation hooks without converting the deploy to success — PR #529 (`09b84cd`)
- [x] 6.2 Cover hash/readback failure, path preservation, matching and non-matching hooks, failed state, alerting, and retry semantics — PR #529; `fileutil_verification_test.go` and `post_write_verification_test.go`

## 7. Hot-reload presence semantics (#267 / #268)

- [x] 7.1 Preserve config-file and raw delay-key presence through config, daemon/CLI reload closures, and `ReloadedConfig` — PRs #540 (`41041ba`) and #544 (`24d511b`)
- [x] 7.2 Clear file-owned root hooks on an omitted/empty successful snapshot while retaining state on missing/read/parse/unknown-field failures — PR #544; `config_reload.go`
- [x] 7.3 Validate all root/target hooks before atomic mutation and abort before deployment for invalid executable hooks with redacted logs — PRs #520 and #544
- [x] 7.4 Apply root before target overrides; implement inheritance, explicit empty, key removal, descriptor removal fallback, and restart-gated topology — PR #544
- [x] 7.5 Preserve `BOSUN_POST_SYNC_HOOKS`, `BOSUN_HOOK_SETTLE_DELAY`, and authoritative `BOSUN_TARGETS` ownership; deep-clone nested hook slices — PR #544
- [x] 7.6 Cover daemon/CLI initial/reload parity, presence cases, environment ownership, atomic rejection, target transitions, slice isolation, redaction, and race behavior — PR #544
- [x] 7.7 Keep `DeployState.LastDeployedCommit` at the last successful deployment across failed pipelines and normalize git fallback paths into the staging-relative namespace — PR #544
- [x] 7.8 Run focused/full/race/vet/lint/build/OpenSpec gates recorded in the merged delivery PRs #520, #529, #544, and #546

## 8. Documentation

- [x] 8.1 Document root/target presence, successful-snapshot, and environment precedence in `skills/onboard/resources/configuration.md` — PR #544
- [x] 8.2 Document the exact unconfigured `/mnt/user` 2-second fallback, valid environment precedence and invalid-input fallback, explicit zero, unconfigured non-FUSE zero, and doctor warning contract in `skills/onboard/resources/gitops.md` — PR #634 (`aaa182a`)
- [x] 8.3 Update `docs/gitops.md` and `docs/troubleshooting.md` for the released hook/change-source/diagnostic contract — PRs #405, #544, and #546
- [x] 8.4 Document the exact unconfigured `/mnt/user` 2-second fallback, valid environment precedence and invalid-input fallback, explicit zero, and unconfigured non-FUSE zero in `AGENTS.md` — PR #634 (`aaa182a`)

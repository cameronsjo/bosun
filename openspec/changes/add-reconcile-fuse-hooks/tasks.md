## Grounding Summary

- Original task count: 30
- Verified shipped: 29
- Partial: 1 (task 1.3)
- Known runtime defects: 0
- Archive blocker: focused recursive suffix/infix regressions through both
  deploy-sync filter consumers

## 1. Glob matching correctness (#232)

- [x] 1.1 Replace prefix-only `**` handling with `doublestar.Match` — PR #401 (`e2670e6`); `internal/reconcile/hooks.go` `matchGlob`
- [x] 1.2 Route hooks, `deploy_paths`, `deploy_sync_paths`, and `deploy_sync_exclude` through the same matcher — PR #401; `hooks.go`, `reconcile.go`, and `discovery.go`
- [ ] 1.3 PARTIAL: suffix/infix and leading-recursive tests cover `matchGlob` and `matchAnyPath` (`hooks_test.go`), but focused `discoverDeployTargets` regressions do not yet exercise recursive suffix/infix behavior through both `deploy_sync_paths` and `deploy_sync_exclude`

## 2. FUSE-safe hook timing (#233)

- [x] 2.1 Sync destination parent directories after atomic rename and before post-write verification/hook execution — PR #402 (`5d7902f`), retained as deterministic unique-parent batching by PR #558 (`41a06ee`); `internal/fileutil/fileutil.go`
- [x] 2.2 Apply a 2-second fallback only when the delay is unconfigured and the deploy path is `/mnt/user` or a descendant; honor explicit zero and retain zero elsewhere — PRs #402 and #544 (`24d511b`); `hooks.go`
- [x] 2.3 Warn in `bosun doctor` when an effective zero delay is used with a segment-aware `/mnt/user` path — PR #402; `internal/cmd/diagnostics_doctor.go`
- [x] 2.4 Cover directory sync seams, FUSE/non-FUSE/default/explicit-zero resolution, and doctor warning/lookalike paths — PRs #402, #544, and #558; `directory_sync_test.go`, `hooks_test.go`, and `diagnostics_doctor_test.go`

## 3. Deletion-aware hooks (#234)

- [x] 3.1 Record removals separately in `DeployResult.DeletedFiles` with `AddDeleted` and staging-relative prefix helpers — PR #405 (`8cade1c`); `internal/reconcile/deploy.go`
- [x] 3.2 Union written and deleted paths before local hook matching, including mixed write/delete deploys — PR #405; `internal/reconcile/reconcile.go`
- [x] 3.3 Cover deletion-only and mixed write/delete matching plus deletion prefixing — PR #405; reconcile/deploy tests

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
- [x] 8.2 Document hook timing, `/mnt/user` fallback, deletion-aware inputs, recursive globs, and reload semantics in `skills/onboard/resources/gitops.md` — PRs #402, #405, #544, and #546
- [x] 8.3 Update `docs/gitops.md` and `docs/troubleshooting.md` for the released hook/change-source/diagnostic contract — PRs #405, #544, and #546
- [x] 8.4 Update `AGENTS.md` for the exact settle-delay default, environment replacement, staging-relative paths, and reload presence semantics — PRs #402 and #544

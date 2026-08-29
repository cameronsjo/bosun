# Change: Ground FUSE-safe post-deploy hooks and glob correctness

## Why

Bosun's FUSE-hook hardening was proposed in PR #311 and delivered incrementally,
principally through PRs #401, #402, #405, #520, #529, #544, and #546. At
grounding time, the active change ledger had never been reconciled with those
merges: ten early tasks remained unchecked, several design statements described
pre-implementation choices, and issue #431 presented the whole change as
unimplemented.

At the time of the spec-only grounding correction, twenty-six of the original
thirty tasks had exact merged code, test, or documentation evidence. Task 1.3
remained partial because
recursive suffix/infix behavior is tested directly for hooks and `deploy_paths`,
but not through both deploy-sync filter consumers (`deploy_sync_paths` and
`deploy_sync_exclude`). Task 2.3 remained partial because `bosun doctor` treated
an omitted file value as zero and warned even though reconcile applied the safe
2-second runtime fallback. Documentation tasks 8.2 and 8.4 remained partial: the
onboard GitOps resource and `AGENTS.md` mention generic fallback behavior and a
2-second example, but neither states the exact unconfigured `/mnt/user`
fallback contract. The shared doublestar implementation has no known runtime
defect. PR #634 (`aaa182a`) subsequently closed all four grounding-time gaps,
and release v0.42.1 (`ca655b5`) supplied the evidence required for archive.

## What Changes

- Ground recursive `**` semantics in the shared matcher used by post-sync hooks,
  `deploy_paths`, `deploy_sync_paths`, and `deploy_sync_exclude`.
- Ground FUSE durability as destination-directory sync plus a 2-second fallback
  only for unconfigured `/mnt/user` targets. Explicit zero remains an operator
  override and non-FUSE targets retain the zero default.
- Ground deletion-aware hook inputs as the union of separate written and deleted
  staging-relative paths; no add/remove selector is introduced.
- Ground bounded no-match diagnostics, fail-closed empty `exec` validation,
  typed post-write verification failure handling, and presence-aware root/target
  reload semantics.
- Modify the existing reconcile hook requirement as a complete replacement of
  that requirement only. The delta preserves its canonical scenarios and
  composes with newer pipeline and directory-aware changes instead of replacing
  either capability.
- At grounding time, leave task 1.3 open for focused `deploy_sync_paths` and
  `deploy_sync_exclude` recursive suffix/infix regressions, using the existing
  one-level `appdata/<child>` discovery contract.
- At grounding time, leave task 2.3 open until doctor distinguishes an omitted
  delay (whose effective runtime value is the 2-second fallback) from explicit
  zero.
- At grounding time, leave tasks 8.2 and 8.4 open until the onboard GitOps
  resource and `AGENTS.md` state that the 2-second fallback applies only to an
  unconfigured exact or descendant `/mnt/user` path, with explicit zero and
  non-FUSE behavior intact.
- Close those four tasks only after the focused behavior, test, and documentation
  evidence ships; PR #634 and v0.42.1 provide that final evidence.

## Grounding Evidence

- PR #401 (`e2670e6`) replaced prefix matching with
  `github.com/bmatcuk/doublestar/v4` and added direct hook/`deploy_paths` tests.
- PR #402 (`5d7902f`) added destination-directory sync, the `/mnt/user` fallback,
  doctor warning, and focused tests; PR #544 (`24d511b`) later made explicit
  zero distinguishable from an unconfigured default in reconcile configuration.
  At grounding time, the doctor check still ignored that distinction and falsely
  warned for the fallback case; PR #634 corrected it.
- PR #405 (`8cade1c`) added `DeletedFiles`, staging-relative prefixing, and union
  evaluation, including the mixed write/delete regression. PR #551
  (`f6ed4a3`) later added direct deletion-only change-source and hook-selection
  regressions.
- PR #520 (`c56905e`) rejects empty `exec` commands across root, target,
  environment, and programmatic configuration.
- PR #529 (`09b84cd`) propagates a typed post-write verification error while
  preserving the renamed path for matching remediation hooks.
- PRs #540 (`41041ba`) and #544 (`24d511b`) delivered presence-aware,
  source-owned, atomic root/target initial-load and reload semantics.
- PR #546 (`323f0aa`) delivered bounded, redacted no-match diagnostics.
- PR #558 (`41a06ee`) retained the durability contract while batching unique
  changed parent-directory syncs.

## Impact

- Affected specs: ADDED capability `reconcile-fuse-hooks`; MODIFIED requirement
  `Post-Sync Container Restart Hooks` in `reconcile`.
- Affected runtime: none in this grounding PR.
- Remaining work at grounding time: focused consumer regressions for recursive
  suffix/infix patterns through both deploy-sync include and exclude filters,
  plus a source-aware doctor fix and regression and exact fallback wording in
  the two partial documentation consumers. PR #634 delivered that work before
  this non-skip-specs archive.
- Documentation evidence: `AGENTS.md`, `docs/gitops.md`,
  `docs/troubleshooting.md`, and the onboard `configuration.md`/`gitops.md`
  resources describe the released hook/change-source/reload behavior, but the
  two partial consumers still needed the exact `/mnt/user` fallback rule above
  at grounding time; PR #634 updated both before archive.

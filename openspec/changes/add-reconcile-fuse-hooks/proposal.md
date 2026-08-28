# Change: Ground FUSE-safe post-deploy hooks and glob correctness

## Why

Bosun's FUSE-hook hardening was proposed in PR #311 and delivered incrementally,
principally through PRs #401, #402, #405, #520, #529, #544, and #546. The
active change ledger was never grounded against those merges: ten early tasks
remain unchecked, several design statements describe pre-implementation choices,
and issue #431 still presents the whole change as unimplemented.

This spec-only correction records the released contract without reimplementing
it or archiving the change. Twenty-six of the original thirty tasks have exact
merged code, test, or documentation evidence. Task 1.3 remains partial because
recursive suffix/infix behavior is tested directly for hooks and `deploy_paths`,
but not through both deploy-sync filter consumers (`deploy_sync_paths` and
`deploy_sync_exclude`). Task 2.3 remains partial because `bosun doctor` treats
an omitted file value as zero and warns even though reconcile applies the safe
2-second runtime fallback. Documentation tasks 8.2 and 8.4 remain partial: the
onboard GitOps resource and `AGENTS.md` mention generic fallback behavior and a
2-second example, but neither states the exact unconfigured `/mnt/user`
fallback contract. The shared doublestar implementation has no known runtime
defect; the focused coverage, doctor, and documentation gaps remain explicit
before archive.

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
- Leave task 1.3 open for focused `deploy_sync_paths` and
  `deploy_sync_exclude` recursive suffix/infix regressions, using the existing
  one-level `appdata/<child>` discovery contract.
- Leave task 2.3 open until doctor distinguishes an omitted delay (whose
  effective runtime value is the 2-second fallback) from explicit zero.
- Leave tasks 8.2 and 8.4 open until the onboard GitOps resource and `AGENTS.md`
  state that the 2-second fallback applies only to an unconfigured exact or
  descendant `/mnt/user` path, with explicit zero and non-FUSE behavior intact.

## Grounding Evidence

- PR #401 (`e2670e6`) replaced prefix matching with
  `github.com/bmatcuk/doublestar/v4` and added direct hook/`deploy_paths` tests.
- PR #402 (`5d7902f`) added destination-directory sync, the `/mnt/user` fallback,
  doctor warning, and focused tests; PR #544 (`24d511b`) later made explicit
  zero distinguishable from an unconfigured default in reconcile configuration,
  but the doctor check still ignores that distinction and falsely warns for the
  fallback case.
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
- Remaining work: focused consumer regressions for recursive suffix/infix
  patterns through both deploy-sync include and exclude filters, plus a
  source-aware doctor fix and regression and exact fallback wording in the two
  partial documentation consumers, followed by gated verification and
  non-skip-specs archive.
- Documentation evidence: `AGENTS.md`, `docs/gitops.md`,
  `docs/troubleshooting.md`, and the onboard `configuration.md`/`gitops.md`
  resources describe the released hook/change-source/reload behavior, but the
  two partial consumers still need the exact `/mnt/user` fallback rule above.

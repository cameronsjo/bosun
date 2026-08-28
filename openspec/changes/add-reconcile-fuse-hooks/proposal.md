# Change: Ground FUSE-safe post-deploy hooks and glob correctness

## Why

Bosun's FUSE-hook hardening was proposed in PR #311 and delivered incrementally,
principally through PRs #401, #402, #405, #520, #529, #544, and #546. The
active change ledger was never grounded against those merges: ten early tasks
remain unchecked, several design statements describe pre-implementation choices,
and issue #431 still presents the whole change as unimplemented.

This spec-only correction records the released contract without reimplementing
it or archiving the change. Twenty-nine of the original thirty tasks have exact
merged code, test, or documentation evidence. Task 1.3 remains partial because
recursive suffix/infix behavior is tested directly for hooks and `deploy_paths`,
but not through both deploy-sync filter consumers (`deploy_sync_paths` and
`deploy_sync_exclude`). The shared doublestar implementation makes no runtime
defect known; the missing consumer-level regressions remain explicit before
archive.

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
  `deploy_sync_exclude` recursive suffix/infix regressions.

## Grounding Evidence

- PR #401 (`e2670e6`) replaced prefix matching with
  `github.com/bmatcuk/doublestar/v4` and added direct hook/`deploy_paths` tests.
- PR #402 (`5d7902f`) added destination-directory sync, the `/mnt/user` fallback,
  doctor warning, and focused tests; PR #544 (`24d511b`) later made explicit
  zero distinguishable from an unconfigured default.
- PR #405 (`8cade1c`) added `DeletedFiles`, staging-relative prefixing, and union
  evaluation, including mixed write/delete and deletion-only regressions.
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
  patterns through both deploy-sync include and exclude filters, followed by
  gated verification and non-skip-specs archive.
- Documentation evidence: `AGENTS.md`, `docs/gitops.md`,
  `docs/troubleshooting.md`, and the onboard `configuration.md`/`gitops.md`
  resources already describe the released behavior.

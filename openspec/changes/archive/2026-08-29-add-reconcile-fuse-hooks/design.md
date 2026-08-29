## Context

The released implementation spans path matching, deploy-result production,
filesystem durability, hook execution, configuration ownership, and both daemon
and one-shot reload construction. This grounding treated merged code and tests
as historical evidence while making the normative delta precise enough to
archive only after the remaining deploy-sync coverage, doctor behavior, and
exact fallback documentation items were delivered. PR #634 (`aaa182a`) delivered
those items before release v0.42.1 (`ca655b5`) and this archive.

## Goals / Non-Goals

- Goals:
  - Record identical recursive-glob semantics for every current consumer.
  - Record the exact FUSE fallback, explicit-zero, and directory-sync contract.
  - Record the actual written/deleted change representation and source priority.
  - Preserve daemon/CLI and root/target configuration consumer parity.
  - Keep the grounding-time deploy-sync consumer coverage gap visible until its
    delivery in PR #634.
- Non-Goals:
  - Runtime changes, archive, or issue mutation in this PR.
  - Filesystem-type probing beyond the shipped segment-aware `/mnt/user` rule.
  - Operation-tagged hook filters or new hook actions.
  - Directory creation tracking owned by `add-directory-aware-deploy-tracking`.

## Decisions

- **Use one doublestar primitive across all path consumers.** `matchGlob` calls
  `doublestar.Match`; hook matching and `deploy_paths` call it through helpers,
  while deploy-sync include/exclude filters call it directly. The normative
  scenarios name each consumer. At grounding time, direct tests covered hooks
  and `deploy_paths`; task 1.3 stayed partial until PR #634 added suffix/infix
  cases through both deploy-sync filters using candidates the one-level
  `appdata/<child>` discovery contract can actually emit.

- **Separate durability from propagation.** Atomic file replacement is followed
  by destination-parent directory sync before post-write verification. Directory
  copies batch and deterministically sync unique changed parents. On platforms
  where directory sync is unsupported, the portable helper preserves the
  platform contract. Propagation is handled separately: an unconfigured default
  on the exact `/mnt/user` path boundary receives 2 seconds; a valid environment
  duration overrides the file, invalid environment input falls back to the file
  or applicable default, and an explicit file value or valid environment value
  of zero disables the fallback; other paths keep zero.

- **Fix the source-unaware doctor warning before archive.** At grounding time,
  reconcile distinguished an omitted delay from explicit zero, but `bosun
  doctor` checked only the decoded file value and therefore warned for an
  omitted `/mnt/user` delay even though the effective runtime delay was the safe
  2-second fallback. PR #634 closed task 2.3 by applying the same presence/source
  distinction and covering omitted, explicit-zero, and positive values while
  retaining segment-aware exclusion of `/mnt/userdata`.

- **Keep writes and deletions distinct, union them for hook matching.**
  `DeployResult.WrittenFiles` and `DeletedFiles` remain separate staging-relative
  slices. Local hook evaluation unions them. Both restart and exec hooks use the
  same matcher; there is no operation selector.

- **Make local change-source priority explicit.** Content-hash results are
  authoritative even when empty. Standard-copy results are direct evidence when
  either written or deleted paths are non-empty and otherwise fall back to a
  normalized git diff from `DeployState.LastDeployedCommit`. Remote deploys and
  unavailable non-empty diff history conservatively make all configured hooks
  eligible; an empty `LastDeployedCommit` is the first-deploy case and skips
  hooks. Directory creation is intentionally left to the dependent
  directory-aware change.

- **Treat post-write verification as a failed deploy with remediation evidence.**
  Once rename succeeds, a readback failure returns
  `fileutil.ErrPostWriteVerification` and preserves the path in the deploy
  result. Matching hooks may run for that path, but reconciliation still fails,
  retains `NeedsRedeploy`, alerts normally, and does not advance the successful
  diff base.

- **Use complete, field-specific configuration snapshots.** A successfully
  parsed present config is authoritative for file-owned hooks: omitted or empty
  root hooks clear them. An omitted settle delay retains its prior effective
  value; explicit zero clears it. Root and all target hooks validate before any
  mutation, then nested slices are cloned. Omitted target hooks inherit root,
  explicit empty clears inheritance, and removed overrides/descriptors discard
  stale operational hooks. Environment-owned root fields and authoritative
  `BOSUN_TARGETS` overrides are never replaced by repo reload.

- **Keep observability bounded and secret-safe.** A non-empty change set with no
  match warns with complete counts and at most five pattern/path samples.
  Absolute or traversal paths are redacted, command arguments and contents are
  excluded, and true no-change is logged separately.

## Consumer and Evidence Map

- Recursive matching: `hooks.go`, `reconcile.go` (`deploy_paths`), and
  `discovery.go` (deploy-sync include/exclude); PR #401.
- Directory durability: `internal/fileutil/fileutil.go` and
  `directory_sync_test.go`; PRs #402 and #558.
- FUSE fallback/doctor: `hooks.go`, `diagnostics_doctor.go`, and focused tests;
  PRs #402 and #544.
- Deletions: `deploy.go`, `reconcile.go`, and deploy/reconcile regressions;
  PR #405.
- Empty exec/post-write failure: configuration validation and
  `post_write_verification_test.go`; PRs #520 and #529.
- Presence/reload: `internal/config`, daemon/CLI constructors,
  `config_reload.go`, target copying, and race regressions; PRs #540/#544.
- Diagnostics/docs: `reconcile.go`, docs, onboard resources, and log tests;
  PR #546. At grounding time, the onboard GitOps resource and `AGENTS.md` still
  needed the exact unconfigured `/mnt/user` 2-second fallback wording tracked by
  tasks 8.2 and 8.4; PR #634 supplied both updates.

## Risks / Trade-offs

- A whole-block MODIFIED requirement can discard newer canonical text if stale;
  this delta starts from the current canonical hook requirement and adds only
  the grounded contract.
- Shared-helper correctness can hide consumer wiring regressions; the
  grounding-time partial task 1.3 required focused coverage of the include and
  exclude filters, which PR #634 supplied before archive.
- Doctor reported a false zero-delay warning for an omitted `/mnt/user` delay
  at grounding time even though reconcile applied 2 seconds; PR #634 corrected
  that runtime/diagnostic mismatch before archive.
- The `/mnt/user` heuristic is intentionally narrow. Operators using other FUSE
  mounts must configure a delay explicitly.
- Best-effort hook action failures remain best-effort; this change does not turn
  hook failures into deployment failure.

## Completion and Archive Plan

The grounding plan, completed by PR #634, release v0.42.1, and this archive, was:

1. Merge this spec-only grounding after independent exact-head review.
2. Add the two missing deploy-sync recursive consumer regression families and
   correct the source-unaware doctor warning in bounded behavior/test work;
   update the two partial documentation consumers with the exact fallback rule;
   run compiler-heavy checks through the agent gate.
3. Mark tasks 1.3, 2.3, 8.2, and 8.4 complete only with merged and released
   evidence.
4. Archive with `openspec archive add-reconcile-fuse-hooks` without
   `--skip-specs`, then compare both canonical results to their deltas.

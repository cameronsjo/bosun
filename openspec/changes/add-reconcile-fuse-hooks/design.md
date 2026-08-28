## Context

The released implementation spans path matching, deploy-result production,
filesystem durability, hook execution, configuration ownership, and both daemon
and one-shot reload construction. This grounding treats merged code and tests as
historical evidence while making the normative delta precise enough to archive
only after the one remaining coverage item is delivered.

## Goals / Non-Goals

- Goals:
  - Record identical recursive-glob semantics for every current consumer.
  - Record the exact FUSE fallback, explicit-zero, and directory-sync contract.
  - Record the actual written/deleted change representation and source priority.
  - Preserve daemon/CLI and root/target configuration consumer parity.
  - Keep the remaining deploy-sync consumer coverage gap visible.
- Non-Goals:
  - Runtime changes, archive, or issue mutation in this PR.
  - Filesystem-type probing beyond the shipped segment-aware `/mnt/user` rule.
  - Operation-tagged hook filters or new hook actions.
  - Directory creation tracking owned by `add-directory-aware-deploy-tracking`.

## Decisions

- **Use one doublestar primitive across all path consumers.** `matchGlob` calls
  `doublestar.Match`; hook matching and `deploy_paths` call it through helpers,
  while deploy-sync include/exclude filters call it directly. The normative
  scenarios name each consumer. Direct tests cover hooks and `deploy_paths`;
  task 1.3 stays partial until suffix/infix cases traverse both deploy-sync
  filters.

- **Separate durability from propagation.** Atomic file replacement is followed
  by destination-parent directory sync before post-write verification. Directory
  copies batch and deterministically sync unique changed parents. On platforms
  where directory sync is unsupported, the portable helper preserves the
  platform contract. Propagation is handled separately: an unconfigured default
  on the exact `/mnt/user` path boundary receives 2 seconds; explicit file or
  environment values always win, including zero; other paths keep zero.

- **Warn on risky effective configuration.** `bosun doctor` warns when a
  `/mnt/user` deploy path has an effective zero delay. The check uses the same
  segment-aware path classification, so `/mnt/userdata` is not a match.

- **Keep writes and deletions distinct, union them for hook matching.**
  `DeployResult.WrittenFiles` and `DeletedFiles` remain separate staging-relative
  slices. Local hook evaluation unions them. Both restart and exec hooks use the
  same matcher; there is no operation selector.

- **Make local change-source priority explicit.** Content-hash results are
  authoritative even when empty. Standard-copy results are direct evidence when
  either written or deleted paths are non-empty and otherwise fall back to a
  normalized git diff from `DeployState.LastDeployedCommit`. Remote deploys and
  unavailable diff history conservatively make all configured hooks eligible.
  Directory creation is intentionally left to the dependent directory-aware
  change.

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
  PR #546.

## Risks / Trade-offs

- A whole-block MODIFIED requirement can discard newer canonical text if stale;
  this delta starts from the current canonical hook requirement and adds only
  the grounded contract.
- Shared-helper correctness can hide consumer wiring regressions; keeping task
  1.3 partial requires focused include/exclude coverage before archive.
- The `/mnt/user` heuristic is intentionally narrow. Operators using other FUSE
  mounts must configure a delay explicitly.
- Best-effort hook action failures remain best-effort; this change does not turn
  hook failures into deployment failure.

## Completion and Archive Plan

1. Merge this spec-only grounding after independent exact-head review.
2. Add the two missing deploy-sync recursive consumer regression families in a
   bounded behavior/test PR and run compiler-heavy checks through the agent gate.
3. Mark task 1.3 complete only with merged and released evidence.
4. Archive with `openspec archive add-reconcile-fuse-hooks` without
   `--skip-specs`, then compare both canonical results to their deltas.

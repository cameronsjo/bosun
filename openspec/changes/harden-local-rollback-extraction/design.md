## Context

Backup archives are consumed by three production rollback paths. Remote compose
rollback already calls `safeExtractBackup`, an in-process `archive/tar` reader
that validates each entry at write time, bounds decompressed bytes, observes
context cancellation, and removes its temporary tree on failure. The two local
consumers instead call `extractBackupArchive`, which invokes the host's external
`tar -xzf` and therefore lacks Bosun's uniform link-target policy.

The local consumers have different responsibilities after extraction:

- `RollbackFromBackupSet` resolves managed files in the extracted tree, copies
  them over live appdata paths, optionally removes files absent from a fresh
  backup, and finally re-applies restored compose files.
- `ComposeUpIsolated` lazily extracts once after the first per-file compose
  failure, invokes compose against a matching backup file, and may reuse that
  extracted path in the final orphan-reconciliation pass.

Those are distinct security boundaries: neither caller may touch live state or
invoke compose with extracted content until the entire archive has passed the
same extraction policy.

## Goals / Non-Goals

- Goals:
  - Give local and remote rollback one archive-extraction security policy.
  - Reject member traversal and escaping symlink/hardlink targets before use.
  - Preserve valid Bosun-produced archives and confined links.
  - Preserve independently bounded rollback/extraction contexts, resource
    bounds, cleanup, and caller-specific error semantics without allowing outer
    failed-deployment cancellation to suppress rollback.
  - Eliminate the external extraction helper once it is truly unused.
- Non-Goals:
  - Selecting or extracting only the compose subtree.
  - Skipping or warning on invalid entries outside the subtree eventually used.
  - Accepting an absolute symlink because it happens to be outside the restored
    subtree. These blast-radius choices belong to #450.
  - Changing backup creation, archive format, anchor freshness, rollback-set
    ownership, compose classification, or sentinel definitions.
  - Changing `RollbackRemoteCompose` behavior beyond regression coverage for
    the shared extractor.

## Decisions

- **Decision: reuse one parser for validation and extraction.** Both local
  callers SHALL call `safeExtractBackup` on
  `<backupPath>/configs.tar.gz`. Validation and extraction remain a single
  in-process pass so there is no header-preflight/external-tar parser mismatch.
  - Alternative considered: add a pre-scan before the existing `tar -xzf`.
    Rejected because two parsers can disagree about PAX/GNU names and link
    placement, recreating the time-of-check/time-of-use gap the remote path
    already removed.

- **Decision: admit content only after full extraction succeeds.** The extractor
  SHALL return a usable root only after every entry has been processed. On any
  traversal, link, corruption, size-bound, I/O, or cancellation failure, it
  SHALL remove the partial temporary tree and return no usable root. The callers
  SHALL perform no live restore copy, backup-based compose invocation, or
  orphan-pass use from that failed extraction.
  - Alternative considered: use already-extracted safe entries after a later
    member fails. Rejected because acceptance would become archive-order
    dependent and could expose a partially validated rollback set.

- **Decision: preserve independent rollback cancellation boundaries.** Each
  local caller SHALL pass `safeExtractBackup` a background-derived context with
  its existing independent timeout and the outer context's logging metadata.
  Cancellation of the outer failed-deployment context SHALL NOT cancel local
  rollback extraction. `safeExtractBackup` SHALL honor cancellation or deadline
  expiry of the independently bounded context it receives, clean the partial
  tree, and return that context cause.
  - Alternative considered: pass the caller's outer method/deployment context
    directly. Rejected because the failure that triggers rollback may already
    have cancelled it, suppressing the recovery attempt before extraction can
    begin.

- **Decision: preserve each caller's outward contract.** For
  `RollbackFromBackupSet`, extraction failure remains a rollback-not-attempted
  outcome and the returned error SHALL keep the extraction cause discoverable
  via `errors.Is`/`errors.As`. For `ComposeUpIsolated`, extraction failure makes
  backup rollback unavailable for affected files; it SHALL preserve the original
  compose failure in the result, log the extraction cause, mark no rollback
  success, and exclude those failed files from the orphan pass.
  - Alternative considered: introduce a new public sentinel for unsafe archives.
    Rejected because #449 is extraction parity, not an error-API expansion.

- **Decision: preserve valid archive layout and confined links.** Bosun's native
  writer stores absolute source paths with their leading slash removed, while
  remote tar archives can express the equivalent member layout. The shared
  extractor SHALL keep resolving these paths as `resolveBackupFile` expects and
  SHALL allow relative symlinks and archive-root-relative hardlinks only when
  their realized targets remain under the extraction root.

- **Decision: remove `extractBackupArchive` only when repository-dead.** Tests
  that directly exercise the legacy helper SHALL migrate to the shared extractor
  or caller behavior. A production-and-test repository search must show no
  callers before deletion; otherwise the helper stays until its remaining
  consumer is migrated in the same implementation PR.

## Risks / Trade-offs

- A locally stored archive that the host tar previously tolerated may now fail
  because an entry or link target escapes the temporary root. This is the
  intended fail-closed security boundary; the original compose failure remains
  visible and live state is not restored from untrusted content.
- Whole-archive validation can reject an invalid entry outside the compose
  subtree that a particular rollback would use. That is the current remote
  policy and remains deliberately uniform here; narrowing the blast radius is
  deferred to #450.
- Moving or renaming the shared helper could accidentally regress remote
  rollback. Existing remote extractor and rollback tests remain mandatory even
  if implementation only changes local call sites.

## Migration Plan

1. Add caller-level security and behavior tests before changing the call sites.
2. Route `RollbackFromBackupSet` to the shared extractor and verify no live copy
   or compose invocation occurs on extraction failure, while preserving its
   background-derived rollback timeout.
3. Route `ComposeUpIsolated` to the shared extractor and verify failed extraction
   cannot feed per-file rollback or the orphan pass, while preserving its
   background-derived extraction timeout.
4. Migrate direct legacy-helper tests and delete `extractBackupArchive` only if a
   final repository search proves it unused.
5. Update the onboard GitOps resource plus operator and security docs in the
   implementation PR, then run the full quality and polish gates.

Rollback is code-only: revert the implementation commit. No archive, config, or
state schema changes are introduced.

## Open Questions

- None blocking. #450 owns any future choice to validate/extract only a selected
  subtree or to skip invalid entries outside it.

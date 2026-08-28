# Change: Harden local rollback archive extraction

## Why

Bosun has one path-safe, in-process backup extractor, but only remote compose
rollback uses it. The two local rollback consumers still shell out to
`tar -xzf` through `extractBackupArchive`, so archive-member placement and link
handling depend on the host tar implementation and do not share the realized-path
and link-target checks already enforced for remote rollback.

The issue names the full-tree consumer by its former `RollbackFromBackup` name;
on current `main` that production path is `RollbackFromBackupSet`. Aligning it
and `ComposeUpIsolated` on `safeExtractBackup` closes the local parity gap without
creating another archive parser or changing rollback policy.

## What Changes

- Route `RollbackFromBackupSet` and `ComposeUpIsolated` through the existing
  in-process `safeExtractBackup` reader for `configs.tar.gz`.
- Require one-pass extraction that confines every archive member's realized path
  to a fresh temporary root and rejects traversal plus absolute or escaping
  symlink targets and escaping hardlink targets before any live restore copy or
  compose invocation can use extracted content.
- Preserve valid Bosun backup layouts, including leading-slash-stripped paths and
  links whose targets remain inside the extraction root.
- Preserve each local caller's background-derived, independently bounded
  rollback/extraction context so cancellation of the outer failed-deployment
  context cannot suppress rollback. Require `safeExtractBackup` to honor the
  independent context it receives, alongside the decompressed-size bound,
  cleanup of partial extraction state, and each caller's established outward
  error contract while retaining the extraction cause in logs or returned
  errors.
- Remove the external-command `extractBackupArchive` helper only after all
  production and test callers have migrated and a repository search proves it is
  dead.
- Make the onboard GitOps resource and the relevant operator/security docs
  mandatory implementation-PR merge gates; this spec PR does not advertise
  behavior that has not shipped.

## Impact

- Affected specs: `reconcile` (ADDED requirement only; the existing `Service
  Orchestration` requirement and all of its scenarios remain intact).
- Affected production code:
  - `internal/reconcile/rollback.go` — `RollbackFromBackupSet`, the current
    full-managed-tree local rollback consumer.
  - `internal/reconcile/compose.go` — `ComposeUpIsolated`, the per-compose-file
    local rollback consumer and its later orphan-reconciliation pass.
  - `internal/reconcile/backup.go` — legacy `extractBackupArchive`, removable
    only when no production or test callers remain.
  - `internal/reconcile/remote_rollback.go` — shared `safeExtractBackup` and the
    existing `RollbackRemoteCompose` consumer whose behavior must remain
    unchanged.
- Affected tests:
  - `internal/reconcile/rollback_test.go`
  - `internal/reconcile/compose_orphan_pass_test.go`
  - `internal/reconcile/deploy_test.go`
  - `internal/reconcile/remote_rollback_extract_test.go`
  - `internal/reconcile/remote_rollback_test.go`
  - legacy extractor coverage in `internal/reconcile/backup_test.go`
- Implementation-PR documentation merge gates:
  - `skills/onboard/resources/gitops.md`
  - `docs/gitops.md`
  - `docs/error-handling.md`
  - `docs/security.md`
- Related scope: Refs #449. Issue #450's subtree-selection and benign
  out-of-scope-link blast-radius policy is explicitly excluded.

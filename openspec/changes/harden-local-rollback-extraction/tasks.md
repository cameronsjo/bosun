## 1. Shared extraction contract

- [x] 1.1 Add/retain table-driven extractor tests for a valid Bosun archive,
  including regular files, confined relative symlink targets, and confined
  archive-root-relative hardlink targets.
- [x] 1.2 Add/retain separate extractor tests that reject member-name traversal,
  an absolute symlink target, a relative symlink target that escapes the root,
  and a hardlink target that escapes the root.
- [x] 1.3 Add/retain cancellation coverage for cancellation before extraction and
  during a regular-entry copy, asserting `context.Canceled` or
  `context.DeadlineExceeded` remains discoverable.
- [x] 1.4 Prove every failed extraction removes its partial temporary tree and
  returns no usable root (with only the always-safe no-op cleanup function)
  before any caller can consume extracted paths; include a fixture with a valid
  early entry followed by an invalid entry.
- [x] 1.5 Keep the existing decompressed-size bound, corrupt-archive handling,
  confined-path resolution, and remote `RollbackRemoteCompose` regression suite
  green while sharing or relocating the extractor.

## 2. Full-tree local rollback consumer

- [x] 2.1 Migrate `RollbackFromBackupSet` in
  `internal/reconcile/rollback.go` from `extractBackupArchive` to
  `safeExtractBackup(<backupPath>/configs.tar.gz)`.
- [x] 2.2 Add a caller-level valid-archive test proving the managed tree is
  restored and compose is invoked only after extraction succeeds.
- [x] 2.3 Add caller-level traversal, absolute/escaping-symlink, and
  escaping-hardlink cases proving no live managed file is copied, removed, or
  re-applied through compose when extraction fails.
- [x] 2.4 Add caller-level extraction-context deadline/cancellation and
  partial-extraction cases using the independently bounded, background-derived
  rollback context. Prove a cancelled outer method context does not suppress the
  extraction attempt, while cancellation or expiry of the independent context
  cleans the partial temp tree before any live restore action.
- [x] 2.5 Preserve the rollback-not-attempted outward contract while returning an
  actionable error for which `errors.Is`/`errors.As` can discover the archive
  extraction cause; cover the error propagation explicitly.

## 3. Per-file local rollback consumer

- [x] 3.1 Migrate `ComposeUpIsolated` in `internal/reconcile/compose.go` from
  `extractBackupArchive` to the same `safeExtractBackup` entry point, retaining
  its lazy single extraction attempt and cleanup lifecycle.
- [x] 3.2 Add a caller-level valid-archive test proving a failed compose file is
  rolled back with its extracted backup copy and that only a verified rollback
  copy can enter the orphan-reconciliation pass.
- [x] 3.3 Add caller-level traversal, absolute/escaping-symlink, and
  escaping-hardlink cases proving no backup-based compose command or orphan-pass
  use occurs when extraction fails.
- [x] 3.4 Add caller-level extraction-context deadline/cancellation and
  partial-extraction cases using the independently bounded, background-derived
  extraction context. Prove a cancelled outer deployment context does not
  suppress the extraction attempt, while cancellation or expiry of the
  independent context cleans the temporary tree before any backup-based compose
  invocation.
- [x] 3.5 Preserve the original per-file compose failure in the result and
  aggregate outcome, log the extraction cause, report `RolledBack == false`, and
  exclude the unrolled failed file from the orphan pass; cover this error
  propagation explicitly.

## 4. Legacy helper cleanup

- [x] 4.1 Migrate direct `extractBackupArchive` tests in
  `internal/reconcile/backup_test.go` to shared-extractor or caller-level coverage.
- [x] 4.2 Search production and test code for `extractBackupArchive`; remove the
  helper and its external `tar -xzf` imports only if the search proves it is
  dead, otherwise migrate every remaining caller in this implementation PR.

## 5. Documentation and merge gates

- [x] 5.1 Update `skills/onboard/resources/gitops.md` to document uniform local
  and remote rollback extraction confinement, link-target rejection,
  cancellation/cleanup behavior, and caller-specific failure outcomes.
- [x] 5.2 Update `docs/gitops.md` and `docs/error-handling.md` with the operator
  behavior for rejected or cancelled rollback archives without claiming #450's
  deferred subtree policy.
- [x] 5.3 Update `docs/security.md` so rollback archive traversal and link-target
  checks describe the actual `internal/reconcile` in-process extractor rather
  than only the separate emergency/snapshot extraction path.
- [x] 5.4 Treat tasks 5.1-5.3 as implementation-PR merge gates; do not update
  those consumer docs in this spec-only PR to advertise unshipped behavior.

## 6. Verification and delivery

- [x] 6.1 Run focused rollback/extractor tests plus `make test`, `make build`, and
  repository lint/format/freshness checks.
- [x] 6.2 Run literal Cadence polish on the implementation diff, fix every
  actionable finding and nit, and record honestly skipped unavailable arms.
- [ ] 6.3 Re-run exact-head hosted CI and Codecov after the final push; obtain an
  independent exact-head review with zero Critical and Important findings before
  merge.

## ADDED Requirements

### Requirement: Local Rollback Archive Extraction Confinement

Every local backup-consuming rollback path SHALL extract
`<backupPath>/configs.tar.gz` with the same in-process, single-reader extraction
policy used by remote compose rollback. `RollbackFromBackupSet` (the current
full-managed-tree successor to `RollbackFromBackup`) and `ComposeUpIsolated`
SHALL NOT invoke an external tar extractor for backup restore.

The extractor SHALL map valid Bosun archive members into a fresh temporary root
in the layout expected by `resolveBackupFile`. For each member, it SHALL validate
the realized destination before writing. Member-name traversal, absolute or
escaping symlink targets, and escaping hardlink targets SHALL be rejected before
they can create or redirect content outside that root. Relative symlinks and
archive-root-relative hardlinks whose realized targets remain within the root
SHALL remain supported.

Each local caller SHALL pass `safeExtractBackup` its existing background-derived,
independently bounded rollback/extraction context, preserving the outer
failed-deployment context's logging metadata without inheriting its
cancellation. Cancellation of the outer method or deployment context SHALL NOT
suppress a local rollback extraction attempt. The extractor SHALL honor
cancellation or deadline expiry of the independent context it receives and the
existing total decompressed size bound. It SHALL return a usable root only after
the complete archive passes. On any validation, corruption, I/O, size-bound, or
independent-context cancellation error, the extractor SHALL remove the partial
temporary tree and return no usable root before a local caller copies to live
state, removes a live path, invokes compose with a backup file, or includes a
backup file in an orphan-reconciliation pass.

`RollbackFromBackupSet` SHALL preserve its rollback-not-attempted outward
contract on extraction failure while returning an actionable error that
keeps the extraction cause discoverable via `errors.Is`/`errors.As`.
`ComposeUpIsolated` SHALL preserve the original compose failure in its per-file
result and aggregate outcome, log the extraction cause, report no successful
rollback for that file, and exclude the unrolled failed file from the
orphan-reconciliation pass.

#### Scenario: Full-tree local rollback accepts a valid archive

- **WHEN** `RollbackFromBackupSet` receives a valid Bosun backup archive whose members and any link targets remain within the extraction root
- **THEN** the archive is extracted in-process and the requested managed files are resolved from the completed temporary tree
- **AND** live managed files are restored before the restored compose files are re-applied

#### Scenario: Per-file local rollback accepts a valid archive

- **WHEN** `ComposeUpIsolated` needs to roll back a failed compose file and the backup archive is valid
- **THEN** the archive is extracted in-process at most once for that operation
- **AND** compose rollback uses the matching file from the completed temporary tree
- **AND** only a successfully rolled-back backup file can be included in the orphan-reconciliation pass

#### Scenario: Archive member traversal is rejected

- **WHEN** a backup archive contains a member name that traverses outside the extraction root
- **THEN** extraction fails before either local rollback consumer can use any extracted content
- **AND** no path outside the temporary root is created or modified

#### Scenario: Absolute symlink target is rejected

- **WHEN** a backup archive contains a symlink with an absolute target
- **THEN** extraction fails before the symlink or a later write through it can escape the temporary root
- **AND** neither local rollback consumer uses the partially extracted archive

#### Scenario: Relative symlink target escaping the root is rejected

- **WHEN** a backup archive contains a symlink whose relative target resolves outside the extraction root
- **THEN** extraction fails before the symlink or a later write through it is admitted
- **AND** neither local rollback consumer uses the partially extracted archive

#### Scenario: Hardlink target escaping the root is rejected

- **WHEN** a backup archive contains a hardlink whose archive-relative target resolves outside the extraction root
- **THEN** extraction fails before the hardlink is created
- **AND** neither local rollback consumer uses the partially extracted archive

#### Scenario: Full-tree rollback survives outer cancellation but honors its independent deadline

- **WHEN** `RollbackFromBackupSet` is invoked with an already-cancelled outer method context after a failed deployment
- **THEN** it still attempts extraction with its background-derived, independently bounded rollback context
- **AND** when that independent context is cancelled or reaches its deadline before or during archive entry processing, extraction returns promptly with its context cause discoverable
- **AND** the partial temporary tree is cleaned before any live managed-tree restore or compose invocation

#### Scenario: Per-file rollback survives outer cancellation but honors its independent deadline

- **WHEN** `ComposeUpIsolated` reaches backup extraction while its outer deployment context is cancelled
- **THEN** it still attempts extraction with its background-derived, independently bounded extraction context
- **AND** when that independent context is cancelled or reaches its deadline before or during archive entry processing, extraction returns promptly and logs its context cause
- **AND** the partial temporary tree is cleaned before any backup-based compose invocation or orphan-pass use

#### Scenario: Failed extraction cleans partial content before live use

- **WHEN** a valid early archive entry is extracted and a later entry fails validation or extraction
- **THEN** the extractor removes the entire partial temporary tree and returns no usable root
- **AND** no live managed file is copied or removed and no compose or orphan-pass command receives a path from that tree

#### Scenario: Full-tree extraction error preserves rollback outcome

- **WHEN** archive extraction fails for `RollbackFromBackupSet`
- **THEN** the method returns its rollback-not-attempted outcome with an actionable extraction cause discoverable via `errors.Is`/`errors.As`
- **AND** it performs no managed-tree restore, deletion, or restored compose invocation

#### Scenario: Per-file extraction error preserves the original compose failure

- **WHEN** archive extraction fails after a compose file fails in `ComposeUpIsolated`
- **THEN** the extraction cause is logged and the original compose failure remains on the per-file result and aggregate outcome
- **AND** the file is not marked rolled back and its failed new path or partial backup path is excluded from the orphan-reconciliation pass

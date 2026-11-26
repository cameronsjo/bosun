## ADDED Requirements

### Requirement: Per-Target Backup Directory

Each effective target SHALL own a backup directory. The implicit default and a lone `default` target (name matched case-insensitively) SHALL use the configured `BackupDir` unchanged. A named target SHALL use `<BackupDir>/<target-name>`. There is no per-target backup-directory override.

#### Scenario: Named targets derive distinct backup directories

- **WHEN** targets `unraid` and `pi` share a configured `BackupDir` of `/app/backups`
- **THEN** `unraid` backs up under `/app/backups/unraid` and `pi` under `/app/backups/pi`
- **AND** no two targets share a backup directory

#### Scenario: Default target keeps the configured backup directory

- **WHEN** Bosun resolves only the implicit or lone `default` target
- **THEN** backups are written under the configured `BackupDir` unchanged
- **AND** no named-target migration is required

### Requirement: Per-Target Backup Operations

Every backup operation in the Configuration Backup requirement SHALL act on the target's own backup directory only: creation, rollback-anchor recovery after a failed backup, retention pruning to `BackupsToKeep`, and removal of corrupt or partial backups. A target SHALL NOT select, count, prune, or delete a sibling's backups, including a sibling's in-progress archive.

#### Scenario: Rollback anchor comes from the target's own backups

- **WHEN** `unraid`'s backup fails
- **AND** `pi` holds a newer verified backup than `unraid`
- **THEN** the rollback anchor is `unraid`'s most recent verified backup
- **AND** `pi`'s backup is never selected

#### Scenario: Retention is per target

- **WHEN** `unraid` completes a verified deploy with `BackupsToKeep` of 1
- **THEN** only backups under `unraid`'s own directory are counted and pruned
- **AND** every backup under `pi`'s directory, including an in-progress archive, is untouched

### Requirement: Shared Backup Root Exclusion

A backup archive SHALL NOT contain the shared backup root, meaning the configured `BackupDir` before per-target derivation, or any backup beneath it. A target's archive therefore never contains a sibling target's backups. For the default target the root equals its own backup directory.

#### Scenario: Archive excludes sibling backups

- **WHEN** a backed-up path contains the shared backup root, such as the full-appdata fallback
- **THEN** the created archive contains neither the shared root nor any target's backups

### Requirement: Backup Directory Names

Scans of a backup directory for rollback anchors, retention (including removal of corrupt or partial backups), and legacy backups SHALL treat only entries named exactly `backup-YYYYMMDD-HHMMSS` as backups. A target directory such as `<BackupDir>/backup-pi` SHALL never be selected, counted, or deleted as a backup. A target name of that exact shape SHALL be rejected, matched case-insensitively, with an error naming the rule.

#### Scenario: A backup-prefixed target directory survives retention

- **WHEN** a run whose backup directory is the shared root prunes backups
- **AND** the root holds a target directory `backup-pi` with its own backups beneath it
- **THEN** `backup-pi` and everything in it is kept
- **AND** only entries named `backup-YYYYMMDD-HHMMSS` are counted and pruned

#### Scenario: A non-timestamp name is not a backup

- **WHEN** a directory named `backup-old` holds a `configs.tar.gz` directly under a scanned backup directory
- **THEN** it is not selected as a rollback anchor and not counted as a legacy backup

#### Scenario: A backup-shaped target name is rejected

- **WHEN** a target is named `backup-20240115-143022`, in any letter case
- **THEN** target resolution rejects it with an error naming the reserved shape
- **AND** a target named `backup-pi` is accepted

### Requirement: Legacy Shared Backups

A `backup-YYYYMMDD-HHMMSS` directory that directly holds `configs.tar.gz` under the shared root, written by an earlier release, SHALL NOT be selected as a rollback anchor, counted for retention, or deleted by a named target. When a named target's reconcile finds one, it SHALL log one warning per run naming the shared root.

#### Scenario: Legacy shared backups are left alone

- **WHEN** a named target finds `backup-YYYYMMDD-HHMMSS` directories holding an archive directly under the shared root
- **THEN** it neither uses, counts, nor deletes them
- **AND** it logs one warning per run naming the shared root

#### Scenario: A target directory named like a backup is not legacy

- **WHEN** a target named `backup-pi` has a directory `<BackupDir>/backup-pi` that holds only its own `backup-YYYYMMDD-HHMMSS` subdirectories
- **THEN** that directory is not treated as a legacy backup
- **AND** no legacy warning is logged for it

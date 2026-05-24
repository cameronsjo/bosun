## MODIFIED Requirements

### Requirement: Configuration Backup

The reconciler SHALL create a timestamped tar.gz backup of existing
configuration files before deploying new ones. Backups SHALL be named
`backup-YYYYMMDD-HHMMSS`.

The reconciler SHALL verify backup integrity after creation by listing the
archive contents. Empty or corrupted archives SHALL cause the backup to fail.
Verification SHALL run under the same cancellation context as creation, so a
caller deadline or cancellation aborts verification rather than blocking
indefinitely.

The backup archive SHALL NOT include the backup destination directory or any
prior backup it contains. When the configured backup destination is nested
within a backed-up path (for example, the reconciler's own appdata directory),
the reconciler SHALL exclude the destination so the archive cannot recursively
include its own growing output.

Backup creation and verification SHALL run under a configurable timeout
(`BackupTimeout`, default 5 minutes, overridable via `BOSUN_BACKUP_TIMEOUT`
accepting a Go duration or a plain number of seconds). When the timeout
elapses, the backup SHALL be treated as a failure.

Old backups SHALL be pruned to retain only the most recent N backups
(configurable via `BackupsToKeep`, default 5).

Backup failures, including timeouts, SHALL log a warning but SHALL NOT abort the
deployment pipeline.

The last backup path SHALL be stored for potential rollback during compose up.

#### Scenario: Local backup created and verified

- **WHEN** a local deployment runs with existing config files
- **THEN** a timestamped tar.gz is created containing the config paths
- **AND** the archive is verified to be non-empty and readable

#### Scenario: Remote backup via SSH

- **WHEN** a remote deployment runs
- **THEN** a tar command runs over SSH to create the archive
- **AND** transient SSH errors are retried with exponential backoff

#### Scenario: Backup destination excluded from the archive

- **WHEN** the configured backup destination is nested within a backed-up path
- **THEN** the created archive does not contain the backup destination directory or any prior backup
- **AND** the archive size does not grow with the number of prior backups present

#### Scenario: Backup exceeds its timeout

- **WHEN** backup creation or verification runs longer than `BackupTimeout`
- **THEN** the backup is aborted and treated as a failure
- **AND** a warning is logged
- **AND** the deployment continues

#### Scenario: Old backups pruned

- **WHEN** more than `BackupsToKeep` backups exist after a new backup
- **THEN** the oldest backups are removed, keeping only the most recent N

#### Scenario: Backup failure does not block deploy

- **WHEN** backup creation fails (e.g., no existing configs to backup)
- **THEN** a warning is logged
- **AND** the deployment continues

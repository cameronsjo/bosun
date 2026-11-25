## MODIFIED Requirements

### Requirement: File Deployment

The reconciler SHALL support two deployment modes: local (direct file operations) and remote (tar-over-SSH for directories and SSH streaming for individual files), and SHALL propagate directory-preparation errors with their real cause rather than discarding them.

Deploy targets SHALL be discovered by scanning staging subdirectories after
template rendering completes, following staging conventions (e.g.
`appdata/<service>/...` and `compose/...`). Each discovered subdirectory or file
becomes a deploy target. The reconciler SHALL sync each discovered target to the
corresponding path under the appdata directory.

When `DeploySyncPaths` is configured (non-empty list of relative path patterns),
only targets matching at least one pattern SHALL be synced. When
`DeploySyncExclude` is configured (non-empty list of glob patterns), targets
matching any exclude pattern SHALL be skipped. Exclude takes precedence over
include when both match.

The `compose/` subdirectory SHALL receive special handling: after syncing, the
reconciler SHALL collect `.yml` files from the deployed compose directory for
`docker compose up`. Other directories are synced without additional
post-processing.

Local deployment SHALL use atomic-like operations: copy source to a temp directory in the same parent, then rename to the target path. This provides atomic directory replacement with `--delete` semantics (files in target not in source are removed).

Remote deployment SHALL use tar-over-SSH for directories (tar source, pipe to SSH for extraction in a temp dir, atomic move to target) and SSH streaming for individual files (stream to a temp file inside a private staging directory beside the target, verify, then atomic move).

Remote single-file deployment SHALL create its private staging directory with an exclusive (non-`-p`) `mkdir` under a restrictive umask, and SHALL remove only a staging directory that the same attempt created. The streamed file SHALL be verified against the local file's SHA-256 before it replaces the target; a mismatch or a missing `sha256sum` SHALL abort the attempt and SHALL leave the existing target unchanged. The deployed file's permission bits SHALL equal the source file's permission bits masked by the remote umask; the one exception is the optional Compose Manager mirror under `/boot/config`, whose FAT filesystem cannot store permission bits, where a failed mode change SHALL be tolerated and logged. Every remote path SHALL be passed to the remote shell as a single quoted word.

Pre-deploy directory creation (local `MkdirAll` and remote `EnsureRemoteDir`) SHALL propagate failures, wrapped to name the failing path and layer. Such errors SHALL NOT be discarded, so a downstream copy or transfer failure never masks an mkdir or mount failure as the apparent cause.

The tar-over-SSH deployment SHALL verify the extracted archive (e.g. file-count parity between the local source and the remote extraction) before performing the atomic move. A partial or empty archive — even when the tar command exits 0 — SHALL abort the deploy and SHALL preserve the existing target directory.

All remote operations SHALL retry on transient SSH errors (connection refused, timeout, network unreachable) with exponential backoff (1s, 2s, 4s, max 3 attempts). On a transfer leg (the directory archive extraction or the single-file stream), a failure reported by the remote command itself (any exit status other than SSH's own 255) SHALL NOT be retried, except a transfer checksum mismatch, which SHALL be retried with fresh staging. A staged tree that fails post-extraction verification SHALL also be retried with fresh staging.

All remote operations SHALL validate the host string against an allowlist pattern and reject strings starting with `-` to prevent SSH option injection.

Remote SSH operations SHALL use strict host-key verification, resolved
from the same candidates as Git (`BOSUN_SSH_KNOWN_HOSTS`, then
`/config/known_hosts`). A candidate SHALL be skipped only when it does not
exist. For the first candidate that exists, or whose existence cannot be
determined, every command SHALL pass `StrictHostKeyChecking=yes` and
`UserKnownHostsFile=<candidate>`. When no candidate exists, every command SHALL
pass only `StrictHostKeyChecking=yes`, leaving OpenSSH's normal trust stores
available for already-pinned hosts while refusing unknown hosts. Commands SHALL
NOT use `accept-new` as an implicit trust decision. The explicit
`BOSUN_SSH_INSECURE_HOST_KEY=true` opt-out SHALL disable these checks for the
operation.

#### Scenario: Auto-discovered directories synced locally

- **WHEN** template rendering produces `staging/appdata/traefik/`, `staging/appdata/gatus/`, and `staging/compose/`
- **THEN** the reconciler discovers all three targets
- **AND** syncs each to the corresponding path under `LocalAppdataPath`
- **AND** collects compose files from the deployed `compose/` directory

#### Scenario: New service deployed without code change

- **WHEN** a user adds a new service template that renders to `staging/appdata/newservice/config.yaml`
- **THEN** the reconciler discovers `appdata/newservice/` as a deploy target
- **AND** syncs it without any change to Bosun's source code

#### Scenario: Allowlist restricts sync targets

- **WHEN** `DeploySyncPaths` is set to `["appdata/traefik", "compose"]`
- **AND** staging contains `appdata/traefik/`, `appdata/gatus/`, and `compose/`
- **THEN** only `appdata/traefik/` and `compose/` are synced
- **AND** `appdata/gatus/` is skipped

#### Scenario: Blocklist excludes targets

- **WHEN** `DeploySyncExclude` is set to `["**/test-*"]`
- **AND** staging contains `appdata/traefik/` and `appdata/test-service/`
- **THEN** `appdata/traefik/` is synced
- **AND** `appdata/test-service/` is skipped

#### Scenario: Exclude takes precedence over include

- **WHEN** `DeploySyncPaths` is set to `["appdata/**"]`
- **AND** `DeploySyncExclude` is set to `["appdata/deprecated"]`
- **AND** staging contains `appdata/traefik/` and `appdata/deprecated/`
- **THEN** `appdata/traefik/` is synced
- **AND** `appdata/deprecated/` is skipped

#### Scenario: Remote deployment with discovered targets

- **WHEN** a remote deployment runs with discovered targets
- **THEN** each discovered directory uses tar-over-SSH
- **AND** each discovered file uses SSH streaming
- **AND** transient SSH errors are retried with exponential backoff

#### Scenario: Local atomic-like directory deployment

- **WHEN** deploying a directory locally
- **THEN** files are copied to a temp directory in the target's parent
- **AND** the old target is renamed aside (e.g., `target.old`)
- **AND** the temp directory is renamed to the target path
- **AND** the aside directory is removed after successful rename
- **AND** if the final rename fails, the aside directory is renamed back to restore the previous state

#### Scenario: Remote deployment with SSH retry

- **WHEN** a remote SSH operation fails with "connection refused"
- **THEN** it retries up to 3 times with exponential backoff
- **AND** succeeds on a subsequent attempt

#### Scenario: Remote transfer command failure is not retried

- **WHEN** the remote command of a transfer leg (directory archive extraction or single-file stream) exits with a status other than 255 (for example, the disk is full)
- **AND** the failure is not a transfer checksum mismatch
- **THEN** the deploy fails on the first attempt without re-sending the payload

#### Scenario: Staged-tree verification failure is retried with fresh staging

- **WHEN** a directory archive extracts but the staged tree fails post-extraction verification
- **THEN** the attempt's staging directory is discarded
- **AND** the next attempt transfers into a freshly created staging directory

#### Scenario: SSH host validation rejects injection

- **WHEN** a target host contains shell metacharacters or starts with `-`
- **THEN** the operation fails with a validation error before any SSH command runs

#### Scenario: Remote file path with shell metacharacters

- **WHEN** a discovered file's remote path contains spaces or shell metacharacters such as `$(…)`, `;`, or quotes
- **THEN** the file is written at exactly that literal path
- **AND** no part of the path is evaluated by the remote shell

#### Scenario: Truncated single-file transfer

- **WHEN** a single-file stream reaches the remote host incomplete
- **THEN** SHA-256 verification fails before the move
- **AND** the existing target file is unchanged
- **AND** the attempt's private staging directory is removed

#### Scenario: Single-file mode is preserved

- **WHEN** a source file with mode `0600` is deployed to a remote host whose umask is `022`
- **THEN** the deployed file has mode `0600`

#### Scenario: Pre-existing staging entry is never reused or removed

- **WHEN** an entry already exists at the private staging directory name chosen by an attempt
- **THEN** the attempt fails without writing through or deleting that entry

#### Scenario: Remote deploy has a configured trust file

- **WHEN** a remote deploy runs and a configured `known_hosts` candidate exists
- **THEN** every SSH leg carries strict checking and that candidate path
- **AND** an unknown host key is refused

#### Scenario: Remote deploy has no configured candidate

- **WHEN** a remote deploy runs without a configured `known_hosts` candidate and without the insecure opt-out
- **THEN** every SSH leg carries `StrictHostKeyChecking=yes`
- **AND** it does not carry `StrictHostKeyChecking=accept-new`
- **AND** an unpinned host is refused before the secret-bearing transfer

#### Scenario: Explicit deploy trust opt-out

- **WHEN** `BOSUN_SSH_INSECURE_HOST_KEY=true`
- **THEN** remote SSH legs omit strict host-key enforcement
- **AND** the opt-out remains visible in operator documentation

#### Scenario: Directory-prep failure surfaces the real cause

- **WHEN** a pre-deploy `MkdirAll` or `EnsureRemoteDir` fails (FUSE mount down, permission denied, SSH dropped)
- **THEN** the deploy fails with an error that names the failing path and the underlying cause
- **AND** the error is not masked by a confusing downstream "file not found" or "transfer failed"

#### Scenario: Partial extracted archive aborts the deploy

- **WHEN** the tar-over-SSH extraction produces an empty or partial archive even though the tar command exits 0
- **THEN** the extracted-file integrity check fails before the atomic move
- **AND** the existing target directory is preserved unchanged

#### Scenario: Empty staging directory produces no deploys

- **WHEN** template rendering produces an empty staging directory
- **THEN** the reconciler logs a warning that no deploy targets were found
- **AND** proceeds to compose up with no file changes

### Requirement: Configuration Backup

The reconciler SHALL create a timestamped tar.gz backup of existing
configuration files before deploying new ones. Backups SHALL be named
`backup-YYYYMMDD-HHMMSS`.

Backup paths SHALL be derived from the discovered deploy targets: each target
that will be synced generates a corresponding backup path under the appdata
directory. This ensures backups stay in sync with deploy targets without
maintaining a separate hardcoded list.

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

#### Scenario: Backup paths derived from discovered targets

- **WHEN** discovery finds `appdata/traefik/`, `appdata/gatus/config.yaml`, and `compose/`
- **THEN** the backup includes `<appdata>/traefik/`, `<appdata>/gatus/config.yaml`
- **AND** does not include compose files (compose has its own rollback via `ComposeUpMultipleWithRollback`)

#### Scenario: Local backup created and verified

- **WHEN** a local deployment runs with existing config files
- **THEN** a timestamped tar.gz is created containing the discovered config paths
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

## ADDED Requirements

### Requirement: Deploy Sync Configuration

The reconciler SHALL support optional configuration to control which staging
directory targets are synced during deployment.

`deploy_sync_paths` in `bosun.yaml` SHALL accept a list of relative path
patterns (matched against staging directory entries). When non-empty, only
matching targets are synced. When empty or absent, all discovered targets are
synced (default: auto-discover everything).

`deploy_sync_exclude` in `bosun.yaml` SHALL accept a list of glob patterns.
Targets matching any exclude pattern SHALL be skipped. Exclude takes precedence
over include.

`BOSUN_DEPLOY_SYNC_PATHS` (JSON string array) SHALL override `deploy_sync_paths`
from `bosun.yaml` when set. `BOSUN_DEPLOY_SYNC_EXCLUDE` (JSON string array)
SHALL override `deploy_sync_exclude` from `bosun.yaml` when set.

The config SHALL be reloaded from the repo's `bosun.yaml` after each git pull,
unless the corresponding env var override is set.

#### Scenario: Default auto-discovery syncs everything

- **WHEN** neither `deploy_sync_paths` nor `deploy_sync_exclude` is configured
- **THEN** all top-level entries in the staging directory are synced

#### Scenario: Allowlist from bosun.yaml

- **WHEN** `bosun.yaml` contains `deploy_sync_paths: ["appdata/traefik", "compose"]`
- **THEN** only `appdata/traefik` and `compose` targets are synced

#### Scenario: Env var overrides config file allowlist

- **WHEN** `bosun.yaml` sets `deploy_sync_paths: ["appdata/traefik"]`
- **AND** `BOSUN_DEPLOY_SYNC_PATHS` is set to `["appdata/traefik", "appdata/gatus"]`
- **THEN** the reconciler uses `["appdata/traefik", "appdata/gatus"]` from the env var

#### Scenario: Exclude from bosun.yaml

- **WHEN** `bosun.yaml` contains `deploy_sync_exclude: ["**/test-*"]`
- **THEN** targets matching `**/test-*` are excluded from sync

#### Scenario: Env var overrides config file blocklist

- **WHEN** `bosun.yaml` sets `deploy_sync_exclude: ["**/test-*"]`
- **AND** `BOSUN_DEPLOY_SYNC_EXCLUDE` is set to `["**/deprecated"]`
- **THEN** the reconciler uses `["**/deprecated"]` from the env var

#### Scenario: Config reloaded after git pull

- **WHEN** `bosun.yaml` in the repo changes `deploy_sync_exclude`
- **AND** no `BOSUN_DEPLOY_SYNC_EXCLUDE` env var is set
- **THEN** the reconciler picks up the new value after the next git pull

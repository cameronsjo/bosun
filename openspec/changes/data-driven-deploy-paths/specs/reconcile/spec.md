## MODIFIED Requirements

### Requirement: File Deployment

The reconciler SHALL support two deployment modes: local (direct file
operations) and remote (SSH+tar or SCP).

Deploy targets SHALL be discovered by scanning the staging directory after
template rendering completes. Each top-level entry (directory or file) in the
staging output becomes a deploy target. The reconciler SHALL sync each
discovered target to the corresponding path under the appdata directory.

When `DeploySyncPaths` is configured (non-empty list of relative path patterns),
only targets matching at least one pattern SHALL be synced. When
`DeploySyncExclude` is configured (non-empty list of glob patterns), targets
matching any exclude pattern SHALL be skipped. Exclude takes precedence over
include when both match.

The `compose/` subdirectory SHALL receive special handling: after syncing, the
reconciler SHALL collect `.yml` files from the deployed compose directory for
`docker compose up`. Other directories are synced without additional
post-processing.

Local deployment SHALL use atomic-like operations: copy source to a temp directory in
the same parent, then rename to the target path. This provides atomic directory
replacement with `--delete` semantics (files in target not in source are removed).

Remote deployment SHALL use tar-over-SSH for directories (tar source, pipe to SSH
for extraction in a temp dir, atomic move to target) and SCP for individual files
(SCP to temp file, then atomic move).

All remote operations SHALL retry on transient SSH errors (connection refused,
timeout, network unreachable) with exponential backoff (1s, 2s, 4s, max 3
attempts).

All remote operations SHALL validate the host string against an allowlist pattern
and reject strings starting with `-` to prevent SSH option injection.

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
- **AND** each discovered file uses SCP
- **AND** transient SSH errors are retried with exponential backoff

#### Scenario: Local atomic-like directory deployment

- **WHEN** deploying a directory locally
- **THEN** files are copied to a temp directory in the target's parent
- **AND** the old target is renamed aside (e.g., `target.old`)
- **AND** the temp directory is renamed to the target path
- **AND** the aside directory is removed after successful rename
- **AND** if the final rename fails, the aside directory is renamed back to restore the previous state

#### Scenario: SSH host validation rejects injection

- **WHEN** a target host contains shell metacharacters or starts with `-`
- **THEN** the operation fails with a validation error before any SSH command runs

#### Scenario: Empty staging directory produces no deploys

- **WHEN** template rendering produces an empty staging directory
- **THEN** the reconciler logs a warning that no deploy targets were found
- **AND** proceeds to compose up with no file changes

## MODIFIED Requirements

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

Old backups SHALL be pruned to retain only the most recent N backups
(configurable via `BackupsToKeep`, default 5).

Backup failures SHALL log a warning but SHALL NOT abort the deployment pipeline.

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

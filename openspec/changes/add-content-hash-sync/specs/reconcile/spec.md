## ADDED Requirements

### Requirement: Content-Hash File Sync

The reconciler SHALL support content-hash comparison during local file deployment
to skip writing files whose content has not changed. This optimization reduces
unnecessary filesystem writes on FUSE-backed storage (e.g., Unraid) where
identical writes invalidate file handles and cause stale config reads.

When content-hash sync is enabled, `CopyFileIfChanged` SHALL read the existing
destination file, compute SHA-256 hashes of both source and destination content,
and skip the write if hashes match. When hashes differ or the destination does
not exist, the file SHALL be written using the existing atomic write mechanism.

`CopyDirIfChanged` SHALL apply content-hash comparison per file and return the
list of files that were actually written (relative paths from destination root).
Files present in the destination but not in the source SHALL still be removed
via the existing directory replacement mechanism.

Content-hash sync SHALL be controlled by the `BOSUN_CONTENT_HASH_SYNC`
environment variable (default: `true`). When disabled, all file writes proceed
unconditionally as before.

Content-hash sync SHALL only apply to local deployments. Remote deployments
(SSH+tar, SCP) SHALL continue with unconditional writes.

#### Scenario: Unchanged file skipped on local deploy

- **WHEN** content-hash sync is enabled
- **AND** a local deploy writes `traefik/dynamic.yml`
- **AND** the existing destination file has identical content
- **THEN** the file write is skipped
- **AND** the file is not included in the written-files list

#### Scenario: Changed file written on local deploy

- **WHEN** content-hash sync is enabled
- **AND** a local deploy writes `traefik/dynamic.yml`
- **AND** the existing destination file has different content
- **THEN** the file is written atomically
- **AND** the file is included in the written-files list

#### Scenario: New file always written

- **WHEN** content-hash sync is enabled
- **AND** the destination file does not exist
- **THEN** the file is written atomically
- **AND** the file is included in the written-files list

#### Scenario: Content-hash sync disabled falls back to unconditional writes

- **WHEN** `BOSUN_CONTENT_HASH_SYNC` is set to `false`
- **THEN** all files are written unconditionally (existing behavior)
- **AND** no content-hash comparison is performed

#### Scenario: Remote deploy unaffected

- **WHEN** deploying via SSH (remote mode)
- **THEN** all files are written unconditionally regardless of content-hash setting

## MODIFIED Requirements

### Requirement: Post-Sync Container Restart Hooks

The reconciler SHALL support configurable post-sync hooks that restart containers
when specific file paths change during deployment.

Hooks SHALL be configured via `PostSyncHooks` with fields: `Paths` (glob patterns
matched against changed files relative to repo root), `Action` (the action to
perform, currently only `restart`), and `Container` (the container name to act on).

After a successful deployment, the reconciler SHALL determine the set of changed
files and match them against hook glob patterns. When content-hash sync is active
and local deploy produced a written-files list, the reconciler SHALL use that list
for hook matching. When no written-files list is available (remote deploy,
content-hash sync disabled, or first deploy), the reconciler SHALL fall back to
git diff between the previous and current commits. Each container SHALL be
restarted at most once per deployment, even if multiple patterns match.

Hooks SHALL only execute when a Docker client is available, dry run is false, hooks
are configured, and a previous commit exists (not on first deploy).

Glob patterns SHALL support `**` for recursive directory matching.

#### Scenario: Container restarted after config change

- **WHEN** a hook is configured with paths `["traefik/conf.d/**"]` and container `traefik`
- **AND** a deployment changes `traefik/conf.d/dynamic.yml`
- **THEN** the reconciler restarts the `traefik` container after compose up
- **AND** logs the hook execution

#### Scenario: No restart when unrelated files change

- **WHEN** a hook is configured with paths `["traefik/conf.d/**"]` and container `traefik`
- **AND** a deployment only changes `docker-compose.yml`
- **THEN** the reconciler does not restart the `traefik` container

#### Scenario: Unsupported hook action skipped

- **WHEN** a hook has an action other than `restart`
- **THEN** the hook is skipped with a warning log

#### Scenario: First deploy skips hooks

- **WHEN** there is no previous commit (first deployment)
- **THEN** post-sync hooks are not evaluated

#### Scenario: Hooks use written-files when content-hash sync active

- **WHEN** content-hash sync is enabled
- **AND** a local deploy produced a written-files list
- **AND** git diff shows changes to `traefik/conf.d/dynamic.yml` and `gatus/config.yaml`
- **AND** only `gatus/config.yaml` was actually written to disk (traefik config unchanged)
- **THEN** only hooks matching `gatus/config.yaml` fire
- **AND** the traefik container is NOT restarted

#### Scenario: Hooks fall back to git diff for remote deploy

- **WHEN** deploying via SSH (remote mode)
- **THEN** post-sync hooks use git diff to determine changed files (existing behavior)

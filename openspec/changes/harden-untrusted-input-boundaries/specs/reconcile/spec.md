## MODIFIED Requirements

### Requirement: Git Repository Sync

The reconciler SHALL clone the repository on first run (when no local repo
exists) and pull on subsequent runs. Clones SHALL use depth 1 (shallow) and
track a single branch.

Pulls SHALL fetch the configured branch from origin, verify the remote branch
exists, and hard-reset the working tree to the remote HEAD.

The reconciler SHALL warn when the local repository has uncommitted changes
and proceed with the pull. The hard reset discards local changes, which are
typically stale artifacts from previous reconciliation runs (template renders,
FUSE symlink diffs). This prevents dirty working trees from blocking
automated deployments.

SSH authentication SHALL be resolved in order: SSH agent (via `SSH_AUTH_SOCK`),
then key files (`BOSUN_SSH_KEY`, `/config/deploy-key`, `/config/ssh-key`,
`~/.ssh/id_ed25519`, `~/.ssh/id_rsa`).

Private HTTPS repository authentication SHALL use HTTP Basic authentication
with `BOSUN_GIT_USERNAME` as the username and `BOSUN_GIT_TOKEN` as the
password. Both variables MUST be configured together and MUST apply identically
to initial clone and subsequent fetch operations in standalone and daemon
reconciliation. When both variables are unset, HTTPS synchronization SHALL
remain anonymous. Bosun SHALL read these values only from their `BOSUN_` names:
it SHALL NOT recognize unprefixed aliases or project-configuration keys. The
pair SHALL apply to the effective repository URL after the existing
`BOSUN_REPO_URL`-over-`REPO_URL` precedence rule.

Bosun MUST send these credentials only to an absolute `https://` repository URL
with a non-empty host, comparing the scheme case-insensitively. A partial
credential pair, credentials configured for another transport, or
userinfo embedded in a repository URL MUST fail before network I/O. Credential
validation SHALL parse standard URLs and reject any userinfo component,
including username-only, password-bearing, and percent-encoded forms; SCP-like
SSH syntax SHALL retain its existing meaning and SHALL NOT be treated as URL
userinfo.

For authenticated Git traffic, every redirect hop MUST remain HTTPS and MUST
retain the configured origin's case-insensitive hostname and effective port;
an omitted HTTPS port and explicit `:443` SHALL be equivalent. Bosun MUST reject
HTTPS-to-HTTP downgrade and cross-origin redirects without forwarding the Basic
Authorization header. Same-origin HTTPS redirects MAY proceed.

Standalone reconcile SHALL validate this contract before entering the
reconciliation pipeline. Daemon startup SHALL validate it before starting any
listener or background reconcile loop. Clone, fetch, and `bosun validate` SHALL
use the same validation rules so no consumer can bypass the pre-network guard.

The credential pair SHALL remain process-environment state. Bosun MUST NOT copy
it into `reconcile.Config`, project YAML, deploy state, metrics, trace
attributes, logs, returned errors, validation diagnostics, daemon `/config`, or
daemon health/status responses. Project config hot reload SHALL NOT define,
replace, or rotate the pair; an operator-supplied rotation takes effect after
the Bosun process is restarted with the new environment.

Before presentation, Bosun SHALL remove `URL.User` from a parseable repository
URL. If an invalid repository URL cannot be safely parsed, Bosun SHALL display a
fixed redacted placeholder rather than echo the raw URL. Transport errors SHALL
be wrapped or sanitized so observable output contains neither raw nor escaped
username/token/userinfo values nor the derived Basic Authorization value, while
still giving stable guidance for authentication failures.

SSH host key verification SHALL use config-controlled known_hosts files,
checked in order: `BOSUN_SSH_KNOWN_HOSTS` environment variable (explicit
override), then `/config/known_hosts` (container convention). The user-profile
path `~/.ssh/known_hosts` SHALL NOT be consulted, because it is an ephemeral
location in container environments that can be polluted by manual `ssh`
commands, causing key mismatches. When no known_hosts file is found, or the
first one found cannot be parsed, host key resolution SHALL fail closed: Git
authentication resolution returns an error and no Git operation runs, so daemon
startup validation rejects the configuration. An unparseable candidate SHALL be
terminal and SHALL NOT cause a later candidate to be substituted for it. The
`BOSUN_SSH_INSECURE_HOST_KEY` environment variable SHALL be the only way to
disable verification, and SHALL be evaluated before any known_hosts resolution.

When the SSH agent supplies the authentication method, the host key policy SHALL
be resolved before the agent's signers are handed to the transport, and the agent
connection SHALL be closed when the policy refuses, so a usable agent is never
offered to an unverified peer.

The SSH channel used for remote deployment SHALL likewise never disable host key
verification implicitly. It SHALL emit `StrictHostKeyChecking=yes` rather than
`accept-new` when no bosun-managed known_hosts candidate resolves, leaving
openssh's own default known-hosts files in play: a host already pinned there
still deploys, and an unpinned host is refused before any archive bytes are
written. Trust-on-first-use is not acceptable on this channel because it carries
the rendered secret material and the shipped container mounts the ssh directory
read-only, so no pin can persist and every deploy would be a first connection.

#### Scenario: Fresh clone on first run

- **WHEN** the repository directory does not exist or is not a git repo
- **THEN** the reconciler performs a shallow clone (depth 1)
- **AND** reports changed=true with the cloned commit hash

#### Scenario: Pull detects new commit

- **WHEN** the remote branch has a new commit
- **THEN** the reconciler fetches and hard-resets to the remote HEAD
- **AND** returns changed=true with before and after commit hashes

#### Scenario: Pull with dirty working tree

- **WHEN** the local repository has uncommitted changes
- **THEN** the reconciler logs a warning about the dirty state
- **AND** proceeds with fetch and hard reset (discarding local changes)
- **AND** the pull succeeds normally

#### Scenario: Branch validation rejects injection

- **WHEN** a branch name starts with `-` or contains shell metacharacters
- **THEN** the operation fails with a validation error before any git command executes

#### Scenario: DiffFiles with unavailable previous commit

- **WHEN** a shallow clone does not contain the previous commit referenced in the deploy state file
- **THEN** `DiffFiles` SHALL return a sentinel error indicating the commit is unavailable
- **AND** `executePostSyncHooks` SHALL treat all files as changed (run hooks against the full file set)
- **AND** a warning SHALL be logged indicating that full hook execution is triggered due to insufficient git history

#### Scenario: Configurable fetch depth

- **WHEN** `BOSUN_GIT_FETCH_DEPTH` is set to a value greater than 1
- **THEN** git clone and fetch operations SHALL use the specified depth
- **AND** the default depth SHALL remain 1 when unset

#### Scenario: Authenticated HTTPS clone

- **WHEN** a private `https://` repository is configured
- **AND** both `BOSUN_GIT_USERNAME` and `BOSUN_GIT_TOKEN` are non-empty
- **THEN** the initial clone authenticates with that username and token using HTTP Basic authentication

#### Scenario: Authenticated HTTPS fetch

- **WHEN** an existing private HTTPS checkout pulls a new remote commit
- **AND** both HTTPS Git credential variables are non-empty
- **THEN** the fetch authenticates with the same username and token used by clone

#### Scenario: Same-origin HTTPS redirect preserves authentication

- **WHEN** an authenticated HTTPS clone or fetch receives a redirect whose destination remains HTTPS with the same hostname and effective port
- **THEN** Bosun follows the redirect
- **AND** the redirected Git request carries the configured Basic credentials

#### Scenario: HTTPS downgrade redirect is rejected

- **WHEN** an authenticated HTTPS clone or fetch is redirected to an `http://` destination
- **THEN** synchronization fails before requesting the downgrade destination
- **AND** no Authorization header is forwarded

#### Scenario: Cross-origin redirect is rejected

- **WHEN** an authenticated HTTPS clone or fetch is redirected to a different hostname or effective port
- **THEN** synchronization fails before requesting the cross-origin destination
- **AND** no Authorization header is forwarded

#### Scenario: Standalone reconcile consumes HTTPS credentials

- **WHEN** `bosun reconcile` synchronizes a private HTTPS repository
- **THEN** it uses the configured HTTPS Git credential pair for clone and fetch

#### Scenario: Standalone reconcile rejects unsafe authentication before pipeline execution

- **WHEN** `bosun reconcile` starts with a partial pair, a credential-bearing non-HTTPS URL, or URL userinfo
- **THEN** command validation fails before the reconciliation pipeline or any Git network request starts
- **AND** the error is actionable and redacted

#### Scenario: Daemon reconcile consumes HTTPS credentials

- **WHEN** the daemon poll or webhook loop synchronizes a private HTTPS repository
- **THEN** it uses the same configured HTTPS Git credential pair as standalone reconcile

#### Scenario: Daemon startup rejects unsafe Git authentication

- **WHEN** daemon configuration contains a partial pair, a credential-bearing non-HTTPS URL, or URL userinfo
- **THEN** daemon validation fails before socket, TCP, or HTTP listeners and background loops start
- **AND** the startup error is actionable and redacted

#### Scenario: Anonymous HTTPS remains supported

- **WHEN** an HTTPS repository is configured and both HTTPS Git credential variables are unset
- **THEN** clone and fetch proceed without an authentication method

#### Scenario: Partial HTTPS credential pair fails closed

- **WHEN** only one of `BOSUN_GIT_USERNAME` or `BOSUN_GIT_TOKEN` is non-empty
- **THEN** repository synchronization fails before network I/O
- **AND** the error identifies the missing environment variable by name without exposing the configured value

#### Scenario: HTTPS credentials use the effective repository URL

- **WHEN** both `BOSUN_REPO_URL` and legacy `REPO_URL` are configured with different URLs
- **AND** the HTTPS Git credential pair is configured
- **THEN** authentication validation and synchronization use `BOSUN_REPO_URL`
- **AND** credentials are never evaluated against or sent to the shadowed legacy URL

#### Scenario: HTTPS credentials reject other transports

- **WHEN** both HTTPS Git credential variables are configured
- **AND** the repository URL is HTTP, SSH, a local path, or another non-HTTPS transport
- **THEN** repository synchronization fails before network I/O
- **AND** the error explains that HTTPS Git credentials require an `https://` repository URL

#### Scenario: HTTPS credentials reject malformed or hostless URLs

- **WHEN** both HTTPS Git credential variables are configured
- **AND** the effective repository URL is malformed or has an HTTPS scheme without a host
- **THEN** repository synchronization fails before network I/O
- **AND** the error does not echo the unsafe raw URL

#### Scenario: URL-embedded credentials are rejected

- **WHEN** a standard repository URL contains username-only, password-bearing, or percent-encoded userinfo
- **THEN** repository synchronization fails before network I/O
- **AND** logs, errors, validation diagnostics, and status responses omit the userinfo
- **AND** the error directs the operator to the dedicated environment variables

#### Scenario: SCP-like SSH URL is not userinfo

- **WHEN** the repository URL uses SCP-like SSH syntax such as `git@example.com:owner/repo.git`
- **AND** HTTPS Git credential variables are unset
- **THEN** Bosun does not reject the `git@` portion as URL userinfo
- **AND** existing SSH authentication resolution proceeds

#### Scenario: Validate reports unsafe HTTPS credential configuration

- **WHEN** `bosun validate` runs with a partial credential pair, credentials for a non-HTTPS URL, or URL-embedded userinfo
- **THEN** validation fails with the same actionable configuration error as runtime synchronization
- **AND** the diagnostic omits all credential and userinfo values

#### Scenario: Authentication failure is actionable and redacted

- **WHEN** a private HTTPS server rejects the configured Basic credentials
- **THEN** clone or fetch returns an actionable authentication error
- **AND** neither raw/escaped credentials nor the derived Basic Authorization value appears in the error, logs, or traces

#### Scenario: New HTTPS credential variables have no legacy aliases

- **WHEN** `GIT_USERNAME` or `GIT_TOKEN` is set without its `BOSUN_` counterpart
- **THEN** Bosun does not use that value for repository authentication

#### Scenario: BOSUN credential names cannot be completed by aliases

- **WHEN** only one `BOSUN_` credential variable is configured
- **AND** the corresponding unprefixed alias is also configured
- **THEN** Bosun reports the `BOSUN_` pair as partial before network I/O
- **AND** the unprefixed value is ignored

#### Scenario: Project config reload cannot rotate Git credentials

- **WHEN** `bosun.yaml` is reloaded during daemon reconciliation
- **THEN** no YAML field can define or replace the HTTPS Git username or token
- **AND** the daemon continues using the process environment received at startup

#### Scenario: Credential rotation requires process restart

- **WHEN** an operator changes the configured HTTPS Git credentials outside the running process
- **THEN** Bosun does not claim hot-reload support for the pair
- **AND** the new pair takes effect after the standalone command or daemon process is restarted

#### Scenario: Git credentials are not persisted

- **WHEN** Bosun constructs reconcile config, saves deploy state, emits metrics/traces, or serves daemon responses
- **THEN** neither HTTPS Git credential value nor a reusable Basic Authorization value is serialized or emitted

#### Scenario: Reconcile presentation redacts repository authentication

- **WHEN** a repository URL or Git transport failure is logged or returned by clone, fetch, or the reconciliation pipeline
- **THEN** parseable URL userinfo is removed and unsafe unparseable URLs use a fixed redacted placeholder
- **AND** raw/escaped credentials and the derived Basic Authorization value are absent

#### Scenario: Daemon config response redacts repository userinfo

- **WHEN** the daemon `/config` response includes the configured repository URL
- **THEN** the response includes only the sanitized URL without userinfo
- **AND** it includes no HTTPS Git credential field

#### Scenario: Daemon status and health responses redact authentication material

- **WHEN** `/status`, `/api/status`, or `/health` presents a repository URL or reconciliation error
- **THEN** raw/escaped credentials, URL userinfo, and the derived Basic Authorization value are absent

#### Scenario: HTTPS credential variables do not alter SSH resolution

- **WHEN** an SSH repository URL is configured and HTTPS Git credential variables are unset
- **THEN** authentication continues to resolve through the SSH agent and key-file chain

#### Scenario: known_hosts resolved from BOSUN_SSH_KNOWN_HOSTS

- **WHEN** `BOSUN_SSH_KNOWN_HOSTS` is set to a valid path
- **THEN** host key verification uses that file exclusively
- **AND** `/config/known_hosts` is not consulted

#### Scenario: known_hosts resolved from container convention path

- **WHEN** `BOSUN_SSH_KNOWN_HOSTS` is not set
- **AND** `/config/known_hosts` exists
- **THEN** host key verification uses `/config/known_hosts`

#### Scenario: No known_hosts found fails closed

- **WHEN** `BOSUN_SSH_KNOWN_HOSTS` is not set
- **AND** `/config/known_hosts` does not exist
- **AND** `BOSUN_SSH_INSECURE_HOST_KEY` is not `true`
- **THEN** Git authentication resolution returns an error naming the remediation
- **AND** no SSH connection is attempted

#### Scenario: Unparseable known_hosts fails closed

- **WHEN** the first known_hosts candidate that exists cannot be parsed
- **THEN** Git authentication resolution returns an error naming that file
- **AND** no later candidate is substituted for it

#### Scenario: Agent connection is closed when the host key policy refuses

- **WHEN** the SSH agent is reachable and no known_hosts candidate resolves
- **THEN** authentication resolution returns an error
- **AND** the agent connection is closed rather than handed to the transport

#### Scenario: Deploy channel refuses an unpinned host

- **WHEN** a remote deploy target has no bosun-managed known_hosts candidate
- **AND** `BOSUN_SSH_INSECURE_HOST_KEY` is not `true`
- **THEN** the ssh invocation uses `StrictHostKeyChecking=yes` rather than `accept-new`
- **AND** an unpinned host is refused before any archive bytes are written
- **AND** a host already pinned in openssh's own default known-hosts files still deploys

#### Scenario: User-profile known_hosts not consulted

- **WHEN** `BOSUN_SSH_KNOWN_HOSTS` is not set
- **AND** `/config/known_hosts` does not exist
- **AND** `~/.ssh/known_hosts` exists with valid host keys
- **THEN** the reconciler does NOT use `~/.ssh/known_hosts` for Git host key verification
- **AND** Git authentication resolution fails closed

#### Scenario: BOSUN_SSH_INSECURE_HOST_KEY disables verification entirely

- **WHEN** `BOSUN_SSH_INSECURE_HOST_KEY=true`
- **THEN** no known_hosts file is consulted
- **AND** all host keys are accepted without verification

### Requirement: Template Rendering

The reconciler SHALL render Go `text/template` files (`.tmpl` extension) from
the infrastructure subdirectory to a staging directory. Non-template files SHALL
be copied as-is. Rendered output SHALL strip the `.tmpl` extension.

Template rendering SHALL use Sprig function library plus custom bosun functions:
`include` (reads file contents) and `fromJsonFile` (reads and parses JSON file).

Template data SHALL be the merged secrets map, accessible via `{{ .key }}` syntax.

Rendered files SHALL be written atomically: write to temp file, set permissions
(0644), then rename to final path. This prevents malformed output from partial
writes.

The staging directory SHALL be cleared before rendering to prevent stale files
from previous runs.

Every repository-supplied template path SHALL be treated as untrusted. The
renderer SHALL refuse a template source that is not a regular file, and SHALL
refuse a path whose final component is a symlink before its target is read, so a
repository-authored symlink cannot cause the renderer to read a file outside the
repository — the SOPS secrets file, the age identity, `bosun.yaml` — and write
its contents into the deployed tree. A symlinked template entry encountered
during the directory walk SHALL be skipped with a warning and the walk SHALL
continue; every other rendering error SHALL abort staging rather than produce a
partial deploy.

#### Scenario: Template rendered with secrets

- **WHEN** a `.tmpl` file references `{{ .network.unraid_ip }}`
- **AND** the secrets contain a `network.unraid_ip` value
- **THEN** the rendered output contains the interpolated value

#### Scenario: Non-template file copied verbatim

- **WHEN** a file without `.tmpl` extension exists in the source directory
- **THEN** it is copied to the staging directory without modification

#### Scenario: Atomic write prevents partial output

- **WHEN** template execution fails mid-render
- **THEN** no output file is created (temp file is cleaned up)
- **AND** previously rendered files from other templates are unaffected

#### Scenario: Symlinked template is refused, not followed

- **WHEN** the repository contains a `.tmpl` entry that is a symlink pointing outside the repository
- **THEN** the renderer refuses it before reading the target
- **AND** the target's contents never appear in the staging tree
- **AND** the remaining templates still render

#### Scenario: Non-regular template source is refused

- **WHEN** a template path resolves to a FIFO, device, or socket
- **THEN** the renderer returns an unsupported-file-type error
- **AND** staging aborts rather than blocking on the read

### Requirement: File Deployment

The reconciler SHALL support two deployment modes: local (direct file
operations) and remote (SSH+tar or SCP).

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

Local deployment SHALL resolve every destination mutation against a pinned
directory handle rather than by path. For both the directory and the single-file
entry point, directory creation, temporary-file creation, rename, removal, and
the directory sync SHALL be performed relative to a root handle opened once on
the deployment root, so a destination component replaced with a symlink after a
containment check cannot redirect a privileged write outside that root. Lexical
containment checking alone is insufficient: the check reasons about a string
while the write resolves that string again through whatever links exist at write
time.

The pinned root SHALL be a directory above any container-writable component, and
SHALL be opened lazily on the first mutation, so a deploy whose walk fails
immediately leaves no destination directory behind. A symlink whose target
remains inside the pinned root SHALL still be followed; the guarantee is
confinement to the root, not immutability within it.

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

#### Scenario: SSH host validation rejects injection

- **WHEN** a target host contains shell metacharacters or starts with `-`
- **THEN** the operation fails with a validation error before any SSH command runs

#### Scenario: Destination subdirectory swapped for an escaping symlink

- **WHEN** a container replaces a destination subdirectory with a symlink to a host path after the containment check and before the write
- **THEN** the write is refused
- **AND** nothing is created at the symlink's target

#### Scenario: Single-file target under a symlinked parent

- **WHEN** a single-file deploy target's parent directory is a pre-existing symlink out of the deployment root
- **THEN** the write is refused rather than silently following it

#### Scenario: Failed walk leaves no destination directory

- **WHEN** a deploy fails while walking the source before any file is written
- **THEN** no destination directory has been created

### Requirement: Reconciliation Locking

The reconciler SHALL acquire an exclusive file-based lock before executing the
pipeline to prevent concurrent reconciliation runs.

On Unix systems, locking SHALL use `flock(2)` with `LOCK_EX|LOCK_NB` for
non-blocking exclusive lock. On Windows, locking SHALL use `LockFileEx` with
`LOCKFILE_EXCLUSIVE_LOCK|LOCKFILE_FAIL_IMMEDIATELY`.

The lock SHALL be released via defer to guarantee release even on errors or
panics.

The default lock file path SHALL be `/var/run/bosun/reconcile.lock`, configurable
via `LockFile`.

The lock file SHALL be reachable only by the daemon's own user. It SHALL be
created with owner-only permissions and opened without following a symlink at its
final component. An exclusive advisory lock is granted on any open descriptor
regardless of open mode, so a world-readable lock file would let any local
principal hold the lock and block every reconcile; the file mode is therefore the
access control.

A lock file that already exists with wider permissions SHALL have its mode
tightened through the open descriptor on the next acquire, so an upgraded
deployment does not retain the permissive mode. A failed tighten SHALL warn and
continue rather than abort, because failing there would cause the same outage the
requirement prevents.

Lock directories the reconciler creates SHALL be owner-only. A pre-existing
directory SHALL keep its mode, because a configured lock path may live in a
directory the reconciler does not own.

#### Scenario: Concurrent reconciliation prevented

- **WHEN** a reconciliation is running and another trigger arrives
- **THEN** the second run fails immediately with a "lock already held" error
- **AND** does not queue or wait

#### Scenario: Lock released after failure

- **WHEN** a reconciliation fails at any stage
- **THEN** the lock is released
- **AND** subsequent reconciliation runs can acquire it

#### Scenario: Lock file is not reachable by other local users

- **WHEN** the reconciler creates its lock file
- **THEN** the file's mode grants access only to the owner
- **AND** the open does not follow a symlink at the final path component

#### Scenario: Pre-existing permissive lock is tightened

- **WHEN** a lock file already exists with wider permissions
- **THEN** its mode is tightened on the next acquire
- **AND** a failure to tighten logs a warning and the reconcile proceeds

### Requirement: Critical Container Health Gate

The reconciler SHALL support a configurable list of critical containers that MUST be healthy after `docker compose up` for the deployment to succeed. When any critical container is unhealthy or missing after the health gate timeout, the reconciler SHALL trigger rollback and fail the deployment.

Critical containers SHALL be configured via `critical_containers` in `bosun.yaml` (a list of container names) and overridable via `BOSUN_CRITICAL_CONTAINERS` environment variable (JSON string array). When the env var is set, it completely replaces the config file value.

The health gate SHALL run after the startup grace period has elapsed but before recording the deployment as successful. The gate SHALL poll critical container health via Docker API `ContainerInspect` every 5 seconds for up to `HealthGateTimeout` (default 60 seconds, configurable via `BOSUN_HEALTH_GATE_TIMEOUT`).

Health status classification for critical containers:
- **healthy**: pass (container is running and Docker healthcheck reports healthy)
- **no healthcheck defined**: pass (cannot gate on undefined checks)
- **unhealthy**: fail (Docker healthcheck reports unhealthy after timeout)
- **starting**: fail if still starting at timeout (treated as not-yet-healthy)
- **missing or not running**: fail (container does not exist or is not in running state)

When the critical container list is empty (default), the health gate SHALL be skipped entirely, preserving backwards compatibility.

The `critical_containers` config SHALL be reloaded from the repo's `bosun.yaml` after each git pull, unless the `BOSUN_CRITICAL_CONTAINERS` env var override is set.

Container-supplied health output SHALL be treated as untrusted text. A health check's output is the stdout and stderr of a command running inside a monitored container, so it is controlled by anyone with code execution there. The reconciler SHALL strip control, formatting, and separator characters from that output before embedding it in any presentation string, and SHALL do so before applying the length cap, so a control character cannot survive by sitting beyond the truncation point. Both the drift printout and the health-gate error path SHALL inherit this neutralization.

#### Scenario: All critical containers healthy

- **WHEN** `critical_containers` is configured with `["traefik", "authelia"]`
- **AND** both containers are running and healthy after compose up
- **THEN** the health gate passes
- **AND** the deployment is recorded as successful
- **AND** post-deploy verification proceeds normally

#### Scenario: Critical container unhealthy triggers rollback

- **WHEN** `critical_containers` is configured with `["traefik", "authelia"]`
- **AND** traefik is healthy but authelia reports "unhealthy" after the health gate timeout
- **THEN** the health gate fails
- **AND** the reconciler triggers rollback to the backup compose files
- **AND** a failure alert is sent identifying authelia as the failing container
- **AND** the deployment is NOT recorded as successful

#### Scenario: Critical container missing triggers rollback

- **WHEN** `critical_containers` is configured with `["traefik", "authelia"]`
- **AND** traefik is running but authelia's container does not exist after compose up
- **THEN** the health gate fails
- **AND** the reconciler triggers rollback

#### Scenario: Critical container without healthcheck passes

- **WHEN** `critical_containers` is configured with `["traefik"]`
- **AND** traefik is running but has no Docker healthcheck defined
- **THEN** the health gate passes (no healthcheck defined = pass)
- **AND** the deployment is recorded as successful

#### Scenario: Health gate timeout with eventual success

- **WHEN** `critical_containers` is configured with `["authelia"]`
- **AND** authelia initially reports "starting" but becomes "healthy" within the timeout
- **THEN** the health gate polls every 5 seconds
- **AND** passes as soon as authelia reports healthy
- **AND** the deployment is recorded as successful

#### Scenario: Empty critical containers list skips gate

- **WHEN** `critical_containers` is empty or not configured
- **THEN** the health gate is skipped entirely
- **AND** the deployment proceeds as before (backwards compatible)

#### Scenario: Env var overrides config file

- **WHEN** `bosun.yaml` sets `critical_containers: ["traefik"]`
- **AND** `BOSUN_CRITICAL_CONTAINERS` is set to `["traefik", "authelia"]`
- **THEN** the health gate uses `["traefik", "authelia"]` from the env var
- **AND** config reload from the repo does not update the critical containers list

#### Scenario: Health gate skipped in dry run

- **WHEN** `DryRun` is true
- **AND** `critical_containers` is configured
- **THEN** the health gate is skipped
- **AND** no Docker API calls are made for health inspection

#### Scenario: Health gate skipped for remote deploys

- **WHEN** `TargetHost` is set (remote deployment)
- **AND** `critical_containers` is configured
- **THEN** the health gate is skipped (Docker API is local-only)
- **AND** a warning is logged indicating the health gate cannot run for remote deploys

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

Validation SHALL resolve against the extraction root itself, not against the
member's name. The extraction root SHALL be pinned once, and every directory
creation, file creation, rename, link, and removal SHALL be performed relative to
that pinned root, so an entry whose parent was turned into a symlink by an
earlier entry in the same archive cannot escape. Lexical validation alone is
insufficient: it reasons about an entry's name while the write resolves through
the tree the archive has already built. The extractor SHALL additionally refuse
any entry whose existing ancestor directory is a symlink, so an archive cannot
leave an escaping symlink inside the root for a later step to follow.

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

#### Scenario: Chained symlink entries cannot escape

- **WHEN** an archive contains a sequence of symlink entries that each validate lexically inside the root but together resolve outside it, followed by a regular entry beneath them
- **THEN** extraction is refused
- **AND** nothing is written outside the extraction root

#### Scenario: Entry beneath a symlinked ancestor is refused

- **WHEN** an entry's existing ancestor directory inside the root is a symlink
- **THEN** extraction is refused for that entry

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

### Requirement: Restart Breaker Baseline Integrity

The restart circuit breaker SHALL NOT silently reset its restart-count baseline merely because the evaluation window elapsed while restarts are still accumulating, so that a sustained slow restart loop still trips.

When evaluating a tracked service whose current restart count exceeds its
baseline (`delta > 0`), the breaker SHALL preserve the earliest unresolved-restart
baseline (its count and timestamp) when the elapsed time exceeds the configured
window, rather than resetting the baseline to the current observation. The breaker
SHALL advance the baseline normally only when no new restarts occurred since the
last check (`delta <= 0`). A service that restarts repeatedly across intervals
longer than `BOSUN_RESTART_WINDOW` SHALL still accumulate toward the threshold and
trip.

At configuration load, the daemon SHALL warn when `BOSUN_DRIFT_INTERVAL` is
greater than `BOSUN_RESTART_WINDOW`, because the breaker observes restart counts
on the drift-check cadence and a window-bounded delta would otherwise be
unobservable.

The breaker SHALL stop only containers within a resolved Compose project scope,
and SHALL refuse to act when no scope resolves. An empty project scope matches
every container on the Docker host; because the breaker holds the Docker socket
and its action is destructive, an unscoped breaker is a confused deputy that
would stop and leave stopped a container bosun does not manage. The guard SHALL
sit at the destructive call so that it covers every caller.

The scope SHALL be resolved the way the reconcile path resolves it: from a single
configured target's project name, else from an explicitly configured root-level
project name. A directory-name fallback SHALL NOT be used as a scope, because it
names no project that Docker labels containers with.

When no scope resolves, the breaker SHALL stop nothing, SHALL preserve existing
restart tracking rather than advancing it — advancing it would require trusting
the same unscoped observation the guard refuses — and SHALL announce the inactive
state at daemon startup, on every drift cycle, and through `bosun doctor`.

#### Scenario: Slow restart loop trips despite long drift interval
- **WHEN** `BOSUN_DRIFT_INTERVAL` is greater than `BOSUN_RESTART_WINDOW` and a container restarts repeatedly across successive drift checks
- **THEN** the breaker preserves the accumulating baseline rather than resetting it each interval
- **AND** the service eventually trips the restart breaker

#### Scenario: Clean check advances the baseline
- **WHEN** a tracked service shows no new restarts since the last check (`delta <= 0`)
- **THEN** the breaker advances the baseline to the current count and timestamp

#### Scenario: Misconfigured intervals warn at load
- **WHEN** the daemon loads configuration with `BOSUN_DRIFT_INTERVAL` greater than `BOSUN_RESTART_WINDOW`
- **THEN** a warning is logged identifying the interval/window mismatch

#### Scenario: Foreign-project container is never stopped
- **WHEN** a restart-looping container outside the resolved Compose project crosses the restart threshold
- **THEN** the breaker does not stop it
- **AND** it does not enter restart tracking

#### Scenario: Unscoped breaker announces itself
- **WHEN** the breaker is enabled and no Compose project scope resolves
- **THEN** it stops nothing and preserves existing restart tracking
- **AND** the inactive state is reported at daemon startup, on each drift cycle, and by `bosun doctor`


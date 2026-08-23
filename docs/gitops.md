# GitOps Reconciliation System

This document describes the bosun GitOps reconciliation system, which automates infrastructure deployment by syncing configuration from a Git repository to target systems.

## Overview

Bosun supports two deployment modes:

| Mode | Description | Use Case |
|------|-------------|----------|
| **Daemon** | Long-running service with Unix socket API | Production, automated GitOps |
| **One-shot** | Single `bosun reconcile` execution | Manual deploys, testing |

The reconcile system implements a GitOps workflow that:

1. Monitors a Git repository for changes
2. Decrypts secrets using SOPS with age encryption
3. Renders templates using Go's native `text/template` engine with [Sprig](https://masterminds.github.io/sprig/) functions
4. Deploys configuration files to local or remote targets
5. Restarts affected services

### When to Use

- **Continuous deployment**: Run the daemon with webhooks or polling for automated GitOps
- **Manual deployment**: Run `bosun reconcile` to apply the latest configuration immediately
- **Testing changes**: Use `--dry-run` to preview what would change before applying

## Daemon Mode

For production deployments, run bosun as a long-running daemon:

```bash
bosun daemon
```

### Unix Socket API

The daemon exposes a Unix socket at `/var/run/bosun.sock` (configurable) for local control:

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/trigger` | POST | Trigger reconciliation |
| `/status` | GET | Get daemon status |
| `/health` | GET | Health check |
| `/ready` | GET | Readiness check |
| `/config` | GET | Get current config |
| `/ping` | GET | Simple ping |
| `/api/drift` | GET | Get drift status (declared vs actual) |
| `/api/containers` | GET | List all containers with summary |
| `/api/trigger` | POST | Trigger reconciliation (WebUI) |
| `/api/status` | GET | Extended status for WebUI |

**Example usage:**

```bash
# Trigger reconciliation
curl --unix-socket /var/run/bosun.sock http://localhost/trigger -X POST

# Check status
curl --unix-socket /var/run/bosun.sock http://localhost/status
```

Or use the CLI:

```bash
bosun trigger                    # Trigger via socket
bosun daemon-status              # Get daemon status
bosun validate                   # Validate config and connectivity
```

### Webhook Providers

The daemon itself serves two webhook endpoints:

| Endpoint | Path | Signature Header |
|----------|------|------------------|
| GitHub | `/webhook/github` | `X-Hub-Signature-256` |
| Generic | `/webhook` | `X-Signature`, or `X-Hub-Signature-256` |

GitLab, Gitea, and Bitbucket are **not** handled directly by the daemon. To receive
them, run the standalone `bosun webhook` receiver, which understands each provider's
signature scheme and forwards normalized triggers to the daemon over its Unix socket:

| Provider | Path (receiver) | Signature Header |
|----------|-----------------|------------------|
| GitHub | `/webhook/github` | `X-Hub-Signature-256` |
| GitLab | `/webhook/gitlab` | `X-Gitlab-Token` |
| Gitea | `/webhook/gitea` | `X-Gitea-Signature` |
| Bitbucket | `/webhook/bitbucket` | `X-Hub-Signature` |

Do not point a Gitea/GitLab/Bitbucket webhook directly at the daemon's `/webhook`
endpoint — its generic handler expects `X-Signature`/`X-Hub-Signature-256`, so a
provider-specific signature header will fail validation. Signatures are validated
using HMAC-SHA256 (or SHA1 for legacy) with constant-time comparison.

### Polling Mode

Enable periodic reconciliation with `--poll-interval`:

```bash
bosun daemon --poll-interval 3600  # Check every hour
```

Set to `0` to disable polling (webhook-only mode).

### Concurrency

The daemon uses single-flight reconciliation with counted trigger coalescing:

1. If a trigger arrives during reconciliation, increment the pending count and retain its source
2. Return immediately (HTTP 202 Accepted)
3. After reconciliation completes, atomically drain the pending batch
4. Run one follow-up reconciliation with sorted distinct-source attribution and sticky `force`
5. Triggers arriving during that follow-up form another batch

This prevents concurrent docker compose operations while ensuring no trigger batch or source attribution is lost.

### Security

- **Socket permissions**: 0660 (owner and group only)
- **SO_PEERCRED**: Logs kernel-reported UID/PID of every caller
- **Bearer auth**: Optional TCP API requires `Authorization: Bearer <token>`
- **Secret injection**: Webhook secret fetched from daemon, never on disk

See [docs/architecture/daemon-split.md](architecture/daemon-split.md) for the full security model.

## Architecture

```
+------------------+     +------------------+     +------------------+
|   Git Repository |---->|    Reconciler    |---->|  Target System   |
|  (dotfiles repo) |     |                  |     |  (Unraid/Docker) |
+------------------+     +------------------+     +------------------+
                               |
                               v
                    +--------------------+
                    |   Component Stack  |
                    +--------------------+
                    | GitOps      - go-git library (in-process)
                    | SOPSOps     - go-sops library (in-process)
                    | TemplateOps - Go text/template + Sprig
                    | DeployOps   - native file copy / tar-over-SSH
                    +--------------------+
```

### Data Flow

Before any target starts, Bosun canonicalizes every effective staging path,
rejects equal or nested slots, and secures or deletes pre-existing evidence. A
slot that can be neither protected nor deleted aborts the whole cycle before
Git sync or secret decryption.

```
 1. Lock Acquisition
        |
        v
 2. Git Sync (clone or pull)
        |
        v
 3. State-Based Skip Logic
    (compare deployed commit vs current, circuit breaker check)
        |
        v (if new commit, force, or state mismatch)
 4. SOPS Decryption
        |
        v
 5. Template Rendering (Go text/template + Sprig)
        |
        v
 6. Extract Declared State (parse rendered compose files;
    fatal if compose dir missing; fatal on zero services unless
    BOSUN_ALLOW_EMPTY_DECLARED_STATE=true)
        |
        v
 7. Backup Creation (tar.gz of current configs)
        |
        v
 8. Deployment (native file copy or tar-over-SSH)
        |
        v
 9. Deploy-Sync Invariant Check
    (per-target: WrittenFiles must exist at destination with
    mtime >= reconcile start; empty WrittenFiles against
    non-empty source fails; bypass via BOSUN_SKIP_DEPLOY_INVARIANT)
        |
        v
10. Service Reload (docker compose up, SIGHUP)
        |
        v
11. Critical Container Health Gate (with rollback)
        |
        v
12. Post-Sync Hooks
        |
        v
13. Post-Deploy Verification (drift check against Docker)
        |
        v
14. Remove Verified Staging
        |
        v
15. Record Deploy State (commit, declared services, timestamp)
        |
        v
16. Release Lock
```

> **Invariant gates at stages 6 and 9** prevent the silent-success failure mode where a deploy reports success but no files actually landed on disk. See [Deploy-Sync Invariants in the skill resource](../skills/onboard/resources/gitops.md) and the [troubleshooting guide](troubleshooting.md) for operator-facing detail on `BOSUN_ALLOW_EMPTY_DECLARED_STATE` and `BOSUN_SKIP_DEPLOY_INVARIANT`.

## Deploy State Tracking

The reconciler maintains persistent state in a JSON file at `/var/lib/bosun/deploy-state.json` (configurable via `BOSUN_STATE_DIR`). This state drives skip logic, circuit breaking, and drift detection. Both daemon and one-shot modes create the state file's parent directory before a real reconciliation; one-shot multi-target runs prepare and verify each target's state directory independently and fail that target before deployment if the directory cannot be written. A one-shot dry run works against a temporary copy of existing state, preserving skip and circuit-breaker decisions without creating or updating the configured state path.

### State File Schema (v2)

```json
{
  "schema_version": 2,
  "last_deployed_commit": "abc123...",
  "deployed_at": "2025-01-15T14:30:22Z",
  "source": "webhook:github",
  "last_attempted_commit": "abc123...",
  "attempt_count": 0,
  "declared_services": [
    {"name": "web", "image": "nginx:1.25"},
    {"name": "api", "image": "myapp:v2"}
  ],
  "drift_checked_at": "2025-01-15T14:35:00Z",
  "drift_items": []
}
```

### State-Based Skip Logic

The reconciler compares the **last deployed commit** (not last fetched) against the current HEAD. This distinction matters: if a deploy fails halfway, the state file still records the *previous* successful commit, ensuring the pipeline re-runs on the next trigger.

### Circuit Breaker

After 3 consecutive failures on the same commit, the reconciler stops retrying:

```
Attempt 1: deploy fails → attempt_count=1, continues
Attempt 2: deploy fails → attempt_count=2, continues
Attempt 3: deploy fails → attempt_count=3, stops
Next trigger: skips unless --force or new commit arrives
```

Use `--force` to override the circuit breaker.

### Atomic State Writes

State is written using the crash-safe pattern: write temp file (same directory) → fsync temp → rename → fsync directory. This prevents corruption from power loss or crashes during write.

### Fail-Open Behavior

Missing or corrupt state files are treated as "never deployed," which triggers a full deploy. This is correct fail-open behavior — it's better to deploy redundantly than to silently skip.

## Drift Detection

Drift detection compares **declared state** (services defined in rendered compose files) against **actual state** (running Docker containers). This closes the feedback loop in the GitOps model — after deploying, verify the deployment took effect.

### How It Works

1. **Declared state** is extracted from rendered compose files during reconciliation (step 6) and saved in the deploy state file.
2. **Actual state** is collected from Docker by querying all containers and filtering by compose project labels.
3. **Comparison** checks each declared service against actual containers.

### Drift Types

| Type | Severity | Condition |
|------|----------|-----------|
| `missing` | Critical | Service not running, or running but state is not `running` (e.g., exited) |
| `unhealthy` | Critical | Service running but Docker health check reports `unhealthy` |
| `image_mismatch` | Warning | Running image tag differs from declared (e.g., `nginx:1.24` vs `nginx:1.25`) |

### Container Matching

Containers are matched to declared services using Docker Compose v2 labels:

- `com.docker.compose.project` — the compose project name
- `com.docker.compose.service` — the service name within the project

Labels are preferred because they are authoritative and unambiguous. For containers without labels, a name-based fallback parses the `<project>-<service>-<replica>` convention.

### Image Comparison

Images are normalized before comparison: bare names (e.g., `nginx`) are treated as `nginx:latest`. Digest references (`nginx@sha256:...`) are compared exactly.

### Daemon Drift Loop

When running as a daemon, drift checks run on a configurable interval (default: 5 minutes, set via `BOSUN_DRIFT_INTERVAL`). The loop:

1. Skips if a reconciliation is in progress (avoids state file race conditions)
2. Loads declared state from the deploy state file
3. Queries Docker for actual state
4. Filters through the debounce layer (if enabled via `BOSUN_DRIFT_ALERT_DEBOUNCE`)
5. Filters through the dedup layer (per-item cooldown via `BOSUN_DRIFT_ALERT_COOLDOWN`)
6. Updates the state file with drift results and debounce/dedup timestamps
7. Sends an alert if critical drift persists past both layers

Set `BOSUN_DRIFT_INTERVAL=0` to disable periodic drift checks.

#### Restart Circuit Breaker

The restart circuit breaker samples container identity and restart counts during each drift check. Once restart counts begin increasing, Bosun preserves the earliest unresolved baseline until a clean sample observes no new restarts, so sustained slow loops still accumulate toward `BOSUN_RESTART_THRESHOLD` even when checks are farther apart than `BOSUN_RESTART_WINDOW`. A deploy can recreate a container and reset Docker's restart count; Bosun treats the changed identity as recreation, keeps an existing trip active, and resolves it only after the same container identity has no additional restarts across the next drift-check interval. Missing containers retain the trip but restart the stability grace when they return; transient inspect failures preserve the last persisted observation and cannot count as recovery. Keep `BOSUN_DRIFT_INTERVAL` at or below `BOSUN_RESTART_WINDOW` for timely detection; daemon configuration and `bosun doctor` warn when the sampling interval is longer.

#### Drift Alert Debounce

Transient drift from I/O pressure, image updates, or daemon restarts generates alert noise that self-resolves within minutes. The debounce layer suppresses alerts until drift persists beyond a configurable window.

The alert pipeline flows: **detect -> debounce filter -> dedup (per-item cooldown) -> send**.

- **`BOSUN_DRIFT_ALERT_DEBOUNCE`** (env var) or **`drift_alert_debounce`** (bosun.yaml): Duration before first alert fires. Default `0` (disabled, alerts fire immediately). Recommended starting value: `5m`.
- First detection records the item in `drift_debounce_items` without alerting.
- If drift resolves before the window expires, the item is silently removed (no alert).
- If drift persists past the window, the item graduates to the dedup/cooldown layer.
- Resolution alerts bypass debounce: they fire immediately for previously alerted items, but not for items that were still in debounce (never alerted).
- A critical type transition (`unhealthy` to `missing`, or the reverse) remains active drift for that service. The replacement type follows normal debounce/dedup handling, and Bosun does not send a resolution until the service has no critical drift.
- Adding an ignore rule for active drift removes its prior alert state silently; Bosun sends a resolution only when the service's critical drift is absent from the unfiltered Docker comparison.
- Debounce state persists across daemon restarts via the state file.

### Post-Deploy Verification

After each successful deployment, the reconciler performs an immediate drift check:

1. Waits for the startup grace period (default: 30 seconds) to let containers start and pass health checks
2. Collects actual state from Docker
3. Compares against declared state
4. Updates drift results in the state file
5. Logs warnings for any drift items (does not fail the reconciliation)

The earlier health gate is a separate, rollback-capable check controlled by
`health_gate_scope` and `BOSUN_HEALTH_GATE_TIMEOUT`. Its timeout defaults to
`60s`; set `BOSUN_HEALTH_GATE_TIMEOUT=0` to disable the gate without changing
the configured scope or container list.

### CLI Access

```bash
bosun drift                  # Show cached drift status
bosun drift --live           # Fresh check against Docker
bosun drift --json           # Machine-readable output
bosun drift --project core   # Filter by compose project
```

### API Access

The daemon exposes drift status at `GET /api/drift`:

```json
{
  "status": "clean",
  "checked_at": "2025-01-15T14:35:00Z",
  "declared_count": 12,
  "drift_item_count": 0,
  "items": []
}
```

Status values: `clean` (no drift), `drifted` (items detected), `unknown` (no deployment recorded).

## Configuration

### Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `REPO_URL` | Yes | - | Git repository URL |
| `REPO_BRANCH` | No | `main` | Branch to track |
| `BOSUN_GIT_USERNAME` | No | - | Private HTTPS Git Basic-auth username; requires `BOSUN_GIT_TOKEN` |
| `BOSUN_GIT_TOKEN` | No | - | Private HTTPS Git Basic-auth password/token; requires `BOSUN_GIT_USERNAME` |
| `REPO_DIR` | No | `/app/repo` | Local clone directory |
| `STAGING_DIR` | No | `/app/staging` | Secret-bearing rendered staging and single-slot failure evidence directory |
| `BACKUP_DIR` | No | `/app/backups` | Configuration backups |
| `LOG_DIR` | No | `/app/logs` | Log files directory |
| `LOCAL_APPDATA` | No | `/mnt/appdata` | Local appdata path |
| `REMOTE_APPDATA` | No | `/mnt/user/appdata` | Remote appdata path |
| `DEPLOY_TARGET` | No | - | SSH target (e.g., `root@192.168.1.8`) |
| `SECRETS_FILES` | No | - | Comma-separated SOPS files |
| `DRY_RUN` | No | `false` | Preview mode |
| `FORCE` | No | `false` | Deploy even without changes |
| `BOSUN_STATE_DIR` | No | `/var/lib/bosun` | Directory for deploy state file |
| `BOSUN_DRIFT_INTERVAL` | No | `5m` | Drift check interval (0 to disable) |
| `BOSUN_RESTART_BREAKER` | No | `true` | Enable restart-loop protection |
| `BOSUN_RESTART_THRESHOLD` | No | `5` | Accumulated restart count that trips the breaker (must be positive) |
| `BOSUN_RESTART_WINDOW` | No | `10m` | Restart observation window; keep at least as long as the drift interval |
| `BOSUN_DRIFT_ALERT_COOLDOWN` | No | `1h` | Cooldown between repeated drift alerts per item |
| `BOSUN_DRIFT_ALERT_DEBOUNCE` | No | `0` | Debounce window before first drift alert (0 = disabled) |
| `BOSUN_DRIFT_RESOLVE_ALERTS` | No | `true` | Send "drift resolved" notifications |
| `BOSUN_HEALTH_GATE_TIMEOUT` | No | `60s` | Health gate polling timeout (`0` disables the gate) |

Private HTTPS repositories use `BOSUN_GIT_USERNAME` and `BOSUN_GIT_TOKEN` as
one required pair for both clone and fetch. With both unset, HTTPS remains
anonymous. Bosun rejects partial pairs, non-HTTPS credential use, credentials
embedded in the repository URL, HTTPS-to-HTTP redirects, and redirects to a
different host or effective port. The pair has no legacy aliases and applies
to the effective repository URL after `BOSUN_REPO_URL` overrides `REPO_URL`.
Credentials remain process-environment state: remove any URL userinfo, rotate
the environment values, and restart Bosun; `bosun.yaml` reload cannot rotate
them.

### Command-Line Flags

```bash
bosun reconcile [flags]

Flags:
  -n, --dry-run         Show what would be done without making changes
  -f, --force           Force deployment even if no changes detected
  -l, --local           Force local deployment mode
  -r, --remote string   Target host for remote deployment (e.g., root@192.168.1.8)
```

### Example Configuration

```bash
# Container deployment
export REPO_URL="git@github.com:user/dotfiles.git"
export REPO_BRANCH="main"
export SECRETS_FILES="infrastructure/secrets.yaml"
export DEPLOY_TARGET="root@192.168.1.8"

bosun reconcile
```

```bash
# Local testing with dry-run
export REPO_URL="git@github.com:user/dotfiles.git"
export SECRETS_FILES="infrastructure/secrets.yaml"

bosun reconcile --dry-run --local
```

## Git Operations

The git subsystem (`internal/reconcile/git.go`) handles repository synchronization using the [go-git](https://github.com/go-git/go-git) library for pure Go, in-process Git operations:

### Clone Behavior

- **Shallow clone**: Uses depth 1 by default to minimize bandwidth; `BOSUN_GIT_FETCH_DEPTH` configures a deeper positive depth
- **Single branch**: Only fetches the configured branch
- **Cleanup on failure**: Removes partial clones if the operation fails
- **In-process**: No external `git` binary required

### Pull Behavior

- **Shallow fetch**: Uses the same configured depth as clone and deepens an existing shallow checkout when needed
- **Hard reset**: Resets to `origin/<branch>` to ensure clean state
- **Change detection**: Compares commit hashes before/after to detect changes
- **Missing diff history fails safe**: An unavailable prior commit is reported explicitly; deploy-path checks run a full deploy and post-sync hooks all fire

### Post-Sync Hook Snapshots

After each pull, bosun treats a successfully decoded project config as an authoritative hook snapshot. Omitting `post_sync_hooks` or setting it to `[]` clears file-sourced hooks; omitting `hook_settle_delay` retains the effective delay, while explicit `0s` disables it. Missing, unreadable, malformed, unknown-field, and invalid-hook configs retain the previous hook/delay state. `BOSUN_POST_SYNC_HOOKS`, `BOSUN_HOOK_SETTLE_DELAY`, and target hooks supplied by `BOSUN_TARGETS` remain authoritative environment replacements.

Root and per-target hooks validate before either hooks or delay change. An omitted target hook key inherits root, explicit `[]` clears inheritance, and removing a target descriptor drops its stale operational hook override while structural removal waits for daemon restart. Logs expose source, target, outcome, and counts, never hook command arguments.

Hook globs use staging-relative paths such as `appdata/traefik/**`. When actual written/deleted paths are unavailable, bosun diffs from `DeployState.LastDeployedCommit` and strips the infra-directory prefix from repo-relative paths before matching. A failed pipeline therefore cannot advance the hook diff base or silently lose its changes on retry.

### Timeouts

| Operation | Timeout |
|-----------|---------|
| Clone | 5 minutes |
| Fetch | 2 minutes |
| Local operations | 30 seconds |

### State Tracking

The `Sync()` method returns:
- `changed bool` - Whether the repository was updated
- `before string` - Commit hash before sync (empty for fresh clones)
- `after string` - Commit hash after sync

## Secrets Management

The SOPS subsystem (`internal/reconcile/sops.go`) handles encrypted secrets using the [go-sops](https://github.com/getsops/sops) library with [age](https://github.com/FiloSottile/age) encryption. All decryption happens in-process without requiring an external `sops` binary.

Secrets-file format is inferred from the filename. Bosun supports YAML (`.yaml`, `.yml`), JSON (`.json`), dotenv (`.env`), and INI (`.ini`), including legacy names such as `secrets.yaml.sops`. Other extensions fail with a clear error; SOPS binary files are not supported because Bosun must merge decrypted secrets into a key/value template map.

### Age Key Resolution

The system checks for age keys in this order:

1. `SOPS_AGE_KEY` environment variable (key content directly)
2. `SOPS_AGE_KEY_FILE` environment variable (path to key file)
3. Default location: `~/.config/sops/age/keys.txt`

### Key Setup

```bash
# Generate a new age key
age-keygen -o ~/.config/sops/age/keys.txt

# Or use an existing key via environment
export SOPS_AGE_KEY="AGE-SECRET-KEY-1..."

# Or specify a custom key file
export SOPS_AGE_KEY_FILE="/path/to/my/key"
```

### SOPS File Validation

Before decryption, the system validates:

1. File exists
2. Valid YAML syntax
3. Contains a `sops` metadata mapping
4. Contains a non-empty MAC and a valid RFC3339 `lastmodified` timestamp
5. Contains at least one key recipient with a non-empty encrypted data key

After structural validation, decryption failures are reported as one of four
sanitized categories: integrity verification, key unavailable, malformed
encrypted data, or an unclassified decryption failure. Integrity failures tell
operators to restore or re-encrypt the file rather than rotate an unrelated
key. Raw SOPS errors are not returned or logged because they can contain
decrypted MACs, encrypted values, key identifiers, or additional local paths;
the requested secrets-file path remains in the surrounding error context.

### Decryption Flow

```
1. Validate SOPS file structure
       |
       v
2. Check age key availability
       |
       v
3. In-process decryption via go-sops library
       |
       v
4. Parse decrypted content to map[string]any
       |
       v
5. Merge multiple files (later files override earlier)
```

### Multiple Secrets Files

When multiple secrets files are specified, they are decrypted and merged:

```bash
export SECRETS_FILES="common/secrets.yaml,infrastructure/secrets.yaml"
```

Keys in later files override earlier ones. Nested maps are recursively merged.

## Templating

The template subsystem (`internal/reconcile/template.go`) uses Go's native `text/template` engine with [Sprig](https://masterminds.github.io/sprig/) functions for template rendering. This provides the same Go template syntax without requiring an external binary.

### Template File Convention

- Files ending in `.tmpl` are processed as templates
- The `.tmpl` extension is removed in the output
- Non-template files are copied as-is

### Accessing Secrets in Templates

Bosun decrypts secrets and passes the resulting map directly as the template's
root data. Access values without reading the secrets file:

```go-template
{{ .network.unraid_ip }}
```

For non-secret supporting data, `include` and `fromJsonFile` may read files
inside the configured include subtree. This example uses an absolute allowlist
root so the file path is independent of the process working directory:

```bash
export BOSUN_TEMPLATE_INCLUDE_DIR=/srv/bosun/includes
```

```go-template
{{ $defaults := fromJsonFile "/srv/bosun/includes/defaults.json" }}
{{ $defaults.timezone }}
```

### Available Template Functions

The template engine provides Go's standard template functions plus all [Sprig functions](https://masterminds.github.io/sprig/). Commonly used:

| Function | Description |
|----------|-------------|
| `env "VAR"` | Get environment variable |
| `include "path"` | Read an allowlisted file as text |
| `fromJsonFile "path"` | Read and parse an allowlisted JSON file |
| `fromJson "..."` | Parse JSON string |
| `toJson .` | Convert to JSON |
| `quote .` | Quote a string |
| `default "val" .` | Default value if empty |
| `upper .` | Uppercase string |
| `lower .` | Lowercase string |
| `trim .` | Trim whitespace |
| `b64enc .` | Base64 encode |
| `b64dec .` | Base64 decode |

### Template Processing

```
1. Find all .tmpl files in source directory
       |
       v
2. For each template:
   a. Read template content
   b. Parse template with Sprig functions
   c. Execute with secrets data
   d. Write output to staging directory
       |
       v
3. Copy non-template files as-is
```

### Environment Filtering

For security, only safe environment variables are exposed to templates:

**Allowed prefixes**: `PATH=`, `HOME=`, `USER=`, `LANG=`, `LC_`, `TERM=`, `XDG_`, `TMPDIR=`, `TMP=`, `TEMP=`

**Blocked patterns**:
- Prefixes: `SOPS_`, `AWS_`, `AZURE_`, `GCP_`, `GOOGLE_`, `CLOUDFLARE_`, etc.
- Suffixes: `_TOKEN`, `_SECRET`, `_KEY`, `_PASSWORD`, `_AUTH`, etc.
- Exact matches: `GITHUB_TOKEN`, `GITLAB_TOKEN`, `NPM_TOKEN`, etc.

## Deployment

The deploy subsystem (`internal/reconcile/deploy.go`) handles file synchronization to local or remote targets.

### Deployment Modes

**Local Mode**: Used when appdata is mounted locally (e.g., container with volume mount)

```bash
bosun reconcile --local
```

**Remote Mode**: Uses tar-over-SSH for file transfer

```bash
bosun reconcile --remote root@192.168.1.8
```

### Mode Detection

If `--local` is not specified, the system auto-detects:

1. If `--remote` or `DEPLOY_TARGET` is set, use remote mode
2. If `LocalAppdataPath` exists on filesystem, use local mode
3. Otherwise, attempt remote mode using `network.unraid_ip` from secrets

### Local Deployment

Uses native Go file operations to sync directories:

```
Staging                    Target
staging/unraid/appdata/ -> /mnt/appdata/
```

### Remote Deployment

Uses tar-over-SSH for efficient file transfer:

```
Staging                    Target
staging/unraid/appdata/ -> root@host:/mnt/user/appdata/
```

The remote deployment creates a tar archive locally, streams it over SSH, and extracts it into a temp directory on the remote host. This avoids requiring rsync on the remote system.

The staged tree is then promoted with a **retain-old rename-swap**: the live target directory is moved aside to `<target>.bosun-old.<timestamp>` (never deleted first), the new tree is moved into place, and the retained copy is removed only after the swap succeeds. If the move-in fails, the swap restores the retained copy. An interrupted deploy therefore always retains at least one complete tree — the old or the new. A crash between the move-aside and the move-in can leave the target *path* temporarily absent (the old tree stays intact at its `.bosun-old.<timestamp>` sibling) until the next deploy self-heals it by promoting the newest retained copy; orphaned copies are cleaned up at the same time. On Unraid's `/mnt/user` FUSE mount the swap is safe (retain-old) rather than kernel-atomic, and a settle delay follows the move so writes propagate before `docker compose up` reads them.

### Deployed Paths

| Source | Destination |
|--------|-------------|
| `staging/unraid/appdata/traefik/` | `appdata/traefik/` |
| `staging/unraid/appdata/authelia/configuration.yml` | `appdata/authelia/configuration.yml` |
| `staging/unraid/appdata/agentgateway/config.yaml` | `appdata/agentgateway/config.yaml` |
| `staging/unraid/appdata/gatus/config.yaml` | `appdata/gatus/config.yaml` |
| `staging/unraid/appdata/tailscale-gateway/serve.json` | `appdata/tailscale-gateway/serve.json` |
| `staging/unraid/compose/` | `appdata/compose/` |

### Service Reload

After deployment:

1. `docker compose up -d --remove-orphans --wait` on core.yml
2. `docker kill --signal=SIGHUP agentgateway` to reload config

### Timeouts

| Operation | Timeout |
|-----------|---------|
| SSH connect | 5 seconds |
| SSH commands | 30 seconds |
| File sync | 5 minutes |
| docker compose up | 10 minutes |

### Retry Logic

SSH and file sync operations retry on transient errors with exponential backoff:

- **Max retries**: 3
- **Backoff sequence**: 1s, 2s, 4s

**Retryable errors**:
- Connection refused/reset
- Connection timed out
- Network unreachable
- No route to host
- Host is down
- I/O timeout
- Temporary failure

## Locking

The reconciler uses file-based locking to prevent concurrent runs.

### Lock File

**Path**: `/tmp/reconcile.lock`

### Unix Implementation

Uses `flock(2)` system call with `LOCK_EX | LOCK_NB` for non-blocking exclusive lock.

```go
syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
```

### Windows Implementation

Uses `LockFileEx` with `LOCKFILE_EXCLUSIVE_LOCK | LOCKFILE_FAIL_IMMEDIATELY`.

### Behavior

- If lock is held by another process, the reconciliation is skipped gracefully
- Lock is released on normal completion, error, or process termination
- Stale locks (from crashed processes) are automatically cleaned up by the OS

## Backup System

Before deployment, the system creates timestamped backups of current configurations.

### Backup Format

```
backups/
  backup-20240115-143022/
    configs.tar.gz
```

### Backed Up Paths

- `appdata/traefik/`
- `appdata/authelia/configuration.yml`
- `appdata/agentgateway/config.yaml`
- `appdata/gatus/config.yaml`

### Remote Backup

For remote deployments, runs `tar -czf -` over SSH and streams to local backup directory.

### Backup Verification

After creation, backups are verified:

1. Archive file exists
2. Archive is non-empty
3. Archive is valid (can list contents with `tar -tzf`)
4. Archive contains at least one file

### Retention

By default, keeps the 5 most recent **valid** backups — those that pass the same
verification used to select a rollback anchor. Older backups are automatically deleted.
Corrupt or partial backup directories (a missing, unlistable, or truncated `configs.tar.gz`)
do not count toward the retention limit and are removed outright, so a broken backup can
never occupy a keep slot and evict an older good one. When a deploy has no managed files to
back up (a fresh host's first deploy), no backup is created and no rollback anchor is recorded.

```go
cfg.BackupsToKeep = 5  // Default
```

## Error Recovery

### Git Failures

- Clone failures clean up partial directories
- Timeout errors provide specific timeout duration
- Network errors include stderr output for debugging

### Secrets Failures

- Missing key files provide setup instructions
- Invalid SOPS files suggest encryption command
- Decryption errors include the secrets-file path and a sanitized category;
  integrity failures are distinct from key and malformed-data failures
- Raw SOPS errors are never returned or logged, including at debug level

### Template Failures

- stderr is sanitized to avoid leaking secrets (truncated to 500 chars)
- Template errors include file path
- Missing map keys stop rendering and identify the missing key; for optional
  keys, use `get . "key" | default "value"` so the lookup is explicit
- Missing directories are created automatically

### Deployment Failures

- SSH errors are parsed into actionable messages:
  - Permission denied: Check authorized_keys
  - Connection refused: Check SSH service
  - Host key verification: Run ssh-keyscan
  - No route to host: Check network/host status
  - Connection timeout: Check firewall rules
  - DNS failure: Check hostname

### Compose Failures

- `ComposeUpWithRollback()` can restore previous config on failure
- Failed compose operations log warnings but don't abort the entire reconciliation
- Container health is verified after compose up

### Partial Failures

Some operations log warnings but continue:

- Backup creation failure when an older verified rollback anchor exists
- tailscale-gateway sync failure (warns, continues)
- agentgateway reload failure (warns, continues)
- Staging cleanup failure when owner-only retention succeeds (warns)

## Security Considerations

### Secret Handling

1. **In-memory processing**: Secrets are decrypted and processed entirely in memory using the go-sops library
2. **No external processes**: Template rendering uses native Go text/template, avoiding secrets in environment variables
3. **Environment filtering**: Only safe env vars are exposed to templates
4. **Error sanitization**: Output is truncated to avoid leaking secrets in logs
5. **Memory**: Secrets are stored in Go maps, garbage collected after use

### SSH Security

1. **Host validation**: Rejects hosts with shell metacharacters (`;`, `&`, `|`, `$`, etc.)
2. **Option injection prevention**: Rejects hosts starting with `-`
3. **BatchMode**: Uses `-o BatchMode=yes` to prevent password prompts
4. **Connection timeout**: 5 second timeout prevents hanging

### Input Validation

All user inputs are validated before use:

| Input | Validation |
|-------|------------|
| SSH host | Regex pattern, no shell metacharacters, no `-` prefix |
| Git branch | Regex pattern, no shell metacharacters, no `-` prefix |
| Container name | Regex pattern, no shell metacharacters, alphanumeric start |
| Docker signal | Allowlist: SIGHUP, SIGTERM, SIGKILL, SIGUSR1, SIGUSR2 |

### Temp File Security

- The effective staging root is `0700` before decrypted output is written;
  payload temp files stay `0600` until atomic rename. Active descendants keep
  their established deploy modes so local copy and remote tar do not change
  destination permissions.
- A failed render or any later failed pipeline stage leaves one diagnostic slot
  at the target's effective `StagingDir`. Retained directories are `0700` and
  regular files are `0600`; symlinks and irregular files make Bosun delete the
  slot without following them.
- A subsequent render replaces that same slot rather than accumulating archives.
  Git sync, config reload, decryption, and deploy-mode failures before rendering
  leave the prior secured evidence unchanged.
- Dry runs retain the secured render. Verified real deployments remove staging
  only after health checks, hooks, and post-deploy verification. If removal
  fails, Bosun warns and retains a proven owner-only tree; inability to harden or
  delete it is a security error and prevents success from being recorded.
- Multi-target staging paths must be canonically disjoint. Bosun validates and
  preflights every target before executing any target, including when a one-shot
  run selects only one target.
- Backup archives use timestamped directories to prevent conflicts

### Container Security

When running as a container:

- Mount SSH keys as read-only volume
- Mount age keys as read-only volume or use environment variable
- Use non-root user if possible
- Limit network access to Git server and target hosts

### Dry-Run Mode

Use `--dry-run` to:

- Preview deploy changes without applying
- Skip service restarts
- Skip backup creation
- Verify configuration without risk

```bash
# Safe way to test configuration changes
bosun reconcile --dry-run
```

## Known Limitations

### Hardcoded Deploy Paths

The deploy step (`deployLocal`/`deployRemote`) syncs a fixed set of directories: traefik, authelia, agentgateway, gatus, tailscale-gateway, and compose. Adding a new service to the reconciliation pipeline currently requires a code change to Bosun itself.

The manifest/provision system is generic, but the reconciler's deploy step is not — it targets specific paths. A future version will make deploy paths data-driven via a deploy manifest. See [bosun-ciy](https://github.com/cameronsjo/bosun/issues?q=bosun-ciy).

### Compose Up Blast Radius

`docker compose up -d --remove-orphans --wait` applies to the entire compose file. If one service in `core.yml` has a broken image tag, the entire compose operation fails. Rollback restores previous configs and re-runs compose up, but there is no per-service granularity.

For homelabs with 10-20 containers in one compose file, a bad image tag on one service affects all services. See [bosun-22q](https://github.com/cameronsjo/bosun/issues?q=bosun-22q).

### Remote Deploy Partial Failure

Remote deployments use tar-over-SSH for config sync followed by `docker compose up` over SSH. If the SSH connection succeeds for config sync but fails during compose up, the configs on disk are ahead of the running containers. The state file will **not** record this as a successful deploy (correct behavior), but a manual intervention or retry may be needed. See [bosun-4t4](https://github.com/cameronsjo/bosun/issues?q=bosun-4t4).

### Full Sync on Every Reconcile

The reconciler syncs all deploy paths on every run — it does not diff "what changed in this commit" to selectively sync affected services. For homelab scale this is fast enough, and Docker Compose won't restart unchanged containers (the `--wait` flag handles this). This is a deliberate simplicity tradeoff, not a bug.

### Backup Scope

Backups capture configuration files only (traefik routes, authelia config, gatus endpoints, compose files). Volume data — databases, application state, media files — is **not** included. If a compose up triggers a database migration (e.g., new PostgreSQL version), the rollback restores old configs but cannot restore the database to its previous state.

This is inherent to the Docker Compose deployment model. Volume backup/restore is the responsibility of a dedicated backup tool (e.g., Duplicati, Restic, Borg).

### Template File-Read Functions

The `include` and `fromJsonFile` functions apply a subtree allowlist before
reading. The default root is `<infraDir>/templates`; `template_include_dir` or
`BOSUN_TEMPLATE_INCLUDE_DIR` can select another root. Lexical traversal and
paths whose resolved symlink target leaves that root are rejected with an error
that names the allowed directory.

Validation and reading are separate filesystem operations, not an atomic or
file-descriptor-anchored transaction. The control assumes an adversarial local
process is not concurrently replacing paths inside the trusted include tree
during rendering; templates from untrusted sources remain unsupported.

### Template Rendering Scope

Template rendering walks the entire source directory for `.tmpl` files, while non-template file copying is limited to the infrastructure subdirectory. A `.tmpl` file placed outside the infra subdirectory will still be rendered. In practice this is harmless (you control the repo), but it expands the rendering surface beyond the intended scope.

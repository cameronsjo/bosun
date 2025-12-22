# GitOps Reconciliation System

This document describes the bosun GitOps reconciliation system, which automates infrastructure deployment by syncing configuration from a Git repository to target systems.

## Overview

The reconcile system implements a GitOps workflow that:

1. Monitors a Git repository for changes
2. Decrypts secrets using SOPS with age encryption
3. Renders templates using chezmoi's template engine
4. Deploys configuration files to local or remote targets
5. Restarts affected services

### When to Use

- **Continuous deployment**: Run in a container on a schedule (cron or interval) to automatically deploy config changes
- **Manual deployment**: Run `bosun reconcile` to apply the latest configuration immediately
- **Testing changes**: Use `--dry-run` to preview what would change before applying

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
                    | GitOps      - git clone/pull
                    | SOPSOps     - secrets decryption
                    | TemplateOps - chezmoi rendering
                    | DeployOps   - rsync/SSH sync
                    +--------------------+
```

### Data Flow

```
1. Lock Acquisition
       |
       v
2. Git Sync (clone or pull)
       |
       v
3. Change Detection (before/after commit comparison)
       |
       v (if changes detected or --force)
4. SOPS Decryption
       |
       v
5. Template Rendering (chezmoi execute-template)
       |
       v
6. Backup Creation (tar.gz of current configs)
       |
       v
7. Deployment (rsync to local or remote)
       |
       v
8. Service Reload (docker compose up, SIGHUP)
       |
       v
9. Cleanup & Lock Release
```

## Configuration

### Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `REPO_URL` | Yes | - | Git repository URL |
| `REPO_BRANCH` | No | `main` | Branch to track |
| `REPO_DIR` | No | `/app/repo` | Local clone directory |
| `STAGING_DIR` | No | `/app/staging` | Rendered templates directory |
| `BACKUP_DIR` | No | `/app/backups` | Configuration backups |
| `LOG_DIR` | No | `/app/logs` | Log files directory |
| `LOCAL_APPDATA` | No | `/mnt/appdata` | Local appdata path |
| `REMOTE_APPDATA` | No | `/mnt/user/appdata` | Remote appdata path |
| `DEPLOY_TARGET` | No | - | SSH target (e.g., `root@192.168.1.8`) |
| `SECRETS_FILES` | No | - | Comma-separated SOPS files |
| `DRY_RUN` | No | `false` | Preview mode |
| `FORCE` | No | `false` | Deploy even without changes |

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

The git subsystem (`internal/reconcile/git.go`) handles repository synchronization with these features:

### Clone Behavior

- **Shallow clone**: Uses `--depth 1` by default to minimize bandwidth
- **Single branch**: Only fetches the configured branch with `--single-branch`
- **Cleanup on failure**: Removes partial clones if the operation fails

### Pull Behavior

- **Shallow fetch**: Uses `--depth 1` for minimal data transfer
- **Hard reset**: Resets to `origin/<branch>` to ensure clean state
- **Change detection**: Compares commit hashes before/after to detect changes

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

The SOPS subsystem (`internal/reconcile/sops.go`) handles encrypted secrets using [SOPS](https://github.com/getsops/sops) with [age](https://github.com/FiloSottile/age) encryption.

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
3. Contains `sops` metadata key

### Decryption Flow

```
1. Validate SOPS file structure
       |
       v
2. Check age key availability
       |
       v
3. Run: sops --input-type yaml --output-type json -d <file>
       |
       v
4. Parse JSON to map[string]any
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

The template subsystem (`internal/reconcile/template.go`) uses [chezmoi](https://www.chezmoi.io/) for template rendering.

### Template File Convention

- Files ending in `.tmpl` are processed as templates
- The `.tmpl` extension is removed in the output
- Non-template files are copied as-is

### Accessing Secrets in Templates

Secrets are passed via a temporary JSON file. Access them using:

```go-template
{{ $secrets := fromJson (include (env "BOSUN_SECRETS_FILE")) }}
{{ $secrets.network.unraid_ip }}
```

### Available Template Functions

Chezmoi provides the full Go template library plus additional functions. Commonly used:

| Function | Description |
|----------|-------------|
| `env "VAR"` | Get environment variable |
| `include "path"` | Read file contents |
| `fromJson "..."` | Parse JSON string |
| `toJson .` | Convert to JSON |
| `quote .` | Quote a string |

### Template Processing

```
1. Find all .tmpl files in source directory
       |
       v
2. For each template:
   a. Read template content
   b. Write secrets to temp file (0600 permissions)
   c. Run: chezmoi execute-template
   d. Write output to staging directory
   e. Clean up temp file
       |
       v
3. Copy non-template files as-is
```

### Environment Filtering

For security, only safe environment variables are passed to chezmoi:

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

**Remote Mode**: Uses rsync over SSH

```bash
bosun reconcile --remote root@192.168.1.8
```

### Mode Detection

If `--local` is not specified, the system auto-detects:

1. If `--remote` or `DEPLOY_TARGET` is set, use remote mode
2. If `LocalAppdataPath` exists on filesystem, use local mode
3. Otherwise, attempt remote mode using `network.unraid_ip` from secrets

### Local Deployment

Uses `rsync -av --delete` to sync directories:

```
Staging                    Target
staging/unraid/appdata/ -> /mnt/appdata/
```

### Remote Deployment

Uses `rsync -avz --delete` over SSH:

```
Staging                    Target
staging/unraid/appdata/ -> root@host:/mnt/user/appdata/
```

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
| rsync | 5 minutes |
| docker compose up | 10 minutes |

### Retry Logic

SSH and rsync operations retry on transient errors with exponential backoff:

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

By default, keeps the 5 most recent backups. Older backups are automatically deleted.

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
- Decryption errors include file path and sops stderr

### Template Failures

- stderr is sanitized to avoid leaking secrets (truncated to 500 chars)
- Template errors include file path
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

- Backup creation failure (warns, continues with deployment)
- tailscale-gateway sync failure (warns, continues)
- agentgateway reload failure (warns, continues)
- Staging cleanup failure (warns)

## Security Considerations

### Secret Handling

1. **Temporary files**: Secrets are written to temp files with `0600` permissions, deleted immediately after use
2. **Environment filtering**: Only safe env vars are passed to subprocess (chezmoi)
3. **Error sanitization**: stderr output is truncated to avoid leaking secrets in logs
4. **Memory**: Secrets are stored in Go maps, garbage collected after use

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

- Staging directory is cleared before each run
- Staging directory is cleaned up after successful deployment
- Backup archives use timestamped directories to prevent conflicts

### Container Security

When running as a container:

- Mount SSH keys as read-only volume
- Mount age keys as read-only volume or use environment variable
- Use non-root user if possible
- Limit network access to Git server and target hosts

### Dry-Run Mode

Use `--dry-run` to:

- Preview rsync changes without applying
- Skip service restarts
- Skip backup creation
- Verify configuration without risk

```bash
# Safe way to test configuration changes
bosun reconcile --dry-run
```

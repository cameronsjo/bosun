# Bosun Workflow Documentation

This document describes the happy and unhappy paths for all major operations in the bosun codebase, including error handling and recovery procedures.

## Table of Contents

- [1. Provision Workflow](#1-provision-workflow)
- [2. Yacht Up/Down/Restart Workflow](#2-yacht-updownrestart-workflow)
- [3. Reconcile/GitOps Workflow](#3-reconcilegitops-workflow)
- [4. Snapshot/Rollback Workflow](#4-snapshotrollback-workflow)
- [5. Doctor/Diagnostics Workflow](#5-doctordiagnostics-workflow)

---

## 1. Provision Workflow

The provision workflow renders manifest files into Docker Compose, Traefik, and Gatus configurations.

### Entry Points

- `bosun provision [stack]` - Render a stack or service manifest
- `bosun provisions` - List available provisions
- `bosun create <template> <name>` - Create a new service from template

### Flowchart

```mermaid
flowchart TD
    Start[bosun provision stack] --> LoadConfig{Load Config}
    LoadConfig -->|Failure| ConfigErr[Error: load config failed]
    LoadConfig -->|Success| CheckValues{Values Overlay?}

    CheckValues -->|Yes| LoadValues[Load Values File]
    CheckValues -->|No| CheckPath{Check Stack/Service Path}
    LoadValues --> CheckPath

    CheckPath -->|Stack exists| RenderStack[Render Stack]
    CheckPath -->|Service exists| RenderService[Render Service]
    CheckPath -->|Neither| PathErr[Error: not found]

    RenderStack --> RenderCheck{Render Success?}
    RenderService --> RenderCheck

    RenderCheck -->|Failure| RenderErr[Error: render failed]
    RenderCheck -->|Success| DryRun{Dry Run?}

    DryRun -->|Yes| PrintYAML[Print YAML Output]
    DryRun -->|No| DiffMode{Diff Mode?}

    DiffMode -->|Yes| ShowDiff[Show Diff]
    DiffMode -->|No| AcquireLock[Acquire Provision Lock]

    AcquireLock --> WriteFiles[Write Output Files]
    WriteFiles --> Done[Done]
    PrintYAML --> Done
    ShowDiff --> Done
```

### Step-by-Step Description

| Step | Operation | Error Handling |
|------|-----------|----------------|
| 1 | Load configuration from `config.yaml` or defaults | Returns error if config cannot be loaded |
| 2 | Load values overlay file if `-f` flag provided | Returns error if file cannot be read or parsed |
| 3 | Check if argument is a stack or service | Returns "stack or service not found" if neither exists |
| 4 | Render stack (loads included services) or single service | Returns error with specific cause (missing provision, YAML parse error) |
| 5 | For each service, load provisions and interpolate variables | Returns error if provision not found or interpolation fails |
| 6 | Merge compose/traefik/gatus outputs | Deep merge with later values taking precedence |
| 7 | Dry run: print YAML to stdout | No file writes |
| 8 | Diff mode: compare against existing files | Shows new vs existing files |
| 9 | Acquire provision lock (prevents concurrent writes) | Returns error if lock held by another process |
| 10 | Write output files to `output/compose/`, `output/traefik/`, `output/gatus/` | Returns error on write failure, lock released on exit |

### Error Scenarios

| Error | Cause | Recovery |
|-------|-------|----------|
| `load config: ...` | Missing or invalid `config.yaml` | Create `config.yaml` or run from project root |
| `load values: ...` | Values overlay file not found or invalid YAML | Check file path and YAML syntax |
| `stack or service not found: X` | No `stacks/X.yml` or `services/X.yml` | Verify stack/service name, run `bosun provisions` |
| `load provision X: ...` | Provision template not found | Create provision in `provisions/X.yml` |
| `render stack: ...` | Service in stack failed to render | Check individual service file for errors |
| `acquire provision lock: ...` | Concurrent provision in progress | Wait for other provision to complete |
| `write outputs: ...` | Permission denied or disk full | Check directory permissions, disk space |

---

## 2. Yacht Up/Down/Restart Workflow

The yacht commands manage Docker Compose services with nautical theming.

### Entry Points

- `bosun yacht up [services...]` - Start services
- `bosun yacht down` - Stop and remove services
- `bosun yacht restart [services...]` - Restart services
- `bosun yacht status` - Show service status

### Flowchart

```mermaid
flowchart TD
    Start[bosun yacht up] --> LoadConfig{Load Config}
    LoadConfig -->|Failure| ConfigErr[Error: load config failed]
    LoadConfig -->|Success| ValidateCompose{Validate Compose File}

    ValidateCompose -->|Invalid| ComposeErr[Error: invalid compose file]
    ValidateCompose -->|Valid| CheckServices{Services Specified?}

    CheckServices -->|Yes| ValidateNames{Validate Service Names}
    CheckServices -->|No| ConnectDocker[Connect to Docker]
    ValidateNames -->|Valid| ConnectDocker

    ConnectDocker --> CheckTraefik{Check Traefik Status}

    CheckTraefik -->|Running| TraefikOK[Traefik OK]
    CheckTraefik -->|Stopped| StartTraefik[Start Traefik]
    CheckTraefik -->|Not found| TraefikWarn[Warning: not deployed]

    StartTraefik --> TraefikOK

    TraefikOK --> ComposeUp[docker compose up -d]
    TraefikWarn --> ComposeUp

    ComposeUp --> Success[Yacht underway]
```

### Step-by-Step Description

| Step | Operation | Error Handling |
|------|-----------|----------------|
| 1 | Load configuration | Returns error with config path |
| 2 | Check compose file exists | Returns "compose file not found: path" |
| 3 | Run `docker compose config --quiet` to validate syntax | Returns "invalid compose file" with parser output |
| 4 | Parse compose YAML to extract service names | Returns error if YAML invalid |
| 5 | Validate provided service names exist | Returns "unknown services: X. Valid services: Y" |
| 6 | Connect to Docker daemon | Warning only, continues if fails |
| 7 | Check if Traefik container is running | Auto-starts if exists but stopped |
| 8 | Execute `docker compose up -d [services]` | Returns full compose error output |

### Error Scenarios

| Error | Cause | Recovery |
|-------|-------|----------|
| `compose file not found: X` | Compose file doesn't exist | Run `bosun provision` first |
| `invalid compose file: ...` | YAML syntax or schema error | Run `docker compose config` to debug |
| `unknown services: X` | Service name not in compose | Check available services in compose file |
| `Could not connect to Docker` | Docker daemon not running | Start Docker daemon |
| `traefik not found` | Traefik not deployed | Deploy Traefik before application services |
| `compose up: ...` | Container failed to start | Check `docker logs <container>` |

---

## 3. Reconcile/GitOps Workflow

The reconcile workflow implements GitOps: sync from git, decrypt secrets, render templates, deploy, and restart services.

### Entry Points

- `bosun reconcile` - Run full reconciliation
- Called automatically via webhook on git push

### Flowchart

```mermaid
flowchart TD
    Start[bosun reconcile] --> AcquireLock{Acquire Lock}
    AcquireLock -->|Already held| LockSkip[Skip: in progress]
    AcquireLock -->|Acquired| GitSync[Sync Repository]

    GitSync --> ChangesCheck{Changes Detected?}
    ChangesCheck -->|No changes| NoChangeSkip[Skip: no changes]
    ChangesCheck -->|Changes or --force| Decrypt[Decrypt Secrets]

    Decrypt --> RenderTemplates[Render Templates]
    RenderTemplates --> DryRunCheck{Dry Run?}

    DryRunCheck -->|Yes| DryRunSkip[Skip: dry run]
    DryRunCheck -->|No| CreateBackup[Create Backup]

    CreateBackup --> DeployMode{Local or Remote?}

    DeployMode -->|Local mount| DeployLocal[Deploy via file copy]
    DeployMode -->|SSH required| DeployRemote[Deploy via tar-over-SSH]

    DeployLocal --> ComposeUp[Compose Up + Signal Reload]
    DeployRemote --> ComposeUp

    ComposeUp --> Cleanup[Cleanup Staging]
    Cleanup --> ReleaseLock[Release Lock]
    ReleaseLock --> Success[Reconciliation complete]
```

### Step-by-Step Description

| Step | Operation | Error Handling |
|------|-----------|----------------|
| 1 | Acquire exclusive file lock (`/var/run/bosun/reconcile.lock`) | Skips if lock held (non-blocking `LOCK_NB`) |
| 2 | Preflight Age identity when secrets files are configured, then sync git repository (clone or pull) | Identity paths must be regular, non-empty, and parseable before Git; clone timeout: 5min, fetch timeout: 2min |
| 3 | Compare before/after commit hashes | Skip if no changes unless `--force` |
| 4 | Validate SOPS files exist and have `sops` metadata key | Returns specific error with fix instructions |
| 5 | Reuse the preflighted Age identity (`SOPS_AGE_KEY`, `SOPS_AGE_KEY_FILE`, or default path) | Returns setup and bind-mount guidance if runtime configuration changed |
| 6 | Decrypt SOPS files to JSON map | Returns error with file path |
| 7 | Clear staging directory | Removes previous staging state |
| 8 | Copy non-template files to staging | Preserves directory structure |
| 9 | Execute Go templates with secrets | In-memory template rendering with Sprig functions |
| 10 | Create backup of current configs | Backup verification: check tar.gz is valid |
| 11 | Deploy via native file copy (local) or tar-over-SSH (remote) | SSH retry with exponential backoff (1s, 2s, 4s) |
| 12 | Run `docker compose up -d --remove-orphans --wait` | Warning if fails (non-fatal) |
| 13 | Signal containers (SIGHUP for hot reload) | Warning if fails (non-fatal) |
| 14 | Cleanup staging directory | Warning if fails (non-fatal) |
| 15 | Release file lock | Always executed via defer |

### Error Scenarios

| Error | Cause | Recovery |
|-------|-------|----------|
| `git clone failed` | Auth failure, network, invalid repo | Check SSH keys, network, repo URL |
| `git clone timed out after 5m` | Slow network or large repo | Retry, or check network |
| `git fetch timed out after 2m` | Network issues | Retry on next trigger |
| `age key not found` | No valid SOPS decryption identity, or an invalid file mount | Run `age-keygen -o ~/.config/sops/age/keys.txt`; pre-create Docker file-bind sources |
| `file is not SOPS-encrypted` | Missing `sops` metadata | Encrypt file: `sops --encrypt --in-place file.yaml` |
| `sops integrity verification failed` | Encrypted file or MAC was modified | Restore the file from a trusted source or re-encrypt it; do not rotate an unrelated key |
| `sops decryption key unavailable` | No configured identity can recover the data key | Verify `SOPS_AGE_KEY` or `SOPS_AGE_KEY_FILE` matches a file recipient |
| `malformed SOPS encrypted data` | Encrypted value or metadata is invalid | Validate or re-encrypt the file with SOPS |
| `sops decryption failed` | Failure could not be safely classified | Validate the file with SOPS and verify the configured Age key |
| `template render failed` | Template syntax error | Fix template, check error output |
| `SSH authentication failed` | Invalid SSH key | Add key to remote authorized_keys |
| `SSH connection refused` | SSH service not running | Start SSH service on remote |
| `SSH host key verification failed` | Unknown host | Run `ssh-keyscan host >> ~/.ssh/known_hosts` |
| `file sync timed out after 5m` | Large files or slow network | Retry, reduce file sizes |
| `docker compose up failed` | Container startup error | Check container logs, compose file |

### SSH Retry Logic

Remote operations use exponential backoff for transient errors:

```
Transient errors (will retry):
- connection refused
- connection reset
- connection timed out
- network is unreachable
- no route to host
- host is down
- operation timed out
- i/o timeout
- temporary failure

Backoff sequence: 1s -> 2s -> 4s (max 3 retries)
```

---

## 4. Snapshot/Rollback Workflow

The snapshot workflow provides point-in-time backups of manifest outputs for quick recovery.

### Entry Points

- Automatic: Snapshot created before each `bosun provision`
- `bosun mayday --list` - List available snapshots
- `bosun mayday --rollback [name]` - Restore a snapshot
- `bosun restore --list` - List reconcile backups
- `bosun restore <backup-name>` - Restore a reconcile backup

### Flowchart (Snapshot/Rollback)

```mermaid
flowchart TD
    Start[bosun mayday --rollback] --> LoadConfig{Load Config}
    LoadConfig -->|Failure| ConfigErr[Error: project root not found]
    LoadConfig -->|Success| ListSnapshots[List Available Snapshots]

    ListSnapshots -->|None| NoSnapshots[Error: no snapshots available]
    ListSnapshots -->|Has snapshots| SelectTarget{Target Specified?}

    SelectTarget -->|Interactive| PromptUser[Prompt User]
    SelectTarget -->|Name given| CheckExists{Snapshot Exists?}
    PromptUser --> CheckExists

    CheckExists -->|Not found| NotFound[Error: snapshot not found]
    CheckExists -->|Found| CheckDisk[Check Disk Space]

    CheckDisk --> PreBackup{Output dir has content?}
    PreBackup -->|Yes| CreateBackup[Create Pre-Rollback Backup]
    PreBackup -->|No| AtomicSwap[Atomic Swap]
    CreateBackup --> AtomicSwap

    AtomicSwap --> Success[Rollback complete]
    Success --> Note[Run yacht up to apply]
```

### Flowchart (Reconcile Restore)

```mermaid
flowchart TD
    Start[bosun restore backup-name] --> FindBackup{Backup Exists?}

    FindBackup -->|Not found| BackupErr[Error: backup not found]
    FindBackup -->|Found| CheckArchive{configs.tar.gz Exists?}

    CheckArchive -->|Missing| ArchiveErr[Error: backup incomplete]
    CheckArchive -->|Found| ExtractStaging[Extract to Staging]

    ExtractStaging --> DeployFiles[Deploy via tar-over-SSH]
    DeployFiles --> CheckCompose{Compose File Exists?}

    CheckCompose -->|Yes| ComposeUp[docker compose up]
    CheckCompose -->|No| SkipRestart[Skip restart]

    ComposeUp --> Done[Restore complete]
    SkipRestart --> Done
```

### Step-by-Step Description

| Step | Operation | Error Handling |
|------|-----------|----------------|
| 1 | List snapshots in `.bosun/snapshots/` directory | Sorted by date, newest first |
| 2 | Verify target snapshot exists | Returns "snapshot not found: name" |
| 3 | Calculate snapshot size for disk check | Returns error if unable to stat files |
| 4 | Check available disk space (snapshot size + 100MB buffer) | Returns "insufficient disk space" |
| 5 | Create pre-rollback backup of current state | Stored as `pre-rollback-YYYYMMDD-HHMMSS` |
| 6 | Copy snapshot to temp directory | Atomic restore pattern |
| 7 | Rename current output to `.restore-old-*` | Preserves original on failure |
| 8 | Rename temp to output (atomic swap) | Restores old on failure |
| 9 | Remove old directory | Cleanup after success |
| 10 | Display restored files | User notification |

### Error Scenarios

| Error | Cause | Recovery |
|-------|-------|----------|
| `Project root not found` | Not in bosun project | cd to project directory |
| `No snapshots available` | Never provisioned or snapshots deleted | Run `bosun provision` first |
| `Snapshot not found: X` | Invalid snapshot name | Run `bosun mayday --list` |
| `insufficient disk space` | Disk full | Free disk space or delete old snapshots |
| `create pre-rollback backup` | Permission or disk space | Check permissions, free space |
| `rename current output` | Permission denied | Check directory ownership |
| `backup archive is corrupted` | Tar file damaged | Use different backup |
| `invalid file path in archive` | Path traversal attempt | Backup may be compromised |

### Snapshot Retention

- Maximum 20 snapshots retained
- Oldest snapshots automatically deleted when limit exceeded
- Minimum 100MB free disk space required for new snapshots

---

## 5. Doctor/Diagnostics Workflow

The doctor workflow runs pre-flight checks to verify the environment is properly configured.

### Entry Points

- `bosun doctor` - Run all diagnostic checks
- `bosun status` - Show running services dashboard
- `bosun drift` - Detect configuration drift
- `bosun lint [target]` - Validate manifests

### Flowchart (Doctor)

```mermaid
flowchart TD
    Start[bosun doctor] --> Required[Required Checks]

    Required --> CheckDocker[Docker Running?]
    CheckDocker --> CheckCompose[Docker Compose v2?]
    CheckCompose --> CheckGit[Git Installed?]

    CheckGit --> Optional[Optional Checks]

    Optional --> CheckRoot[Project Root?]
    CheckRoot --> CheckAgeKey[Age Key?]
    CheckAgeKey --> CheckSOPS[SOPS?]
    CheckSOPS --> CheckManifest[Manifest Dir?]
    CheckManifest --> CheckWebhook[Webhook?]

    CheckWebhook --> Summary{Results}
    Summary -->|Required failed| NotSeaworthy[Ship not seaworthy!]
    Summary -->|Warnings only| CanSail[Ship can sail, check warnings]
    Summary -->|All clear| ReadyToSail[All systems go!]
```

### Check Categories

| Check | Type | Fix Instructions |
|-------|------|------------------|
| Docker Running | REQUIRED | `systemctl start docker` or start Docker Desktop |
| Docker Compose v2 | REQUIRED | Install Docker Desktop or compose plugin |
| Git Installed | REQUIRED | Install via package manager |
| Project Root | WARNING | Run from project directory with config.yaml |
| Age Key | WARNING | `age-keygen -o ~/.config/sops/age/keys.txt` |
| SOPS | WARNING | Install via package manager |
| uv | WARNING | `curl -LsSf https://astral.sh/uv/install.sh \| sh` |
| Manifest Directory | WARNING | Create `manifest/` directory |
| Webhook Endpoint | WARNING | Start bosun container |

### Exit Codes

| Code | Meaning |
|------|---------|
| 0 | All required checks passed (warnings allowed) |
| 1 | One or more required checks failed |

### Lint Checks

The `bosun lint` command validates manifests:

| Check | Description |
|-------|-------------|
| Provisions exist | Verifies `provisions/` directory has templates |
| Service files valid | YAML parseable, has `name:` and `provisions:` |
| Stack files valid | YAML parseable, has `include:` |
| Dependencies | Services with `-db` suffix should be in `depends_on` |
| Network config | Traefik-enabled services should be on `proxynet` |
| Port conflicts | No duplicate host port bindings |
| Dependency cycles | No circular `depends_on` references |

---

## Recovery Procedures Summary

### Quick Reference

| Scenario | Command | Notes |
|----------|---------|-------|
| Manifest render broken | `bosun mayday --rollback` | Restore previous snapshot |
| GitOps deploy broken | `bosun restore <backup>` | Restore from reconcile backup |
| Container won't start | `docker logs <container>` | Check container logs |
| Check environment | `bosun doctor` | Verify all dependencies |
| See what's running | `bosun status` | Dashboard of services |
| Detect drift | `bosun drift` | Git vs running state |
| Validate before deploy | `bosun lint` | Check manifests |
| Emergency container remove | `bosun overboard <name>` | Force remove container |

### Recovery Decision Tree

```mermaid
flowchart TD
    Broken[Something is broken] --> Triage{What's broken?}

    Triage -->|Manifest outputs| Rollback[bosun mayday --rollback]
    Triage -->|Live configs| Restore[bosun restore backup-name]
    Triage -->|Single container| Logs[docker logs container]
    Triage -->|Environment| Doctor[bosun doctor]
    Triage -->|Unknown| Mayday[bosun mayday]

    Rollback --> StillBroken{Still broken?}
    Restore --> StillBroken

    StillBroken -->|Yes| OlderBackup[Check older backup/snapshot]
    StillBroken -->|No| YachtUp[Run bosun yacht up]

    Logs --> ConfigIssue{Config issue?}
    ConfigIssue -->|Yes| FixConfig[Fix config, bosun provision]
    ConfigIssue -->|No| AppError[Check container logs for app error]

    Mayday --> ReviewErrors[Review recent errors]
    ReviewErrors --> Triage
```

---

## Timeout Constants

| Operation | Timeout | Source |
|-----------|---------|--------|
| Git clone | 5 minutes | `GitCloneTimeout` |
| Git fetch | 2 minutes | `GitFetchTimeout` |
| Git local ops | 30 seconds | `GitLocalTimeout` |
| SSH connect | 5 seconds | `SSHConnectTimeout` |
| SSH operations | 30 seconds | `SSHTimeout` |
| File sync | 5 minutes | `FileSyncTimeout` |
| Docker compose up | 10 minutes (configurable via `BOSUN_COMPOSE_UP_TIMEOUT`) | `DefaultComposeUpTimeout` |
| Backup creation/verification and post-success retention verification/cleanup (separate deadlines) | 5 minutes each (configurable via `BOSUN_BACKUP_TIMEOUT`) | `DefaultBackupTimeout` |
| Post-deploy health check | 60 seconds (configurable via `BOSUN_HEALTH_CHECK_TIMEOUT`) | `HealthCheckTimeout` |
| Health check poll interval | 5 seconds (configurable via `BOSUN_HEALTH_CHECK_INTERVAL`) | `HealthCheckInterval` |
| Doctor checks | 10 seconds | `doctorCheckTimeout` |
| HTTP client | 5 seconds | `httpClientTimeout` |
| Docker ping | 5 seconds | `dockerPingTimeout` |

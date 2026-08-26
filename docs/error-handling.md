# Error Handling in Bosun

This document describes the error handling patterns, sentinel errors, and recovery strategies used throughout the bosun codebase.

## Error Philosophy

Bosun follows a **fail-fast with actionable messages** approach:

1. **Fail Fast**: Detect errors early and return immediately rather than attempting to continue with corrupted or partial state
2. **Actionable Messages**: Every error message should answer three questions:
   - What happened?
   - Why did it happen?
   - How do I fix it?
3. **No Silent Failures**: Errors are always surfaced, never swallowed
4. **Wrapping with Context**: Use `fmt.Errorf("context: %w", err)` to preserve the error chain while adding context

### Error Wrapping Pattern

```go
// Good: adds context while preserving original error
if err := doSomething(); err != nil {
    return fmt.Errorf("failed to sync repository: %w", err)
}

// Good: actionable error with remediation
return fmt.Errorf("provision not found: %s", provisionPath)
```

## Sentinel Errors

Bosun defines sentinel errors for conditions that callers may want to handle specifically:

### `ErrAgeKeyNotFound`

**Location**: `internal/reconcile/sops.go`

```go
var ErrAgeKeyNotFound = errors.New("age key not found")
```

**Purpose**: Returned when no age key is available for SOPS decryption.

**When Returned**: When no direct `SOPS_AGE_KEY` is set, the `CheckAgeKey()`
function returns this error (wrapped with actionable guidance) when:
- `SOPS_AGE_KEY_FILE` is missing, not a regular file, empty, unreadable, or
  does not contain a parseable Age identity
- Default key location (`~/.config/sops/age/keys.txt`) does not exist
- The default key exists but fails the same file and identity validation

**Example Error with Remediation**:
```
age key not found

To fix:
  1. Generate key: age-keygen -o ~/.config/sops/age/keys.txt
  2. Or set SOPS_AGE_KEY_FILE=/path/to/key
  3. Or set SOPS_AGE_KEY environment variable with the key content
```

### `ErrNotSOPSFile`

**Location**: `internal/internal/reconcile/sops.go`

```go
var ErrNotSOPSFile = errors.New("file is not SOPS-encrypted")
```

**Purpose**: Returned when a file lacks complete, structurally valid SOPS metadata.

**When Returned**: `ValidateSOPSFile()` returns this when a YAML file has no `sops` metadata mapping, no MAC, no valid `lastmodified` timestamp, or no key recipient with an encrypted data key.

**Example Error with Remediation**:
```
file is not SOPS-encrypted: secrets.yml does not contain 'sops' metadata key. Encrypt it with: sops --encrypt --in-place secrets.yml
```

### Deploy-Sync Invariant Sentinels

**Location**: `internal/reconcile/drift.go`, `internal/reconcile/verify.go`

```go
var ErrComposeDirMissing            = errors.New("staging compose directory does not exist")
var ErrNoDeclaredServices           = errors.New("no declared services in staging compose directory")
var ErrDeployInvariantEmptyWrite    = errors.New("deploy invariant: destination regular file missing or content-different without a recorded regular-file write")
var ErrDeployInvariantStaleMtime    = errors.New("deploy invariant: destination path has stale mtime")
var ErrDeployInvariantMissingFile   = errors.New("deploy invariant: destination path missing")
var ErrDeployInvariantWrongType     = errors.New("deploy invariant: destination path has wrong type")
```

**Purpose**: Surface the silent-success failure mode where reconcile reports success but no files actually land on disk. `ErrComposeDirMissing` is always fatal (misconfigured staging path); `ErrNoDeclaredServices` is overridable via `BOSUN_ALLOW_EMPTY_DECLARED_STATE=true` for genuinely empty repos. The `ErrDeployInvariant*` set is overridable via `BOSUN_SKIP_DEPLOY_INVARIANT=true` for diagnostic deploys (logged at `Warn` with `override=true`).

When `ErrComposeDirMissing` fires, the reconciler scans the infra dir's sibling directories for one containing `compose/` and, if found, appends a `BOSUN_INFRA_DIR=<dir>` suggestion to the surfaced error (GH#214 — `BOSUN_INFRA_DIR="."` while infra was nested under `unraid/`). The hint is diagnostic only; the failure remains unconditional.

**When Returned**:
- `ErrComposeDirMissing` / `ErrNoDeclaredServices` — from `ExtractDeclaredState` between stages 5 and 7 of the pipeline.
- `ErrDeployInvariant*` — from the post-deploy invariant gate at stage 9, before `docker compose up` runs. Created directories and written files must exist with fresh mtimes; a recorded directory must remain a real directory rather than a symlink or another file type. When no regular file was written, every regular source file must already be present and byte-identical at the destination even if the change set contains newly created directories.

See `docs/troubleshooting.md` for operator-facing remediation steps and `docs/gitops.md` for the full pipeline diagram showing where these gates fire.

## Error Categories

### Configuration Errors

Errors related to missing or invalid configuration files.

| Error Pattern | Location | Example |
|---------------|----------|---------|
| Missing required env var | `cmd/reconcile.go` | `REPO_URL environment variable is required` |
| Project root not found | `config/config.go` | `project root not found (no bosun/ or manifest/ directory)` |
| Provision not found | `manifest/provision.go` | `provision not found: /path/to/provisions/webapp.yml` |
| Invalid YAML syntax | `reconcile/sops.go` | `invalid YAML syntax in secrets.yml: yaml: ...` |
| Missing variables | `manifest/interpolate.go` | `missing variables: ${domain}, ${port}` |

### Connection Errors

Errors related to Docker, SSH, and Git connectivity.

| Error Pattern | Location | Remediation Provided |
|---------------|----------|---------------------|
| Docker socket | `docker/client.go` | `create docker client: ...` |
| SSH auth failed | `reconcile/deploy.go` | Check SSH key in authorized_keys |
| SSH host key | `reconcile/deploy.go` | Run `ssh-keyscan` command |
| SSH connection refused | `reconcile/deploy.go` | Check SSH service/port |
| SSH no route to host | `reconcile/deploy.go` | Check network connectivity |
| Git clone timeout | `reconcile/git.go` | `clone repository timed out after 5m0s` |
| Git fetch failed | `reconcile/git.go` | Error from go-git library |

**SSH Error Parsing**: The `parseSSHError()` function in `deploy.go` converts generic SSH errors into actionable messages:

```go
switch {
case strings.Contains(stderrLower, "permission denied"):
    return fmt.Errorf("SSH authentication failed for %s: permission denied. Check that your SSH key is added to the remote host's authorized_keys", host)
case strings.Contains(stderrLower, "host key verification failed"):
    return fmt.Errorf("SSH host key verification failed for %s: run 'ssh-keyscan %s >> ~/.ssh/known_hosts' to add the host key", host, host)
// ... more cases
}
```

### Permission Errors

Errors related to file system and Docker permissions.

| Error Pattern | Location | Context |
|---------------|----------|---------|
| Lock file access | `lock/lock.go` | `open lock file: permission denied` |
| Deploy state directory | `cmd/reconcile.go` | `create state directory "/path": permission denied` |
| Staging directory | `reconcile/reconcile.go` | `failed to create staging directory` |
| Backup directory | `reconcile/deploy.go` | `failed to create backup directory` |
| Rendered template permissions | `reconcile/template.go` | Reports output create/chmod/rename failures; successful files use `0644` and may contain secrets |

### Validation Errors

Input validation errors that prevent command injection and ensure data integrity.

**Location**: `internal/internal/reconcile/validation.go`

| Validation | Pattern | Error Example |
|------------|---------|---------------|
| SSH host | `^([a-zA-Z0-9_-]+@)?[a-zA-Z0-9.-]+$` | `invalid host: cannot start with '-' (potential SSH option injection)` |
| Git branch | `^[a-zA-Z0-9_/.-]+$` | `invalid branch: contains shell metacharacter ";"` |
| Container name | `^[a-zA-Z0-9][a-zA-Z0-9_.-]*$` | `invalid container name: cannot start with '-'` |
| Docker signal | Allowlist | `invalid signal "SIGFOO": must be one of SIGHUP, SIGTERM, SIGKILL, SIGUSR1, SIGUSR2` |

**Shell Metacharacter Blocklist**:
```go
shellMetachars = []string{";", "&", "|", "$", "`", "(", ")", "{", "}", "<", ">", "\\", "\n", "\r", "'", "\""}
```

### Transient Errors

Network-related errors that may resolve on retry.

**Detection**: The `isTransientSSHError()` function in `deploy.go` identifies retryable errors:

```go
transientPatterns := []string{
    "connection refused",
    "connection reset",
    "connection timed out",
    "network is unreachable",
    "no route to host",
    "host is down",
    "operation timed out",
    "i/o timeout",
    "temporary failure",
}
```

## Error Messages

### Pattern for Good Error Messages

Every error should include:

1. **What happened**: The operation that failed
2. **Why it happened**: The underlying cause
3. **How to fix it**: Remediation steps (when possible)

**Example from SOPS key checking**:

```go
if err := validateAgeIdentityFile(keyFile); err != nil {
    return ageIdentityFileError("SOPS_AGE_KEY_FILE", keyFile, err)
}
```

### CLI Error Display

The `internal/ui` package provides consistent error formatting:

| Function | Icon | Color | Usage |
|----------|------|-------|-------|
| `ui.Error()` | X | Red | Non-fatal errors |
| `ui.Fatal()` | X | Red | Fatal errors (calls `os.Exit(1)`) |
| `ui.Warning()` | Triangle | Yellow | Warnings that don't stop execution |

## Retry Logic

### SSH Retry with Exponential Backoff

**Location**: `internal/internal/reconcile/deploy.go`

```go
const (
    DefaultMaxRetries = 3
    InitialBackoff    = 1 * time.Second
)
```

**Retry Sequence**: 1s -> 2s -> 4s (exponential backoff)

**What Retries**:
- `DeployRemote()` - tar-over-SSH file transfer
- `DeployRemoteFile()` - single file transfer over SSH
- `EnsureRemoteDir()` - mkdir over SSH
- `ComposeUpRemote()` - docker compose over SSH
- `SignalContainerRemote()` - docker kill over SSH
- `BackupRemote()` - tar backup over SSH

**What Does NOT Retry**:
- Non-transient errors (permission denied, invalid config)
- Context cancellation
- Local operations

**Implementation**:

```go
func retryWithBackoff(ctx context.Context, maxRetries int, operation func() error) error {
    var lastErr error
    backoff := InitialBackoff

    for attempt := 1; attempt <= maxRetries; attempt++ {
        lastErr = operation()
        if lastErr == nil {
            return nil
        }

        if ctx.Err() != nil {
            return ctx.Err()
        }

        // Only retry on transient errors
        if !isTransientSSHError(lastErr) {
            return lastErr
        }

        if attempt < maxRetries {
            select {
            case <-ctx.Done():
                return ctx.Err()
            case <-time.After(backoff):
                backoff *= 2
            }
        }
    }

    return fmt.Errorf("operation failed after %d attempts: %w", maxRetries, lastErr)
}
```

### Operation Timeouts

| Operation | Timeout | Constant |
|-----------|---------|----------|
| SSH connect check | 5s | `SSHConnectTimeout` |
| SSH commands | 30s | `SSHTimeout` |
| File sync transfers | 5m | `FileSyncTimeout` |
| Docker compose up | 10m (configurable via `BOSUN_COMPOSE_UP_TIMEOUT`) | `DefaultComposeUpTimeout` |
| Backup creation/verification and post-success retention verification/cleanup (separate deadlines) | 5m each (configurable via `BOSUN_BACKUP_TIMEOUT`) | `DefaultBackupTimeout` |
| Post-deploy health check | 60s (configurable via `BOSUN_HEALTH_CHECK_TIMEOUT`) | `HealthCheckTimeout` |
| Health check poll interval | 5s (configurable via `BOSUN_HEALTH_CHECK_INTERVAL`) | `HealthCheckInterval` |
| Restart breaker window | 10m (configurable via `BOSUN_RESTART_WINDOW`) | `RestartWindow` |
| Git clone | 5m | `GitCloneTimeout` |
| Git fetch | 2m | `GitFetchTimeout` |
| Git local ops | 30s | `GitLocalTimeout` |

## Error Recovery

### Common Failure Scenarios

#### SOPS Decryption Fails

**Symptoms**: `age key not found` or `file is not SOPS-encrypted`

**Recovery**:
1. Ensure the Age key at `~/.config/sops/age/keys.txt` is a regular, non-empty,
   parseable identity file
2. Or set `SOPS_AGE_KEY_FILE` to a valid key location; pre-create Docker file
   bind-mount sources so a missing source is not materialized as a directory
3. Verify the secrets file is encrypted: look for `sops:` key in YAML

#### SSH Connection Fails

**Symptoms**: `SSH authentication failed` or `connection refused`

**Recovery**:
1. Test SSH manually: `ssh user@host`
2. Add host key: `ssh-keyscan hostname >> ~/.ssh/known_hosts`
3. Check SSH key: `ssh-add -l`
4. Verify `BOSUN_SSH_KEY` or an existing conventional key candidate is a
   regular, non-empty, parseable private key file
5. Verify network connectivity: `ping hostname`

#### Docker Not Available

**Symptoms**: `create docker client: ...`

**Recovery**:
1. Check Docker is running: `docker info`
2. Check socket permissions: `ls -la /var/run/docker.sock`
3. Add user to docker group: `sudo usermod -aG docker $USER`

#### Lock Already Held

**Symptoms**: `another provision operation is already running`

**Recovery**:
1. Wait for the other operation to complete
2. If stale, remove lock file: `rm manifest/.bosun/locks/provision.lock`
3. Check PID in lock file to verify process is still running

#### Git Clone/Fetch Timeout

**Symptoms**: `clone repository timed out after 5m0s`

**Recovery**:
1. Check network connectivity
2. Verify repository URL is accessible
3. Check for firewall blocking git protocol
4. Check SSH key availability for private repositories

### Rollback Capabilities

**Reconcile Backups**: The reconcile command creates timestamped backups before deployment:
- Location: `/app/backups/backup-YYYYMMDD-HHMMSS/`
- Contents: `configs.tar.gz` with the current regular-file footprint of the
  rendered deploy targets at their appdata destinations

**Restore Command**: Use `bosun restore --list` to see backups, `bosun restore <name>` to restore.

**Compose Rollback**: `ComposeUpWithRollback()` attempts to restore previous config if compose up fails.

### Rollback Archive Failures

Rollback archives are extracted in-process into a fresh temporary root and are
usable only after the complete archive passes confinement, link-target,
corruption, whole-decompressed-stream size, I/O, and independent-context checks.
The size bound includes tar headers, padding, skipped bodies, and trailing data;
reading to true gzip EOF also validates the trailer checksum. Any failure removes
the partial root before Bosun copies or deletes live files or invokes compose
with a backup path.

For full-tree health-gate rollback, archive failure returns the existing
rollback-not-attempted outcome and wraps the archive error, so callers can use
`errors.Is` or `errors.As` to inspect causes such as context expiry or
`*os.PathError`. For per-file isolated compose rollback, Bosun logs the archive
cause but preserves the original compose failure in both the file result and the
aggregate error; the file remains not rolled back and cannot enter the orphan
pass.

Both local consumers derive an independently timed extraction context from the
background while preserving reconcile log metadata. Cancellation of the outer
failed-deployment context does not suppress rollback, but cancellation or expiry
of the independent extraction context stops extraction and cleans its temporary
tree.

## Logging vs Returning

### When to Log

- Informational progress: `ui.Info("Syncing repository...")`
- Warnings that don't stop execution: a backup creation failure after Bosun
  selects an older verified rollback anchor
- Success confirmations: `ui.Success("Deployment complete!")`

### When to Return

- Fatal errors that stop the operation
- Errors that the caller needs to handle
- Validation failures

### Pattern

```go
// A backup error continues only when the current footprint is complete and an
// older verified rollback anchor is available.
if err := r.createBackup(ctx, secrets, localDeploy); err != nil {
    ui.Warning("Backup failed: %v", err)
    if errors.Is(err, ErrBackupFootprintIncomplete) {
        return err
    }
    if policyErr := r.applyBackupFailurePolicy(ctx, err); policyErr != nil {
        return policyErr
    }
}

// Return fatal errors
if err := r.syncRepo(ctx); err != nil {
    return fmt.Errorf("failed to sync repository: %w", err)
}
```

### CLI Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Any error (via `ui.Fatal()` or returned error) |

## Security Considerations

### Secret Sanitization

The `sanitizeStderr()` function in `template.go` prevents secrets from leaking in error messages:

```go
func sanitizeStderr(stderr string) string {
    const maxLen = 500
    if len(stderr) > maxLen {
        stderr = stderr[:maxLen] + "... (truncated)"
    }
    return stderr
}
```

### Environment Variable Filtering

Sensitive environment variables are not passed to child processes:

```go
excludePrefixes := []string{
    "SOPS_", "AWS_", "AZURE_", "GCP_", "GOOGLE_",
    "API_KEY", "SECRET", "TOKEN", "PASSWORD", "CREDENTIAL",
}
excludeSuffixes := []string{
    "_TOKEN", "_SECRET", "_KEY", "_PASS", "_PASSWORD",
}
```

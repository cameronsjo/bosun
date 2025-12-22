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

**Location**: `/Users/cameron/Projects/unops/internal/reconcile/sops.go`

```go
var ErrAgeKeyNotFound = errors.New("age key not found")
```

**Purpose**: Returned when no age key is available for SOPS decryption.

**When Returned**: The `CheckAgeKey()` function returns this error (wrapped with actionable guidance) when:
- `SOPS_AGE_KEY` environment variable is not set
- `SOPS_AGE_KEY_FILE` points to a non-existent file
- Default key location (`~/.config/sops/age/keys.txt`) does not exist

**Example Error with Remediation**:
```
age key not found

To fix:
  1. Generate key: age-keygen -o ~/.config/sops/age/keys.txt
  2. Or set SOPS_AGE_KEY_FILE=/path/to/key
  3. Or set SOPS_AGE_KEY environment variable with the key content
```

### `ErrNotSOPSFile`

**Location**: `/Users/cameron/Projects/unops/internal/reconcile/sops.go`

```go
var ErrNotSOPSFile = errors.New("file is not SOPS-encrypted")
```

**Purpose**: Returned when a file lacks the required SOPS metadata.

**When Returned**: `ValidateSOPSFile()` returns this when a YAML file does not contain the `sops` metadata key.

**Example Error with Remediation**:
```
file is not SOPS-encrypted: secrets.yml does not contain 'sops' metadata key. Encrypt it with: sops --encrypt --in-place secrets.yml
```

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
| Staging directory | `reconcile/reconcile.go` | `failed to create staging directory` |
| Backup directory | `reconcile/deploy.go` | `failed to create backup directory` |
| Secrets file permissions | `reconcile/template.go` | Sets 0600 on temp secrets file |

### Validation Errors

Input validation errors that prevent command injection and ensure data integrity.

**Location**: `/Users/cameron/Projects/unops/internal/reconcile/validation.go`

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
return fmt.Errorf("%w: SOPS_AGE_KEY_FILE is set to %q but file does not exist.\n\nTo fix:\n  1. Create the key file at the specified path\n  2. Or set SOPS_AGE_KEY_FILE to an existing key file\n  3. Or run: age-keygen -o ~/.config/sops/age/keys.txt", ErrAgeKeyNotFound, keyFile)
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

**Location**: `/Users/cameron/Projects/unops/internal/reconcile/deploy.go`

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
| Docker compose up | 10m | `ComposeUpTimeout` |
| Git clone | 5m | `GitCloneTimeout` |
| Git fetch | 2m | `GitFetchTimeout` |
| Git local ops | 30s | `GitLocalTimeout` |

## Error Recovery

### Common Failure Scenarios

#### SOPS Decryption Fails

**Symptoms**: `age key not found` or `file is not SOPS-encrypted`

**Recovery**:
1. Ensure age key exists at `~/.config/sops/age/keys.txt`
2. Or set `SOPS_AGE_KEY_FILE` to your key location
3. Verify the secrets file is encrypted: look for `sops:` key in YAML

#### SSH Connection Fails

**Symptoms**: `SSH authentication failed` or `connection refused`

**Recovery**:
1. Test SSH manually: `ssh user@host`
2. Add host key: `ssh-keyscan hostname >> ~/.ssh/known_hosts`
3. Check SSH key: `ssh-add -l`
4. Verify network connectivity: `ping hostname`

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
- Contents: `configs.tar.gz` with Traefik, Authelia, Agentgateway, Gatus configs

**Restore Command**: Use `bosun restore --list` to see backups, `bosun restore <name>` to restore.

**Compose Rollback**: `ComposeUpWithRollback()` attempts to restore previous config if compose up fails.

## Logging vs Returning

### When to Log

- Informational progress: `ui.Info("Syncing repository...")`
- Warnings that don't stop execution: `ui.Warning("Backup partially failed: %v", err)`
- Success confirmations: `ui.Success("Deployment complete!")`

### When to Return

- Fatal errors that stop the operation
- Errors that the caller needs to handle
- Validation failures

### Pattern

```go
// Log warnings but continue
if err := r.createBackup(ctx, secrets); err != nil {
    ui.Warning("Backup partially failed: %v", err)
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

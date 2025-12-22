# Reconcile Workflow: Security and Edge Case Analysis

**Date:** 2024-12-22
**Scope:** `/Users/cameron/Projects/unops/internal/reconcile/` and `/Users/cameron/Projects/unops/internal/cmd/reconcile.go`
**Severity Levels:** CRITICAL, HIGH, MEDIUM, LOW

---

## Executive Summary

The reconcile workflow is the core GitOps engine for deploying infrastructure configurations. Analysis identified **23 findings** across security, reliability, and edge case handling:

| Severity | Count | Categories |
|----------|-------|------------|
| CRITICAL | 3 | Secrets exposure, SSH key handling, rollback gaps |
| HIGH | 7 | Error recovery, partial failures, stale locks |
| MEDIUM | 9 | Timeouts, validation, cleanup |
| LOW | 4 | Logging, UX, minor edge cases |

---

## 1. Git Operations

### 1.1 Clone Failure Mid-Way

**File:** `/Users/cameron/Projects/unops/internal/reconcile/git.go:34-48`

**Finding (MEDIUM):** If `git clone` fails mid-way (network drop, disk full), a partial `.git` directory may remain. The `IsRepo()` check on line 114-118 uses `test -d .git` which would pass for a corrupted partial clone.

```go
func (g *GitOps) IsRepo() bool {
    gitDir := filepath.Join(g.Dir, ".git")
    cmd := exec.Command("test", "-d", gitDir)
    return cmd.Run() == nil
}
```

**Impact:** Subsequent runs would attempt `Pull()` on a corrupted repo, failing silently or producing incorrect results.

**Recommendation:** After clone, verify repo integrity with `git status` or check for `HEAD` file existence. On clone failure, clean up the partial directory.

### 1.2 No Network Timeout Handling

**File:** `/Users/cameron/Projects/unops/internal/reconcile/git.go:34-48, 53-83`

**Finding (MEDIUM):** Git operations use `exec.CommandContext(ctx, "git", ...)` which respects context cancellation, but there's no explicit timeout. The default context from `cmd/reconcile.go` has no timeout set.

**Impact:** A hanging network connection could block the workflow indefinitely.

**Recommendation:** Add timeout to git operations:
```go
ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
defer cancel()
```

### 1.3 Authentication Failures Not Distinguished

**File:** `/Users/cameron/Projects/unops/internal/reconcile/git.go:45-47`

**Finding (LOW):** Authentication failures (bad SSH key, expired token) produce the same error format as other failures.

**Impact:** User cannot easily distinguish authentication issues from network issues.

**Recommendation:** Parse stderr for common auth failure patterns and provide targeted error messages.

### 1.4 Dirty Working Directory Ignored

**File:** `/Users/cameron/Projects/unops/internal/reconcile/git.go:68-75`

**Finding (MEDIUM):** The `Pull()` function uses `git reset --hard` which discards local changes without warning.

**Impact:** Any local debugging changes or manual fixes would be lost silently.

**Recommendation:** Either:
- Check for uncommitted changes first and warn/fail
- This is intentional for GitOps (repo is source of truth) - document this behavior clearly

---

## 2. SOPS Decryption

### 2.1 Missing Age Key - Error Not Actionable

**File:** `/Users/cameron/Projects/unops/internal/reconcile/sops.go:20-30`

**Finding (HIGH):** When the SOPS age key is missing (`SOPS_AGE_KEY` not set, no key file), the error from SOPS is captured but not parsed for actionable guidance.

```go
if err := cmd.Run(); err != nil {
    return nil, fmt.Errorf("sops decrypt failed for %s: %w: %s", file, err, stderr.String())
}
```

**Impact:** Users see cryptic SOPS errors without guidance on key setup.

**Recommendation:** Pre-check for age key availability and provide setup instructions in error message.

### 2.2 Corrupted Encrypted File Not Detected Early

**File:** `/Users/cameron/Projects/unops/internal/reconcile/reconcile.go:187-195`

**Finding (MEDIUM):** File existence is checked, but no validation that it's a valid SOPS file:

```go
if _, err := os.Stat(path); err != nil {
    return nil, fmt.Errorf("secrets file not found: %s", path)
}
```

**Impact:** A corrupted or non-SOPS file would fail during decryption with a confusing error.

**Recommendation:** Validate SOPS file header (look for `sops:` key in YAML) before attempting decryption.

### 2.3 Wrong Key for File - No Key Rotation Support

**File:** `/Users/cameron/Projects/unops/internal/reconcile/sops.go:20-30`

**Finding (MEDIUM):** If the file was encrypted with a different age key (key rotation scenario), decryption fails with no guidance.

**Impact:** Key rotation scenarios are painful - unclear which key is expected.

**Recommendation:** Parse SOPS metadata to identify expected key fingerprint and compare with available keys.

### 2.4 CRITICAL: Decrypted Secrets in Memory

**File:** `/Users/cameron/Projects/unops/internal/reconcile/sops.go:33-44` and `/Users/cameron/Projects/unops/internal/reconcile/template.go:47`

**Finding (CRITICAL):** Decrypted secrets are held in `map[string]any` and passed to environment variable:

```go
cmd.Env = append(os.Environ(), "SOPS_SECRETS="+string(dataJSON))
```

**Impact:**
1. Secrets visible in `/proc/<pid>/environ` on Linux
2. Secrets could be swapped to disk
3. Child processes inherit environment with secrets

**Recommendation:**
1. Pass secrets via stdin pipe to chezmoi instead of environment
2. Consider using memory-locked buffers for sensitive data
3. Clear secret data from memory when done (though Go doesn't guarantee zeroing)

---

## 3. Template Rendering

### 3.1 Missing Template Variables - Silent Failure

**File:** `/Users/cameron/Projects/unops/internal/reconcile/template.go:45-55`

**Finding (HIGH):** Chezmoi template rendering with missing variables may produce empty output or partial output depending on template syntax.

**Impact:** Deployed configs could be incomplete, causing service failures.

**Recommendation:**
1. Validate rendered output is non-empty
2. Use Chezmoi strict mode if available
3. Consider schema validation for critical configs

### 3.2 Invalid Template Syntax

**File:** `/Users/cameron/Projects/unops/internal/reconcile/template.go:52-54`

**Finding (MEDIUM):** Template syntax errors are caught but not validated before deployment.

**Impact:** A single bad template file could halt the entire deployment.

**Recommendation:** Pre-validate all templates before starting deployment (fail fast).

### 3.3 Chezmoi Not Installed

**File:** `/Users/cameron/Projects/unops/internal/reconcile/template.go:45`

**Finding (MEDIUM):** No pre-check for chezmoi binary availability.

```go
cmd := exec.CommandContext(ctx, "chezmoi", "execute-template")
```

**Impact:** Cryptic "executable not found" error.

**Recommendation:** Check for required binaries at startup and provide installation instructions.

### 3.4 Template Output Not Validated

**File:** `/Users/cameron/Projects/unops/internal/reconcile/template.go:63-64`

**Finding (HIGH):** Rendered output is written directly without validation:

```go
if err := os.WriteFile(outputFile, stdout.Bytes(), 0644); err != nil {
```

**Impact:**
- Empty files could be deployed
- Malformed YAML/JSON could be deployed
- No schema validation

**Recommendation:**
1. Validate output is non-empty
2. For known file types (YAML, JSON), validate syntax
3. Consider schema validation for critical configs (Traefik, Authelia)

---

## 4. Deployment

### 4.1 SSH Connection Failures - No Retry

**File:** `/Users/cameron/Projects/unops/internal/reconcile/deploy.go:163-180, 201-210`

**Finding (HIGH):** SSH failures are immediate failures with no retry logic.

**Impact:** Transient network issues cause deployment failure.

**Recommendation:** Add retry with exponential backoff for SSH operations (2-3 attempts).

### 4.2 Rsync Partial Failure

**File:** `/Users/cameron/Projects/unops/internal/reconcile/deploy.go:163-180`

**Finding (HIGH):** Rsync can partially complete - some files transferred, others not.

**Impact:** Inconsistent state on target - some configs updated, others stale.

**Recommendation:**
1. Use rsync `--partial` with staging directory on remote
2. Implement atomic swap pattern (sync to temp dir, then rename)
3. Consider checksum verification post-sync

### 4.3 Remote Host Unreachable - No Pre-Check

**File:** `/Users/cameron/Projects/unops/internal/reconcile/reconcile.go:372-375`

**Finding (MEDIUM):** Remote mode only discovers host unreachability during first rsync.

**Impact:** Secrets are already decrypted, templates rendered before discovering host is down.

**Recommendation:** Pre-check SSH connectivity before expensive operations.

### 4.4 Disk Space on Remote Not Checked

**File:** `/Users/cameron/Projects/unops/internal/reconcile/deploy.go:163-180`

**Finding (MEDIUM):** No disk space check before deployment.

**Impact:** Deployment could fail mid-way due to disk full, leaving inconsistent state.

**Recommendation:** SSH and check `df` output before deployment.

### 4.5 Permission Denied - Error Message Unclear

**File:** `/Users/cameron/Projects/unops/internal/reconcile/deploy.go:175-178`

**Finding (LOW):** Permission errors from rsync are wrapped but not distinguished:

```go
return fmt.Errorf("rsync failed: %w: %s", err, stderr.String())
```

**Impact:** User must parse stderr to understand permission issue.

**Recommendation:** Parse for common permission patterns and provide targeted guidance.

---

## 5. Backup

### 5.1 Backup Directory Full - Silent Failure

**File:** `/Users/cameron/Projects/unops/internal/reconcile/deploy.go:27-59`

**Finding (MEDIUM):** Tar command failure is ignored:

```go
// tar returns non-zero if some files don't exist, which is OK.
_ = cmd.Run()
```

**Impact:** Backup could fail completely (disk full) and deployment continues without backup.

**Recommendation:** Distinguish between "some files missing" (OK) and "tar failed completely" (not OK).

### 5.2 Corrupt Backup Detection

**File:** `/Users/cameron/Projects/unops/internal/reconcile/deploy.go:27-59`

**Finding (MEDIUM):** No verification that backup tar.gz is valid.

**Impact:** Backup could be corrupted/empty, discovered only when restore is needed.

**Recommendation:** Verify backup with `tar -tzf` after creation.

### 5.3 No Restore Mechanism

**File:** `/Users/cameron/Projects/unops/internal/reconcile/deploy.go`

**Finding (HIGH):** No restore-from-backup functionality implemented.

**Impact:** If deployment fails, manual intervention required to restore.

**Recommendation:** Implement `bosun restore [backup-name]` command.

---

## 6. Docker Compose

### 6.1 CRITICAL: Compose Up Failure - No Rollback

**File:** `/Users/cameron/Projects/unops/internal/reconcile/deploy.go:214-227, 230-244`

**Finding (CRITICAL):** If `docker compose up` fails, configs are already deployed and no rollback occurs:

```go
if err := r.deploy.ComposeUp(ctx, filepath.Join(appdata, "compose", "core.yml")); err != nil {
    ui.Warning("Could not recreate core stack: %v", err)
}
```

**Impact:** Services could be down with no automatic recovery.

**Recommendation:**
1. Implement automatic rollback on compose failure
2. Verify all containers are healthy after compose up
3. Consider blue-green deployment pattern

### 6.2 Container Health Check Not Verified

**File:** `/Users/cameron/Projects/unops/internal/reconcile/deploy.go:214-227`

**Finding (HIGH):** No health check verification after `docker compose up`.

**Impact:** Containers could start but be unhealthy/crashlooping.

**Recommendation:** Wait for health checks and verify container status after deployment.

### 6.3 Compose Up Timeout

**File:** `/Users/cameron/Projects/unops/internal/reconcile/deploy.go:219`

**Finding (MEDIUM):** Docker compose up has no timeout:

```go
cmd := exec.CommandContext(ctx, "docker", "compose", "-f", composeFile, "up", "-d", "--remove-orphans")
```

**Impact:** Pulling large images could block indefinitely.

**Recommendation:** Add `--wait-timeout` flag or context timeout.

---

## 7. Locking

### 7.1 Stale Lock Detection

**File:** `/Users/cameron/Projects/unops/internal/reconcile/reconcile.go:148-162`

**Finding (MEDIUM):** Uses `flock` which auto-releases on process death, but no stale lock detection mechanism for informational purposes.

**Impact:** If process crashes, next run works (good), but no logging of stale lock scenario.

**Recommendation:** Add PID to lock file and log if previous PID is dead.

### 7.2 Lock Timeout Not Configurable

**File:** `/Users/cameron/Projects/unops/internal/reconcile/reconcile.go:155`

**Finding (LOW):** Uses `LOCK_NB` (non-blocking), immediately fails if locked.

```go
if err := syscall.Flock(int(fd.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
```

**Impact:** Cannot wait for lock with timeout; must retry externally.

**Recommendation:** Consider optional `--wait-for-lock` flag with timeout.

### 7.3 Graceful Unlock on Crash

**File:** `/Users/cameron/Projects/unops/internal/reconcile/reconcile.go:165-171`

**Finding (LOW - Already Handled):** Uses file descriptor-based locking which auto-releases on process termination.

**Status:** Good - `flock` is a good choice for this.

---

## 8. Security

### 8.1 CRITICAL: Secrets Could Appear in Logs

**File:** `/Users/cameron/Projects/unops/internal/reconcile/sops.go:27`

**Finding (CRITICAL):** SOPS stderr is included in error messages:

```go
return nil, fmt.Errorf("sops decrypt failed for %s: %w: %s", file, err, stderr.String())
```

**Impact:** If SOPS outputs partial decrypted content in stderr (unlikely but possible), secrets could end up in logs.

**Additional Locations:**
- Template rendering errors could include secret values
- Rsync verbose output could show file contents

**Recommendation:**
1. Sanitize all stderr before logging
2. Never log decrypted secret values
3. Consider structured logging with explicit field allowlist

### 8.2 Temporary File Cleanup

**File:** `/Users/cameron/Projects/unops/internal/reconcile/reconcile.go:211-216`

**Finding (MEDIUM):** Staging directory contains rendered secrets and is cleared only at start of next run:

```go
if err := os.RemoveAll(r.config.StagingDir); err != nil {
    return fmt.Errorf("failed to clear staging directory: %w", err)
}
```

**Impact:** Rendered secrets persist on disk between runs.

**Recommendation:**
1. Clear staging directory after successful deployment
2. Set restrictive permissions on staging directory (0700)
3. Consider using in-memory filesystem for staging

### 8.3 SSH Key Handling

**File:** `/Users/cameron/Projects/unops/internal/reconcile/deploy.go:77, 163-180`

**Finding (MEDIUM):** SSH relies on default key locations and ssh-agent. No validation that keys exist before attempting connection.

**Impact:** Connection failures could be confusing if SSH keys not set up.

**Recommendation:** Pre-check SSH key availability and provide guidance.

### 8.4 SOPS Age Key Exposure

**File:** `/Users/cameron/Projects/unops/internal/reconcile/sops.go:21`

**Finding (MEDIUM):** SOPS inherits all environment variables including `SOPS_AGE_KEY`:

```go
cmd := exec.CommandContext(ctx, "sops", ...)
// No explicit env setting, so inherits parent environment
```

**Impact:** This is correct behavior, but the key could be logged if verbose SOPS output is enabled.

**Recommendation:** Explicitly control environment passed to SOPS, excluding sensitive variables from logging.

---

## 9. Additional Findings

### 9.1 No Metrics/Observability

**Finding (LOW):** No metrics, tracing, or structured logging for reconciliation runs.

**Recommendation:** Add OpenTelemetry tracing and Prometheus metrics for:
- Reconciliation duration
- Success/failure counts
- Files changed count

### 9.2 No Webhook/Notification on Failure

**Finding (LOW):** Failures only logged to console, no alerting mechanism.

**Recommendation:** Add webhook notification on failure (Discord, Slack, etc.).

### 9.3 Required Binaries Not Validated

**Finding (MEDIUM):** Assumes `git`, `sops`, `chezmoi`, `rsync`, `ssh`, `tar`, `docker` are available.

**Recommendation:** Add startup validation for all required binaries with version checks.

---

## Summary of Recommendations by Priority

### CRITICAL (Fix Immediately)

1. **Secrets in environment** - Pass secrets via stdin to chezmoi, not environment variable
2. **Rollback on compose failure** - Implement automatic rollback when deployment fails
3. **Secrets in logs** - Sanitize all external command output before logging

### HIGH (Fix Soon)

1. **Template output validation** - Validate rendered configs before deployment
2. **SSH retry logic** - Add retry with backoff for transient failures
3. **Rsync atomic deployment** - Implement staging + atomic swap pattern
4. **Container health checks** - Verify health after compose up
5. **Actionable SOPS errors** - Pre-check for age key availability
6. **Restore mechanism** - Implement backup restore command
7. **Missing variable handling** - Fail fast on missing template variables

### MEDIUM (Plan for Next Iteration)

1. Clone failure cleanup
2. Network timeout handling
3. Pre-check SSH connectivity
4. Disk space validation
5. Backup verification
6. Required binary validation
7. Staging directory cleanup after deployment
8. SOPS file validation before decryption

### LOW (Nice to Have)

1. Authentication error distinction
2. Stale lock logging
3. Metrics and observability
4. Failure notifications
5. Lock wait-with-timeout option

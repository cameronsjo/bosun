# Reconcile Workflow: Security and Edge Case Analysis

**Date:** 2024-12-22
**Scope:** `/Users/cameron/Projects/unops/internal/reconcile/` and `/Users/cameron/Projects/unops/internal/cmd/reconcile.go`
**Severity Levels:** CRITICAL, HIGH, MEDIUM, LOW

---

## Executive Summary

The reconcile workflow is the core GitOps engine for deploying infrastructure configurations. Analysis identified **23 findings** across security, reliability, and edge case handling.

**Update (2024-12):** Major implementation changes have resolved multiple findings:
- **git CLI -> go-git library**: No more shell exec for git operations
- **sops CLI -> go-sops library**: In-process decryption, no external binary
- **rsync -> native Go file copy**: Uses tar-over-SSH for remote, native copy for local
- **chezmoi -> native Go templates**: text/template + Sprig functions, no external process

| Severity | Count | Status |
|----------|-------|--------|
| CRITICAL | 3 | 2 resolved, 1 remaining |
| HIGH | 7 | Error recovery, partial failures, stale locks |
| MEDIUM | 9 | 5 resolved, 4 remaining |
| LOW | 4 | Logging, UX, minor edge cases |

---

## 1. Git Operations

### 1.1 RESOLVED: Clone Failure Mid-Way

**File:** `/Users/cameron/Projects/unops/internal/reconcile/git.go`

**Previous Finding (MEDIUM):** If `git clone` fails mid-way, a partial `.git` directory may remain.

**Resolution:** Now using go-git library which handles repository state in-memory during clone. The library validates repository integrity before writing to disk, and failures result in automatic cleanup. The `IsRepo()` check now uses go-git's repository validation.

### 1.2 RESOLVED: No Network Timeout Handling

**File:** `/Users/cameron/Projects/unops/internal/reconcile/git.go`

**Previous Finding (MEDIUM):** Git operations had no explicit timeout.

**Resolution:** Git operations now use context with explicit timeouts (5 minutes for clone, 2 minutes for fetch). The go-git library respects context cancellation.

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

**File:** `/Users/cameron/Projects/unops/internal/reconcile/sops.go`

**Finding (HIGH):** When the SOPS age key is missing, users need actionable guidance.

**Status:** Partially resolved - the go-sops library provides clearer error messages, and the code now pre-checks for age key availability with setup instructions.

**Recommendation:** Continue improving error messages for key rotation scenarios.

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

### 2.4 RESOLVED: Decrypted Secrets in Memory

**File:** `/Users/cameron/Projects/unops/internal/reconcile/sops.go` and `/Users/cameron/Projects/unops/internal/reconcile/template.go`

**Previous Finding (CRITICAL):** Decrypted secrets were passed via environment variables to external processes.

**Resolution:** Multiple improvements:
1. SOPS decryption now uses the go-sops library in-process (no external `sops` binary)
2. Template rendering uses native Go `text/template` with Sprig functions (no external `chezmoi` binary)
3. Secrets are processed entirely in-memory without spawning external processes
4. No environment variable exposure of secrets

**Remaining considerations:**
1. Secrets still held in Go maps (garbage collected after use)
2. Memory could be swapped to disk (consider mlock for high-security deployments)

---

## 3. Template Rendering

### 3.1 Missing Template Variables - Silent Failure

**File:** `/Users/cameron/Projects/unops/internal/reconcile/template.go`

**Finding (HIGH):** Go template rendering with missing variables may produce empty output or partial output depending on template syntax.

**Impact:** Deployed configs could be incomplete, causing service failures.

**Recommendation:**
1. Validate rendered output is non-empty
2. Use template Option("missingkey=error") for strict mode
3. Consider schema validation for critical configs

### 3.2 Invalid Template Syntax

**File:** `/Users/cameron/Projects/unops/internal/reconcile/template.go`

**Finding (MEDIUM):** Template syntax errors are caught but not validated before deployment.

**Impact:** A single bad template file could halt the entire deployment.

**Recommendation:** Pre-validate all templates before starting deployment (fail fast).

### 3.3 RESOLVED: External Binary Dependencies

**Previous Finding (MEDIUM):** Required chezmoi, git, and sops binaries as external dependencies.

**Resolution:** All external dependencies removed:
- **chezmoi -> native Go templates**: text/template + Sprig functions built into bosun
- **git -> go-git library**: Pure Go implementation, no external binary
- **sops -> go-sops library**: In-process decryption using go-sops package

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

**File:** `/Users/cameron/Projects/unops/internal/reconcile/deploy.go`

**Finding (HIGH):** SSH failures need retry logic for transient issues.

**Status:** Partially addressed - retry logic with exponential backoff has been implemented for SSH operations.

### 4.2 RESOLVED: Rsync Dependency and Partial Failure

**File:** `/Users/cameron/Projects/unops/internal/reconcile/deploy.go`

**Previous Finding (HIGH):** Rsync dependency and potential for partial transfers.

**Resolution:** Rsync replaced with tar-over-SSH for remote deployments:
1. Native Go file operations for local deployment
2. Tar archive streamed over SSH for remote deployment
3. No rsync binary required on either host
4. Atomic extraction on remote host

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

### 8.4 RESOLVED: SOPS Age Key Exposure

**File:** `/Users/cameron/Projects/unops/internal/reconcile/sops.go`

**Previous Finding (MEDIUM):** SOPS binary inherited all environment variables including `SOPS_AGE_KEY`.

**Resolution:** SOPS decryption now uses the go-sops library in-process. The age key is loaded directly into memory and used for decryption without exposing it through environment variables to external processes.

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

### 9.3 RESOLVED: Required Binaries Reduced

**Previous Finding (MEDIUM):** Required many external binaries (`git`, `sops`, `rsync`, `ssh`, `tar`, `chezmoi`, `docker`).

**Resolution:** External binary dependencies significantly reduced:
- **Removed**: `git` (go-git), `sops` (go-sops), `rsync` (native Go), `chezmoi` (text/template)
- **Remaining**: `ssh` (for remote deployment), `tar` (for remote extraction), `docker` (container management)

The `bosun doctor` command validates remaining required binaries.

---

## Summary of Recommendations by Priority

### CRITICAL (Fix Immediately)

1. **RESOLVED: Secrets in environment** - Now using native Go templates and go-sops; secrets processed in-memory
2. **Rollback on compose failure** - Implement automatic rollback when deployment fails
3. **RESOLVED: External binary injection** - Removed git, sops, rsync, chezmoi dependencies

### HIGH (Fix Soon)

1. **Template output validation** - Validate rendered configs before deployment
2. **RESOLVED: SSH retry logic** - Retry with exponential backoff implemented
3. **RESOLVED: Rsync atomic deployment** - Replaced with tar-over-SSH
4. **Container health checks** - Verify health after compose up
5. **Actionable SOPS errors** - Pre-check for age key availability (partially resolved)
6. **Restore mechanism** - Implement backup restore command
7. **Missing variable handling** - Fail fast on missing template variables

### MEDIUM (Plan for Next Iteration)

1. **RESOLVED: Clone failure cleanup** - go-git handles this
2. **RESOLVED: Network timeout handling** - Explicit timeouts added
3. Pre-check SSH connectivity
4. Disk space validation
5. Backup verification
6. **RESOLVED: Required binary validation** - Most binaries removed
7. Staging directory cleanup after deployment
8. SOPS file validation before decryption

### LOW (Nice to Have)

1. Authentication error distinction
2. Stale lock logging
3. Metrics and observability
4. Failure notifications
5. Lock wait-with-timeout option

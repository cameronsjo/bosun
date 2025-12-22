# Edge Case Analysis: Diagnostics, Emergency, and Init Commands

This document analyzes potential edge cases, missing error handling, and robustness issues in the bosun CLI's diagnostic, emergency, and initialization workflows.

## 1. Doctor Command (`internal/cmd/diagnostics.go`)

### Current Behavior

The `doctor` command runs pre-flight checks for Docker, Git, SOPS, age keys, and other dependencies.

### Edge Cases Identified

| Issue | Severity | Current Behavior | Recommendation |
|-------|----------|------------------|----------------|
| **No timeout on checks** | High | Checks can hang indefinitely (e.g., Docker ping, HTTP request to webhook) | Add context timeout (e.g., 10s) per check |
| **HTTP request to localhost:8080 has no timeout** | Medium | Line 472: `http.Get()` uses default client with no timeout | Use `http.Client{Timeout: 5*time.Second}` |
| **Docker ping has 5s timeout but others do not** | Medium | Only `client.Ping()` has timeout; other Docker calls do not | Wrap all Docker operations with timeout context |
| **No parallel check execution** | Low | Checks run sequentially, slow when Docker is unresponsive | Consider concurrent checks with aggregate results |
| **Age key file path edge case** | Low | Assumes `$HOME` exists and is readable | Handle missing/inaccessible home directory |
| **Remediation steps incomplete** | Medium | Some checks show "not found" without fix instructions | Add remediation for all failed checks |

### Missing Checks

- Disk space availability
- Network connectivity (can reach Docker Hub, GHCR)
- Write permissions on output directories
- File descriptor limits

---

## 2. Lint Command (`internal/cmd/diagnostics.go`)

### Current Behavior

Validates manifest YAML files, checks dependencies, and detects port conflicts.

### Edge Cases Identified

| Issue | Severity | Current Behavior | Recommendation |
|-------|----------|------------------|----------------|
| **No YAML syntax validation** | High | Only checks for presence of `name:` and `provisions:` strings | Use proper YAML parser for syntax validation |
| **Port regex misses edge cases** | Medium | Line 805: `(?:loadbalancer\.server\.port|"(\d+):)` only catches Traefik labels and quoted port mappings | Also check `ports:` section with various formats |
| **Dependency cycle detection missing** | High | `checkDependencies()` checks if depends_on exists but not for cycles | Implement topological sort / cycle detection |
| **uv dependency for validation** | Medium | Falls back to no validation if `uv` not installed | Consider Go-native YAML validation fallback |
| **Dry-run render can hang** | High | Line 711-715: `cmd.Run()` has no timeout | Add context with timeout |
| **Port conflict false positives** | Medium | Same stack can legitimately use same port for different services | Track port-to-service mapping within stacks |
| **No validation of image references** | Medium | Doesn't check if images exist or have valid format | Add image reference validation |
| **Empty provisions array** | Low | File with `provisions: []` would pass validation | Warn on empty provisions |

### Port Detection Gaps

The current regex:
```go
portRegex := regexp.MustCompile(`(?:loadbalancer\.server\.port|"(\d+):)`)
```

Misses:
- Unquoted ports: `8080:80`
- Port ranges: `8000-8010:8000-8010`
- Host-bound ports: `127.0.0.1:8080:80`
- Short syntax: `- 80`

---

## 3. Drift Detection (`internal/cmd/diagnostics.go`)

### Current Behavior

Compares running containers against rendered compose files to detect drift.

### Edge Cases Identified

| Issue | Severity | Current Behavior | Recommendation |
|-------|----------|------------------|----------------|
| **Compose file parsing is regex-based** | High | `extractServicesFromCompose()` uses fragile regex parsing | Use proper YAML parser |
| **Image tag vs digest comparison** | High | String comparison: `nginx:latest` != `nginx@sha256:...` | Normalize image references before comparison |
| **Partial drift not reported clearly** | Medium | Shows each service individually but no summary stats | Add "3/10 services drifted" summary |
| **Missing compose files** | Medium | Line 287: `filepath.Glob()` error ignored | Handle and report glob errors |
| **Container rename detection** | Medium | Renamed containers show as orphan + missing | Consider ID-based matching |
| **Network drift not detected** | Medium | Only checks image, not networks, volumes, env vars | Extend drift detection scope |
| **Read-only infrastructure containers** | Low | Hard-coded list: `traefik`, `authelia`, `gatus` | Make configurable |

### Regex Parsing Issues

The `extractServicesFromCompose()` function (lines 642-694) has these problems:
- Assumes exactly 2-space indentation
- Can't handle YAML anchors/aliases
- Can't handle multi-line values
- Doesn't handle quoted service names

---

## 4. Status Command (`internal/cmd/diagnostics.go`)

### Current Behavior

Shows yacht health dashboard with container status, resources, and recent activity.

### Edge Cases Identified

| Issue | Severity | Current Behavior | Recommendation |
|-------|----------|------------------|----------------|
| **Docker stats can timeout** | High | `GetAllContainerStats()` loops through all containers with no aggregate timeout | Add overall timeout, skip slow containers |
| **Memory formatting edge case** | Low | `formatBytes()` works but doesn't handle negative values | Guard against negative input |
| **CPU percentage can exceed 100%** | Low | Multi-core systems can show >100% | Document or cap at 100% per core |
| **Empty container list** | Low | Shows empty sections with no explanation | Add "No containers running" message in Applications section |
| **Container list errors silently ignored** | Medium | Lines 59, 93, 150: errors from `ListContainers()` ignored | Log or display warnings |
| **Disk usage can be slow** | Medium | `DiskUsage()` can take time on systems with many images | Add timeout or make optional |

### Stats Collection Edge Cases

From `internal/docker/client.go` line 293-308:
```go
for _, ctr := range containers {
    s, err := c.GetContainerStats(ctx, ctr.Name)
    if err != nil {
        continue // Skip containers that fail
    }
    ...
}
```

If a container is being stopped during stats collection, this can block. No per-container timeout exists.

---

## 5. Mayday/Snapshots (`internal/cmd/emergency.go`, `internal/snapshot/snapshot.go`)

### Current Behavior

Creates snapshots before provisions, allows rollback to previous states.

### Edge Cases Identified

| Issue | Severity | Current Behavior | Recommendation |
|-------|----------|------------------|----------------|
| **No disk space check before snapshot** | High | `Create()` doesn't check available disk space | Check disk space before copying |
| **Corrupted snapshot directory** | High | No validation of snapshot integrity | Add checksum or manifest file |
| **Partial copy on error** | Medium | Line 64: `os.RemoveAll(snapshotPath)` cleanup on error, but race possible | Use atomic rename pattern |
| **Restore is not atomic** | High | Line 155: `RemoveAll()` then `copyDir()` - failure between leaves broken state | Copy to temp, then atomic rename |
| **Concurrent provision race** | High | No locking - two provisions can run simultaneously | Add file-based lock |
| **Pre-rollback backup not in snapshot list** | Low | `pre-rollback-*` backups listed with regular snapshots | Filter or separate |
| **Large snapshot directory** | Medium | No size limit per snapshot | Add configurable max size |
| **Symbolic links not handled** | Medium | `copyFile()` doesn't preserve symlinks | Use `os.Lstat()` and handle symlinks |

### Rollback Atomicity Issue

```go
// Current restore flow (lines 154-168):
os.RemoveAll(outDir)        // Step 1: Delete current
os.MkdirAll(outDir, 0755)   // Step 2: Recreate directory
copyDir(snapshotPath, outDir)// Step 3: Copy from snapshot
```

If step 3 fails (disk full, permissions, etc.), the output directory is left empty.

**Recommended pattern:**
```go
tempDir := outDir + ".restore-temp"
copyDir(snapshotPath, tempDir)  // Copy to temp
os.Rename(outDir, outDir + ".old")  // Atomic rename
os.Rename(tempDir, outDir)  // Atomic rename
os.RemoveAll(outDir + ".old")  // Cleanup
```

### Disk Space Check

Add before snapshot creation:
```go
func checkDiskSpace(dir string, requiredBytes int64) error {
    var stat syscall.Statfs_t
    if err := syscall.Statfs(dir, &stat); err != nil {
        return err
    }
    available := stat.Bavail * uint64(stat.Bsize)
    if int64(available) < requiredBytes {
        return fmt.Errorf("insufficient disk space: need %d, have %d", requiredBytes, available)
    }
    return nil
}
```

---

## 6. Init Command (`internal/cmd/init.go`)

### Current Behavior

Creates project structure, generates age keys, initializes git repository.

### Edge Cases Identified

| Issue | Severity | Current Behavior | Recommendation |
|-------|----------|------------------|----------------|
| **age-keygen failure ignored** | Medium | Lines 88-89: Falls back to placeholder string | Fail or warn more prominently |
| **git init failure continues** | Low | Line 115: Warning logged but init continues | Appropriate behavior, but could be clearer |
| **Reinit safety incomplete** | Medium | Only checks for `bosun/docker-compose.yml` | Also check `manifest/` directory |
| **Permission issues not handled** | High | `os.MkdirAll()` can fail silently on permission errors | Add explicit permission check first |
| **Existing .sops.yaml with different key** | Medium | Skips if exists - doesn't validate contents | Warn if public key doesn't match |
| **Home directory not found** | Medium | Line 179: Returns error if `UserHomeDir()` fails | Handle with fallback path |
| **Path traversal in args** | Low | User can pass `../../etc` as directory | Validate path is safe |
| **Interactive prompts in non-TTY** | Medium | `promptYesNo()` reads from stdin | Detect TTY, fail gracefully or use flag |

### Reinit Detection Gap

Current check (lines 56-66):
```go
if _, err := os.Stat(bosunDir); err == nil {
    if _, err := os.Stat(composeFile); err == nil {
        // Prompt for reinit
    }
}
```

Missing checks:
- `manifest/` directory with content
- `.sops.yaml` file
- `.git` directory

---

## 7. Radio Commands (`internal/cmd/comms.go`)

### Current Behavior

Tests webhook endpoint and checks Tailscale status.

### Edge Cases Identified

| Issue | Severity | Current Behavior | Recommendation |
|-------|----------|------------------|----------------|
| **Tailscale not installed** | Low | Line 94-98: Shows warning and install instructions | Appropriate behavior |
| **Webhook timeout is 5s** | Low | Line 43: Good timeout set | N/A |
| **Tailscale JSON parse failure** | Low | Line 104-109: Falls back to plain text | Appropriate behavior |
| **Network errors not classified** | Medium | Generic error message for all failures | Differentiate: timeout, connection refused, DNS failure |
| **Hardcoded localhost:8080** | Medium | Line 46: Not configurable | Use config or environment variable |
| **No retry on transient failures** | Low | Single attempt | Consider single retry with backoff |
| **Exit node detection incomplete** | Low | Shows `E` indicator but no action | Could offer to route through exit node |

---

## Summary: Priority Fixes

### P0 - Critical (Data Loss / Security Risk)

1. **Snapshot restore atomicity** - Partial restore can leave broken state
2. **Concurrent provision race** - No locking mechanism
3. **No disk space check** - Snapshots can fill disk

### P1 - High (Functionality Broken)

4. **YAML validation is regex-based** - False positives/negatives in drift detection
5. **No dependency cycle detection** - Can deploy circular dependencies
6. **No timeouts on external calls** - Doctor/lint can hang indefinitely
7. **Image comparison is string-based** - Drift false positives with digests

### P2 - Medium (Poor UX / Degraded Functionality)

8. **Port conflict detection incomplete** - Misses common formats
9. **Error handling silent** - Many errors logged but not surfaced
10. **Network drift not detected** - Only images compared
11. **Hardcoded infrastructure containers** - Not configurable

### P3 - Low (Minor Issues)

12. **Formatting edge cases** - Negative bytes, >100% CPU
13. **Non-TTY handling** - Interactive prompts fail
14. **Missing install instructions** - Some checks lack remediation

---

## Recommended Implementation Order

1. Add context timeouts to all external calls (doctor, lint, drift)
2. Implement atomic snapshot restore with temp directory
3. Add file-based locking for provision operations
4. Add disk space check before snapshot creation
5. Replace regex YAML parsing with proper YAML library
6. Implement dependency cycle detection
7. Normalize image references for drift comparison
8. Expand port conflict detection regex
9. Make infrastructure containers configurable
10. Add integrity validation to snapshots

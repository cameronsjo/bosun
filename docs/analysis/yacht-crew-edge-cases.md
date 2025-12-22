# Yacht and Crew Workflow Edge Case Analysis

This document identifies missing error handling, edge cases, user experience issues, security concerns, and robustness problems in the bosun CLI's Yacht and Crew commands.

## Summary

| Category | Critical | High | Medium | Low |
|----------|----------|------|--------|-----|
| Missing Error Handling | 3 | 5 | 4 | 2 |
| Edge Cases | 2 | 4 | 3 | 2 |
| User Experience | 0 | 2 | 5 | 3 |
| Security | 1 | 2 | 1 | 0 |
| Robustness | 1 | 3 | 2 | 1 |

---

## Yacht Commands

### yacht up

**File**: `/Users/cameron/Projects/unops/internal/cmd/yacht.go` (lines 30-62)

#### Missing Error Handling

| Severity | Issue | Location | Recommendation |
|----------|-------|----------|----------------|
| HIGH | Compose file existence not validated before use | `yacht.go:55-56` | Check if `cfg.ComposeFile` exists before calling `compose.Up()`. Provide actionable error: "Compose file not found at {path}. Run 'bosun init' or check your project structure." |
| MEDIUM | Docker daemon connectivity not verified | `yacht.go:43-52` | If Docker is not running, error is only a warning. User may be confused when `compose.Up()` also fails. Consider failing fast with clear message. |
| MEDIUM | No validation of service names passed as args | `yacht.go:56` | Services like `"; rm -rf /"` would be passed to shell. Validate service names against compose file. |

#### Edge Cases

| Severity | Issue | Scenario | Recommendation |
|----------|-------|----------|----------------|
| HIGH | Compose file syntax errors not caught early | User has invalid YAML | Pre-validate compose file with `docker compose config` before attempting up |
| MEDIUM | Partial service startup failure | Some services start, others fail | Report which services succeeded/failed individually |
| LOW | Empty services array | User runs `yacht up ` with trailing space | Handle gracefully, document behavior |

#### User Experience

| Severity | Issue | Recommendation |
|----------|-------|----------------|
| MEDIUM | No progress indicator for long-running operations | Add spinner or progress output for multi-service startups |
| LOW | "Raising anchor..." message doesn't indicate what's happening | Include compose file path in output: "Starting services from {path}..." |

---

### yacht down

**File**: `/Users/cameron/Projects/unops/internal/cmd/yacht.go` (lines 65-86)

#### Missing Error Handling

| Severity | Issue | Location | Recommendation |
|----------|-------|----------|----------------|
| HIGH | No confirmation for destructive action | `yacht.go:78-80` | Consider `--force` flag or interactive confirmation for down command |
| MEDIUM | No check if services are already stopped | `yacht.go:78` | Check status first and warn if nothing is running |

#### Edge Cases

| Severity | Issue | Scenario | Recommendation |
|----------|-------|----------|----------------|
| MEDIUM | Orphaned volumes/networks not addressed | User expects clean teardown | Offer `--volumes` flag to match `docker compose down -v` |
| LOW | Dependencies on external services | Containers depend on external networks | Warn about potential dependency issues |

---

### yacht restart

**File**: `/Users/cameron/Projects/unops/internal/cmd/yacht.go` (lines 88-109)

#### Missing Error Handling

| Severity | Issue | Location | Recommendation |
|----------|-------|----------|----------------|
| MEDIUM | Services that fail to restart not identified | `yacht.go:102-103` | Parse output to identify failed services |
| LOW | No timeout for restart operations | `yacht.go:102` | Add configurable timeout with `--timeout` flag |

#### Edge Cases

| Severity | Issue | Scenario | Recommendation |
|----------|-------|----------|----------------|
| MEDIUM | Restart of non-existent service | User typos service name | Validate service exists in compose file before restart |

---

### yacht status

**File**: `/Users/cameron/Projects/unops/internal/cmd/yacht.go` (lines 111-132)

#### Missing Error Handling

| Severity | Issue | Location | Recommendation |
|----------|-------|----------|----------------|
| LOW | Empty output not handled specially | `yacht.go:129` | Check for empty output and display "No services defined" or "All services stopped" |

#### User Experience

| Severity | Issue | Recommendation |
|----------|-------|----------------|
| MEDIUM | Raw docker compose ps output | Parse and format output for consistency with other commands |
| LOW | No health status indication | Highlight unhealthy services |

---

## Crew Commands

### crew list

**File**: `/Users/cameron/Projects/unops/internal/cmd/crew.go` (lines 42-80)

#### Missing Error Handling

| Severity | Issue | Location | Recommendation |
|----------|-------|----------|----------------|
| HIGH | Docker daemon not running | `crew.go:49-52` | Detect specific Docker daemon errors and provide actionable messages: "Docker daemon is not running. Start Docker Desktop or run 'sudo systemctl start docker'" |

#### Edge Cases

| Severity | Issue | Scenario | Recommendation |
|----------|-------|----------|----------------|
| LOW | Containers with very long names | Name exceeds terminal width | Truncate names with ellipsis like ports are |

#### User Experience

| Severity | Issue | Recommendation |
|----------|-------|----------------|
| MEDIUM | No health column in output | Add HEALTH column showing healthy/unhealthy/starting |
| LOW | Port truncation loses information | Add `--wide` flag for full port display |

---

### crew logs

**File**: `/Users/cameron/Projects/unops/internal/cmd/crew.go` (lines 82-123)

#### Missing Error Handling

| Severity | Issue | Location | Recommendation |
|----------|-------|----------|----------------|
| HIGH | Container not found error not distinguished | `crew.go:106-108` | Check if container exists first, provide "Container 'X' not found. Run 'crew list' to see available containers" |
| MEDIUM | Network timeout during log streaming | `crew.go:113` | Add timeout and reconnection logic for follow mode |

#### Edge Cases

| Severity | Issue | Scenario | Recommendation |
|----------|-------|----------|----------------|
| HIGH | Container name with special characters | Name like `my-app_1` or `my.app` | Test and document supported name formats |
| MEDIUM | Very large log files | User requests `--tail 1000000` | Cap tail value or warn about memory usage |
| MEDIUM | Container stops during follow | Stream ends unexpectedly | Detect and report container exit |

#### Robustness

| Severity | Issue | Location | Recommendation |
|----------|-------|----------|----------------|
| HIGH | stdCopy doesn't handle TTY containers | `crew.go:113`, `crew.go:186-221` | TTY containers don't use multiplexed streams. Detect TTY and handle differently. |
| MEDIUM | Signal handler goroutine leak | `crew.go:95-98` | Signal handler goroutine continues after normal exit. Use `signal.Stop(sigCh)` |

---

### crew inspect

**File**: `/Users/cameron/Projects/unops/internal/cmd/crew.go` (lines 125-154)

#### Missing Error Handling

| Severity | Issue | Location | Recommendation |
|----------|-------|----------|----------------|
| MEDIUM | Container not found error message | `crew.go:140-142` | Provide more helpful error with suggestions |

#### Security Concerns

| Severity | Issue | Location | Recommendation |
|----------|-------|----------|----------------|
| CRITICAL | Environment variables exposed in output | `crew.go:145-151`, `containers.go:79` | Environment often contains secrets (API keys, passwords). Add `--hide-env` or mask sensitive values by default |
| HIGH | Labels may contain sensitive data | `containers.go:78` | Consider filtering or masking Traefik auth labels, etc. |

#### User Experience

| Severity | Issue | Recommendation |
|----------|-------|----------------|
| MEDIUM | Full JSON dump overwhelming | Provide summary view by default, `--json` for full output |
| LOW | No syntax highlighting | Consider colored JSON output |

---

### crew restart

**File**: `/Users/cameron/Projects/unops/internal/cmd/crew.go` (lines 156-180)

#### Missing Error Handling

| Severity | Issue | Location | Recommendation |
|----------|-------|----------|----------------|
| MEDIUM | Container not found | `crew.go:172-174` | Check existence first with helpful message |
| LOW | Restart timeout not configurable | `client.go:226-227` | Allow `--timeout` flag |

#### Edge Cases

| Severity | Issue | Scenario | Recommendation |
|----------|-------|----------|----------------|
| MEDIUM | Container fails to restart | Unhealthy after restart | Check health status after restart, warn user |

---

## Docker Client

### client.go

**File**: `/Users/cameron/Projects/unops/internal/docker/client.go`

#### Robustness

| Severity | Issue | Location | Recommendation |
|----------|-------|----------|----------------|
| MEDIUM | Context not propagated with timeout | Various methods | Wrap contexts with reasonable timeouts for non-streaming operations |
| MEDIUM | GetAllContainerStats silently skips failures | `client.go:302-304` | Log or track which containers failed |
| LOW | ID truncation hardcoded to 12 chars | `client.go:148`, `containers.go:72` | Use constant or configurable value |

#### Security

| Severity | Issue | Location | Recommendation |
|----------|-------|----------|----------------|
| MEDIUM | Raw() method exposes underlying client | `client.go:87-90` | Consider removing or adding warning comment |

---

### compose.go

**File**: `/Users/cameron/Projects/unops/internal/docker/compose.go`

#### Security Concerns

| Severity | Issue | Location | Recommendation |
|----------|-------|----------|----------------|
| HIGH | Command injection via service names | `compose.go:32-33`, `compose.go:57-58` | Validate service names against safe pattern `^[a-zA-Z0-9_-]+$` before passing to exec |

#### Missing Error Handling

| Severity | Issue | Location | Recommendation |
|----------|-------|----------|----------------|
| HIGH | Compose file not validated | `compose.go:31-41` | Check file exists before exec, provide clear error |
| MEDIUM | Docker binary not found | All exec calls | Check `docker` is in PATH, provide installation instructions |

#### Edge Cases

| Severity | Issue | Scenario | Recommendation |
|----------|-------|----------|----------------|
| MEDIUM | Compose file path with spaces | `/path/to my file/docker-compose.yml` | Test and ensure proper quoting (appears to be handled correctly) |
| MEDIUM | Status parsing failure | Format changes in docker compose | Add version detection or handle parse errors gracefully |

#### Robustness

| Severity | Issue | Location | Recommendation |
|----------|-------|----------|----------------|
| MEDIUM | No context timeout for exec commands | All methods | Use context deadline for exec commands |

---

### containers.go

**File**: `/Users/cameron/Projects/unops/internal/docker/containers.go`

#### Edge Cases

| Severity | Issue | Location | Recommendation |
|----------|-------|----------|----------------|
| LOW | Empty port bindings | `containers.go:101-108` | Handle nil bindings array |
| LOW | Time parsing failures silent | `containers.go:181-183` | Consider logging parse failures in debug mode |

---

## Cross-Cutting Concerns

### Config Loading

**File**: `/Users/cameron/Projects/unops/internal/config/config.go`

| Severity | Issue | Location | Recommendation |
|----------|-------|----------|----------------|
| HIGH | Infinite loop on root filesystem | `config.go:33-55` | Windows drive roots or network paths may not work. Add iteration limit. |
| MEDIUM | No caching of config | Multiple calls per command | Consider caching to avoid repeated filesystem traversal |

### General Patterns

| Severity | Issue | Recommendation |
|----------|-------|----------------|
| MEDIUM | Inconsistent context usage | Commands create `context.Background()` directly. Consider accepting context from parent or using timeout contexts. |
| MEDIUM | No structured logging | Add debug logging for troubleshooting. Use environment variable like `BOSUN_DEBUG=1` |
| LOW | Error message inconsistency | Some use lowercase, some use sentence case. Standardize on lowercase with no trailing punctuation per Go conventions. |

---

## Recommended Priority Fixes

### Critical (Fix Immediately)

1. **Environment variable exposure in crew inspect** - Secrets could be leaked to logs or screens
2. **Command injection in compose.go** - Service names passed directly to shell

### High Priority

1. **Docker daemon not running detection** - Poor UX when Docker isn't available
2. **Compose file validation** - Catch errors early with helpful messages
3. **Container not found handling** - Provide actionable error messages
4. **TTY container log handling** - stdCopy breaks for TTY containers

### Medium Priority

1. **Service name validation** - Prevent invalid names reaching docker
2. **Progress indicators** - Long operations need feedback
3. **Timeouts on operations** - Prevent hanging on network issues
4. **Health status display** - Important info hidden from user

---

## Testing Recommendations

### Unit Tests to Add

```go
// compose_test.go
func TestUp_ComposeFileNotFound(t *testing.T) {}
func TestUp_InvalidServiceName(t *testing.T) {}
func TestUp_DockerNotRunning(t *testing.T) {}

// crew_test.go
func TestLogs_ContainerNotFound(t *testing.T) {}
func TestLogs_TTYContainer(t *testing.T) {}
func TestLogs_ContextCancellation(t *testing.T) {}

// client_test.go
func TestNewClient_DockerNotRunning(t *testing.T) {}
func TestPing_Timeout(t *testing.T) {}
```

### Integration Tests

1. Test with Docker daemon stopped
2. Test with malformed compose files
3. Test with containers using TTY
4. Test with special character container names
5. Test signal handling during log streaming

---

## References

- **Files Analyzed**:
  - `/Users/cameron/Projects/unops/internal/cmd/yacht.go`
  - `/Users/cameron/Projects/unops/internal/cmd/crew.go`
  - `/Users/cameron/Projects/unops/internal/docker/client.go`
  - `/Users/cameron/Projects/unops/internal/docker/compose.go`
  - `/Users/cameron/Projects/unops/internal/docker/containers.go`
  - `/Users/cameron/Projects/unops/internal/config/config.go`
  - `/Users/cameron/Projects/unops/internal/docker/interface.go`

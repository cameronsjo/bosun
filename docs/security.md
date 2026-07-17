# Bosun Security Documentation

This document describes the security architecture, practices, and controls implemented in Bosun.

## Security Principles

Bosun follows a defense-in-depth approach with these core principles:

1. **Least Privilege**: Operations use minimal permissions required
2. **Secure by Default**: Security measures are always enabled (no opt-out)
3. **Fail Secure**: Errors fail closed, never exposing sensitive data
4. **Input Validation**: All external inputs are validated before use
5. **Secret Isolation**: Secrets are isolated from logs, environment variables, and error messages

## Secrets Management

### SOPS Integration

Bosun uses [SOPS](https://github.com/getsops/sops) (Secrets OPerationS) for encrypting sensitive configuration files. SOPS provides:

- Encryption at rest for YAML/JSON files
- Support for multiple key management backends
- Partial encryption (only values are encrypted, keys remain readable)
- Git-friendly encrypted file format

**Implementation**: `internal/internal/reconcile/sops.go`

```go
// SOPSOps provides SOPS decryption operations
type SOPSOps struct{}

// Decrypt decrypts a SOPS-encrypted file and returns the plaintext bytes
func (s *SOPSOps) Decrypt(ctx context.Context, file string) ([]byte, error)
```

### Age Encryption

Bosun uses [age](https://age-encryption.org/) as the encryption backend for SOPS. Age provides:

- Modern, audited cryptography (X25519 + ChaCha20-Poly1305)
- Simple key format (single line text files)
- No external dependencies or key servers

**Key Location Priority** (checked in order):

1. `SOPS_AGE_KEY` environment variable (inline key)
2. `SOPS_AGE_KEY_FILE` environment variable (path to key file)
3. Default: `~/.config/sops/age/keys.txt`

### Key Generation and Storage

**Implementation**: `internal/internal/cmd/init.go`

During `bosun init`, age keys are generated with secure defaults:

```go
// Create key directory with restricted permissions
if err := os.MkdirAll(keyDir, 0700); err != nil {
    return "", fmt.Errorf("create key directory: %w", err)
}

// Generate key using age-keygen
keygen := exec.Command("age-keygen", "-o", ageKeyFile)

// Set secure permissions on key file
if err := os.Chmod(ageKeyFile, 0600); err != nil {
    return "", fmt.Errorf("set key permissions: %w", err)
}
```

**Security Controls**:

| Resource | Permission | Rationale |
|----------|------------|-----------|
| Key directory (`~/.config/sops/age/`) | `0700` | Owner-only access |
| Key file (`keys.txt`) | `0600` | Owner read/write only |

### Key Rotation Procedures

To rotate age keys:

1. **Generate new key**:
   ```bash
   age-keygen -o ~/.config/sops/age/keys-new.txt
   ```

2. **Update `.sops.yaml`** with the new public key:
   ```yaml
   creation_rules:
     - path_regex: .*\.sops\.yaml$
       age: age1newpublickeyhere...
   ```

3. **Re-encrypt existing secrets**:
   ```bash
   # For each encrypted file
   sops updatekeys secrets.sops.yaml
   ```

4. **Verify decryption** works with new key

5. **Archive old key** securely (do not delete immediately - needed for backups)

## Secret Handling in Templates

### Temporary File Approach

**Implementation**: `internal/internal/reconcile/template.go`

Secrets are passed to templates via temporary files rather than environment variables. This prevents:

- Secret leakage in process listings (`ps aux`)
- Secret exposure in shell history
- Secret inheritance by child processes

```go
// Write secrets to a temporary file with restricted permissions (0600)
// instead of passing the actual secret values via environment variables
secretsFile, err := os.CreateTemp("", "bosun-secrets-*.json")
if err != nil {
    return fmt.Errorf("failed to create temp secrets file: %w", err)
}
secretsPath := secretsFile.Name()
defer func() {
    secretsFile.Close()
    os.Remove(secretsPath) // Cleanup after use
}()

// Set restrictive permissions before writing
if err := os.Chmod(secretsPath, 0600); err != nil {
    return fmt.Errorf("failed to set secrets file permissions: %w", err)
}
```

**Template Access Pattern**:
```go
// Templates access secrets via file path (not content):
// {{ $secrets := fromJson (include (env "BOSUN_SECRETS_FILE")) }}
cmd.Env = append(filterSafeEnv(os.Environ()), "BOSUN_SECRETS_FILE="+secretsPath)
```

### File Permission Standards

| File Type | Permission | Rationale |
|-----------|------------|-----------|
| Secrets temp file | `0600` | Owner read/write only |
| Rendered output files | `0644` | World-readable (contains no secrets) |
| Output directories | `0755` | Standard directory permissions |
| Staging directories | `0755` | Standard directory permissions |

### Cleanup Procedures

Temporary secret files are cleaned up using Go's `defer` pattern:

```go
defer func() {
    secretsFile.Close()
    os.Remove(secretsPath)
}()
```

This ensures cleanup occurs even if template rendering fails.

## SSH Security

### Connection Validation

**Implementation**: `internal/internal/reconcile/deploy.go`

SSH connections include multiple security controls:

```go
// SSH connection with security options
cmd := exec.CommandContext(ctx, "ssh",
    "-o", "ConnectTimeout=5",    // Prevent hanging on unreachable hosts
    "-o", "BatchMode=yes",        // Disable password prompts (key-only)
    host, "exit", "0",
)
```

**Security Options**:

| Option | Value | Purpose |
|--------|-------|---------|
| `ConnectTimeout` | 5 seconds | Prevent DoS via slow hosts |
| `BatchMode` | yes | Disable interactive prompts, enforce key auth |

### Retry on Transient Errors

Bosun implements exponential backoff retry for transient SSH errors:

```go
// Transient error patterns that trigger retry
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

Non-transient errors (authentication failures, host key verification) fail immediately.

### Timeout Controls

**Operation Timeouts** (defined in `internal/internal/reconcile/deploy.go`):

| Operation | Timeout | Rationale |
|-----------|---------|-----------|
| SSH Connect | 5 seconds | Quick failure detection |
| SSH Commands | 30 seconds | Reasonable for remote ops |
| Rsync Transfer | 5 minutes | Large file transfers |
| Docker Compose Up | 10 minutes | Container pulls/startup |

### Host Validation

**Implementation**: `internal/internal/reconcile/validation.go`

SSH hosts are validated to prevent command injection:

```go
// Reject SSH option injection (arguments starting with -)
if strings.HasPrefix(host, "-") {
    return fmt.Errorf("invalid host: cannot start with '-' (potential SSH option injection)")
}

// Reject shell metacharacters
shellMetachars := []string{";", "&", "|", "$", "`", "(", ")", "{", "}", "<", ">", "\\", "\n", "\r", "'", "\""}
for _, char := range shellMetachars {
    if strings.Contains(host, char) {
        return fmt.Errorf("invalid host: contains shell metacharacter %q", char)
    }
}

// Validate format with regex
hostPattern = regexp.MustCompile(`^([a-zA-Z0-9_-]+@)?[a-zA-Z0-9.-]+$`)
```

## Environment Variable Filtering

### Blocked Variables

**Implementation**: `internal/internal/reconcile/template.go`

Environment variables are filtered to prevent secret leakage to child processes:

**Excluded Prefixes**:

| Prefix | Reason |
|--------|--------|
| `SOPS_` | Contains encryption keys |
| `AWS_` | Cloud credentials |
| `AZURE_` | Cloud credentials |
| `GCP_`, `GOOGLE_` | Cloud credentials |
| `DO_` | DigitalOcean credentials |
| `LINODE_` | Linode credentials |
| `VULTR_` | Vultr credentials |
| `CLOUDFLARE_` | Cloudflare credentials |
| `HETZNER_` | Hetzner credentials |
| `OVH_` | OVH credentials |
| `API_KEY` | Generic API keys |
| `SECRET` | Generic secrets |
| `TOKEN` | Generic tokens |
| `PASSWORD` | Passwords |
| `CREDENTIAL` | Credentials |

**Excluded Suffixes**:

| Suffix | Reason |
|--------|--------|
| `_TOKEN` | Auth tokens |
| `_SECRET` | Secret values |
| `_KEY` | API/encryption keys |
| `_PASS`, `_PASSWORD` | Passwords |
| `_AUTH` | Auth credentials |
| `_CREDENTIAL`, `_CREDENTIALS` | Credentials |

**Excluded Exact Matches**:

| Variable | Reason |
|----------|--------|
| `GITHUB_TOKEN` | CI/CD token |
| `GITLAB_TOKEN` | CI/CD token |
| `NPM_TOKEN` | Registry auth |
| `DOCKER_AUTH` | Registry auth |
| `REGISTRY_AUTH` | Registry auth |
| `SSH_AUTH_SOCK` | SSH agent socket |
| `GPG_TTY` | GPG signing |

### Safe Variables (Allowed)

Only these prefixes are passed to child processes:

- `PATH=` - Required for command execution
- `HOME=` - User home directory
- `USER=` - Current user
- `LANG=` - Locale settings
- `LC_` - Locale categories
- `TERM=` - Terminal type
- `XDG_` - XDG base directories
- `TMPDIR=`, `TMP=`, `TEMP=` - Temp directories

### Error Output Sanitization

Template errors are sanitized to prevent secret leakage:

```go
func sanitizeStderr(stderr string) string {
    // Truncate long output that might contain secrets
    const maxLen = 500
    if len(stderr) > maxLen {
        stderr = stderr[:maxLen] + "... (truncated)"
    }
    return stderr
}
```

## File Locking

### Lock File Implementation

**Implementation**: `internal/internal/lock/lock.go`

Bosun uses file-based locking to prevent concurrent operations:

```go
// Lock structure
type Lock struct {
    path string    // e.g., .bosun/locks/provision.lock
    file *os.File
}

// Acquire exclusive lock (non-blocking)
if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
    // Another process holds the lock
}
```

**Platform Support**:

- **Unix**: Uses `flock(2)` system call
- **Windows**: Uses `LockFileEx` API

### Lock File Locations

| Lock | Path | Purpose |
|------|------|---------|
| Provision | `.bosun/locks/provision.lock` | Prevent concurrent renders |
| Reconcile | `/tmp/reconcile.lock` | Prevent concurrent deploys |

### Lock File Contents

Lock files contain the PID of the holding process for debugging:

```go
// Write PID to lock file for debugging
f.Truncate(0)
f.Seek(0, 0)
fmt.Fprintf(f, "%d\n", os.Getpid())
```

### Lock Release

Locks are automatically released when:

1. The process calls `Release()`
2. The process terminates (kernel releases flock)
3. The file descriptor is closed

## Path Traversal Protection

### Tar Extraction Validation

**Implementation**: `internal/internal/cmd/emergency.go`

Tar archives are validated to prevent directory traversal attacks (zip slip):

```go
// Sanitize path to prevent directory traversal
target := filepath.Join(destDir, header.Name)
if !strings.HasPrefix(target, filepath.Clean(destDir)+string(os.PathSeparator)) {
    return fmt.Errorf("invalid file path in archive: %s", header.Name)
}
```

This prevents malicious archives containing paths like:
- `../../../etc/passwd`
- `/etc/shadow`
- `foo/../../bar`

### File Size Limits

Extracted files are limited to prevent resource exhaustion:

```go
// Limit copy size as a security measure
const maxFileSize = 100 * 1024 * 1024 // 100MB max per file
if _, err := io.CopyN(outFile, tr, maxFileSize); err != nil && err != io.EOF {
    return err
}
```

## Input Validation

### Validated Inputs

**Implementation**: `internal/internal/reconcile/validation.go`

| Input | Pattern | Rejects |
|-------|---------|---------|
| SSH Host | `^([a-zA-Z0-9_-]+@)?[a-zA-Z0-9.-]+$` | Shell metacharacters, option injection |
| Git Branch | `^[a-zA-Z0-9_/.-]+$` | Shell metacharacters, option injection |
| Container Name | `^[a-zA-Z0-9][a-zA-Z0-9_.-]*$` | Shell metacharacters, option injection |
| Docker Signal | Allowlist only | Arbitrary signals |

### Shell Metacharacter Rejection

All validated inputs reject these characters:

```go
shellMetachars = []string{
    ";", "&", "|", "$", "`",
    "(", ")", "{", "}",
    "<", ">", "\\",
    "\n", "\r", "'", "\""
}
```

### Option Injection Prevention

Inputs starting with `-` are rejected to prevent:

- SSH option injection: `ssh -oProxyCommand=... evil`
- Git option injection: `git clone --upload-pack=evil ...`
- Docker option injection: `docker --config=evil ...`

## Template Security

### `include` Function Scope

The `include` template function reads arbitrary files from the local filesystem:

```go
"include": func(path string) (string, error) {
    data, err := os.ReadFile(path)
    // ...
}
```

**Current threat model**: Templates come from your own Git repository, which you control. The `include` function is used to read secrets files via `{{ include (env "BOSUN_SECRETS_FILE") }}`.

**Risk**: If Bosun ever processes templates from untrusted sources, this is an arbitrary file read vulnerability. A malicious template could `{{ include "/etc/shadow" }}` or read any file the Bosun process has access to.

**Mitigation**: Path validation for the `include` function is planned. See [bosun-4su](https://github.com/cameronsjo/bosun/issues?q=bosun-4su). Until then, only render templates from trusted repositories.

### Template Rendering Scope

The template renderer walks the entire cloned repository directory for `.tmpl` files. Non-template files are only copied from the infrastructure subdirectory. This means a `.tmpl` file placed outside the expected path will still be rendered, potentially producing unexpected output files.

**Mitigation**: Limit `.tmpl` files to the infrastructure subdirectory in your repository. Consider code review rules that flag `.tmpl` files in unexpected locations.

### Auth Ingress Chain

The auth stack — Traefik, Authelia, and Tailscale gateway — forms a dependency chain for external access. A partial compose up failure where Authelia is down but Traefik is up could serve routes without authentication middleware.

**Current mitigations**:

- Traefik's `forwardAuth` middleware fails closed — if Authelia is unreachable, requests receive 502 errors rather than passing through unauthenticated
- Post-deploy drift verification catches missing or unhealthy containers and logs warnings
- All external routes defined via provisions include `forwardAuth` middleware by default

**Planned**: A dedicated health gate that verifies the full auth chain is healthy before declaring a deploy successful. See [bosun-r9n](https://github.com/cameronsjo/bosun/issues?q=bosun-r9n).

## Daemon Webhook Authentication

The daemon's HTTP trigger endpoints (`/webhook`, `/webhook/github`,
`/webhook/manual`) authenticate requests with an HMAC-SHA256 signature over the
request body, validated with constant-time comparison against `WEBHOOK_SECRET`
(GitHub convention: `X-Hub-Signature-256`; generic/manual: `X-Signature`).

**Webhook auth fails closed.** When no webhook secret is configured, every
trigger request is rejected with `403` and no reconcile runs. This is the
default posture; a missing secret MUST NOT silently grant anonymous callers
deploy control. The daemon logs a loud warning at startup naming the active
posture.

Operators on isolated, trusted networks MAY opt out with
`BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK=true` (strict lowercase match, like the
other escape hatches). With the opt-out active:

- The daemon warns at startup that unauthenticated triggers are enabled
- Every accepted unauthenticated request logs a `SECURITY:` warning with the
  caller's remote address

The opt-out never bypasses a configured secret — when `WEBHOOK_SECRET` is set,
signature validation always runs.

The bind address defaults to all interfaces because container-side callers
(reverse proxy, metrics scrapers, dashboards) reach the daemon over the docker
bridge, not loopback. `BOSUN_LISTEN_ADDR` narrows the bind where the network
topology allows it; the bind address is **not** an authentication control — it
does not protect the Unix socket trigger or the `/metrics` and `/api/widget`
endpoints, which are tracked separately.

## Operator Escape Hatches as Risk Surface

Bosun ships several "escape hatch" environment variables that disable safety checks for legitimate operational scenarios (genuinely empty repos, diagnostic deploys, transient infrastructure conditions). These flags reduce the system's defense-in-depth and SHOULD be treated as a risk surface in their own right.

### Inventory

| Env var | What it disables | Default | Intended use |
|---------|------------------|---------|--------------|
| `BOSUN_ALLOW_EMPTY_DECLARED_STATE` | `ErrNoDeclaredServices` gate at pipeline stage 6 (no parseable services in staging compose dir) | `false` (strict) | Genuinely empty repos, scaffolding |
| `BOSUN_SKIP_DEPLOY_INVARIANT` | Post-deploy mtime + WrittenFiles gate at pipeline stage 9 | `false` (strict) | Diagnostic deploys, repro of intermittent issues |
| `BOSUN_SSH_INSECURE_HOST_KEY` | SSH host-key verification | `false` (strict) | Initial bootstrap before `known_hosts` is populated |
| `BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK` | Fail-closed webhook authentication (secret-less trigger requests rejected with `403`) | `false` (strict) | Trusted, isolated networks where a webhook secret is impractical |

`ErrComposeDirMissing` (stage 6) has no escape hatch by design — a missing staging compose directory always indicates a misconfigured deploy path, never an intentional state.

### Attack scenarios these hatches enable

- **Tampered-staging persistence**: an attacker who can write to the staging directory but cannot reach the destination filesystem (e.g., compromised renderer, leaked SOPS key without root) cannot persist tampered content past stage 9 by default. If `BOSUN_SKIP_DEPLOY_INVARIANT=true` is set in the daemon environment, the post-deploy gate is disabled and tampered content can land if the attacker also disables content-hash sync. The `override=true` log line is the canary.
- **Misconfigured-staging masking**: an attacker who can manipulate `BOSUN_INFRA_DIR` (or the rendered staging path) to point at an empty directory would, by default, trip `ErrNoDeclaredServices` and halt the reconcile. With `BOSUN_ALLOW_EMPTY_DECLARED_STATE=true` set, the deploy continues against an empty service set, which can be used to silently take services down by removing them from the apparent declared state.
- **Bootstrap MITM**: `BOSUN_SSH_INSECURE_HOST_KEY=true` accepts any host key, opening a window for MITM during the bootstrap deploy. If this flag persists past first-deploy, an in-network attacker can substitute the deploy target.

### Operator guidance

1. **Never set escape hatches in the daemon's persistent environment** (systemd unit, Docker `env_file`, etc.). They should be set on the command line for a single one-shot `bosun reconcile` invocation and never persist.
2. **Alert on `override=true` log lines** — the reconcile pipeline emits a `Warn`-level log with `override=true` every time a strict gate is bypassed. A monitoring rule on this field surfaces accidental persistence within one deploy cycle.
3. **Treat any deploy with an escape hatch active as suspect for that cycle** — re-run a strict deploy as soon as the underlying condition (empty repo, FUSE issue, missing `known_hosts`) is resolved.
4. **`bosun doctor` flags persistent escape hatches** — if any of these vars is set in the daemon environment at boot time, it appears in the diagnostic output as a `WARNING` (not `ERROR` — these are legitimate operational tools, just risky if forgotten).

### Why we ship escape hatches anyway

The alternative is forcing operators to patch and rebuild Bosun for every edge case. That moves the risk surface from a one-line env var (visible in monitoring, easy to roll back) to a forked binary (invisible, hard to roll back). Escape hatches make the unsafe path the *visible* path.

## Best Practices

### Key Management

1. **Generate unique keys per environment** (dev, staging, production)
2. **Store production keys in HSM** or cloud KMS when possible
3. **Rotate keys annually** or after personnel changes
4. **Never commit private keys** to version control
5. **Use `age-keygen`** rather than importing existing keys

### Secret Rotation

1. **Rotate secrets regularly** (quarterly minimum)
2. **Update encrypted files** when rotating secrets
3. **Use unique secrets per service** (no shared credentials)
4. **Audit secret access** via git history of `.sops.yaml` files

### Audit Logging

1. **Git history** tracks all secret file changes
2. **Lock files** contain PIDs for debugging
3. **SSH connections** use BatchMode (logged by SSH daemon)
4. **Timeouts** prevent silent failures

### Deployment Security

1. **Use SSH keys** with passphrases
2. **Configure known_hosts** before first deployment
3. **Set ConnectTimeout** to prevent hanging
4. **Verify host keys** to prevent MITM attacks

## Security Checklist

Before deploying with Bosun:

- [ ] Age keys generated with `0600` permissions
- [ ] `.sops.yaml` configured with correct public key
- [ ] SSH keys configured on target hosts
- [ ] `known_hosts` populated for all targets
- [ ] No secrets in environment variables
- [ ] No secrets in git history (use `git-secrets` or similar)
- [ ] Production keys stored securely (not on developer machines)
- [ ] Audit log retention configured

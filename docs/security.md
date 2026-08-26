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

Inline key content takes precedence. Resolved files must be regular,
non-empty, and contain at least one parseable Age identity. Bosun rejects an
invalid file before decryption and explains that Docker can create a directory
when a file bind-mount source is missing. When secrets files are configured,
daemon and one-shot startup perform this validation before Git; the daemon
binds no API listeners on failure.

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

### In-Process Template Data

**Implementation**: `internal/reconcile/sops.go` and
`internal/reconcile/template.go`

Bosun decrypts SOPS input into an in-memory map and passes that map directly as
the Go template root. Templates access a value such as `network.unraid_ip` with
`{{ .network.unraid_ip }}`; no plaintext secrets file path is injected into the
template environment. This avoids exposing plaintext secret values in process
arguments, shell history, or a temporary interchange file.

Templates can intentionally materialize secrets in rendered configuration.
Access to the staging and deployment filesystems is therefore part of the
security boundary; do not treat rendered output as non-sensitive by default.

### File Permission Standards

| File Type | Permission | Rationale |
|-----------|------------|-----------|
| Rendered output files | `0644` | Current renderer mode; output may contain secrets, so restrict host access accordingly |
| Output directories | `0755` | Standard directory permissions |
| Staging directories | `0755` | Standard directory permissions |

### Cleanup Procedures

Decrypted secret data is held in Go values; Bosun does not create a plaintext
secret interchange file to remove or promise explicit memory zeroing. Each
rendered template output produced in the reconciler's staging directory is
first written to a same-directory temporary file. The reconciler removes that
temporary file if rendering fails and atomically renames it to the final staging
path on success.

`bosun render --output` has different semantics: it directly creates or
truncates the requested output file and executes the template into it. A render
failure can therefore leave a partial output file. The CLI does not currently
provide the reconciler's atomic staging-write guarantee.

Reconciliation clears the staging directory before rendering and removes it
after a successful non-dry-run deployment. A failed render or deployment, and a
dry run, can leave rendered staging files for diagnosis. Those files may contain
secrets and must be protected and removed according to the operator's retention
policy. Successfully written files from `bosun render --output` are requested
output, not temporary state, and remain until the operator removes them.

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

**Implementations**: `internal/reconcile/remote_rollback.go` for reconcile
rollback archives and `internal/cmd/emergency.go` for emergency restore
archives.

All reconcile rollback consumers use the same in-process `archive/tar` reader.
It maps each member into a fresh temporary root and validates the realized
destination before writing. It rejects member-name traversal, absolute or
escaping symlink targets, and escaping archive-root-relative hardlink targets
before they can create or redirect content outside the root. Confined relative
symlinks and hardlinks remain supported.

The complete archive must pass before the extracted root is returned. A
validation, corruption, I/O, whole-decompressed-stream size, or
independent-context failure deletes the partial temporary tree, so local
rollback cannot copy or remove live content or pass a partial backup path to
compose or orphan cleanup. The stream bound covers tar headers, padding, skipped
bodies, and trailing data, and a drain to true gzip EOF validates the trailer
checksum.
Local rollback's extraction timeout is derived from a fresh background context,
so cancellation of the failed deployment does not suppress recovery; the
extractor still honors cancellation or expiry of that independent context.

Emergency restore separately validates archive paths before writing:

```go
// Sanitize path to prevent directory traversal
target := filepath.Join(destDir, header.Name)
if !strings.HasPrefix(target, filepath.Clean(destDir)+string(os.PathSeparator)) {
    return fmt.Errorf("invalid file path in archive: %s", header.Name)
}
```

These controls prevent malicious archives containing paths like:
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

The `include` and `fromJsonFile` template functions read files from the local
filesystem. Reads are confined to a **subtree allowlist** — by default
`<infraDir>/templates` — so requested paths are limited to that subtree during
normal rendering rather than permitting arbitrary filesystem reads:

```go
"include": func(path string) (string, error) {
    if err := validateIncludePath(path, includeDir); err != nil {
        return "", err
    }
    data, err := os.ReadFile(path)
    // ...
}
```

**Threat model**: Templates come from your own Git repository, which you
control. The allowlist is a defense-in-depth control so that even a mistaken or
malicious template cannot read the SOPS secrets file, age keys, or `bosun.yaml`
that live in the infra root alongside (but above) `templates/`.

**Enforcement** (`internal/reconcile/template.go`, shared by reconciliation and
`bosun render`): the requested path is resolved (`filepath.Abs`), checked for
lexical containment via `filepath.Rel`
(a `..`-escape, an absolute-outside path, or a `Rel` error all fail closed), and
then re-checked after `filepath.EvalSymlinks` so a symlink whose target escapes
the subtree is rejected. A `{{ include "/etc/shadow" }}` or a read of a sibling
`*.sops.yaml` is now rejected with an error that names the allowed root. The
allowlist confines by **location**: a hardlink whose path is inside `templates/`
remains readable. Validation and `os.ReadFile` are separate operations, so this
is not a race-free or file-descriptor-anchored guarantee against a local actor
concurrently replacing paths during rendering. The trust model assumes the
repository and include tree are not adversarially mutated while a render runs.

**Configuration**: the allowlist root is `<infraDir>/templates` by default and
is overridable with the `template_include_dir` config field or
`BOSUN_TEMPLATE_INCLUDE_DIR` (relative values resolve against the infra dir,
absolute values are used as-is). This is a breaking change for configs that
included files from outside `templates/`; move those files under the subtree or
point the override at their location. For `bosun render`, the infra directory is
the discovered project root plus `BOSUN_INFRA_DIR`; outside a Bosun project it
is the current directory.

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

GitHub pusher attribution is treated as untrusted even after signature
validation. Both the daemon endpoint and the standalone webhook receiver strip
control, formatting, and line-separator characters and cap the remaining name
at 256 Unicode code points before writing it to logs or using it in the
reconcile source propagated to tracing and Sentry. The same sanitization applies
when the explicit unauthenticated-webhook opt-out is active.

All daemon HTTP transports — the webhook listener, Unix socket API, and
optional bearer-authenticated TCP API — allow at most 5 seconds to receive
request headers and set a 32 KiB request-header parsing limit. These
pre-handler limits reduce exposure to slow-header and oversized-header
denial-of-service attacks without changing the existing per-operation
`BOSUN_API_TIMEOUT` behavior.

### Unix socket publication and permissions

The daemon publishes its Unix control socket at mode `0660` by default and
honors an explicitly configured `daemon.Config.SocketMode`. On Unix, Bosun
binds the listener inside a private `0700` staging directory, applies the
configured permission bits there, and atomically publishes the socket at its
final path without replacing a racing entry. The public path therefore never
exists at the operating system's more-permissive default mode, and Bosun does
not change the process-global umask while other goroutines may be creating
files.

On Unix, startup refuses to delete a stale-path entry unless it is actually a
Unix socket; symlinks, regular files, and directories are left untouched.
Shutdown removes the socket only when the path still identifies the inode Bosun
published, so a replacement entry is never deleted as though Bosun owned it.
Windows builds retain AF_UNIX compatibility, but Unix permission bits are not
an access-control boundary there; platform ACLs and request authorization
remain authoritative.

### Unix socket peer authorization

Socket file mode controls which processes can connect, but does not by itself
authorize a deployment. On Linux, Bosun reads `SO_PEERCRED` for each accepted
connection and authorizes mutating socket requests only when the peer UID is
the daemon's effective UID or appears in `BOSUN_SOCKET_ALLOWED_UIDS`. The
allowlist is a comma-separated list of numeric UIDs; malformed, negative, or
out-of-range entries fail configuration validation instead of partially
applying the list.

Missing peer credentials and non-Linux platforms fail closed: `POST /trigger`
returns `403` and does not reconcile. Operators who deliberately use socket
permissions as their entire trust boundary can set
`BOSUN_ALLOW_UNAUTHENTICATED_SOCKET=true` (strict lowercase match). This
security opt-out is logged at startup and for every accepted unauthenticated
mutation. Read-only socket endpoints remain governed by the socket's filesystem
permissions.

## Daemon Metrics and Widget Authentication

The `/metrics` (Prometheus) and `/api/widget` (Homepage) endpoints disclose the
deployed commit and daemon health — information disclosure on an all-interfaces
bind. They authenticate with a **read-scope** bearer token, kept deliberately
separate from the control bearer.

- **`BOSUN_METRICS_TOKEN`** is the read-scope credential a scraper presents as
  `Authorization: Bearer <token>`. It authorizes *only* these read endpoints.
- **`BOSUN_BEARER_TOKEN`** (the TCP control bearer) also authorizes `/trigger`
  and `/api/restart`. Because it is strictly more privileged, it is *also*
  accepted on the read endpoints — but a scraper MUST NOT be given it, since
  that would hand a control credential to a monitoring system. Provision
  scrapers with `BOSUN_METRICS_TOKEN` alone.

**Metrics auth fails closed.** With neither token configured, `/metrics` and
`/api/widget` reject every request with `403` and the daemon logs a loud warning
at startup. Operators on isolated, trusted networks MAY opt out with
`BOSUN_ALLOW_UNAUTHENTICATED_METRICS=true` (strict lowercase match); with the
opt-out active the daemon warns at startup and logs a `SECURITY:` warning per
accepted unauthenticated request. A configured token always takes precedence
over the opt-out.

The bind address defaults to all interfaces because container-side callers
(reverse proxy, metrics scrapers, dashboards) reach the daemon over the docker
bridge, not loopback. `BOSUN_LISTEN_ADDR` narrows the bind where the network
topology allows it; the bind address is **not** an authentication control — the
Unix socket trigger, the webhook endpoints, and the `/metrics` and `/api/widget`
endpoints each enforce their own auth as described above.

## Operator Escape Hatches as Risk Surface

Bosun ships several "escape hatch" environment variables that disable safety checks for legitimate operational scenarios (genuinely empty repos, diagnostic deploys, transient infrastructure conditions). These flags reduce the system's defense-in-depth and SHOULD be treated as a risk surface in their own right.

### Inventory

| Env var | What it disables | Default | Intended use |
|---------|------------------|---------|--------------|
| `BOSUN_ALLOW_EMPTY_DECLARED_STATE` | `ErrNoDeclaredServices` gate at pipeline stage 6 (no parseable services in staging compose dir) | `false` (strict) | Genuinely empty repos, scaffolding |
| `BOSUN_SKIP_DEPLOY_INVARIANT` | Post-deploy created/written path existence, type, fresh-mtime, and no-file-write content gates at pipeline stage 9 | `false` (strict) | Diagnostic deploys, repro of intermittent issues |
| `BOSUN_SSH_INSECURE_HOST_KEY` | SSH host-key verification | `false` (strict) | Initial bootstrap before `known_hosts` is populated |
| `BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK` | Fail-closed webhook authentication (secret-less trigger requests rejected with `403`) | `false` (strict) | Trusted, isolated networks where a webhook secret is impractical |
| `BOSUN_ALLOW_UNAUTHENTICATED_METRICS` | Fail-closed metrics/widget authentication (token-less `/metrics` and `/api/widget` requests rejected with `403`) | `false` (strict) | Trusted, isolated networks where a metrics token is impractical |
| `BOSUN_ALLOW_UNAUTHENTICATED_SOCKET` | Fail-closed peer-credential authorization for mutating Unix socket requests | `false` (strict) | Platforms without peer credentials, when socket permissions are an intentionally sufficient trust boundary |

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

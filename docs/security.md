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

**Implementation**: `/Users/cameron/Projects/unops/internal/reconcile/sops.go`

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

**Implementation**: `/Users/cameron/Projects/unops/internal/cmd/init.go`

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

**Implementation**: `/Users/cameron/Projects/unops/internal/reconcile/template.go`

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

**Implementation**: `/Users/cameron/Projects/unops/internal/reconcile/deploy.go`

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

**Operation Timeouts** (defined in `/Users/cameron/Projects/unops/internal/reconcile/deploy.go`):

| Operation | Timeout | Rationale |
|-----------|---------|-----------|
| SSH Connect | 5 seconds | Quick failure detection |
| SSH Commands | 30 seconds | Reasonable for remote ops |
| Rsync Transfer | 5 minutes | Large file transfers |
| Docker Compose Up | 10 minutes | Container pulls/startup |

### Host Validation

**Implementation**: `/Users/cameron/Projects/unops/internal/reconcile/validation.go`

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

**Implementation**: `/Users/cameron/Projects/unops/internal/reconcile/template.go`

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

**Implementation**: `/Users/cameron/Projects/unops/internal/lock/lock.go`

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

**Implementation**: `/Users/cameron/Projects/unops/internal/cmd/emergency.go`

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

**Implementation**: `/Users/cameron/Projects/unops/internal/reconcile/validation.go`

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

# Bosun Architecture Decision Record

This document explores all architectural routes and decisions for bosun's daemon architecture, with the goal of supporting any Linux system—not just Unraid.

## Table of Contents

1. [Deployment Targets](#deployment-targets)
2. [Architecture Patterns](#architecture-patterns)
3. [Daemon Management](#daemon-management)
4. [Communication Patterns](#communication-patterns)
5. [Webhook Handling](#webhook-handling)
6. [Configuration Management](#configuration-management)
7. [Security Model](#security-model)
8. [Decision Matrix](#decision-matrix)

---

## Deployment Targets

### Target Environments

| Environment | Init System | Container Runtime | Constraints |
|-------------|-------------|-------------------|-------------|
| **Unraid** | Slackware (rc scripts) | Docker | Array start/stop lifecycle |
| **Ubuntu/Debian** | systemd | Docker/Podman | Standard Linux |
| **RHEL/Fedora** | systemd | Podman preferred | SELinux policies |
| **Alpine** | OpenRC | Docker/Podman | Minimal base |
| **NixOS** | systemd | Docker/Podman | Declarative config |
| **Proxmox LXC** | systemd | Docker (nested) | Container-in-container |
| **Bare Container** | None (PID 1) | N/A | Kubernetes, Compose |

### Key Insight

The daemon should be **init-system agnostic** at its core. Packaging/installation can adapt to each target, but the binary itself should:

- Handle SIGTERM/SIGINT gracefully
- Not assume any specific init system
- Work as PID 1 in containers
- Support configuration via env vars and/or config files

---

## Architecture Patterns

### Pattern A: Monolith Container

Everything in one container.

```
┌─────────────────────────────────┐
│         bosun container         │
│                                 │
│  HTTP Server (webhooks)         │
│  Polling Loop                   │
│  Git Operations                 │
│  Template Rendering             │
│  Docker Compose                 │
│                                 │
│  Mounts: docker.sock, config    │
└─────────────────────────────────┘
```

**Pros:**
- Simplest deployment
- Single image to maintain
- Works everywhere Docker runs

**Cons:**
- Dies when Docker restarts
- Can't deploy itself (chicken/egg)
- Full attack surface if webhook compromised

### Pattern B: Host Daemon + Webhook Container (Split)

Daemon on host, webhook in container.

```
┌─────────────────────────────────────────┐
│                  HOST                    │
│                                          │
│  ┌────────────────────────────────────┐ │
│  │          bosun daemon               │ │
│  │  (systemd/openrc/rc script)         │ │
│  │                                     │ │
│  │  localhost:9999/trigger             │ │
│  └────────────────────────────────────┘ │
│                    ▲                     │
│                    │                     │
│  ┌─────────────────┴──────────────────┐ │
│  │       bosun-webhook container       │ │
│  │         (optional)                  │ │
│  └─────────────────────────────────────┘ │
└──────────────────────────────────────────┘
```

**Pros:**
- Daemon survives container runtime restarts
- Webhook is disposable/optional
- Reduced attack surface
- Can deploy other containers including itself

**Cons:**
- Two components to manage
- Requires host-level installation
- Container → host communication complexity

### Pattern C: Host Daemon Only (Polling)

No container, no webhook. Pure polling.

```
┌─────────────────────────────────────────┐
│                  HOST                    │
│                                          │
│  ┌────────────────────────────────────┐ │
│  │          bosun daemon               │ │
│  │                                     │ │
│  │  Poll every N minutes              │ │
│  │  No external exposure              │ │
│  └────────────────────────────────────┘ │
└──────────────────────────────────────────┘
```

**Pros:**
- Simplest security model (no inbound connections)
- No webhook infrastructure needed
- Works behind any firewall/NAT

**Cons:**
- Delayed deploys (up to poll interval)
- Still need host installation

### Pattern D: Sidecar in Compose Stack

Bosun as a service in the stack it manages.

```yaml
# infrastructure/docker-compose.yml
services:
  bosun:
    image: bosun:latest
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    # Manages this very stack

  traefik:
    # ...

  authelia:
    # ...
```

**Pros:**
- Self-contained stack
- GitOps for the GitOps tool

**Cons:**
- Circular dependency risk
- If bosun breaks, can't fix via GitOps

---

## Daemon Management

### Init System Integration

#### systemd (Ubuntu, Debian, Fedora, RHEL, Arch)

```ini
# /etc/systemd/system/bosun.service
[Unit]
Description=Bosun GitOps Daemon
After=network-online.target docker.service
Wants=network-online.target

[Service]
Type=notify
ExecStart=/usr/local/bin/bosun daemon
Restart=on-failure
RestartSec=5
User=bosun
Group=bosun

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/bosun

# Allow docker socket access
SupplementaryGroups=docker

[Install]
WantedBy=multi-user.target
```

**Key points:**
- `Type=notify` for proper readiness signaling (requires [go-systemd](https://github.com/coreos/go-systemd))
- `After=docker.service` ensures Docker is available
- Security hardening via systemd sandboxing

#### OpenRC (Alpine, Gentoo)

```sh
#!/sbin/openrc-run
# /etc/init.d/bosun

name="bosun"
description="Bosun GitOps Daemon"
command="/usr/local/bin/bosun"
command_args="daemon"
command_user="bosun:bosun"
command_background="yes"
pidfile="/run/${RC_SVCNAME}.pid"

depend() {
    need net
    after docker
}
```

#### Unraid (Slackware rc scripts)

```sh
#!/bin/bash
# /etc/rc.d/rc.bosun

DAEMON=/usr/local/bin/bosun
PIDFILE=/var/run/bosun.pid

case "$1" in
  start)
    echo "Starting bosun..."
    setsid $DAEMON daemon &
    echo $! > $PIDFILE
    ;;
  stop)
    echo "Stopping bosun..."
    [ -f $PIDFILE ] && kill $(cat $PIDFILE)
    rm -f $PIDFILE
    ;;
  restart)
    $0 stop
    sleep 1
    $0 start
    ;;
  status)
    [ -f $PIDFILE ] && kill -0 $(cat $PIDFILE) 2>/dev/null && echo "Running" || echo "Stopped"
    ;;
esac
```

### Graceful Shutdown Pattern

The daemon MUST handle signals properly ([source](https://victoriametrics.com/blog/go-graceful-shutdown/)):

```go
func (d *Daemon) Run(ctx context.Context) error {
    // Create cancellable context for shutdown
    ctx, cancel := context.WithCancel(ctx)
    defer cancel()

    // Handle signals
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

    // Start components
    go d.server.Start()
    go d.pollLoop(ctx)

    // Wait for signal
    sig := <-sigCh
    log.Info("Received signal, shutting down", "signal", sig)

    // Graceful shutdown with timeout
    shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer shutdownCancel()

    // Shutdown in reverse order of startup
    d.server.Shutdown(shutdownCtx)
    d.cancelPolling()
    d.waitForReconcile()

    return nil
}
```

---

## Communication Patterns

### Container → Host Communication

When webhook container needs to call host daemon:

| Method | Docker Version | Security | Reliability |
|--------|---------------|----------|-------------|
| `host.docker.internal` + `host-gateway` | 20.10+ | Good | Excellent |
| Bridge gateway IP (`172.17.0.1`) | Any | Good | Varies |
| Host network mode | Any | Poor | Excellent |
| Unix socket mount | Any | Good | Excellent |
| TCP via host IP | Any | Moderate | Good |

**Recommended: Unix Socket**

```yaml
# docker-compose.yml
services:
  bosun-webhook:
    volumes:
      - /var/run/bosun.sock:/var/run/bosun.sock:ro
```

```go
// Webhook container calls daemon
client := &http.Client{
    Transport: &http.Transport{
        DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
            return net.Dial("unix", "/var/run/bosun.sock")
        },
    },
}
resp, err := client.Post("http://localhost/trigger", "application/json", nil)
```

**Benefits:**
- No network exposure
- Works on all Docker versions
- Clear security boundary
- Easy to permission (file ownership)

### Trigger API Design

**Authentication: None required.**

Threat model: If an attacker can reach the Unix socket or localhost:9999, they're already on the host with sufficient privileges. At that point they have access to the Docker socket, SSH keys, and SOPS keys anyway. The socket file permission (ownership, mode 0660) IS the authentication.

```
POST /trigger
Content-Type: application/json

{
  "source": "github",           // github, gitlab, gitea, manual, poll
  "ref": "refs/heads/main",     // git ref that changed
  "commit": "abc123",           // commit SHA (optional)
  "actor": "username"           // who triggered (optional)
}

Response: 202 Accepted
{
  "id": "reconcile-12345",
  "status": "queued"
}
```

```
GET /status
Response: 200 OK
{
  "state": "idle",              // idle, reconciling, error
  "last_reconcile": "2024-01-15T10:30:00Z",
  "last_commit": "abc123",
  "last_error": null,
  "uptime": "24h15m"
}
```

---

## Webhook Handling

### Webhook Sources

| Source | Signature Header | Algorithm | Payload Format |
|--------|-----------------|-----------|----------------|
| GitHub | `X-Hub-Signature-256` | HMAC-SHA256 | JSON |
| GitLab | `X-Gitlab-Token` | Plain token | JSON |
| Gitea | `X-Gitea-Signature` | HMAC-SHA256 | JSON |
| Bitbucket | `X-Hub-Signature` | HMAC-SHA256 | JSON |
| Generic | `X-Signature` | HMAC-SHA256 | JSON |

### Webhook Container Options

**Option 1: Custom bosun-webhook**

Tiny Go binary, ~5MB image:

```go
func main() {
    http.HandleFunc("/webhook/github", handleGitHub)
    http.HandleFunc("/webhook/gitlab", handleGitLab)
    http.HandleFunc("/webhook/generic", handleGeneric)
    http.HandleFunc("/health", handleHealth)
    http.ListenAndServe(":8080", nil)
}
```

**Option 2: Existing Webhook Receivers**

- [webhook](https://github.com/adnanh/webhook) - Lightweight, configurable
- [smee.io](https://smee.io/) - GitHub's webhook proxy
- [Traefik](https://traefik.io/) - If already using, add middleware

**Option 3: Tunnel with Built-in Webhook**

- Cloudflare Tunnel with Access policies
- Tailscale Funnel → direct to daemon

### Decision: Build Custom

Reasons:
- Control over signature validation
- Branch filtering logic
- Unified logging/metrics
- Small attack surface
- No external dependencies

---

## Configuration Management

### Configuration Sources (Priority Order)

1. **CLI Flags** - Highest priority, for testing/override
2. **Environment Variables** - Container/systemd friendly
3. **Config File** - `/etc/bosun/config.yaml` or `~/.config/bosun/config.yaml`
4. **Defaults** - Sensible out-of-box

### Configuration Schema

```yaml
# /etc/bosun/config.yaml
repo:
  url: git@github.com:user/infrastructure.git
  branch: main
  ssh_key: /etc/bosun/ssh/id_ed25519
  poll_interval: 1h

sops:
  age_key_file: /etc/bosun/age-key.txt

paths:
  compose_dir: compose/
  secrets_file: secrets.yaml.sops
  state_dir: /var/lib/bosun

daemon:
  trigger_socket: /var/run/bosun.sock
  # OR
  trigger_port: 9999
  trigger_bind: 127.0.0.1

alerts:
  discord_webhook_url: ${DISCORD_WEBHOOK_URL}
  # sendgrid, twilio, etc.

logging:
  level: info
  format: json  # or text
```

### Environment Variable Mapping

```bash
BOSUN_REPO_URL=git@github.com:user/infrastructure.git
BOSUN_REPO_BRANCH=main
BOSUN_POLL_INTERVAL=3600
BOSUN_TRIGGER_SOCKET=/var/run/bosun.sock
BOSUN_STATE_DIR=/var/lib/bosun
SOPS_AGE_KEY_FILE=/etc/bosun/age-key.txt
DISCORD_WEBHOOK_URL=https://discord.com/api/webhooks/...
```

---

## Security Model

### Principle of Least Privilege

| Component | Capabilities Needed | Attack Surface |
|-----------|---------------------|----------------|
| **Daemon** | Docker socket, git SSH key, SOPS key | High (trusted) |
| **Webhook** | HTTP only, call daemon socket | Low (untrusted) |

### Daemon Hardening

```ini
# systemd hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
PrivateDevices=true
ProtectKernelTunables=true
ProtectControlGroups=true
RestrictSUIDSGID=true
```

### Webhook Container Hardening

```yaml
services:
  bosun-webhook:
    read_only: true
    security_opt:
      - no-new-privileges:true
    cap_drop:
      - ALL
    user: "65534:65534"  # nobody
```

### Secret Management

| Secret | Storage Location | Access |
|--------|-----------------|--------|
| SOPS Age Key | `/etc/bosun/age-key.txt` (mode 0400) | Daemon only |
| SSH Deploy Key | `/etc/bosun/ssh/id_ed25519` (mode 0400) | Daemon only |
| Webhook Secret | Env var or config file | Both |
| Alert Credentials | Env var or config file | Daemon only |

---

## Decision Matrix

### Architecture Pattern

| Criterion | Weight | Monolith | Split | Polling-Only |
|-----------|--------|----------|-------|--------------|
| Simplicity | 3 | ⭐⭐⭐ | ⭐⭐ | ⭐⭐⭐ |
| Security | 4 | ⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐⭐ |
| Reliability | 4 | ⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐ |
| Deploy Speed | 2 | ⭐⭐⭐ | ⭐⭐⭐ | ⭐ |
| Portability | 3 | ⭐⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐⭐ |
| **Total** | | 42 | 50 | 47 |

**Recommendation: Split Architecture** with optional webhook container.

### Communication Method

| Criterion | Weight | Unix Socket | host.docker.internal | Host Network |
|-----------|--------|-------------|---------------------|--------------|
| Security | 4 | ⭐⭐⭐⭐ | ⭐⭐⭐ | ⭐ |
| Simplicity | 3 | ⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ |
| Portability | 3 | ⭐⭐⭐ | ⭐⭐ | ⭐⭐⭐ |
| **Total** | | 32 | 30 | 24 |

**Recommendation: Unix Socket** with fallback to `host.docker.internal`.

---

## Proposed Final Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         LINUX HOST                               │
│                                                                  │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │                    bosun daemon                             │ │
│  │                (systemd/openrc/rc)                          │ │
│  │                                                             │ │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐ │ │
│  │  │ Poll Loop   │  │ Trigger API │  │ Reconciler          │ │ │
│  │  │             │  │             │  │                     │ │ │
│  │  │ interval:1h │  │ unix socket │  │ git → sops → tmpl  │ │ │
│  │  │             │  │ /trigger    │  │ → compose up       │ │ │
│  │  └──────┬──────┘  └──────┬──────┘  └──────────┬──────────┘ │ │
│  │         │                │                    │            │ │
│  │         └────────────────┴────────────────────┘            │ │
│  │                          │                                  │ │
│  │                    ┌─────▼─────┐                           │ │
│  │                    │  Alerter  │                           │ │
│  │                    │ Discord   │                           │ │
│  │                    └───────────┘                           │ │
│  └────────────────────────────────────────────────────────────┘ │
│                              ▲                                   │
│                              │ /var/run/bosun.sock               │
│                              │                                   │
│  ┌───────────────────────────┴────────────────────────────────┐ │
│  │              bosun-webhook (container)                      │ │
│  │                     OPTIONAL                                │ │
│  │                                                             │ │
│  │  Validates: GitHub, GitLab, Gitea, Generic                  │ │
│  │  Filters: branch, event type                                │ │
│  │  Calls: unix:///var/run/bosun.sock/trigger                  │ │
│  │                                                             │ │
│  │  Exposed via: Tailscale Funnel / Cloudflare Tunnel          │ │
│  └─────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
```

## Decisions Made

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Architecture | Split (daemon + webhook container) | Reliability, security isolation |
| Binary | Single binary, subcommands | One artifact, shared code, different runtime modes |
| Communication | Unix socket primary, TCP opt-in | No network exposure, file perms = auth |
| Trigger Auth | Socket: SO_PEERCRED audit. TCP: Bearer token | Audit trail + defense in depth |
| Webhook Secret | Daemon-injected at runtime | Never stored in git or on disk |
| Webhook | Custom multi-provider | Control, small surface, no deps |
| Default Setup | Polling-only | Webhook is advanced opt-in |
| Init System | `bosun init` generates unit files | No .deb/.rpm for v1 |
| systemd | Use go-systemd for notify | Proper readiness signaling |
| Concurrency | Mutex + dirty flag coalescing | Never parallel reconciles |
| Drift | No correction, event-driven only | v1 is deployment agent, not state enforcer |
| Rollback | Manual (`git revert` + push) | Automated rollback deferred to v1.1 |

## Commands

```
# Server modes
bosun daemon             # Long-running daemon (polling + socket API)
bosun webhook            # HTTP relay → daemon socket (for container)

# Standalone operations (no daemon required)
bosun reconcile          # One-shot reconcile, runs locally
bosun rollback [commit]  # Restore previous snapshot
bosun validate           # Dry-run, show what would change

# Daemon interaction (requires running daemon)
bosun trigger            # Tell daemon to reconcile now
bosun status             # Query daemon state

# Setup & diagnostics
bosun doctor             # Pre-flight checks
bosun init               # Setup wizard
bosun version            # Version info
```

**Design principle**: Verbs (`reconcile`, `rollback`, `validate`) run locally and exit. `trigger`/`status` talk to the daemon socket.

## v1 Requirements (Blocking)

1. [ ] **Concurrency**: Implement reconcile mutex + dirty flag coalescing
2. [ ] **Audit**: Add SO_PEERCRED logging for socket callers (UID/PID)
3. [ ] **Webhook secrets**: Daemon generates/injects secret into webhook container
4. [ ] **Socket API**: Unix socket with input validation (signals, not commands)
5. [ ] **TCP safeguards**: Disabled by default, localhost-only, bearer token required

## v1 Implementation

1. [ ] Refactor daemon to use Unix socket for trigger API
2. [ ] Add `bosun webhook` command (HTTP relay to socket)
3. [ ] Add `bosun trigger` command (poke daemon)
4. [ ] Add `bosun status` command (query daemon state)
5. [ ] Add `bosun validate` command (dry-run)
6. [ ] Enhance `bosun init` to generate systemd/OpenRC/rc unit files
7. [ ] Enhance `bosun doctor` to verify full environment
8. [ ] Add multi-provider webhook validation (GitHub, GitLab, Gitea, generic)
9. [ ] Document polling-only as default setup

## v1.1+ (Deferred)

- [ ] Automated rollback on compose failure
- [ ] Drift detection/reporting
- [ ] Multi-stack selective reconciliation
- [ ] Unraid plugin `.plg` file
- [ ] .deb/.rpm packages

## References

- [Go systemd integration](https://vincent.bernat.ch/en/blog/2017-systemd-golang)
- [Graceful shutdown patterns](https://victoriametrics.com/blog/go-graceful-shutdown/)
- [OpenRC service scripts](https://wiki.alpinelinux.org/wiki/Writing_Init_Scripts)
- [Sidecar pattern](https://learn.microsoft.com/en-us/azure/architecture/patterns/sidecar)
- [host.docker.internal](https://www.baeldung.com/ops/docker-compose-add-host)

# Bosun Architecture Review Document

**Purpose**: Complete context for architectural review and critique.

---

## What is Bosun?

Bosun is a GitOps daemon for Docker Compose on bare metal Linux systems. It enables "git push → containers deployed" workflows for homelabs and self-hosted infrastructure without Kubernetes.

**Target environments**: Unraid, Ubuntu, Debian, Fedora, Alpine, Proxmox LXC, any Linux with Docker/Podman.

### The Problem

You have 40 Docker containers across Traefik, Authelia, monitoring, media, etc. Managing them means:
- SSH into server
- Edit compose files
- Run `docker compose up -d`
- Hope you didn't break anything
- No audit trail, no rollback

### The Solution

```
git push → bosun pulls → secrets decrypted → templates rendered → docker compose up
```

Bosun watches a Git repository containing:
- Docker Compose templates (Go templates with Sprig)
- Encrypted secrets (SOPS + Age)
- Configuration values

On changes (webhook or polling), it:
1. Clones/pulls the repo
2. Decrypts secrets with SOPS
3. Renders Go templates
4. Runs `docker compose up -d`
5. Sends alerts (Discord, email, SMS)

---

## Architecture Decision: Split Daemon + Webhook

### Why Split?

| Concern | Single Container | Split Architecture |
|---------|------------------|-------------------|
| Docker restarts | Bosun dies | Daemon keeps running |
| Webhook compromise | Full system access | Only trigger capability |
| Array stopped (Unraid) | Bosun stops | Daemon survives |
| Container runtime issues | Can't self-heal | Daemon independent |

### Components

```
┌─────────────────────────────────────────────────────────────────┐
│                         LINUX HOST                               │
│                                                                  │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │                    bosun daemon                             │ │
│  │                (systemd/openrc/rc script)                   │ │
│  │                                                             │ │
│  │  • Polls git repo at configurable interval (default 1h)    │ │
│  │  • Listens on Unix socket for triggers                     │ │
│  │  • Clones repo, decrypts SOPS, renders templates           │ │
│  │  • Runs docker compose up/down                             │ │
│  │  • Sends alerts on success/failure                         │ │
│  │  • Snapshots before deploy, rollback on failure            │ │
│  │                                                             │ │
│  │  API (Unix socket /var/run/bosun.sock):                    │ │
│  │    POST /trigger  - Trigger reconcile                      │ │
│  │    GET  /health   - Liveness check                         │ │
│  │    GET  /status   - Current state, last reconcile          │ │
│  │    GET  /metrics  - Prometheus metrics                     │ │
│  └────────────────────────────────────────────────────────────┘ │
│                              ▲                                   │
│                              │ Unix socket                       │
│  ┌───────────────────────────┴────────────────────────────────┐ │
│  │              bosun webhook (Docker container)               │ │
│  │                       OPTIONAL                              │ │
│  │                                                             │ │
│  │  • Receives webhooks from GitHub/GitLab/Gitea              │ │
│  │  • Validates HMAC signatures                                │ │
│  │  • Filters by branch (only tracked branch triggers)        │ │
│  │  • Forwards to daemon socket                                │ │
│  │                                                             │ │
│  │  Exposed via: Tailscale Funnel / Cloudflare Tunnel          │ │
│  └─────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
```

---

## Key Decisions Made

| Decision | Choice | Rationale |
|----------|--------|-----------|
| **Architecture** | Split (host daemon + optional webhook container) | Reliability, security isolation, survives Docker restarts |
| **Binary** | Single binary, subcommands | One artifact to build/distribute, shared code |
| **Communication** | Unix socket primary, TCP localhost fallback | No network exposure, file permissions = authentication |
| **Trigger Auth** | None on daemon API | If attacker can reach socket, they own the host anyway |
| **Webhook Validation** | HMAC-SHA256 per provider | GitHub, GitLab, Gitea, generic all supported |
| **Init System** | Agnostic binary + packaged installers | Works on systemd, OpenRC, Slackware rc |
| **systemd Integration** | Use go-systemd for notify | Proper readiness signaling |

### Commands

```
# Server modes
bosun daemon             # Long-running daemon (polling + socket API)
bosun webhook            # HTTP relay → daemon socket (runs in container)

# Standalone operations (no daemon required)
bosun reconcile          # One-shot reconcile, runs locally
bosun rollback [commit]  # Restore previous snapshot
bosun validate           # Dry-run, show what would change

# Daemon interaction (requires running daemon)
bosun trigger            # Tell daemon to reconcile now
bosun status             # Query daemon state

# Setup & diagnostics
bosun doctor             # Pre-flight checks
bosun init               # Interactive setup wizard
bosun version            # Version info
```

**Design principle**: Verbs (`reconcile`, `rollback`, `validate`) run locally and exit. `trigger`/`status` talk to the daemon socket.

---

## Communication Design

### Unix Socket (Primary)

**Path**: `/var/run/bosun.sock` (or `/run/bosun/bosun.sock`)

**Security model**: Socket owned by `bosun:bosun`, mode 0660. Webhook container mounts socket and runs with group access. No authentication needed—file permissions ARE the auth.

**Container access**:
```yaml
services:
  bosun-webhook:
    volumes:
      - /var/run/bosun.sock:/var/run/bosun.sock
    user: "1000:bosun"  # User with bosun group
```

### TCP Fallback (Opt-in)

**Bind**: `127.0.0.1:9999` (localhost only, never 0.0.0.0)

**Use case**: Environments where socket mounting is awkward (some container runtimes, testing).

**Configuration**: Explicit opt-in, not automatic fallback.

### Trigger API

```
POST /trigger
Content-Type: application/json

{
  "source": "github",           // github, gitlab, gitea, manual, poll
  "ref": "refs/heads/main",     // git ref that changed
  "commit": "abc123",           // optional: commit SHA
  "actor": "username"           // optional: who triggered
}

Response: 202 Accepted
{
  "id": "reconcile-12345",
  "status": "queued"
}
```

**No authentication required**. Threat model: if you can reach the socket, you're on the host. At that point you have access to Docker socket, SSH keys, SOPS keys—game over anyway. Socket permissions are the access control.

---

## Webhook Container Design

### Purpose

Thin HTTP relay that:
1. Receives webhooks from Git providers
2. Validates signatures (HMAC-SHA256)
3. Filters by branch
4. Forwards valid triggers to daemon socket

### Supported Providers

| Provider | Signature Header | Event Filter |
|----------|-----------------|--------------|
| GitHub | `X-Hub-Signature-256` | `push` to tracked branch |
| GitLab | `X-Gitlab-Token` | `push` to tracked branch |
| Gitea | `X-Gitea-Signature` | `push` to tracked branch |
| Generic | `X-Signature` | Any POST |

### Container Image

Same `bosun` binary, different entrypoint:

```dockerfile
FROM alpine:3.21
COPY bosun /usr/local/bin/bosun
CMD ["bosun", "webhook"]
```

**Hardening**:
```yaml
services:
  bosun-webhook:
    image: ghcr.io/cameronsjo/bosun:latest
    command: ["bosun", "webhook"]
    read_only: true
    security_opt:
      - no-new-privileges:true
    cap_drop:
      - ALL
    volumes:
      - /var/run/bosun.sock:/var/run/bosun.sock
    environment:
      WEBHOOK_SECRET: ${WEBHOOK_SECRET}
      TRACKED_BRANCH: main
```

---

## Daemon Lifecycle

### Init System Integration

**systemd** (Ubuntu, Debian, Fedora, RHEL):
```ini
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
SupplementaryGroups=docker

# Hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/bosun

[Install]
WantedBy=multi-user.target
```

**OpenRC** (Alpine):
```sh
#!/sbin/openrc-run
name="bosun"
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

**Unraid** (Slackware rc):
```sh
#!/bin/bash
DAEMON=/usr/local/bin/bosun
PIDFILE=/var/run/bosun.pid

case "$1" in
  start) setsid $DAEMON daemon & echo $! > $PIDFILE ;;
  stop) [ -f $PIDFILE ] && kill $(cat $PIDFILE); rm -f $PIDFILE ;;
esac
```

### Graceful Shutdown

```go
func (d *Daemon) Run(ctx context.Context) error {
    ctx, cancel := context.WithCancel(ctx)
    defer cancel()

    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

    go d.server.Start()
    go d.pollLoop(ctx)

    sig := <-sigCh
    log.Info("Shutting down", "signal", sig)

    shutdownCtx, _ := context.WithTimeout(context.Background(), 30*time.Second)
    d.server.Shutdown(shutdownCtx)
    d.waitForReconcile()

    return nil
}
```

---

## Security Model

### Trust Boundaries

```
┌─────────────────────────────────────────────────────────────┐
│                    TRUSTED (Host)                            │
│                                                              │
│  bosun daemon                                                │
│    ├── Docker socket access                                  │
│    ├── SSH deploy key (git clone)                           │
│    ├── SOPS age key (secret decryption)                     │
│    └── Alert credentials (Discord, SendGrid, Twilio)        │
│                                                              │
└─────────────────────────────────────────────────────────────┘
                              ▲
                              │ Unix socket (file perms)
┌─────────────────────────────┴───────────────────────────────┐
│                  SEMI-TRUSTED (Webhook Container)            │
│                                                              │
│  bosun webhook                                               │
│    ├── Webhook secret (for signature validation)            │
│    ├── Socket write access                                  │
│    └── NO access to: Docker, SSH keys, SOPS keys, alerts    │
│                                                              │
└─────────────────────────────────────────────────────────────┘
                              ▲
                              │ HTTPS (signature validated)
┌─────────────────────────────┴───────────────────────────────┐
│                    UNTRUSTED (Internet)                      │
│                                                              │
│  GitHub/GitLab/Gitea webhooks                               │
│    └── Must provide valid HMAC signature                    │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

### Daemon Socket Threat Model

**Claim**: No authentication needed on daemon trigger API.

**Rationale**:
- Socket at `/var/run/bosun.sock` with mode 0660, owner `bosun:bosun`
- Only processes with bosun group can write to it
- If attacker has that access, they already have:
  - Host shell access
  - Ability to read SOPS keys
  - Ability to access Docker socket
  - Game over regardless

**Mitigation**: Socket permissions are the access control. This is standard Unix security model (same as Docker socket).

### Webhook Validation

The webhook container IS a trust boundary. It must:
1. Validate HMAC signatures (timing-safe comparison)
2. Filter by branch (only deploy on tracked branch pushes)
3. Rate limit (prevent trigger spam)
4. Optionally: IP allowlist (GitHub publishes webhook IPs)

---

## Configuration

### Environment Variables

```bash
# Repository
BOSUN_REPO_URL=git@github.com:user/infrastructure.git
BOSUN_REPO_BRANCH=main
BOSUN_POLL_INTERVAL=3600

# Paths
BOSUN_STATE_DIR=/var/lib/bosun
BOSUN_COMPOSE_DIR=compose/
BOSUN_SECRETS_FILE=secrets.yaml.sops

# Socket/API
BOSUN_SOCKET_PATH=/var/run/bosun.sock
# OR for TCP fallback:
BOSUN_LISTEN_ADDR=127.0.0.1:9999

# Secrets
SOPS_AGE_KEY_FILE=/etc/bosun/age-key.txt

# Alerts
DISCORD_WEBHOOK_URL=https://discord.com/api/webhooks/...
SENDGRID_API_KEY=...
TWILIO_ACCOUNT_SID=...

# Webhook container only
WEBHOOK_SECRET=github-webhook-secret
TRACKED_BRANCH=main
DAEMON_SOCKET=/var/run/bosun.sock
```

### Config File (Optional)

```yaml
# /etc/bosun/config.yaml
repo:
  url: git@github.com:user/infrastructure.git
  branch: main
  poll_interval: 1h

paths:
  state_dir: /var/lib/bosun
  compose_dir: compose/
  secrets_file: secrets.yaml.sops

daemon:
  socket_path: /var/run/bosun.sock

alerts:
  discord_webhook_url: ${DISCORD_WEBHOOK_URL}
```

---

## State and Recovery

### Persisted State

```
/var/lib/bosun/
├── state.json           # Current state, last commit, timestamps
├── last-successful.sha  # Last successfully deployed commit
├── last-attempted.sha   # Last attempted commit (may have failed)
└── snapshots/           # Pre-deploy compose file backups
    ├── 2024-01-15T10:30:00Z/
    │   ├── traefik.yml
    │   └── authelia.yml
    └── 2024-01-14T15:00:00Z/
        └── ...
```

### Crash Recovery

On startup, daemon checks:
1. Was there an incomplete reconcile? (last-attempted != last-successful)
2. If so, log warning and optionally alert
3. Next reconcile will fix state

---

## Observability

### Logging

Structured JSON to stdout (systemd captures to journal):

```json
{
  "level": "info",
  "ts": "2024-01-15T10:30:00Z",
  "msg": "Reconcile completed",
  "commit": "abc123",
  "duration_ms": 4523,
  "changed": ["traefik", "authelia"]
}
```

### Metrics (Prometheus)

```
# HELP bosun_reconcile_total Total reconciliations
# TYPE bosun_reconcile_total counter
bosun_reconcile_total{status="success"} 42
bosun_reconcile_total{status="failure"} 3

# HELP bosun_reconcile_duration_seconds Reconciliation duration
# TYPE bosun_reconcile_duration_seconds histogram
bosun_reconcile_duration_seconds_bucket{le="1"} 10
bosun_reconcile_duration_seconds_bucket{le="5"} 35
bosun_reconcile_duration_seconds_bucket{le="30"} 45

# HELP bosun_last_reconcile_timestamp Unix timestamp of last reconcile
# TYPE bosun_last_reconcile_timestamp gauge
bosun_last_reconcile_timestamp 1705315800
```

### Health Endpoints

```
GET /health → 200 OK (daemon is running)
GET /ready  → 200 OK (can accept reconciles) or 503 (busy/starting)
GET /status → 200 OK + JSON state
```

---

## Known Gaps and Future Work

### Identified in Review

1. **Concurrency model**: What happens when triggers overlap?
   - Plan: Queue with coalesce. If reconciling, mark pending. After completion, check pending and re-run if set.

2. **Rate limiting**: Webhook spam protection
   - Plan: Max 1 trigger per 30 seconds, debounce within window

3. **Multi-stack support**: Multiple compose files in one repo
   - Plan: v1 deploys all. Future: selective reconciliation based on changed paths.

4. **Rollback command**: `bosun rollback [commit]`
   - Plan: Snapshots exist, command to restore not yet implemented

5. **Dry run**: `bosun reconcile --dry-run`
   - Plan: Render templates, show diff, don't apply

### Out of Scope for v1

- Self-update mechanism (use package manager)
- Multi-repo support (one daemon per repo)
- Kubernetes support (use Flux/ArgoCD)
- Web UI (CLI and alerts only)

---

## Questions for Review

1. **Is the split architecture (host daemon + webhook container) the right call?** Or is monolith simpler for the target audience (homelabbers)?

2. **Is the socket-permissions-as-auth threat model sound?** Any edge cases where this breaks down?

3. **Should we support TCP at all?** Or just Unix socket and tell people to deal with it?

4. **Is go-systemd worth the dependency** for proper `Type=notify` support?

5. **What are we missing?** What will bite us in production that we haven't considered?

6. **Is this over-engineered for the problem space?** Homelabs are not production. Is simpler better?

---

## References

- [go-systemd for notify](https://github.com/coreos/go-systemd)
- [Graceful shutdown in Go](https://victoriametrics.com/blog/go-graceful-shutdown/)
- [OpenRC init scripts](https://wiki.alpinelinux.org/wiki/Writing_Init_Scripts)
- [Tailscale Unraid plugin (daemon pattern)](https://github.com/dkaser/unraid-tailscale)
- [Docker host.docker.internal](https://www.baeldung.com/ops/docker-compose-add-host)
- [Sidecar pattern](https://learn.microsoft.com/en-us/azure/architecture/patterns/sidecar)

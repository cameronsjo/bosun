# Bosun Split Architecture: Host Daemon + Webhook Container

## Overview

Split bosun into two components for resilience and flexibility:

1. **Host Daemon** (`bosun daemon`) - Runs on Unraid host, survives array stop/start
2. **Webhook Container** (`bosun-webhook`) - Optional, disposable, handles external triggers

```
┌─────────────────────────────────────────────────────────────────┐
│                         UNRAID HOST                              │
│                                                                  │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │                    bosun daemon                             │ │
│  │                    (plugin binary)                          │ │
│  │                                                             │ │
│  │  • Polling loop (configurable, default 1h)                  │ │
│  │  • Git clone/pull from config repo                          │ │
│  │  • SOPS decryption with age key                             │ │
│  │  • Go template rendering                                    │ │
│  │  • docker compose up/down                                   │ │
│  │  • Snapshot before deploy, rollback on failure              │ │
│  │  • Discord/SendGrid/Twilio alerting                         │ │
│  │                                                             │ │
│  │  APIs:                                                       │ │
│  │    Unix socket:  /var/run/bosun.sock (primary)              │ │
│  │      POST /trigger      - Trigger reconcile                 │ │
│  │      GET  /status       - Current state                     │ │
│  │      GET  /health       - Health check                      │ │
│  │    HTTP webhooks: localhost:8080 (webhook server)           │ │
│  │      POST /webhook/github  - GitHub push events             │ │
│  │      POST /webhook         - Generic HMAC webhook           │ │
│  │    Optional TCP API: 127.0.0.1:9090 (bearer-auth)          │ │
│  │      Enabled via BOSUN_ENABLE_TCP, disabled by default      │ │
│  │                                                             │ │
│  └────────────────────────────────────────────────────────────┘ │
│                              ▲                                   │
│                              │ POST /trigger                     │
│                              │                                   │
│  ┌───────────────────────────┴────────────────────────────────┐ │
│  │              bosun webhook (standalone receiver)             │ │
│  │                       OPTIONAL                              │ │
│  │                                                             │ │
│  │  • Handles GitHub, GitLab, Gitea, Bitbucket webhooks       │ │
│  │  • Provider-specific HMAC/token signature validation        │ │
│  │  • Forwards normalized triggers to daemon socket            │ │
│  │  • Connects via Unix socket or TCP API                      │ │
│  │                                                             │ │
│  │  Exposed via: Tailscale Funnel / Cloudflare Tunnel          │ │
│  └─────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
```

## Why Split?

| Concern | Single Container | Split Architecture |
|---------|------------------|-------------------|
| Array stopped | Bosun stops | Daemon keeps running |
| Webhook compromise | Full system access | Only trigger capability |
| Container restart | Reconcile interrupted | Daemon unaffected |
| Network issues | Polling stops | Daemon polls independently |
| Maintenance | All or nothing | Update webhook without touching daemon |

## Component Details

### Host Daemon

**Lifecycle:**
- Starts at boot via `/boot/config/go` or Unraid plugin
- Runs before Docker starts (can deploy containers)
- Survives array stop/start
- Graceful shutdown on SIGTERM

**Installation Options:**

1. **Plugin (recommended)**
   - `.plg` file in Community Applications
   - `/etc/rc.d/rc.bosun` for start/stop control
   - Proper upgrade/remove lifecycle
   - Binary at `/usr/local/emhttp/plugins/bosun/bin/bosun`

2. **User Script (simpler)**
   - Binary at `/boot/config/plugins/bosun/bosun`
   - Start from `/boot/config/go`: `setsid /boot/config/plugins/bosun/bosun daemon &`
   - Manual updates

**Configuration:**
```bash
# /boot/config/plugins/bosun/bosun.env
BOSUN_REPO_URL=git@github.com:user/infrastructure.git
BOSUN_REPO_BRANCH=main
BOSUN_POLL_INTERVAL=3600
BOSUN_SOCKET_PATH=/var/run/bosun.sock
BOSUN_ENABLE_TCP=false
SOPS_AGE_KEY_FILE=/boot/config/plugins/bosun/age-key.txt
DISCORD_WEBHOOK_URL=https://discord.com/api/webhooks/...
```

**State Directory:**
```
/boot/config/plugins/bosun/
├── bosun              # Binary
├── bosun.env          # Configuration
├── age-key.txt        # SOPS decryption key
├── ssh/               # Deploy keys for private repos
│   ├── id_ed25519
│   └── known_hosts
└── state/
    ├── last-commit    # Last deployed commit SHA
    ├── last-reconcile # Timestamp of last reconcile
    └── snapshots/     # Pre-deploy snapshots for rollback
```

### Webhook Container

**Purpose:** Thin HTTP relay that validates and forwards GitHub webhooks.

**Image:** `ghcr.io/cameronsjo/bosun-webhook:latest`

**Standalone Webhook Receiver:**

The `bosun webhook` command runs a standalone receiver that handles multiple Git providers. It validates signatures and forwards normalized triggers to the daemon.

```bash
bosun webhook --socket /var/run/bosun.sock --secret $WEBHOOK_SECRET
```

Or in docker-compose:

```yaml
services:
  bosun-webhook:
    image: ghcr.io/cameronsjo/bosun:latest
    entrypoint: ["bosun", "webhook", "--fetch-secret"]
    restart: unless-stopped
    environment:
      BOSUN_SOCKET_PATH: /var/run/bosun.sock
    volumes:
      - /var/run/bosun.sock:/var/run/bosun.sock  # Connect to daemon socket
    ports:
      - "8080:8080"  # Expose webhook receiver
```

**Endpoints:**
- `POST /webhook/github` - GitHub push events
- `POST /webhook/gitlab` - GitLab push events
- `POST /webhook/gitea` - Gitea push events
- `POST /webhook/bitbucket` - Bitbucket push events
- `GET /health` - Health check

**Flow:**
1. Provider sends webhook to `https://bosun.example.com/webhook/<provider>`
2. Receiver validates signature using provider-specific header
3. Receiver filters by branch (if configured)
4. Receiver forwards to daemon Unix socket via POST /trigger
5. Returns 202 Accepted to provider

**Important:** Do NOT point GitLab, Gitea, or Bitbucket webhooks directly at the daemon. They use provider-specific signature headers that the daemon's generic `/webhook` handler does not understand. Always use the standalone `bosun webhook` receiver for these providers.

**Security:**
- No access to Docker socket
- No access to secrets/keys
- No filesystem access
- Only capability: trigger reconcile via HTTP

## Communication Patterns

### Webhook Receiver → Daemon

The standalone `bosun webhook` receiver connects to the daemon via Unix socket or TCP API.

**Unix socket (recommended):**
```yaml
volumes:
  - /var/run/bosun.sock:/var/run/bosun.sock
```
Mount the socket into the receiver container. Secure by file permissions (0660).

**TCP API (optional, off by default):**
Enable with `BOSUN_ENABLE_TCP=true` in daemon config. Requires bearer token auth:
```bash
bosun webhook --tcp 127.0.0.1:9090 --token mytoken
```

### Daemon APIs

**Unix socket (primary, local-only):**
```bash
POST /trigger
GET  /status
GET  /health
```
File-based permissions (0660), kernel UID/PID logging. Always available.

**HTTP webhooks (port 8080, configurable):**
```
POST /webhook/github    - GitHub webhook
POST /webhook           - Generic HMAC (X-Signature or X-Hub-Signature-256)
GET  /health            - Health check
```
Accessible from outside (use reverse proxy for access control). GitLab/Gitea/Bitbucket should NOT be pointed at the daemon — use `bosun webhook` receiver instead.

**TCP API (optional, off by default):**
```
127.0.0.1:9090 (configurable via BOSUN_TCP_ADDR)
Requires Authorization: Bearer <token>
POST /trigger           - Trigger reconciliation
```
Enable with `BOSUN_ENABLE_TCP=true`. Localhost-only by default for safety.

## Failure Modes

| Scenario | Behavior |
|----------|----------|
| Webhook container down | Daemon continues polling |
| Daemon down | Webhook returns 502, no deploys |
| Git repo unreachable | Daemon logs error, retries next poll |
| Docker socket unavailable | Daemon logs error, skips compose operations |
| Invalid compose config | Snapshot restored, alert sent |
| SOPS decryption fails | Reconcile aborted, alert sent |

## Unraid Plugin Structure

If packaging as a proper Unraid plugin:

```
bosun.plg                           # Plugin definition
├── ENTITY: plugin, version, etc
├── FILE: bosun-<version>.txz       # Binary package
└── POST-INSTALL SCRIPT:
    - Extract binary to /usr/local/emhttp/plugins/bosun/
    - Create symlink: /usr/local/sbin/bosun
    - Create rc.bosun in /etc/rc.d/
    - Start daemon: /etc/rc.d/rc.bosun start

/etc/rc.d/rc.bosun                  # Service control script
├── start: setsid /usr/local/sbin/bosun daemon &
├── stop: pkill -f "bosun daemon"
├── status: pgrep -f "bosun daemon"
└── restart: stop && start

/boot/config/plugins/bosun/         # Persistent config (on flash)
├── bosun.cfg                       # Settings from WebUI
├── age-key.txt                     # SOPS key
└── ssh/                            # Deploy keys
```

## Migration Path

### From Current Single-Container

1. Deploy daemon on host (via go script or plugin)
2. Configure daemon with same env vars
3. Test polling works
4. Deploy webhook container pointing to daemon
5. Update GitHub webhook URL
6. Remove old bosun container

### Rollback

1. Stop daemon
2. Deploy old single-container bosun
3. Update GitHub webhook URL

## Open Questions

1. **Plugin vs User Script?**
   - Plugin: Proper install/remove, WebUI settings, CA listing
   - User Script: Simpler, faster to iterate, no CA approval process

2. **Trigger Authentication?**
   - Localhost-only (no auth needed)?
   - Bearer token for extra safety?
   - Same webhook secret as GitHub validation?

3. **WebUI Integration?**
   - Status page showing last reconcile, current state?
   - Manual trigger button?
   - Log viewer?

4. **Multiple Repos?**
   - Single daemon, multiple repo configs?
   - Multiple daemon instances?

## References

- [Tailscale Plugin Pattern](https://github.com/dkaser/unraid-tailscale)
- [Unraid /boot/config/go](https://forums.unraid.net/topic/95227-is-bootconfiggo-still-used/)
- [host.docker.internal on Linux](https://www.baeldung.com/ops/docker-compose-add-host)
- [Unraid Plugin Development](https://forums.unraid.net/topic/52623-plugin-system-documentation/)

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
│  │  HTTP API (localhost:9999):                                 │ │
│  │    POST /trigger      - Trigger reconcile                   │ │
│  │    GET  /health       - Health check                        │ │
│  │    GET  /status       - Current state, last reconcile       │ │
│  │                                                             │ │
│  └────────────────────────────────────────────────────────────┘ │
│                              ▲                                   │
│                              │ POST /trigger                     │
│                              │                                   │
│  ┌───────────────────────────┴────────────────────────────────┐ │
│  │              bosun-webhook (Docker container)               │ │
│  │                       OPTIONAL                              │ │
│  │                                                             │ │
│  │  • Receives GitHub webhook POST                             │ │
│  │  • HMAC-SHA256 signature validation                         │ │
│  │  • Filters by branch (only trigger on tracked branch)       │ │
│  │  • Calls daemon at host.docker.internal:9999/trigger        │ │
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
BOSUN_TRIGGER_PORT=9999
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

**docker-compose.yml:**
```yaml
services:
  bosun-webhook:
    image: ghcr.io/cameronsjo/bosun-webhook:latest
    container_name: bosun-webhook
    restart: unless-stopped
    environment:
      WEBHOOK_SECRET: ${WEBHOOK_SECRET}
      DAEMON_URL: http://host.docker.internal:9999
      TRACKED_BRANCH: main
    extra_hosts:
      - "host.docker.internal:host-gateway"
    ports:
      - "8080:8080"  # Or expose via Tailscale/Cloudflare
    # No volumes needed - stateless
```

**Endpoints:**
- `POST /webhook/github` - Receives GitHub push events
- `GET /health` - Container health check

**Flow:**
1. GitHub sends push webhook to `https://bosun.example.com/webhook/github`
2. Container validates `X-Hub-Signature-256` with `WEBHOOK_SECRET`
3. Container checks if push is to `TRACKED_BRANCH`
4. Container POSTs to `DAEMON_URL/trigger`
5. Returns 202 Accepted to GitHub

**Security:**
- No access to Docker socket
- No access to secrets/keys
- No filesystem access
- Only capability: trigger reconcile via HTTP

## Communication Patterns

### Container → Host Daemon

**Option 1: host.docker.internal (recommended)**
```yaml
extra_hosts:
  - "host.docker.internal:host-gateway"
environment:
  DAEMON_URL: http://host.docker.internal:9999
```
Requires Docker 20.10+

**Option 2: Bridge Gateway IP**
```yaml
environment:
  DAEMON_URL: http://172.17.0.1:9999
```
Works on older Docker, but IP may vary.

**Option 3: Host Network Mode**
```yaml
network_mode: host
environment:
  DAEMON_URL: http://localhost:9999
```
Loses network isolation.

### Daemon Trigger API

Simple authenticated trigger endpoint:

```
POST /trigger
Authorization: Bearer <trigger-token>

Response: 202 Accepted
{
  "status": "accepted",
  "message": "Reconcile triggered"
}
```

Or simpler - localhost-only, no auth needed (can't be reached externally):
```
POST /trigger

Response: 202 Accepted
```

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

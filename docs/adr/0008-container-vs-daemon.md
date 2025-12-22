# ADR-0008: Container vs Daemon Deployment

## Status

Accepted

## Context

unops components need to run reliably on the server. The question: should everything be a Docker container, or should some components run as native daemons (systemd services)?

**The chicken-and-egg problem:**
```
Bosun runs in Docker
         ↓
Bosun runs `docker compose up`
         ↓
Docker crashes
         ↓
Bosun dies with Docker
         ↓
Nothing to restart Docker or bosun
         ↓
Manual SSH intervention required
```

## Decision

**Default: All containers.** Optional: Bosun as systemd service for advanced users.

```
┌─────────────────────────────────────────────────────────────────────────────-┐
│                           Deployment Options                                 │
├─────────────────────────────────────────────────────────────────────────────-┤
│                                                                              │
│  Option A: All Containers (Default)         Option B: Hybrid (Advanced)      │
│  ─────────────────────────────              ────────────────────────────     │
│                                                                              │
│  ┌─────────────────────────┐               ┌─────────────────────────┐       │
│  │        Docker           │               │        systemd          │       │
│  │  ┌───────────────────┐  │               │  ┌───────────────────┐  │       │
│  │  │      Bosun        │  │               │  │      Bosun        │  │       │
│  │  │    Tailscale      │  │               │  │    (native)       │  │       │
│  │  │    Agentgateway   │  │               │  └───────────────────┘  │       │
│  │  │    MCP Servers    │  │               │  ┌───────────────────┐  │       │
│  │  │    Your Apps      │  │               │  │    Tailscale      │  │       │
│  │  └───────────────────┘  │               │  │    (native)       │  │       │
│  └─────────────────────────┘               │  └───────────────────┘  │       │
│                                            └───────────┬─────────────┘       │
│  Simpler. Consistent.                                  │                     │
│  Single deployment model.                   ┌──────────▼──────────┐          │
│                                             │       Docker        │          │
│  Risk: Docker crash = all down              │  ┌───────────────┐  │          │
│                                             │  │ Agentgateway  │  │          │
│                                             │  │ MCP Servers   │  │          │
│                                             │  │ Your Apps     │  │          │
│                                             │  └───────────────┘  │          │
│                                             └─────────────────────┘          │
│                                                                              │
│                                             More resilient.                  │
│                                             Bosun survives Docker crash.     │
│                                                                              │
└──────────────────────────────────────────────────────────────────────────-───┘
```

## Component Analysis

| Component | Container | Daemon | Recommendation |
|-----------|-----------|--------|----------------|
| **Bosun** | Easy deploy, dies with Docker | Survives crashes, can restart Docker | Container (default), daemon (opt-in) |
| **Tailscale** | Works with state volume | More robust, system integration | Container (default), daemon (advanced) |
| **Cloudflared** | Standard deployment | Unnecessary complexity | Container only |
| **Agentgateway** | Standard deployment | No benefit | Container only |
| **MCP Servers** | Standard deployment | No benefit | Container only |
| **Your Apps** | Standard deployment | No benefit | Container only |

## Why Default to Containers

### 1. Consistency
One deployment model. One set of tools. One mental model.

### 2. Platform Support
- Unraid: Designed around Docker, native daemons are awkward
- Synology: Same story
- TrueNAS: Same story
- Generic Linux: Either works

### 3. Updates
```bash
# Container: trivial
docker pull bosun:latest && docker compose up -d

# Daemon: more steps
systemctl stop bosun
curl -L ... -o /usr/local/bin/bosun
systemctl start bosun
```

### 4. Rollback
```bash
# Container: trivial
docker compose up -d --force-recreate  # uses previous image

# Daemon: hope you kept the old binary
```

### 5. Dependencies
Container bundles everything: sops, age, chezmoi, webhook, git.
Daemon requires installing each on the host.

## Why Offer Daemon Option

### 1. Resilience
Bosun can survive Docker crashes and potentially restart Docker:

```bash
# bosun.service can include:
ExecStartPre=/usr/bin/systemctl start docker
```

### 2. Bootstrap
Daemon can start before Docker, ensuring network (Tailscale) is ready.

### 3. Recovery
If Docker corrupts, daemon can:
- Pull fresh images
- Rebuild containers
- Send alerts

## Implementation

### Option A: Container (Default)

```yaml
# bosun/docker-compose.yml
services:
  bosun:
    image: ghcr.io/unops/bosun:latest
    restart: always
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - ./config:/config:ro
    # ... rest of config
```

Docker's `restart: always` handles most failure cases.

### Option B: Systemd Service (Advanced)

```bash
# /etc/systemd/system/bosun.service
[Unit]
Description=unops Bosun
After=network-online.target
Wants=network-online.target
Before=docker.service

[Service]
Type=simple
ExecStart=/usr/local/bin/bosun serve
Restart=always
RestartSec=5
Environment=SOPS_AGE_KEY_FILE=/etc/bosun/age-key.txt

[Install]
WantedBy=multi-user.target
```

```bash
# Installation
curl -L https://github.com/unops/bosun/releases/latest/download/bosun-linux-amd64 \
  -o /usr/local/bin/bosun
chmod +x /usr/local/bin/bosun
systemctl enable --now bosun
```

### Tailscale: Native vs Container

**Container (default):**
```yaml
services:
  tailscale:
    image: tailscale/tailscale:latest
    volumes:
      - tailscale-state:/var/lib/tailscale
    # ...
```

**Native (advanced):**
```bash
curl -fsSL https://tailscale.com/install.sh | sh
tailscale up --auth-key=$TS_AUTHKEY
```

Native Tailscale survives Docker restarts and can provide network to bosun daemon.

## Failure Scenarios

| Scenario | All Containers | Hybrid |
|----------|---------------|--------|
| Container crash | Docker restarts it | Docker restarts it |
| Docker crash | Everything down, manual recovery | Bosun alive, can restart Docker |
| Docker hang | Stuck, manual recovery | Bosun can detect and restart Docker |
| Host reboot | Docker starts, containers start | systemd starts bosun, then Docker |
| Disk full | Depends on which disk | Same |
| OOM killer | Might kill bosun | Might kill bosun |

## Recommendation

1. **Start with containers.** Simpler, works for 95% of cases.
2. **If you need maximum resilience**, install bosun as systemd service.
3. **If you want Tailscale to survive Docker**, install Tailscale natively.

The architecture supports both. Choose based on your reliability requirements.

## Future: Bosun Binary

If demand exists, ship bosun as:
- Docker image (default)
- Static binary for Linux amd64/arm64
- systemd unit file
- Installation script

```bash
# Future installation
curl -fsSL https://unops.dev/install.sh | sh
# Detects platform, installs binary or container as appropriate
```

## References

- [Docker restart policies](https://docs.docker.com/config/containers/start-containers-automatically/)
- [Tailscale in Docker](https://tailscale.com/kb/1282/docker)
- [systemd service files](https://www.freedesktop.org/software/systemd/man/systemd.service.html)

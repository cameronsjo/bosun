# ADR-0008: Container vs Daemon Deployment

## Status

Accepted

## Context

unops components need to run reliably on the server. The question: should everything be a Docker container, or should some components run as native daemons (systemd services)?

**The chicken-and-egg problem:**
```
Conductor runs in Docker
         ↓
Conductor runs `docker compose up`
         ↓
Docker crashes
         ↓
Conductor dies with Docker
         ↓
Nothing to restart Docker or conductor
         ↓
Manual SSH intervention required
```

## Decision

**Default: All containers.** Optional: Conductor as systemd service for advanced users.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Deployment Options                                 │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  Option A: All Containers (Default)         Option B: Hybrid (Advanced)     │
│  ─────────────────────────────              ────────────────────────────    │
│                                                                              │
│  ┌─────────────────────────┐               ┌─────────────────────────┐      │
│  │        Docker           │               │        systemd          │      │
│  │  ┌───────────────────┐  │               │  ┌───────────────────┐  │      │
│  │  │    Conductor      │  │               │  │    Conductor      │  │      │
│  │  │    Tailscale      │  │               │  │    (native)       │  │      │
│  │  │    Agentgateway   │  │               │  └───────────────────┘  │      │
│  │  │    MCP Servers    │  │               │  ┌───────────────────┐  │      │
│  │  │    Your Apps      │  │               │  │    Tailscale      │  │      │
│  │  └───────────────────┘  │               │  │    (native)       │  │      │
│  └─────────────────────────┘               │  └───────────────────┘  │      │
│                                            └───────────┬─────────────┘      │
│  Simpler. Consistent.                                  │                    │
│  Single deployment model.                   ┌──────────▼──────────┐         │
│                                             │       Docker        │         │
│  Risk: Docker crash = all down              │  ┌───────────────┐  │         │
│                                             │  │ Agentgateway  │  │         │
│                                             │  │ MCP Servers   │  │         │
│                                             │  │ Your Apps     │  │         │
│                                             │  └───────────────┘  │         │
│                                             └─────────────────────┘         │
│                                                                              │
│                                             More resilient.                  │
│                                             Conductor survives Docker crash. │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Component Analysis

| Component | Container | Daemon | Recommendation |
|-----------|-----------|--------|----------------|
| **Conductor** | Easy deploy, dies with Docker | Survives crashes, can restart Docker | Container (default), daemon (opt-in) |
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
docker pull conductor:latest && docker compose up -d

# Daemon: more steps
systemctl stop conductor
curl -L ... -o /usr/local/bin/conductor
systemctl start conductor
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
Conductor can survive Docker crashes and potentially restart Docker:

```bash
# conductor.service can include:
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
# conductor/docker-compose.yml
services:
  conductor:
    image: ghcr.io/unops/conductor:latest
    restart: always
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - ./config:/config:ro
    # ... rest of config
```

Docker's `restart: always` handles most failure cases.

### Option B: Systemd Service (Advanced)

```bash
# /etc/systemd/system/conductor.service
[Unit]
Description=unops Conductor
After=network-online.target
Wants=network-online.target
Before=docker.service

[Service]
Type=simple
ExecStart=/usr/local/bin/conductor serve
Restart=always
RestartSec=5
Environment=SOPS_AGE_KEY_FILE=/etc/conductor/age-key.txt

[Install]
WantedBy=multi-user.target
```

```bash
# Installation
curl -L https://github.com/unops/conductor/releases/latest/download/conductor-linux-amd64 \
  -o /usr/local/bin/conductor
chmod +x /usr/local/bin/conductor
systemctl enable --now conductor
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

Native Tailscale survives Docker restarts and can provide network to conductor daemon.

## Failure Scenarios

| Scenario | All Containers | Hybrid |
|----------|---------------|--------|
| Container crash | Docker restarts it | Docker restarts it |
| Docker crash | Everything down, manual recovery | Conductor alive, can restart Docker |
| Docker hang | Stuck, manual recovery | Conductor can detect and restart Docker |
| Host reboot | Docker starts, containers start | systemd starts conductor, then Docker |
| Disk full | Depends on which disk | Same |
| OOM killer | Might kill conductor | Might kill conductor |

## Recommendation

1. **Start with containers.** Simpler, works for 95% of cases.
2. **If you need maximum resilience**, install conductor as systemd service.
3. **If you want Tailscale to survive Docker**, install Tailscale natively.

The architecture supports both. Choose based on your reliability requirements.

## Future: Conductor Binary

If demand exists, ship conductor as:
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

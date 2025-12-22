# bosun

**Helm for home.**

---

## The Story

You're the captain of your homelab. 40 containers. Traefik. Secrets everywhere. It's a lot.

You shouldn't have to swab the deck yourself.

That's what the bosun is for.

Push your orders to GitHub. Bosun handles the rest—wrangling containers, managing secrets, keeping everything ship-shape while you're topside sipping rosé and pretending your homelab didn't just have a meltdown at 2 AM.

```
git push → bosun receives orders → crew deployed → yacht runs smooth
```

No Kubernetes. No drama. Just smooth sailing.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              Your Yacht (Server)                             │
│                                                                              │
│  ┌──────────────────────────────────────────────────────────────────────┐   │
│  │                             Bosun                                     │   │
│  │  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐    │   │
│  │  │ Radio   │→ │  Fetch  │→ │ Decrypt │→ │ Prep    │→ │ Deploy  │    │   │
│  │  │(Webhook)│  │ Orders  │  │ Secrets │  │ Configs │  │  Crew   │    │   │
│  │  └─────────┘  └─────────┘  └─────────┘  └─────────┘  └─────────┘    │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
│         ▲                                                    │              │
│         │                                                    ▼              │
│  ┌──────┴──────┐                                    ┌───────────────┐       │
│  │  Tailscale  │                                    │  Your Crew    │       │
│  │   (Radio)   │                                    │ (Containers)  │       │
│  └──────┬──────┘                                    │ traefik nginx │       │
│         │                                           │ postgres redis│       │
└─────────│───────────────────────────────────────────└───────────────┘───────┘
          │
          ▼
    ┌──────────┐
    │ Captain  │
    │ (GitHub) │
    └──────────┘
```

---

## What's On Board

| Role | What They Do |
|------|--------------|
| **Bosun** | Receives orders, decrypts secrets, deploys containers. The whole operation. |
| **Manifest** | Crew provisioning. Write 10 lines, generate compose + Traefik + Gatus configs. |
| **Provisions** | Tailscale/Cloudflare tunnels, Authelia auth, Watchtower, Agentgateway. Swappable. |

## Philosophy

- **Captain gives orders, bosun executes.** Push to git, everything updates.
- **No drama below deck.** ~100 lines of shell beats 10,000 lines of Go.
- **Every crew member has a backup.** Batteries included, all swappable.
- **One yacht, many ports.** Monorepo support for multi-server setups.

---

## Quick Start

### 1. Provision secrets

```bash
# Generate encryption keys
age-keygen -o ~/.config/sops/age/keys.txt

# Create .sops.yaml
cat > .sops.yaml << 'EOF'
creation_rules:
  - path_regex: .*\.yaml$
    age: <your-public-key>
EOF

# Encrypt the guest list
sops -e secrets.yaml > secrets.yaml.sops
```

### 2. Write the manifest

```yaml
# compose/myapp.yml.tmpl
{{- $secrets := fromJson (env "SOPS_SECRETS") -}}
services:
  myapp:
    image: myapp:latest
    environment:
      API_KEY: {{ $secrets.auth.api_key }}
```

### 3. Hire the bosun

```bash
docker compose -f bosun/docker-compose.yml up -d
```

### 4. Give orders

```bash
git add . && git commit -m "deploy the fleet" && git push
# Webhook fires → bosun pulls → secrets decrypt → crew deployed
```

---

## The Bosun

The bosun runs the deck. Responsibilities:
- Receives orders via webhook (or checks in hourly)
- Pulls the latest manifest from GitHub
- Decrypts secrets with SOPS + Age
- Preps configs with Chezmoi templates
- Deploys crew via `docker compose up -d`

```
bosun/
├── Dockerfile           # Alpine + sops + age + chezmoi + webhook
├── docker-compose.yml
├── hooks.yaml           # Radio configuration
└── scripts/
    ├── entrypoint.sh    # Morning briefing
    ├── reconcile.sh     # The actual work (~100 lines)
    ├── healthcheck.sh   # Status check
    └── notify.sh        # Discord alerts
```

## The Manifest (Crew Provisioning)

Write 10 lines, deploy a full crew:

```
┌────────────────┐     ┌────────────────┐
│ Crew Manifest  │     │   Positions    │
│   (~10 lines)  │     │  (reusable)    │
│                │     │                │
│ name: myapp    │     │ - deckhand     │
│ positions: [..]│     │ - interior     │
│ config:        │     │ - chef         │
│   port: 3000   │     │ - engineer     │
└───────┬────────┘     └───────┬────────┘
        │                      │
        └──────────┬───────────┘
                   │
                   ▼
          ┌────────────────┐
          │    manifest    │
          │    render      │
          └────────┬───────┘
                   │
        ┌──────────┼──────────┐
        ▼          ▼          ▼
┌──────────┐ ┌──────────┐ ┌──────────┐
│ compose/ │ │ traefik/ │ │  gatus/  │
│ crew.yml │ │routes.yml│ │ watch.yml│
└──────────┘ └──────────┘ └──────────┘
```

```yaml
# manifest/services/myapp.yml
name: myapp
positions: [deckhand, interior, engineer]
config:
  image: ghcr.io/org/myapp:latest
  port: 3000
  subdomain: myapp
  domain: example.com
  group: Apps
services:
  postgres:
    version: 17
    db: myapp
    db_password: "{{ $secrets.apps.myapp.db_password }}"
```

```bash
cd manifest
uv run manifest.py render stacks/apps.yml
```

---

## Comms (Tunnel Options)

The yacht needs a radio. Pick one:

| Radio | Setup | Custom Domain | Features |
|-------|-------|---------------|----------|
| **Tailscale Funnel** | 1 command | No (*.ts.net) | Zero config |
| **Cloudflare Tunnel** | Dashboard + token | Yes | DDoS, caching |

See [ADR-0005: Tunnel Providers](docs/adr/0005-tunnel-providers.md).

---

## Crew Rotation (Image Updates)

Two ways to rotate crew:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                    Manifest Changes (New Orders)                             │
│                                                                              │
│    Edit YAML ──→ git push ──→ Radio ──→ Bosun ──→ docker compose up         │
│                                                                              │
│    Use for: compose files, routes, secrets, environment                     │
└─────────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────────┐
│                    Crew Upgrades (New Uniforms)                              │
│                                                                              │
│    Code push ──→ CI Build ──→ Registry ──→ Watchtower ──→ docker pull       │
│                                                                              │
│    Use for: application code, dependencies, runtime updates                 │
└─────────────────────────────────────────────────────────────────────────────┘
```

See [ADR-0002: Watchtower Webhook](docs/adr/0002-watchtower-webhook-deploy.md).

---

## Fleet Management (Multi-Server)

One captain, many yachts. Hub receives orders, dispatches to fleet:

```
                         ┌──────────────┐
                         │   Captain    │
                         │   (GitHub)   │
                         └──────┬───────┘
                                │
                                ▼
                    ┌───────────────────────┐
                    │    Harbor Master      │
                    │    (Hub Bosun)        │
                    │                       │
                    │  routes by manifest:  │
                    │  yachts/main/*   → A  │
                    │  yachts/backup/* → B  │
                    │  shared/*        → *  │
                    └───────────┬───────────┘
                                │
              ┌─────────────────┼─────────────────┐
              │                 │                 │
              ▼                 ▼                 ▼
      ┌───────────────┐ ┌───────────────┐ ┌───────────────┐
      │  Main Yacht   │ │ Backup Yacht  │ │   Tender      │
      │   (Unraid)    │ │    (VPS)      │ │ (Pi Cluster)  │
      └───────────────┘ └───────────────┘ └───────────────┘
```

See [ADR-0004: Multi-Server Monorepo](docs/adr/0004-multi-server-monorepo.md).

---

## Ship's Log (Documentation)

| ADR | Status | Summary |
|-----|--------|---------|
| [0001: Manifest System](docs/adr/0001-service-composer.md) | Accepted | DRY crew provisioning |
| [0002: Watchtower Webhook](docs/adr/0002-watchtower-webhook-deploy.md) | Accepted | Crew rotation automation |
| [0003: Dagger for Bosun](docs/adr/0003-dagger-for-conductor.md) | Deferred | Shell scripts sufficient |
| [0004: Fleet Management](docs/adr/0004-multi-server-monorepo.md) | Proposed | Multi-yacht architecture |
| [0005: Radio Options](docs/adr/0005-tunnel-providers.md) | Accepted | Tailscale vs Cloudflare |
| [0006: Bosun Auth](docs/adr/0006-conductor-authentication.md) | Proposed | Authelia integration |
| [0007: Agentgateway](docs/adr/0007-agentgateway-mcp-proxy.md) | Draft | MCP via Agentgateway |
| [0008: Container vs Daemon](docs/adr/0008-container-vs-daemon.md) | Accepted | When to use systemd |
| [0009: Unraid Community Apps](docs/adr/0009-unraid-community-apps.md) | Evaluating | CA registration |

---

## Requirements

- Docker + Docker Compose
- Linux server (tested: Unraid, Debian, Ubuntu)
- GitHub repo for manifests
- Age key for encryption
- (Optional) Tailscale or Cloudflare account

## Guides

- [Unraid Setup Guide](docs/guides/unraid-setup.md) - Complete walkthrough
- [Unraid Templates](unraid-templates/) - Community Apps templates

---

## Prior Art & Provisions

- [Helm](https://helm.sh/) - The Kubernetes package manager we're simplifying
- [Flux](https://fluxcd.io/) - GitOps for Kubernetes
- [ArgoCD](https://argoproj.github.io/cd/) - GitOps for Kubernetes
- [Watchtower](https://containrrr.dev/watchtower/) - Container image updates
- [SOPS](https://github.com/getsops/sops) - Secrets encryption
- [Chezmoi](https://www.chezmoi.io/) - Template management

---

## Support

If bosun keeps your yacht running smooth, consider buying the crew a coffee:

[![Ko-fi](https://ko-fi.com/img/githubbutton_sm.svg)](https://ko-fi.com/cameronsjo)

---

## License

MIT

---

*No tip required. But we appreciate a star.*

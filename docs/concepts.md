# Bosun Concepts

## Architecture

<!-- site-diagram: architecture -->
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

## Components

| Component | Role |
|-----------|------|
| **Bosun** | Receives orders, decrypts secrets, deploys containers |
| **Manifest** | Crew provisioning - generate compose/traefik/gatus from 10-line service definitions |
| **Provisions** | Reusable config templates (container, database, routing, monitoring) |

## The Bosun

The bosun runs the deck:
- Receives orders via webhook (or checks in hourly)
- Pulls the latest manifest from GitHub
- Decrypts secrets with SOPS + Age
- Preps configs with Go templates + Sprig functions
- Deploys crew via `docker compose up -d`

```
bosun/
├── Dockerfile           # Alpine + sops + age + webhook
├── docker-compose.yml
├── hooks.yaml           # Radio configuration
└── scripts/
    ├── entrypoint.sh    # Morning briefing
    ├── reconcile.sh     # The actual work (~100 lines)
    ├── healthcheck.sh   # Status check
    └── notify.sh        # Discord alerts
```

## The Manifest

Write 10 lines, deploy a full crew:

```
┌────────────────┐     ┌────────────────┐
│ Crew Manifest  │     │  Provisions    │
│   (~10 lines)  │     │  (reusable)    │
│                │     │                │
│ name: myapp    │     │ - container    │
│ provisions: .. │     │ - database     │
│ config:        │     │ - routing      │
│   port: 3000   │     │ - monitoring   │
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

Example service manifest:

```yaml
# manifest/services/myapp.yml
name: myapp
provisions: [container, reverse-proxy, postgres, monitoring]
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

## Crew Rotation (Image Updates)

Two deployment paths:

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

**Known gap**: These two update mechanisms are decoupled. Watchtower pulls new images independently of Bosun's GitOps loop. If a new image requires a config change, those updates happen through different pipelines. A per-service `updatePolicy` (pinned vs auto) is planned to let Bosun own both image updates and config updates, making Watchtower optional. See the [image update policy issue](https://github.com/cameronsjo/bosun/issues?q=bosun-djh).

## Fleet Management (Multi-Server)

One captain, many yachts:

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

## Tunnel Options

The yacht needs a radio:

| Radio | Setup | Custom Domain | Features |
|-------|-------|---------------|----------|
| **Tailscale Funnel** | 1 command | No (*.ts.net) | Zero config |
| **Cloudflare Tunnel** | Dashboard + token | Yes | DDoS, caching |

See [ADR-0005: Tunnel Providers](adr/0005-tunnel-providers.md).

# unops

**Helm for home.** GitOps for Docker Compose on bare metal.

---

## The Backstory

You've got a homelab. Maybe an Unraid box, a Raspberry Pi cluster, or an old Dell OptiPlex running Debian. You're running 20, 40, maybe 60 containers. Life is good.

Then you make a change. And another. Configs drift. You SSH in "just this once" to fix something. That fix becomes permanent. Three months later, you can't remember what you changed or why.

You look at Kubernetes. ArgoCD. Flux. They're beautiful. They're also designed for teams of 50 running thousands of pods across multiple clusters. Your homelab has one node. You don't need a control plane. You need `docker compose up`.

**unops is the missing piece.**

It's GitOps without the Kubernetes tax. Push to GitHub, your server updates. Encrypted secrets with SOPS. Templated configs with Chezmoi. Instant deploys via webhook. Daily polling as fallback.

```
git push → webhook → decrypt → render → docker compose up
```

That's it. That's the whole thing.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              Your Server                                     │
│                                                                              │
│  ┌──────────────────────────────────────────────────────────────────────┐   │
│  │                           Conductor                                   │   │
│  │  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐    │   │
│  │  │ Webhook │→ │Git Pull │→ │  SOPS   │→ │ Chezmoi │→ │ Compose │    │   │
│  │  │Receiver │  │         │  │ Decrypt │  │ Render  │  │   Up    │    │   │
│  │  └─────────┘  └─────────┘  └─────────┘  └─────────┘  └─────────┘    │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
│         ▲                                                    │              │
│         │                                                    ▼              │
│  ┌──────┴──────┐                                    ┌───────────────┐       │
│  │  Tailscale  │                                    │Your Containers│       │
│  │   Funnel    │                                    │  app1  app2   │       │
│  └──────┬──────┘                                    │  app3  app4   │       │
│         │                                           └───────────────┘       │
└─────────│───────────────────────────────────────────────────────────────────┘
          │
          ▼
    ┌──────────┐
    │  GitHub  │
    │ Webhooks │
    └──────────┘
```

---

## What You Get

| Component | Purpose |
|-----------|---------|
| **Conductor** | Orchestrates deployments. Webhook receiver, reconciliation loop, secret decryption. |
| **Composer** | DRY service definitions. Write 10 lines, generate compose + Traefik + Gatus configs. |
| **Batteries** | Tailscale/Cloudflare tunnels, Authelia auth, Watchtower, Agentgateway for MCP. Swappable. |

## Philosophy

- **Batteries included, batteries swappable.** Defaults work. Replace any component.
- **Shell scripts over frameworks.** ~100 lines of bash beats 10,000 lines of Go for this use case.
- **Escape hatches everywhere.** Raw passthrough when abstractions don't fit.
- **One repo, many servers.** Monorepo with hub-and-spoke for multi-server setups.

---

## Quick Start

### 1. Set up secrets

```bash
# Generate Age key
age-keygen -o ~/.config/sops/age/keys.txt

# Create .sops.yaml
cat > .sops.yaml << 'EOF'
creation_rules:
  - path_regex: .*\.yaml$
    age: <your-public-key>
EOF

# Encrypt secrets
sops -e secrets.yaml > secrets.yaml.sops
```

### 2. Create a compose template

```yaml
# compose/myapp.yml.tmpl
{{- $secrets := fromJson (env "SOPS_SECRETS") -}}
services:
  myapp:
    image: myapp:latest
    environment:
      API_KEY: {{ $secrets.auth.api_key }}
```

### 3. Deploy the conductor

```bash
docker compose -f conductor/docker-compose.yml up -d
```

### 4. Push and watch

```bash
git add . && git commit -m "deploy myapp" && git push
# Webhook fires → conductor pulls → secrets decrypt → compose up
```

---

## Components

### Conductor

The conductor leads the orchestra. It:
- Receives GitHub webhooks (or polls hourly as fallback)
- Pulls the latest configs
- Decrypts secrets with SOPS + Age
- Renders templates with Chezmoi
- Runs `docker compose up -d`

```
conductor/
├── Dockerfile           # Alpine + sops + age + chezmoi + webhook
├── docker-compose.yml
├── hooks.yaml           # Webhook configuration
└── scripts/
    ├── entrypoint.sh    # Container startup
    ├── reconcile.sh     # Core sync logic (~100 lines)
    ├── healthcheck.sh
    └── notify.sh        # Discord notifications
```

### Composer

Write 10 lines, generate 100. The composer turns service manifests into complete configs:

```
┌────────────────┐     ┌────────────────┐
│ Service Manifest│     │    Profiles    │
│   (~10 lines)  │     │   (reusable)   │
│                │     │                │
│ name: myapp    │     │ - container    │
│ profiles: [...]│     │ - healthcheck  │
│ config:        │     │ - reverse-proxy│
│   port: 3000   │     │ - postgres     │
└───────┬────────┘     └───────┬────────┘
        │                      │
        └──────────┬───────────┘
                   │
                   ▼
          ┌────────────────┐
          │   compose.py   │
          │    render      │
          └────────┬───────┘
                   │
        ┌──────────┼──────────┐
        ▼          ▼          ▼
┌──────────┐ ┌──────────┐ ┌──────────┐
│ compose/ │ │ traefik/ │ │  gatus/  │
│ myapp.yml│ │dynamic.yml│ │endpoints │
│          │ │          │ │   .yml   │
│ services │ │ routers  │ │ monitors │
│ networks │ │ services │ │ alerts   │
│ volumes  │ │ tls      │ │          │
└──────────┘ └──────────┘ └──────────┘
```

```yaml
# services/myapp.yml
name: myapp
profiles: [container, healthcheck, reverse-proxy, monitoring]
config:
  image: ghcr.io/org/myapp:latest
  port: 3000
  subdomain: myapp
  domain: example.com
services:
  postgres:
    version: 17
    db: myapp
```

Outputs:
- `output/compose/myapp.yml` - Docker Compose with healthchecks, networks, sidecars
- `output/traefik/dynamic.yml` - Traefik routers and services
- `output/gatus/endpoints.yml` - Monitoring endpoints

```bash
cd composer
uv run compose.py render stacks/apps.yml
```

---

## Tunnel Options

Expose webhooks securely. Pick one:

| Provider | Setup | Custom Domain | Extra Features |
|----------|-------|---------------|----------------|
| **Tailscale Funnel** | 1 command | No (*.ts.net) | Zero config, built-in |
| **Cloudflare Tunnel** | Dashboard + token | Yes | DDoS, caching, Access |

See [ADR-0005: Tunnel Providers](docs/adr/0005-tunnel-providers.md).

---

## Image Updates

Two patterns for two use cases:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                        Config Changes (GitOps)                               │
│                                                                              │
│    Edit YAML ──→ git push ──→ Webhook ──→ Conductor ──→ docker compose up   │
│                                                                              │
│    Use for: compose files, traefik routes, secrets, environment vars        │
└─────────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────────┐
│                        Code Changes (Watchtower)                             │
│                                                                              │
│    Code push ──→ CI Build ──→ GHCR ──→ Watchtower API ──→ docker pull/up    │
│                                                                              │
│    Use for: application code, dependencies, runtime updates                 │
└─────────────────────────────────────────────────────────────────────────────┘
```

| Pattern | Trigger | Use Case |
|---------|---------|----------|
| **GitOps** | Config change in repo | Infrastructure, compose files, secrets |
| **Watchtower** | Image push to GHCR | App code changes |

For app code (not config), trigger Watchtower after CI builds:

```yaml
# .github/workflows/deploy.yml
- name: Trigger Watchtower
  run: |
    curl -X POST -H "Authorization: Bearer ${{ secrets.WATCHTOWER_TOKEN }}" \
      "${{ secrets.WATCHTOWER_URL }}?images=ghcr.io/org/myapp"
```

See [ADR-0002: Watchtower Webhook](docs/adr/0002-watchtower-webhook-deploy.md).

---

## Multi-Server (Future)

One repo, many servers. Hub receives webhooks, broadcasts to server conductors:

```
                         ┌──────────────┐
                         │    GitHub    │
                         │   Webhook    │
                         └──────┬───────┘
                                │
                                ▼
                    ┌───────────────────────┐
                    │    Hub Conductor      │
                    │  (single endpoint)    │
                    │                       │
                    │  routes by path:      │
                    │  servers/unraid/* → A │
                    │  servers/vps/*   → B │
                    │  shared/*        → * │
                    └───────────┬───────────┘
                                │
              ┌─────────────────┼─────────────────┐
              │                 │                 │
              ▼                 ▼                 ▼
      ┌───────────────┐ ┌───────────────┐ ┌───────────────┐
      │    Unraid     │ │     VPS       │ │   Pi Cluster  │
      │   Conductor   │ │   Conductor   │ │   Conductor   │
      │               │ │               │ │               │
      │  (internal    │ │  (internal    │ │  (internal    │
      │   listener)   │ │   listener)   │ │   listener)   │
      └───────────────┘ └───────────────┘ └───────────────┘
```

See [ADR-0004: Multi-Server Monorepo](docs/adr/0004-multi-server-monorepo.md).

---

## Documentation

| ADR | Status | Summary |
|-----|--------|---------|
| [0001: Service Composer](docs/adr/0001-service-composer.md) | Accepted | DRY service definitions |
| [0002: Watchtower Webhook](docs/adr/0002-watchtower-webhook-deploy.md) | Accepted | Image update automation |
| [0003: Dagger for Conductor](docs/adr/0003-dagger-for-conductor.md) | Deferred | Shell scripts sufficient |
| [0004: Multi-Server Monorepo](docs/adr/0004-multi-server-monorepo.md) | Proposed | Hub-and-spoke architecture |
| [0005: Tunnel Providers](docs/adr/0005-tunnel-providers.md) | Accepted | Tailscale vs Cloudflare |
| [0006: Conductor Auth](docs/adr/0006-conductor-authentication.md) | Proposed | Authelia integration |
| [0007: Agentgateway MCP Proxy](docs/adr/0007-agentgateway-mcp-proxy.md) | Draft | MCP servers via Agentgateway + Authelia |
| [0008: Container vs Daemon](docs/adr/0008-container-vs-daemon.md) | Accepted | When to use systemd vs Docker |

---

## Requirements

- Docker + Docker Compose
- Linux server (tested: Unraid, Debian, Ubuntu)
- GitHub repo for configs
- Age key for encryption
- (Optional) Tailscale or Cloudflare account

---

## Prior Art

- [Flux](https://fluxcd.io/) - GitOps for Kubernetes
- [ArgoCD](https://argoproj.github.io/cd/) - GitOps for Kubernetes
- [Watchtower](https://containrrr.dev/watchtower/) - Container image updates
- [SOPS](https://github.com/getsops/sops) - Secrets encryption
- [Chezmoi](https://www.chezmoi.io/) - Dotfile/template management

---

## License

MIT

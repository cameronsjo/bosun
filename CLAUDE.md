# Bosun - AI Context

GitOps for Docker Compose on bare metal. "Helm for home."

## Nautical Theme

Everything uses nautical/Below Deck terminology:
- **Bosun** = orchestrator (receives orders, deploys containers)
- **Manifest** = service definitions (crew manifest)
- **Provisions** = reusable config templates (supplies stocked aboard)
- **Captain** = GitHub (gives orders)
- **Radio** = webhook/tunnel (Tailscale Funnel or Cloudflare Tunnel)
- **Crew** = containers

## Directory Structure

```
bosun/
├── bosun/                    # GitOps orchestrator
│   ├── Dockerfile
│   ├── docker-compose.yml
│   ├── hooks.yaml
│   └── scripts/
│       ├── entrypoint.sh
│       ├── reconcile.sh      # Main logic (~100 lines shell)
│       ├── healthcheck.sh
│       └── notify.sh
├── manifest/                 # Service composition tool
│   ├── manifest.py           # Python renderer
│   ├── pyproject.toml
│   ├── provisions/           # Reusable config templates
│   │   ├── container.yml
│   │   ├── healthcheck.yml
│   │   ├── homepage.yml
│   │   ├── reverse-proxy.yml
│   │   ├── monitoring.yml
│   │   ├── postgres.yml
│   │   └── redis.yml
│   ├── services/             # Service definitions
│   └── stacks/               # Groups of services
├── docs/
│   ├── concepts.md           # Architecture diagrams
│   ├── adr/                  # Architecture Decision Records
│   └── guides/
│       └── unraid-setup.md
└── unraid-templates/         # Unraid Community Apps XML
```

## Key Concepts

### Flow
```
git push → webhook → bosun pulls → SOPS decrypt → chezmoi template → docker compose up
```

### Manifest Rendering
Service manifest (~10 lines) + provisions = compose/traefik/gatus configs

```yaml
# manifest/services/myapp.yml
name: myapp
provisions: [container, reverse-proxy, postgres, monitoring]
config:
  image: ghcr.io/org/myapp:latest
  port: 3000
  subdomain: myapp
  domain: example.com
```

### Variable Interpolation
Provisions use `${var}` syntax, resolved from service config before YAML parse.

### Deep Merge Semantics
- Dicts: recursive merge
- Lists: replace (except `networks`/`depends_on` use union, `endpoints` extend)

## Tech Stack

- **SOPS + Age**: Secret encryption
- **Chezmoi**: Template rendering
- **webhook**: GitHub webhook receiver
- **Docker Compose**: Container orchestration

## Commands

```bash
# Render a stack
cd manifest && uv run manifest.py render stacks/apps.yml

# List provisions
cd manifest && uv run manifest.py provisions

# Dry run
cd manifest && uv run manifest.py render stacks/apps.yml --dry-run
```

## Design Principles

1. **Captain gives orders, bosun executes** - Push to git, everything updates
2. **No drama below deck** - ~100 lines of shell beats 10,000 lines of Go
3. **Every crew member has a backup** - Batteries included, all swappable
4. **One yacht, many ports** - Monorepo support for multi-server

## Naming Convention

| Old Name | New Name | Rationale |
|----------|----------|-----------|
| conductor | bosun | Nautical theme |
| composer | manifest | Ship's manifest |
| profiles | provisions | Supplies stocked aboard |

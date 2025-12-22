# unops

GitOps for Docker Compose on bare metal. The K8s-free alternative to Flux/Argo.

## What is unops?

unops is a lightweight GitOps toolkit for managing Docker Compose deployments on Linux servers. It provides:

- **Secret management** with SOPS + Age encryption
- **Template rendering** with Chezmoi
- **Webhook-triggered deploys** from GitHub
- **Polling fallback** for reliability
- **Watchtower integration** for container image updates
- **Service composition** (planned) for DRY service definitions

```
git push → webhook → decrypt secrets → render templates → docker compose up
```

## Components

### Runner

The GitOps runner container handles deployment automation:

```
runner/
├── Dockerfile           # Alpine + sops + age + chezmoi + webhook
├── docker-compose.yml   # Runner deployment config
├── hooks.yaml           # Webhook configuration
└── scripts/
    ├── entrypoint.sh    # Container startup
    ├── reconcile.sh     # Core sync logic
    ├── healthcheck.sh   # Health endpoint
    └── notify.sh        # Discord notifications
```

### Composer (Planned)

Service definition abstraction - define once, generate compose + traefik + monitoring:

```
composer/
├── compose.py           # Renderer CLI
├── profiles/            # Reusable fragments
├── services/            # Service manifests
└── stacks/              # Stack definitions
```

See [ADR-0001: Service Composer](docs/adr/0001-service-composer.md) for the full design.

## Quick Start

### 1. Set up Age encryption

```bash
# Generate key pair
age-keygen -o age-key.txt

# Configure SOPS
cat > .sops.yaml << 'EOF'
creation_rules:
  - path_regex: .*\.yaml$
    age: <your-public-key>
EOF
```

### 2. Create secrets file

```yaml
# secrets.yaml (will be encrypted)
network:
  server_ip: 192.168.1.100
auth:
  api_key: your-secret-key
```

```bash
sops -e secrets.yaml > secrets.yaml.sops
```

### 3. Create compose template

```yaml
# compose/myapp.yml.tmpl
{{- $secrets := fromJson (env "SOPS_SECRETS") -}}
services:
  myapp:
    image: myapp:latest
    environment:
      API_KEY: {{ $secrets.auth.api_key }}
```

### 4. Deploy runner

```bash
docker compose -f runner/docker-compose.yml up -d
```

### 5. Configure GitHub webhook

Point your repo webhook to:
```
https://your-server/hooks/github-push
```

## Watchtower Integration

For app code changes (not config changes), use Watchtower HTTP API:

```yaml
# In your GitHub Actions workflow
- name: Trigger deploy
  run: |
    curl -sf -X POST \
      -H "Authorization: Bearer ${{ secrets.WATCHTOWER_TOKEN }}" \
      "${{ secrets.WATCHTOWER_URL }}?images=ghcr.io/org/app"
```

See [ADR-0002: Watchtower Webhook Deploy](docs/adr/0002-watchtower-webhook-deploy.md).

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         Your Server                              │
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────────────┐  │
│  │   Runner    │    │  Watchtower │    │   Your Services     │  │
│  │             │    │             │    │                     │  │
│  │ • webhook   │    │ • HTTP API  │    │ • app1              │  │
│  │ • reconcile │    │ • polling   │    │ • app2              │  │
│  │ • sops/age  │    │             │    │ • ...               │  │
│  └──────┬──────┘    └──────┬──────┘    └─────────────────────┘  │
│         │                  │                                     │
└─────────│──────────────────│─────────────────────────────────────┘
          │                  │
          ▼                  ▼
    ┌──────────┐      ┌──────────┐
    │  GitHub  │      │   GHCR   │
    │  Webhook │      │  Images  │
    └──────────┘      └──────────┘
```

## Two Deployment Patterns

| Pattern | Trigger | Use Case |
|---------|---------|----------|
| **GitOps** | Config change in repo | Infrastructure, compose files, secrets |
| **Watchtower** | Image push to GHCR | App code changes |

## Requirements

- Docker + Docker Compose
- Linux server (tested on Unraid, Debian, Ubuntu)
- GitHub repo for configs
- Age key for encryption

## Documentation

- [ADR-0001: Service Composer](docs/adr/0001-service-composer.md) - Service definition abstraction
- [ADR-0002: Watchtower Webhook](docs/adr/0002-watchtower-webhook-deploy.md) - Image update automation

## License

MIT

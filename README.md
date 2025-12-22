# bosun

**Helm for home.**

You're the captain of your homelab. 40 containers. Traefik. Secrets everywhere.
You shouldn't have to swab the deck yourself. That's what the bosun is for.

```
git push → bosun receives orders → crew deployed → yacht runs smooth
```

No Kubernetes. No drama. Just smooth sailing.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              Your Yacht (Server)                            │
│                                                                             │
│  ┌────────────────────────────────────────────────────────────────────┐    │
│  │                            Bosun                                   │    │
│  │  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐  │    │
│  │  │  Radio  │→ │  Fetch  │→ │ Decrypt │→ │  Prep   │→ │ Deploy  │  │    │
│  │  │(Webhook)│  │ Orders  │  │ Secrets │  │ Configs │  │  Crew   │  │    │
│  │  └─────────┘  └─────────┘  └─────────┘  └─────────┘  └─────────┘  │    │
│  └────────────────────────────────────────────────────────────────────┘    │
│        ▲                                                   │               │
│        │                                                   ▼               │
│  ┌─────┴─────┐                                    ┌──────────────┐         │
│  │ Tailscale │                                    │  Your Crew   │         │
│  │  Funnel   │                                    │ (Containers) │         │
│  └─────┬─────┘                                    └──────────────┘         │
└────────│───────────────────────────────────────────────────────────────────┘
         │
         ▼
   ┌──────────┐
   │ Captain  │
   │ (GitHub) │
   └──────────┘
```

## What's On Board

| Component | Role |
|-----------|------|
| **Bosun** | Receives orders, decrypts secrets, deploys containers |
| **Manifest** | Write 10 lines, generate compose + Traefik + Gatus configs |
| **Provisions** | Reusable config templates - batteries included, all swappable |

## Quick Start

```bash
# 1. Generate encryption key
age-keygen -o ~/.config/sops/age/keys.txt

# 2. Create .sops.yaml with your public key
cat > .sops.yaml << 'EOF'
creation_rules:
  - path_regex: .*\.yaml$
    age: <your-public-key>
EOF

# 3. Encrypt secrets
sops -e secrets.yaml > secrets.yaml.sops

# 4. Start bosun
docker compose -f bosun/docker-compose.yml up -d

# 5. Push orders
git add . && git commit -m "deploy the fleet" && git push
```

## Documentation

- **[Concepts](docs/concepts.md)** - Architecture, components, diagrams
- **[Unraid Setup](docs/guides/unraid-setup.md)** - Complete walkthrough
- **[Unraid Templates](unraid-templates/)** - Community Apps templates

### Architecture Decisions

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

## Requirements

- Docker + Docker Compose
- Linux server (tested: Unraid, Debian, Ubuntu)
- GitHub repo for manifests
- Age key for encryption
- (Optional) Tailscale or Cloudflare account

## Support

[![Ko-fi](https://ko-fi.com/img/githubbutton_sm.svg)](https://ko-fi.com/cameronsjo)

## License

MIT

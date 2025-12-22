# ADR-0004: Multi-Server Monorepo Architecture

## Status

Proposed

## Context

Currently each server runs its own bosun and exposes its own webhook endpoint. With multiple servers this creates:

- Multiple gateway entries per server
- Multiple webhook URLs to configure in GitHub
- Each server reaching out to GitHub independently
- Duplicated bosun infrastructure

## Decision

Implement a **hub-and-spoke** architecture with a central bosun hub that:

1. Receives all GitHub webhooks (single endpoint)
2. Determines target server(s) from repo/path configuration
3. Broadcasts reconciliation requests to server bosuns via internal network

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         Monorepo                                 │
│  infrastructure/                                                 │
│  ├── servers/                                                    │
│  │   ├── unraid/           # Server-specific configs            │
│  │   │   ├── stacks/                                            │
│  │   │   └── secrets.yaml.sops                                  │
│  │   ├── vps-1/                                                 │
│  │   └── homelab-2/                                             │
│  ├── shared/               # Shared profiles, services          │
│  │   ├── profiles/                                              │
│  │   └── services/                                              │
│  └── hub/                  # Hub bosun config                   │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                        Hub Bosun                                 │
│  • Single webhook endpoint (webhook.example.com)                │
│  • Receives GitHub push events                                   │
│  • Parses changed paths to determine target servers              │
│  • Broadcasts to server bosuns via Tailscale/WireGuard          │
└─────────────────────────────────────────────────────────────────┘
         │                    │                    │
         ▼                    ▼                    ▼
┌─────────────┐      ┌─────────────┐      ┌─────────────┐
│   Unraid    │      │   VPS-1     │      │ Homelab-2   │
│    Bosun    │      │    Bosun    │      │    Bosun    │
│             │      │             │      │             │
│ • No gateway│      │ • No gateway│      │ • No gateway│
│ • Internal  │      │ • Internal  │      │ • Internal  │
│   listener  │      │   listener  │      │   listener  │
└─────────────┘      └─────────────┘      └─────────────┘
```

## Path-Based Routing

Hub determines target servers by analyzing changed files:

```yaml
# hub/routing.yml
routes:
  - pattern: "servers/unraid/**"
    targets: [unraid]
  - pattern: "servers/vps-1/**"
    targets: [vps-1]
  - pattern: "shared/**"
    targets: [unraid, vps-1, homelab-2]  # All servers
  - pattern: "**.md"
    targets: []  # No deployment for docs
```

## Communication Protocol

Hub → Server communication over private network (Tailscale/WireGuard):

```bash
# Hub broadcasts to server bosun
curl -X POST http://unraid.tailnet:8080/reconcile \
  -H "X-Hub-Token: $HUB_TOKEN" \
  -d '{"ref": "main", "paths": ["servers/unraid/stacks/apps.yml"]}'
```

Servers only accept requests from hub (no public exposure).

## Benefits

| Aspect | Before | After |
|--------|--------|-------|
| Gateway entries | N (one per server) | 1 (hub only) |
| GitHub webhooks | N | 1 |
| External exposure | All servers | Hub only |
| Outbound connections | All servers → GitHub | Hub → GitHub |
| Configuration | Per-server | Centralized |

## Implementation Phases

### Phase 1: Monorepo Structure
- Reorganize configs into `servers/<name>/` pattern
- Shared profiles/services directory
- Per-server secrets files

### Phase 2: Hub Bosun
- Webhook receiver with path parsing
- Routing configuration
- Internal broadcast mechanism

### Phase 3: Server Bosuns
- Remove gateway/webhook exposure
- Internal-only listener for hub broadcasts
- Token-based authentication

## Alternatives Considered

| Alternative | Why not |
|-------------|---------|
| GitHub Actions per server | Still N workflows, complexity in matrix |
| Single bosun, remote exec | Security concerns with remote commands |
| Kubernetes | Overkill, doesn't fit bare-metal focus |

## Risks

1. **Hub SPOF** - Mitigate: Keep polling fallback on servers
2. **Network complexity** - Mitigate: Simple HTTP, no persistent connections
3. **Secrets distribution** - Mitigate: Each server keeps own secrets, hub never sees them

## References

- ADR-0001: Manifest System
- ADR-0002: Watchtower Webhook Deploy

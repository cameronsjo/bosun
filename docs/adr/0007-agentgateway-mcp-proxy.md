# ADR-0007: Agentgateway as MCP Proxy

## Status

Draft

## Context

AI agents (Claude, ChatGPT, custom) need access to MCP (Model Context Protocol) servers running in the homelab. These servers provide tools like file access, database queries, home automation, and more.

Challenges:
1. MCP servers run on internal network
2. AI agents connect from the internet (Claude.ai, mobile apps)
3. Need authentication without breaking MCP protocol
4. Multiple MCP servers, single entry point
5. Want observability (tracing, metrics)

## Decision

**Use [Agentgateway](https://github.com/agentgateway/agentgateway) as the MCP proxy, with Authelia providing OAuth2/OIDC authentication.**

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              Your Server                                     │
│                                                                              │
│  ┌─────────────┐     ┌─────────────────┐     ┌─────────────────────────┐   │
│  │  Authelia   │────▶│  Agentgateway   │────▶│     MCP Servers         │   │
│  │             │     │                 │     │                         │   │
│  │ • OIDC      │     │ • JWT verify    │     │ • obsidian-mcp          │   │
│  │ • OAuth2    │     │ • Routing       │     │ • filesystem-mcp        │   │
│  │ • 2FA       │     │ • Tracing       │     │ • database-mcp          │   │
│  │             │     │ • Rate limiting │     │ • home-assistant-mcp    │   │
│  └─────────────┘     └─────────────────┘     └─────────────────────────┘   │
│         ▲                    ▲                                              │
│         │                    │                                              │
│  ┌──────┴────────────────────┴──────┐                                       │
│  │        Tailscale Funnel          │                                       │
│  │   gateway.tail*.ts.net:443       │                                       │
│  └──────────────┬───────────────────┘                                       │
│                 │                                                            │
└─────────────────│────────────────────────────────────────────────────────────┘
                  │
                  ▼
          ┌───────────────┐
          │   Claude.ai   │
          │   ChatGPT     │
          │   Custom AI   │
          └───────────────┘
```

## Authentication Flow

### Option A: JWT via Authelia OIDC (Recommended)

```
1. AI agent redirects to Authelia login
2. User authenticates (password + 2FA)
3. Authelia issues JWT
4. Agent includes JWT in MCP requests
5. Agentgateway validates JWT
6. Request forwarded to MCP server
```

```yaml
# agentgateway config
auth:
  jwt:
    issuer: https://auth.example.com
    audience: agentgateway
    jwks_uri: https://auth.example.com/jwks.json
```

### Option B: API Keys (Simpler)

```yaml
# agentgateway config
auth:
  apiKey:
    keys:
      - name: claude-desktop
        key: ${AGENTGATEWAY_API_KEY}
        scopes: ["mcp:*"]
```

### Option C: Hybrid (Production)

- API keys for trusted first-party agents (Claude Desktop)
- OIDC for third-party or web-based agents
- Rate limiting per key/user

## Authelia OIDC Client

```yaml
# authelia configuration.yml
identity_providers:
  oidc:
    clients:
      - client_id: agentgateway
        client_name: MCP Gateway
        client_secret: ${AGENTGATEWAY_OIDC_SECRET}
        public: false
        authorization_policy: two_factor
        redirect_uris:
          - https://gateway.example.com/oauth/callback
        scopes:
          - openid
          - profile
          - groups
        token_endpoint_auth_method: client_secret_post
```

## Agentgateway Configuration

```yaml
# agentgateway/config.yaml
listeners:
  - id: mcp
    address: 0.0.0.0:8080
    protocol: MCP
    auth:
      jwt:
        issuer: https://auth.example.com
        audience: agentgateway

targets:
  - id: obsidian
    address: obsidian-mcp:8080
    protocol: MCP

  - id: filesystem
    address: filesystem-mcp:8080
    protocol: MCP

  - id: homeassistant
    address: homeassistant-mcp:8080
    protocol: MCP

routing:
  - match:
      tool_prefix: "obsidian_"
    target: obsidian
  - match:
      tool_prefix: "fs_"
    target: filesystem
  - match:
      tool_prefix: "ha_"
    target: homeassistant

observability:
  tracing:
    endpoint: http://jaeger:4317
  metrics:
    enabled: true
```

## Docker Compose Integration

```yaml
# compose/mcp-stack.yml
services:
  agentgateway:
    image: ghcr.io/agentgateway/agentgateway:latest
    environment:
      AGENTGATEWAY_CONFIG: /config/config.yaml
    volumes:
      - ./agentgateway/config.yaml:/config/config.yaml:ro
    networks:
      - proxynet    # For Authelia access
      - mcp-net     # For MCP server access
    labels:
      traefik.enable: "true"
      traefik.http.routers.mcp.rule: Host(`gateway.example.com`)
      traefik.http.routers.mcp.middlewares: authelia@docker

  obsidian-mcp:
    image: ghcr.io/user/obsidian-mcp:latest
    networks:
      - mcp-net
    # No external exposure - only via agentgateway

  filesystem-mcp:
    image: ghcr.io/modelcontextprotocol/filesystem-mcp:latest
    volumes:
      - /data:/data:ro
    networks:
      - mcp-net

networks:
  proxynet:
    external: true
  mcp-net:
    internal: true  # No external access
```

## Composer Profile

```yaml
# profiles/mcp-server.yml
compose:
  services:
    ${name}:
      networks:
        - mcp-net
      # No traefik labels - internal only
      # Agentgateway handles external access
```

## Security Considerations

1. **Network Isolation**: MCP servers on internal `mcp-net`, no direct external access
2. **Authentication**: All external requests through Authelia
3. **Authorization**: Per-tool permissions via Agentgateway CEL expressions
4. **Rate Limiting**: Prevent abuse at gateway level
5. **Audit Logging**: All tool calls logged with user identity
6. **2FA Required**: Authelia policy enforces two-factor for MCP access

## Authorization (Future)

CEL expressions for fine-grained control:

```yaml
# agentgateway config
authorization:
  rules:
    - match:
        tool: "fs_write"
      allow: 'claims.groups.contains("admin")'
    - match:
        tool: "ha_*"
      allow: 'claims.groups.contains("home-automation")'
    - match:
        tool: "obsidian_*"
      allow: 'true'  # All authenticated users
```

## Alternatives Considered

| Alternative | Why not |
|-------------|---------|
| Direct MCP exposure | No auth, no multiplexing |
| Traefik ForwardAuth only | MCP protocol needs more than HTTP auth |
| Custom proxy | Agentgateway already exists and is maintained |
| OAuth2 Proxy | Doesn't understand MCP protocol |

## Migration Path

1. Deploy Agentgateway alongside existing setup
2. Configure Authelia OIDC client
3. Migrate MCP servers to mcp-net
4. Update AI agent configs to use gateway
5. Remove direct MCP server exposure

## References

- [Agentgateway](https://github.com/agentgateway/agentgateway)
- [MCP Specification](https://modelcontextprotocol.io)
- [Authelia OIDC](https://www.authelia.com/configuration/identity-providers/oidc/)
- [ADR-0006: Conductor Authentication](0006-conductor-authentication.md)

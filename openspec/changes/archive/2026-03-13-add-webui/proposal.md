# Change: Add Web UI Dashboard (MVP)

## Why

The roadmap identifies "WebUI for review/trigger" as a P2 priority. Currently, operators must use CLI or raw API calls to monitor status and trigger reconciliations. A minimal web dashboard provides:

- Quick status checks without SSH
- One-click reconciliation triggers
- Visual container health overview

## What Changes

- **NEW**: React/TypeScript SPA deployed as a separate container (`bosun-webui`)
- **NEW**: Daemon exposes `/api/*` endpoints for WebUI consumption
- **NEW**: Dashboard page with status, trigger, and container summary
- **NEW**: Container list with health badges and restart capability
- **NEW**: Basic log viewer (last N lines, no streaming)

### MVP Scope (v1)

| Feature | Included | Deferred |
|---------|----------|----------|
| Dashboard with status | ✓ | |
| Manual trigger button | ✓ | |
| Container list + health | ✓ | |
| Container restart | ✓ | |
| Basic log viewer | ✓ | |
| Dark mode | ✓ | |
| Bearer token auth | ✓ | |
| OIDC/Authelia | | v2 |
| SSE streaming | | v2 |
| Reconcile history | | v2 |
| Log search | | v2 |

### Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         HOST                                     │
│                                                                  │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │                    bosun daemon                             │ │
│  │                                                             │ │
│  │  API (TCP with bearer token):                               │ │
│  │    GET  /api/status       - Daemon status                   │ │
│  │    GET  /api/containers   - Container list                  │ │
│  │    GET  /api/containers/:id/logs - Last N log lines         │ │
│  │    POST /api/containers/:id/restart - Restart container     │ │
│  │    POST /api/trigger      - Trigger reconcile               │ │
│  │                                                             │ │
│  └────────────────────────────────────────────────────────────┘ │
│                              ▲                                   │
│                              │ HTTP (bearer token)               │
│                              │                                   │
│  ┌───────────────────────────┴────────────────────────────────┐ │
│  │                  bosun-webui (Docker)                       │ │
│  │                                                             │ │
│  │  • React SPA served by nginx                                │ │
│  │  • API calls proxied to daemon                              │ │
│  │  • Bearer token auth                                        │ │
│  │                                                             │ │
│  └─────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
```

## Impact

- **Affected specs**: None (new capability)
- **Affected code**:
  - `internal/daemon/api.go` - New API endpoints
  - NEW: `webui/` directory with React SPA
  - NEW: `Dockerfile.webui`
  - NEW: `docker-compose.webui.yml` example
- **Breaking changes**: None (additive)

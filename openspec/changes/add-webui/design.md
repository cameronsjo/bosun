# Design: Web UI Dashboard (MVP)

## Context

Bosun provides CLI-only interaction. A minimal web dashboard enables quick status checks and one-click triggers without SSH access.

**Constraints**:
- MUST work behind reverse proxy (Traefik)
- MUST NOT require daemon binary changes for deployment
- SHOULD ship fast to validate demand before investing more

## Goals / Non-Goals

**Goals**:
- Status dashboard with health indicators
- One-click reconciliation trigger
- Container list with restart capability
- Basic log viewing

**Non-Goals (v1)**:
- OIDC/SSO integration (use bearer token)
- Real-time streaming (use polling)
- Reconcile history (just show last run)
- Log search/filtering
- Mobile-native experience

## Decisions

### 1. Frontend Stack

**Decision**: React 18 + TypeScript + Vite + Tailwind CSS

**Rationale**:
- User explicitly chose React/TypeScript
- Vite for fast builds
- Tailwind for rapid UI development
- No heavy state management library - just `fetch` + `useState` + `useEffect`

**What we're NOT using**:
- TanStack Query - overkill for 5 API calls with simple polling
- Redux/Zustand - no complex client state
- SSE/WebSockets - polling every 5s is simpler

### 2. Data Fetching

**Decision**: Simple polling with `setInterval`

```typescript
// Simple pattern for all data fetching
const [data, setData] = useState(null);
const [error, setError] = useState(null);

useEffect(() => {
  const fetchData = async () => {
    try {
      const res = await fetch('/api/status', { headers: authHeaders });
      setData(await res.json());
      setError(null);
    } catch (e) {
      setError(e.message);
    }
  };

  fetchData();
  const interval = setInterval(fetchData, 5000);
  return () => clearInterval(interval);
}, []);
```

**Rationale**: SSE adds reconnection complexity. Polling is simpler and sufficient for a dashboard that updates every 5 seconds anyway.

### 3. Authentication

**Decision**: Bearer token (reuse `BOSUN_BEARER_TOKEN`)

**Flow**:
1. User configures `BOSUN_BEARER_TOKEN` in both daemon and webui containers
2. WebUI stores token in memory (from env var injected at build/runtime)
3. All API calls include `Authorization: Bearer <token>`

**Why not OIDC**: Adds significant complexity. Bearer token validates demand before investing in SSO. Can add OIDC in v2.

### 4. API Design

**New endpoints on daemon** (added to existing TCP server):

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/status` | GET | Extended status with poll info |
| `/api/containers` | GET | All containers with health |
| `/api/containers/:id/logs` | GET | Last N lines (query: `?lines=100`) |
| `/api/containers/:id/restart` | POST | Restart container |
| `/api/trigger` | POST | Trigger reconciliation |

All endpoints require bearer token. Returns JSON.

### 5. Error Handling

**Decision**: Graceful degradation with clear error states

| Scenario | UI Behavior |
|----------|-------------|
| Daemon unreachable | Show "Daemon offline" banner, disable actions, show stale data with timestamp |
| API error | Show error toast, keep last good data visible |
| Auth failure | Show "Authentication failed" with instructions |

### 6. Dark Mode

**Decision**: System preference with manual toggle

- Default to `prefers-color-scheme`
- Toggle stored in `localStorage`
- Tailwind `dark:` classes

### 7. Container Image

**Decision**: Multi-stage build, nginx runtime

```dockerfile
# Build
FROM node:22-alpine AS build
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

# Runtime
FROM nginx:alpine
COPY --from=build /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
```

**Environment handling**:
- `BOSUN_API_URL` injected at container start via nginx sub_filter or entrypoint script
- `BOSUN_BEARER_TOKEN` passed to nginx for proxy auth header

## Risks / Trade-offs

| Risk | Mitigation |
|------|------------|
| Token in env var | Document secure practices; OIDC in v2 |
| No streaming logs | Last N lines sufficient for troubleshooting; streaming in v2 |
| Polling load | 5s interval is minimal; can make configurable |

## Open Questions

None - MVP scope is intentionally minimal to ship fast.

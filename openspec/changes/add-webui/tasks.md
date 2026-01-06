# Tasks: Add Web UI Dashboard (MVP)

## 1. Daemon API

- [ ] 1.1 Add `/api/status` endpoint (health, uptime, last reconcile, next poll)
- [ ] 1.2 Add `/api/containers` endpoint (list all with health status)
- [ ] 1.3 Add `/api/containers/:id/logs` endpoint (last N lines, query param)
- [ ] 1.4 Add `/api/containers/:id/restart` endpoint
- [ ] 1.5 Add `/api/trigger` endpoint (reuse existing trigger logic)
- [ ] 1.6 Write tests for API endpoints

## 2. WebUI Setup

- [ ] 2.1 Initialize Vite + React + TypeScript project in `webui/`
- [ ] 2.2 Configure Tailwind CSS with dark mode
- [ ] 2.3 Create API client with bearer token auth
- [ ] 2.4 Create reusable error boundary and loading states

## 3. UI Implementation

- [ ] 3.1 Build dashboard page (status card, trigger button, container summary)
- [ ] 3.2 Build container list page (table with health badges, restart button)
- [ ] 3.3 Build log viewer page (container selector, last N lines display)
- [ ] 3.4 Add dark mode toggle
- [ ] 3.5 Add "daemon offline" error banner

## 4. Container & Deployment

- [ ] 4.1 Create Dockerfile.webui (multi-stage: node build -> nginx)
- [ ] 4.2 Create nginx.conf with SPA routing and API proxy
- [ ] 4.3 Create docker-compose.webui.yml example
- [ ] 4.4 Add GitHub Actions workflow to build and publish image
- [ ] 4.5 Write setup documentation (docs/webui.md)

## Dependencies

```
1.* (API) ──────────────────┐
                            ├──> 4.* (Container)
2.* (Setup) -> 3.* (UI) ────┘
```

Tasks in section 1 and 2 can run in parallel.

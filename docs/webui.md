# Bosun WebUI

The Bosun WebUI provides a browser-based dashboard for monitoring and controlling your Bosun GitOps daemon.

## Features

- **Dashboard**: Real-time status of daemon health, state, and next scheduled poll
- **Container List**: View all managed containers with health badges and status
- **Container Actions**: Restart containers directly from the UI
- **Log Viewer**: View container logs with configurable line count
- **Manual Trigger**: Trigger reconciliation on demand
- **Dark Mode**: System-aware theme with manual toggle
- **Offline Detection**: Banner notification when daemon is unreachable

## Quick Start

### Using Docker Compose

```yaml
services:
  webui:
    image: ghcr.io/cameronsjo/bosun-webui:latest
    ports:
      - "8080:8080"
    environment:
      - BOSUN_API_URL=http://bosun:9090
      - BOSUN_BEARER_TOKEN=your-token-here
```

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `BOSUN_API_URL` | `http://bosun:9090` | URL of the Bosun daemon API |
| `BOSUN_BEARER_TOKEN` | (empty) | Bearer token for API authentication |

## Deployment Options

### Standalone Container

```bash
docker run -d \
  --name bosun-webui \
  -p 8080:8080 \
  -e BOSUN_API_URL=http://your-bosun-host:9090 \
  -e BOSUN_BEARER_TOKEN=your-token \
  ghcr.io/cameronsjo/bosun-webui:latest
```

### With Traefik

The WebUI works seamlessly behind Traefik. Add labels to expose it:

```yaml
services:
  webui:
    image: ghcr.io/cameronsjo/bosun-webui:latest
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.bosun-webui.rule=Host(`bosun.example.com`)"
      - "traefik.http.routers.bosun-webui.entrypoints=websecure"
      - "traefik.http.routers.bosun-webui.tls.certresolver=letsencrypt"
```

### With Authelia

For authentication, place Authelia in front of the WebUI:

```yaml
services:
  webui:
    image: ghcr.io/cameronsjo/bosun-webui:latest
    labels:
      - "traefik.http.routers.bosun-webui.middlewares=authelia@docker"
```

## Architecture

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   Browser   │────▶│   nginx     │────▶│   Bosun     │
│   (React)   │◀────│   WebUI     │◀────│   Daemon    │
└─────────────┘     └─────────────┘     └─────────────┘
                          │
                          ▼
                    /api/* proxy
```

The WebUI is a static React SPA served by nginx. API requests are proxied to the Bosun daemon.

## Development

### Prerequisites

- Node.js 22+
- npm

### Local Development

```bash
cd webui

# Install dependencies
npm install

# Start dev server with hot reload
npm run dev

# Build for production
npm run build
```

The dev server proxies `/api/*` requests to `http://localhost:9090` by default. Set `BOSUN_API_URL` environment variable to change this.

### Building the Docker Image

```bash
cd webui
docker build -t bosun-webui .
```

## API Endpoints

The WebUI consumes these daemon API endpoints:

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/status` | GET | Daemon health and state |
| `/api/containers` | GET | List all containers |
| `/api/containers/{id}/logs` | GET | Get container logs |
| `/api/containers/{id}/restart` | POST | Restart a container |
| `/api/trigger` | POST | Trigger reconciliation |

## Security Considerations

- The WebUI runs as a non-root user inside the container
- Bearer token is injected at runtime (not baked into the image)
- Security headers are set by nginx (X-Frame-Options, X-Content-Type-Options, etc.)
- For production, place the WebUI behind a reverse proxy with TLS
- Consider using Authelia or similar for user authentication

## Troubleshooting

### "Daemon Offline" Banner

This appears when the WebUI cannot reach the Bosun daemon API. Check:

1. Is the daemon running? `docker ps | grep bosun`
2. Is the network correct? The WebUI must be able to reach the daemon
3. Is the API URL configured correctly? Check `BOSUN_API_URL`
4. Is authentication required? Set `BOSUN_BEARER_TOKEN`

### 401/403 Errors

The daemon requires authentication but the token is missing or invalid:

1. Check if the daemon has bearer auth enabled
2. Verify `BOSUN_BEARER_TOKEN` is set correctly
3. Restart the WebUI container after changing environment variables

### Container Actions Not Working

If restart fails:

1. Check the daemon has access to the Docker socket
2. Verify the container exists and is managed by the daemon
3. Check daemon logs for error details

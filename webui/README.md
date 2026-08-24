# Bosun WebUI

Web dashboard for the Bosun GitOps daemon.

## Features

- Real-time daemon status monitoring
- Container list with health badges
- Container restart actions
- Log viewer with configurable line count
- Manual reconciliation trigger
- Dark mode with system preference detection
- Offline detection banner

## Development

```bash
# Install dependencies
npm ci

# Start dev server (proxies to localhost:9090)
npm run dev

# Build for production
npm run build

# Type check
npm run typecheck

# Lint
npm run lint
```

## Docker

```bash
# Build image
docker build -t bosun-webui .

# Run container
docker run -p 8080:8080 \
  -e BOSUN_API_URL=http://bosun:9090 \
  -e BOSUN_BEARER_TOKEN=your-token \
  bosun-webui
```

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `BOSUN_API_URL` | `http://bosun:9090` | Daemon API URL |
| `BOSUN_BEARER_TOKEN` | (empty) | API auth token |

See [docs/webui.md](../docs/webui.md) for full documentation.

## Tech Stack

- React 19
- TypeScript 6
- Vite 8
- ESLint 10
- Tailwind CSS 4
- nginx (production)

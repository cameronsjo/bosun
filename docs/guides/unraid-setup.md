# Unraid Setup Guide

Complete guide to setting up unops on Unraid.

## Overview

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              Unraid Server                                   │
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │                    Docker Compose Manager                            │    │
│  │  /boot/config/plugins/compose.manager/projects/                     │    │
│  │    ├── core/           ← Conductor deploys here                     │    │
│  │    ├── apps/                                                         │    │
│  │    └── mcp/                                                          │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                      ▲                                       │
│                                      │                                       │
│  ┌──────────────────────────────────┴──────────────────────────────────┐    │
│  │                         Conductor                                    │    │
│  │                                                                      │    │
│  │  1. Receives webhook or polls                                        │    │
│  │  2. git pull                                                         │    │
│  │  3. sops decrypt → SOPS_SECRETS env                                  │    │
│  │  4. chezmoi execute-template                                         │    │
│  │  5. docker compose up -d                                             │    │
│  │                                                                      │    │
│  └──────────────────────────────────────────────────────────────────────┘    │
│         ▲                                                                    │
│         │ webhook                                                            │
│  ┌──────┴──────┐                                                             │
│  │  Tailscale  │                                                             │
│  │   Funnel    │                                                             │
│  └──────┬──────┘                                                             │
└─────────│────────────────────────────────────────────────────────────────────┘
          │
          ▼
    ┌──────────┐      ┌──────────┐
    │  GitHub  │      │   Your   │
    │ Webhooks │      │   Repo   │
    └──────────┘      └──────────┘
```

## Prerequisites

- Unraid 6.12+ with Docker enabled
- Community Applications plugin installed
- GitHub account
- SSH access to Unraid

## Step 1: Prepare Your Config Repository

### Create Repository Structure

```bash
# On your local machine
mkdir infrastructure && cd infrastructure
git init

# Create directory structure
mkdir -p compose secrets
touch .sops.yaml secrets.yaml compose/.gitkeep
```

### Generate Age Key

```bash
# Install age (macOS)
brew install age

# Or on Linux
# Download from https://github.com/FiloSottile/age/releases

# Generate keypair
age-keygen -o age-key.txt

# Output looks like:
# Public key: age1xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
# Save this public key for .sops.yaml
```

### Configure SOPS

```yaml
# .sops.yaml
creation_rules:
  - path_regex: .*\.yaml$
    age: age1xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx  # Your public key
```

### Create Secrets File

```yaml
# secrets.yaml
network:
  domain: example.com
  server_ip: 192.168.1.100

auth:
  jwt_secret: your-random-secret-here

apps:
  myapp:
    db_password: super-secret-password
```

### Encrypt Secrets

```bash
sops -e secrets.yaml > secrets.yaml.sops
rm secrets.yaml  # Don't commit unencrypted!
```

### Create Compose Template

```yaml
# compose/myapp.yml.tmpl
{{- $secrets := fromJson (env "SOPS_SECRETS") -}}
services:
  myapp:
    image: myapp:latest
    container_name: myapp
    restart: unless-stopped
    environment:
      DB_PASSWORD: {{ $secrets.apps.myapp.db_password }}
```

### Push to GitHub

```bash
echo "secrets.yaml" >> .gitignore
echo "age-key.txt" >> .gitignore
git add .
git commit -m "Initial infrastructure setup"
git remote add origin https://github.com/YOUR_USER/infrastructure.git
git push -u origin main
```

## Step 2: Install Conductor on Unraid

### Add Template Repository

1. Go to **Apps** in Unraid
2. Click the **Settings** gear icon
3. Scroll to **Template Repositories**
4. Add:
   ```
   https://github.com/cameronsjo/unops
   ```
5. Click **Save**

### Install Conductor

1. Search for "unops"
2. Click **unops-conductor**
3. Configure:

| Setting | Value |
|---------|-------|
| Config Path | `/mnt/user/appdata/unops-conductor` |
| Git Repository URL | `https://github.com/YOUR_USER/infrastructure.git` |
| Age Key File | `/config/age-key.txt` |
| GitHub Webhook Secret | (generate random string) |
| Discord Webhook URL | (optional) |

4. Click **Apply**

### Copy Age Key to Unraid

```bash
# From your local machine
scp age-key.txt root@unraid:/mnt/user/appdata/unops-conductor/age-key.txt
```

### Verify Installation

```bash
# SSH to Unraid
ssh root@unraid

# Check logs
docker logs unops-conductor

# Test health endpoint
curl http://localhost:8080/health
```

## Step 3: Configure GitHub Webhook

### Option A: Tailscale Funnel (Recommended)

1. Install Tailscale container on Unraid
2. Enable Funnel:
   ```bash
   docker exec tailscale tailscale funnel 8080
   ```
3. Get your Funnel URL: `https://unraid.tail12345.ts.net`

### Option B: Cloudflare Tunnel

1. Create tunnel in Cloudflare Zero Trust dashboard
2. Route `hooks.yourdomain.com` → `http://unraid:8080`

### Add Webhook to GitHub

1. Go to your repo → **Settings** → **Webhooks** → **Add webhook**
2. Configure:
   - **Payload URL**: `https://your-funnel-url/hooks/github-push`
   - **Content type**: `application/json`
   - **Secret**: Same secret you set in conductor config
   - **Events**: Just the push event
3. Click **Add webhook**

### Test Webhook

1. Make a commit to your repo
2. Push to GitHub
3. Check conductor logs:
   ```bash
   docker logs -f unops-conductor
   ```

You should see:
```
[INFO] Received webhook for ref: refs/heads/main
[INFO] Pulling latest changes...
[INFO] Decrypting secrets...
[INFO] Rendering templates...
[INFO] Running docker compose up...
[INFO] Deployment complete!
```

## Step 4: Add More Services

### Create New Compose Template

```yaml
# compose/homepage.yml.tmpl
{{- $secrets := fromJson (env "SOPS_SECRETS") -}}
services:
  homepage:
    image: ghcr.io/gethomepage/homepage:latest
    container_name: homepage
    restart: unless-stopped
    environment:
      TZ: America/Chicago
    volumes:
      - /mnt/user/appdata/homepage:/app/config
    ports:
      - 3000:3000
```

### Deploy

```bash
git add compose/homepage.yml.tmpl
git commit -m "Add homepage"
git push
# Webhook triggers, conductor deploys
```

## Compose Manager Integration

The conductor deploys to Unraid's Compose Manager directory:

```
/boot/config/plugins/compose.manager/projects/
```

After deployment, your projects appear in:
- **Docker** → **Compose** tab in Unraid UI
- Can be started/stopped from UI
- Logs visible in UI

## Troubleshooting

### Deployment Not Triggering

```bash
# Check webhook delivery in GitHub
# Repo → Settings → Webhooks → Recent Deliveries

# Check conductor logs
docker logs unops-conductor | tail -50

# Verify webhook endpoint
curl -X POST http://localhost:8080/hooks/github-push \
  -H "Content-Type: application/json" \
  -d '{"ref": "refs/heads/main"}'
```

### SOPS Decryption Failing

```bash
# Check Age key exists
docker exec unops-conductor cat /config/age-key.txt

# Test decryption manually
docker exec unops-conductor sops -d /config/repo/secrets.yaml.sops

# Verify SOPS_AGE_KEY_FILE env var
docker exec unops-conductor env | grep SOPS
```

### Docker Compose Errors

```bash
# Check Docker socket access
docker exec unops-conductor docker ps

# Run compose manually
docker exec unops-conductor docker compose -f /compose/myapp.yml up -d

# Check rendered template
docker exec unops-conductor cat /config/rendered/myapp.yml
```

### Network Issues

```bash
# Check if conductor can reach GitHub
docker exec unops-conductor curl -I https://github.com

# Check DNS
docker exec unops-conductor nslookup github.com
```

## Best Practices

### 1. Use Separate Secrets Files

```
secrets/
├── core.yaml.sops      # Traefik, Authelia
├── apps.yaml.sops      # App-specific secrets
└── mcp.yaml.sops       # MCP server secrets
```

### 2. Organize Compose Files

```
compose/
├── core/
│   ├── traefik.yml.tmpl
│   └── authelia.yml.tmpl
├── apps/
│   ├── homepage.yml.tmpl
│   └── immich.yml.tmpl
└── mcp/
    └── agentgateway.yml.tmpl
```

### 3. Use Networks

```yaml
# In your compose templates
networks:
  proxynet:
    external: true
  mcp-net:
    external: true
```

### 4. Add Health Checks

```yaml
services:
  myapp:
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:3000/health"]
      interval: 30s
      timeout: 5s
      retries: 3
```

### 5. Enable Notifications

Set `DISCORD_WEBHOOK_URL` to get deploy notifications:

```
✅ Deployment successful
Repository: infrastructure
Branch: main
Commit: abc1234
Services: homepage, traefik, authelia
```

## Next Steps

- [Service Composer](../adr/0001-service-composer.md) - Generate configs from manifests
- [Watchtower Integration](../adr/0002-watchtower-webhook-deploy.md) - Auto-update container images
- [Multi-Server Setup](../adr/0004-multi-server-monorepo.md) - Manage multiple servers

# Unraid Templates

Docker templates for installing unops components on Unraid via Community Applications.

## Installation

### Method 1: Add Template Repository (Recommended)

1. In Unraid, go to **Apps** → **Settings** (gear icon)
2. Scroll to **Template Repositories**
3. Add this URL:
   ```
   https://github.com/cameronsjo/unops
   ```
4. Click **Save**
5. Search for "unops" in Apps

### Method 2: Manual XML Install

1. Download the XML template:
   ```bash
   wget https://raw.githubusercontent.com/cameronsjo/unops/main/unraid-templates/unops-conductor.xml \
     -O /boot/config/plugins/dockerMan/templates-user/unops-conductor.xml
   ```
2. Go to **Docker** → **Add Container**
3. Select "unops-conductor" from the template dropdown

## Available Templates

| Template | Description | Status |
|----------|-------------|--------|
| [unops-conductor](unops-conductor.xml) | GitOps orchestrator | Available |

## Prerequisites

Before installing the conductor, you need:

### 1. Age Key for SOPS

Generate an Age keypair for secret encryption:

```bash
# On your local machine
age-keygen -o age-key.txt

# Copy the private key to Unraid
scp age-key.txt root@unraid:/mnt/user/appdata/unops-conductor/age-key.txt
```

Keep the public key (starts with `age1...`) for your `.sops.yaml` config.

### 2. GitHub Repository

Create a repo with your Docker Compose configs:

```
infrastructure/
├── .sops.yaml           # SOPS configuration
├── secrets.yaml.sops    # Encrypted secrets
└── compose/
    ├── app1.yml.tmpl    # Chezmoi templates
    └── app2.yml.tmpl
```

### 3. GitHub Webhook (Optional)

For instant deploys (vs hourly polling):

1. Go to your repo → **Settings** → **Webhooks**
2. Add webhook:
   - **Payload URL**: `http://your-unraid:8080/hooks/github-push`
   - **Content type**: `application/json`
   - **Secret**: Generate a random string, save for template config
   - **Events**: Just the push event

For external access, expose via Tailscale Funnel or Cloudflare Tunnel.

### 4. Discord Webhook (Optional)

For deployment notifications:

1. In Discord, go to channel **Settings** → **Integrations** → **Webhooks**
2. Create webhook, copy URL
3. Add to template config

## Configuration

| Variable | Required | Description |
|----------|----------|-------------|
| `UNOPS_REPO_URL` | Yes | GitHub repo URL (HTTPS) |
| `SOPS_AGE_KEY_FILE` | Yes | Path to Age private key |
| `GITHUB_WEBHOOK_SECRET` | Yes | Webhook validation secret |
| `DISCORD_WEBHOOK_URL` | No | Discord notification webhook |
| `UNOPS_POLL_INTERVAL` | No | Seconds between polls (default: 3600) |
| `TZ` | No | Timezone (default: America/Chicago) |

## Network Configuration

The conductor needs to reach:

- **GitHub** (outbound): To clone/pull your config repo
- **Docker socket**: To run `docker compose` commands
- **Your containers**: If they're on custom networks

### Joining Custom Networks

If your containers use `proxynet` or similar:

1. After installing, go to **Docker** → **unops-conductor** → **Edit**
2. Add to **Extra Parameters**:
   ```
   --network=proxynet
   ```

Or for multiple networks:
```
--network=proxynet --network=mcp-net
```

## Compose Manager Integration

The template mounts Unraid's Compose Manager projects directory by default:

```
/boot/config/plugins/compose.manager/projects → /compose
```

This allows the conductor to manage projects that also appear in the Compose Manager UI.

## Troubleshooting

### Check Logs

```bash
docker logs -f unops-conductor
```

### Verify Webhook

```bash
# Check health endpoint
curl http://unraid:8080/health

# Test webhook manually
curl -X POST http://unraid:8080/hooks/test
```

### SOPS Decryption Errors

```bash
# Verify Age key is readable
docker exec unops-conductor cat /config/age-key.txt

# Test decryption
docker exec unops-conductor sops -d /config/repo/secrets.yaml.sops
```

### Docker Socket Errors

```bash
# Verify socket is mounted
docker exec unops-conductor ls -la /var/run/docker.sock

# Test Docker access
docker exec unops-conductor docker ps
```

## Updating

The template is configured with `--restart=unless-stopped`. To update:

1. **Apps** → **Docker** → **unops-conductor** → **Check for Updates**
2. Or manually: `docker pull ghcr.io/cameronsjo/unops-conductor:latest`

## Support

- **Issues**: https://github.com/cameronsjo/unops/issues
- **Docs**: https://github.com/cameronsjo/unops

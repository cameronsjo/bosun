# Command Reference

Complete reference for all bosun CLI commands.

## Global Flags

| Flag | Description |
|------|-------------|
| `--help`, `-h` | Show help for any command |
| `--version`, `-v` | Show version |

## Setup Commands

### init

Interactive setup wizard to configure your yacht.

```bash
bosun init
```

Creates a `bosun.yaml` configuration file in the current directory.

## Yacht Commands

Manage Docker Compose services (the whole fleet).

### yacht up

Start the yacht (docker compose up -d).

```bash
bosun yacht up
bosun yacht up [services...]
```

**Examples:**

```bash
bosun yacht up                    # Start all services
bosun yacht up traefik authelia   # Start specific services
```

Automatically checks if Traefik is running before starting other services.

### yacht down

Dock the yacht (docker compose down).

```bash
bosun yacht down
```

Stops and removes all services defined in the compose file.

### yacht restart

Quick turnaround (docker compose restart).

```bash
bosun yacht restart
bosun yacht restart [services...]
```

**Examples:**

```bash
bosun yacht restart              # Restart all services
bosun yacht restart myapp        # Restart specific service
```

### yacht status

Check if we're seaworthy.

```bash
bosun yacht status
```

Shows the status of all services in the compose file.

## Crew Commands

Manage individual containers.

### crew list

Show all hands on deck.

```bash
bosun crew list
bosun crew list -a
```

**Flags:**

| Flag | Description |
|------|-------------|
| `-a`, `--all` | Show all containers (including stopped) |

**Example output:**

```
NAME          STATUS              PORTS
traefik       Up 3 days           80/tcp, 443/tcp
authelia      Up 3 days (healthy) 9091/tcp
myapp         Up 2 hours          8080/tcp
```

### crew logs

Tail crew member logs.

```bash
bosun crew logs <name>
bosun crew logs <name> -f
bosun crew logs <name> -n 50
```

**Flags:**

| Flag | Description |
|------|-------------|
| `-f`, `--follow` | Follow log output |
| `-n`, `--tail` | Number of lines to show (default: 100) |

**Examples:**

```bash
bosun crew logs traefik           # Last 100 lines
bosun crew logs traefik -f        # Stream logs
bosun crew logs traefik -n 20     # Last 20 lines
```

### crew inspect

Show detailed crew info.

```bash
bosun crew inspect <name>
```

Outputs container details as formatted JSON.

### crew restart

Send crew member for coffee break.

```bash
bosun crew restart <name>
```

Restarts a specific container.

## Manifest Commands

Render service manifests to compose/traefik/gatus configs.

### provision

Render a stack or service manifest.

```bash
bosun provision <stack>
bosun provision <stack> -n
bosun provision <stack> -d
bosun provision <stack> -f prod.yaml
```

**Flags:**

| Flag | Description |
|------|-------------|
| `-n`, `--dry-run` | Show output without writing files |
| `-d`, `--diff` | Show diff against existing files |
| `-f`, `--values` | Apply values overlay file |

**Examples:**

```bash
bosun provision core              # Render the 'core' stack
bosun provision core -n           # Dry run - preview output
bosun provision core -f prod.yaml # Apply production values
```

**Output:**

Creates files in the output directory:

- `compose/<stack>.yml` - Docker Compose file
- `traefik/dynamic.yml` - Traefik dynamic config
- `gatus/endpoints.yml` - Gatus monitoring endpoints

### provisions

List available provisions.

```bash
bosun provisions
```

**Example output:**

```
Available provisions:
  - container
  - healthcheck
  - homepage
  - monitoring
  - postgres
  - redis
  - reverse-proxy
```

### create

Scaffold new service from template.

```bash
bosun create <template> <name>
```

**Templates:**

| Template | Description |
|----------|-------------|
| `webapp` | Web application with Traefik routing |
| `api` | API service with health checks |
| `worker` | Background worker service |
| `static` | Static file server |

**Examples:**

```bash
bosun create webapp myapp
bosun create api myapi
bosun create worker myworker
```

Creates a service manifest in `manifest/services/<name>.yml`.

## Radio Commands

Communication and connectivity commands.

### radio test

Test webhook endpoint.

```bash
bosun radio test
```

Sends a GET request to `http://localhost:8080/health` to verify the webhook receiver is running.

### radio status

Check Tailscale/tunnel status.

```bash
bosun radio status
```

Displays:

- Connection state (Running, Stopped, NeedsLogin)
- This device info (hostname, IP, DNS)
- Network info (tailnet, peer count)
- Online peers

## Diagnostics Commands

### status

Show yacht health dashboard.

```bash
bosun status
```

Displays:

- Crew status (running/total containers, health)
- Infrastructure (traefik, authelia, gatus)
- Applications (all other containers)
- Resources (memory, CPU, volumes)
- Recent activity

### log

Show release history.

```bash
bosun log
bosun log <n>
```

**Arguments:**

| Argument | Description |
|----------|-------------|
| `n` | Number of entries to show (default: 10) |

Displays:

- Recent manifest changes (git log)
- Last provisions (file timestamps)
- Deploy tags

### drift

Show drift between declared services (from deploy state) and actual running containers.

By default reads cached drift status from the deploy state file. The daemon updates this automatically on a configurable interval (default: 5 minutes). Use `--live` to perform a fresh check against Docker.

```bash
bosun drift                    # Show last cached drift result
bosun drift --live             # Check Docker right now
bosun drift --json             # Machine-readable output
bosun drift --live --json      # Live check with JSON output
bosun drift --project core     # Filter to a specific compose project
bosun drift --target=nas       # Show drift for a specific target
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--live` | `false` | Perform a live drift check against Docker |
| `--json` | `false` | Output as JSON |
| `--state-file` | `/var/lib/bosun/deploy-state.json` | Path to deploy state file |
| `--project` | `""` | Docker Compose project name for filtering |
| `--target` | `""` | Show drift for a specific named target (from `targets:` config) |

**Drift types:**

| Type | Severity | Description |
|------|----------|-------------|
| `missing` | Critical | Declared service is not running (or exited) |
| `unhealthy` | Critical | Service is running but health check is failing |
| `image_mismatch` | Warning | Running image differs from declared image |

**Container matching:** Uses Docker Compose v2 labels (`com.docker.compose.project`, `com.docker.compose.service`) for authoritative matching. Falls back to name-based parsing (`<project>-<service>-<replica>`) for containers without labels.

### doctor

Pre-flight checks - is the ship seaworthy?

```bash
bosun doctor
```

Checks:

- Docker running
- Docker Compose v2 installed
- Git installed
- Project root found
- Age key present
- SOPS installed
- Manifest directory exists
- Webhook responding
- Traefik configuration (if Traefik service detected):
  - HTTPS redirect configured
  - `exposedByDefault` set to false
  - Security headers middleware present
  - Docker socket not mounted directly (recommends docker-socket-proxy)

### lint

Validate all manifests before deploy.

```bash
bosun lint
bosun lint [target]
```

Validates:

- Provisions exist
- Service manifests have required fields
- Stack manifests are valid
- Dependencies are correct
- No port conflicts

## Upgrade Commands

### upgrade traefik

Check and apply Traefik security and performance defaults.

```bash
bosun upgrade traefik                # Show recommendations (dry-run by default)
bosun upgrade traefik --yes          # Apply all recommendations without prompting
bosun upgrade traefik --dry-run      # Explicit dry-run mode
bosun upgrade traefik --compose ./compose/core.yml  # Specify compose file
bosun upgrade traefik --dynamic ./traefik/conf.d     # Specify dynamic config dir
```

**Flags:**

| Flag | Description |
|------|-------------|
| `--dry-run` | Show recommendations without applying |
| `-y`, `--yes` | Apply all recommendations without prompting |
| `--compose` | Path to compose file containing Traefik service |
| `--dynamic` | Path to Traefik dynamic config directory |

**Checks performed:**

| Check | What It Looks For | Status If Missing |
|-------|-------------------|-------------------|
| HTTPS Redirect | `--entrypoints.web.http.redirections.entrypoint.to=websecure` | missing |
| Exposed By Default | `--providers.docker.exposedbydefault=false` | warn |
| Default Rule | `--providers.docker.defaultRule` | missing |
| Security Headers | `secure-defaults` middleware in dynamic config | missing |
| Compression | `default-compress` middleware in dynamic config | missing |
| ACME Resolver | `--certificatesresolvers.*.acme.*` flags | missing |

**Auto-detection:** If `--compose` is not specified, bosun scans the output directory, project root, and `bosun/` directory for compose files containing a `traefik:*` image or a service named `traefik`. Dynamic config directory is detected from Traefik volume mounts (paths containing `conf.d`, `dynamic`, or `rules`).

**Template safety:** If the compose file is a Go template (`.tmpl` extension or contains `{{`), fixes are displayed but not auto-applied.

## Emergency Commands

### mayday

Show recent errors across all crew.

```bash
bosun mayday
bosun mayday -l
bosun mayday -r <snapshot>
bosun mayday -r interactive
```

**Flags:**

| Flag | Description |
|------|-------------|
| `-l`, `--list` | List available snapshots |
| `-r`, `--rollback` | Rollback to a snapshot |

**Examples:**

```bash
bosun mayday                    # Show recent errors
bosun mayday -l                 # List snapshots
bosun mayday -r interactive     # Interactive rollback menu
bosun mayday -r 2024-01-15_143022  # Rollback to specific snapshot
```

### overboard

Force remove a problematic container.

```bash
bosun overboard <name>
```

Forcefully removes a container. Use with caution.

## Daemon Commands

Run bosun as a long-running daemon for production GitOps deployments.

### daemon

Run the GitOps daemon.

```bash
bosun daemon
bosun daemon -n
bosun daemon -p 9090
bosun daemon -i 1800
```

**Flags:**

| Flag | Description |
|------|-------------|
| `-n`, `--dry-run` | Dry run mode (no actual changes) |
| `-p`, `--port` | HTTP server port (default: 8080) |
| `-i`, `--poll-interval` | Poll interval in seconds (default: 3600, 0 disables) |

**Features:**

- Unix socket API at `/var/run/bosun.sock` (primary)
- Optional TCP API with bearer token auth
- HTTP endpoints for webhooks and health checks
- Polling-based reconciliation
- Graceful shutdown on SIGTERM/SIGINT

**Endpoints:**

| Path | Method | Description |
|------|--------|-------------|
| `/health` | GET | Health check (JSON) |
| `/ready` | GET | Readiness check |
| `/webhook` | POST | Generic webhook trigger |
| `/webhook/github` | POST | GitHub push webhook |
| `/webhook/gitlab` | POST | GitLab push webhook |
| `/webhook/gitea` | POST | Gitea push webhook |
| `/webhook/bitbucket` | POST | Bitbucket push webhook |

### trigger

Trigger reconciliation via the daemon.

```bash
bosun trigger
bosun trigger -s "manual"
bosun trigger --socket /tmp/bosun.sock
bosun trigger --tcp localhost:9090 --token mytoken
```

**Flags:**

| Flag | Description |
|------|-------------|
| `-f`, `--force` | Force full reconciliation regardless of state |
| `-s`, `--source` | Source identifier (default: "cli") |
| `--socket` | Path to daemon socket (default: /var/run/bosun.sock) |
| `--tcp` | TCP address for remote daemon |
| `--token` | Bearer token for TCP auth |
| `-t`, `--timeout` | Timeout in seconds (default: 30) |

### daemon-status

Show daemon health and state.

```bash
bosun daemon-status
bosun daemon-status --json
bosun daemon-status --socket /tmp/bosun.sock
```

**Flags:**

| Flag | Description |
|------|-------------|
| `--json` | Output as JSON |
| `--socket` | Path to daemon socket |

**Output:**

```
=== Bosun Daemon Status ===

  ● State: idle
    Uptime: 2h30m
    Last Reconcile: 5m ago
  ✓ Health: healthy
  ✓ Ready: true
```

### validate

Validate configuration and daemon connectivity.

```bash
bosun validate
bosun validate --full
bosun validate --socket /tmp/bosun.sock
```

**Flags:**

| Flag | Description |
|------|-------------|
| `--full` | Run full dry-run reconciliation |
| `--socket` | Path to daemon socket |
| `-t`, `--timeout` | Timeout in seconds (default: 30) |

**Checks:**

1. Environment variables (REPO_URL, etc.)
2. Daemon connectivity
3. Repository access
4. Full dry-run (with `--full`)

### webhook

Run standalone webhook receiver.

```bash
bosun webhook
bosun webhook -p 9000
bosun webhook --fetch-secret
```

**Flags:**

| Flag | Description |
|------|-------------|
| `-p`, `--port` | HTTP port (default: 8080) |
| `--socket` | Path to daemon socket |
| `--secret` | Webhook secret for signature validation |
| `--fetch-secret` | Fetch secret from daemon (never stored on disk) |

The webhook receiver validates signatures and forwards valid requests to the daemon's trigger endpoint. Supports GitHub, GitLab, Gitea, and Bitbucket webhook formats.

**Daemon-Injected Secrets:**

Use `--fetch-secret` to have the webhook server fetch the secret from the daemon at startup. This way the secret is never stored on disk in the webhook container.

### init --systemd

Generate systemd unit files for daemon deployment.

```bash
bosun init --systemd
```

Creates files in `systemd/`:

| File | Description |
|------|-------------|
| `bosund.service` | Systemd service unit |
| `bosund.socket` | Socket activation unit |
| `bosund.env.example` | Environment template |
| `install.sh` | Installation script |

**Installation:**

```bash
cd systemd && sudo ./install.sh
```

## GitOps Command

### reconcile

Run the GitOps reconciliation workflow (one-shot mode).

```bash
bosun reconcile
bosun reconcile -n
bosun reconcile -f
bosun reconcile -l
bosun reconcile -r user@host
bosun reconcile --target=nas
```

**Flags:**

| Flag | Description |
|------|-------------|
| `-n`, `--dry-run` | Show what would be done without changes |
| `-f`, `--force` | Force deployment even if no changes |
| `-l`, `--local` | Force local deployment mode |
| `-r`, `--remote` | Target host for remote deployment |
| `--target` | Reconcile a single named target (from `targets:` config) |

**Workflow:**

1. Acquire lock (prevent concurrent runs)
2. Clone/pull repository (go-git library, in-process)
3. Decrypt secrets (go-sops library, in-process)
4. Render templates (native Go text/template + Sprig)
5. Create backup of current configs
6. Deploy (native file copy or tar-over-SSH)
7. Docker compose up
8. SIGHUP to agentgateway
9. Release lock

**Environment Variables:**

| Variable | Description | Default |
|----------|-------------|---------|
| `REPO_URL` | Git repository URL | Required |
| `REPO_BRANCH` | Git branch to track | `main` |
| `REPO_DIR` | Local repo directory | `/app/repo` |
| `STAGING_DIR` | Staging directory | `/app/staging` |
| `BACKUP_DIR` | Backup directory | `/app/backups` |
| `LOG_DIR` | Log directory | `/app/logs` |
| `LOCAL_APPDATA` | Local appdata path | `/mnt/appdata` |
| `REMOTE_APPDATA` | Remote appdata path | `/mnt/user/appdata` |
| `DEPLOY_TARGET` | Target host | Local if unset |
| `SECRETS_FILES` | Comma-separated SOPS files | None |
| `DRY_RUN` | Enable dry run | `false` |
| `FORCE` | Force deployment | `false` |

## Pirate Mode (Easter Egg)

```bash
bosun yarr
```

Shows command aliases for true pirates.

## Command Aliases

All commands have nautical aliases:

| Command | Alias |
|---------|-------|
| `yacht` | `hoist` |
| `crew` | `scallywags` |
| `provision` | `plunder`, `loot`, `forge` |
| `radio` | `parrot` |
| `status` | `bridge` |
| `log` | `ledger` |
| `drift` | - |
| `doctor` | `checkup` |
| `lint` | `inspect` |
| `mayday` | `mutiny` |
| `overboard` | `plank` |

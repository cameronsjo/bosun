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

Detect config drift between git and running state.

```bash
bosun drift
```

Compares:

- Manifest services vs running containers
- Expected images vs running images
- Orphaned containers (running but not in manifest)

Exit code 1 if drift detected.

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

## GitOps Command

### reconcile

Run the GitOps reconciliation workflow.

```bash
bosun reconcile
bosun reconcile -n
bosun reconcile -f
bosun reconcile -l
bosun reconcile -r user@host
```

**Flags:**

| Flag | Description |
|------|-------------|
| `-n`, `--dry-run` | Show what would be done without changes |
| `-f`, `--force` | Force deployment even if no changes |
| `-l`, `--local` | Force local deployment mode |
| `-r`, `--remote` | Target host for remote deployment |

**Workflow:**

1. Acquire lock (prevent concurrent runs)
2. Clone/pull repository
3. Decrypt secrets with SOPS
4. Render templates with Chezmoi
5. Create backup of current configs
6. Deploy (rsync or local copy)
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
| `drift` | `compass` |
| `doctor` | `checkup` |
| `lint` | `inspect` |
| `mayday` | `mutiny` |
| `overboard` | `plank` |

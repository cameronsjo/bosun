# Bosun CLI Commands

Complete reference for every bosun command. All commands support `--help` for inline help.

**Global flags:** `--help`/`-h` (help), `--version` (version), `--verbose` (debug output). Exit codes: `0` = success, `1` = error.

---

## Setup Commands

### `bosun init [directory]`

Create a new bosun project with directory structure, encryption keys, and starter files.

```bash
bosun init                     # Initialize in current directory
bosun init my-homelab          # Initialize in new directory
bosun init --yes               # Non-interactive (skip all prompts)
bosun init --systemd           # Also generate systemd unit files
```

**Alias:** `christen`

Creates: `bosun/`, `manifest/{provisions,services,stacks}/`, `.sops.yaml`, `.gitignore`, `README.md`. With `--systemd`, also generates systemd unit files in `systemd/`.

### `bosun doctor`

Pre-flight checks. Validates Docker, Compose v2, Git, SOPS, Age key, project root, manifest directory, and webhook status.

```bash
bosun doctor
```

**Alias:** `checkup`

### `bosun update`

Update bosun to the latest release. Shows version info and changelog. **Aliases:** `upgrade`, `selfupdate`

```bash
bosun update                   # Download and install latest version
bosun update --check           # Check for updates without installing
```

---

## Yacht Commands (Stack Management)

Manage Docker Compose stacks. All yacht commands validate the compose file before operating.

**Parent alias:** `hoist` (e.g., `bosun hoist up`)

### `bosun yacht up [services...]`

Start Docker Compose services. Validates compose syntax, checks that Traefik is running (starting it if needed), then runs `docker compose up -d`.

```bash
bosun yacht up                 # Start all services
bosun yacht up nginx redis     # Start specific services
```

### `bosun yacht down`

Stop and remove all services defined in the compose file.

```bash
bosun yacht down
```

### `bosun yacht restart [services...]`

Restart services. Validates compose file and service names before restarting.

```bash
bosun yacht restart            # Restart all services
bosun yacht restart myapp      # Restart specific service
```

### `bosun yacht status`

Show status of all services in the compose file.

```bash
bosun yacht status
```

---

## Crew Commands (Container Management)

Manage individual containers. **Parent alias:** `scallywags`

### `bosun crew list`

List running containers with name, status, and ports.

```bash
bosun crew list                # Running containers only
bosun crew list -a             # Include stopped containers
```

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

### `bosun crew logs <name>`

View container logs.

```bash
bosun crew logs traefik        # Last 100 lines
bosun crew logs traefik -f     # Stream logs (follow)
bosun crew logs traefik -n 20  # Last 20 lines
```

| Flag | Default | Description |
|------|---------|-------------|
| `-f`, `--follow` | `false` | Follow log output |
| `-n`, `--tail` | `100` | Number of lines to show |

### `bosun crew inspect <name>`

Show detailed container information as formatted JSON. Includes image, ports, environment, mounts, labels, and network configuration.

```bash
bosun crew inspect myapp
```

### `bosun crew restart <name>`

Restart a specific container.

```bash
bosun crew restart myapp
```

---

## Manifest Commands

Render service manifests into Docker Compose, Traefik, and Gatus configurations.

### `bosun provision <stack>`

Render a stack or service manifest into output files.

```bash
bosun provision core                  # Render the 'core' stack
bosun provision core -n               # Dry run -- preview without writing
bosun provision core -d               # Show diff against existing files
bosun provision core -f prod.yaml     # Apply values overlay file
```

**Aliases:** `plunder`, `loot`, `forge`

| Flag | Description |
|------|-------------|
| `-n`, `--dry-run` | Show output without writing files |
| `-d`, `--diff` | Show diff against existing files |
| `-f`, `--values` | Apply values overlay file |

**Output files created:**

| File | Content |
|------|---------|
| `compose/<stack>.yml` | Docker Compose file |
| `traefik/dynamic.yml` | Traefik dynamic routing config |
| `gatus/endpoints.yml` | Gatus monitoring endpoints |

### `bosun provisions`

List all available provisions (templates).

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

### `bosun create <template> <name>`

Scaffold a new service manifest from a template.

```bash
bosun create webapp myapp
bosun create api myapi
bosun create worker myworker
bosun create static mysite
```

| Template | Description |
|----------|-------------|
| `webapp` | Web application with Traefik routing |
| `api` | API service with health checks |
| `worker` | Background worker service |
| `static` | Static file server |

Creates `manifest/services/<name>.yml`.

### `bosun lint [target]`

Validate manifests before deploying.

```bash
bosun lint                     # Validate all manifests
bosun lint mystack             # Validate specific target
```

**Alias:** `inspect`

**Validates:**
- Provisions exist and are loadable
- Service manifests have required fields
- Stack manifests are well-formed
- Dependencies are correct
- No port conflicts

### `bosun render [file.tmpl...]`

Render Go templates with decrypted SOPS secrets. Useful for previewing templated configs before pushing to git.

```bash
bosun render config.yml.tmpl             # Render single template to stdout
bosun render unraid/compose/*.tmpl       # Render all templates in directory
bosun render -s secrets.sops.yaml *.tmpl # Specify secrets file
bosun render -o /tmp/rendered unraid/    # Render to output directory
```

| Flag | Description |
|------|-------------|
| `-s`, `--secrets` | SOPS secrets file (default: auto-detect from `BOSUN_SECRETS_FILE` env var, or `secrets.sops.yaml`) |
| `-o`, `--output` | Output directory (prints to stdout if not set) |

Templates have access to:
- Secrets data via `{{ . }}` (the root context)
- All Sprig template functions
- Custom functions: `include`, `fromJsonFile`

---

## GitOps Commands

### `bosun daemon`

Run the GitOps daemon. Long-running service that watches for changes and auto-deploys.

```bash
bosun daemon                          # Default: webhook-only, port 8080
bosun daemon -n                       # Dry run mode (no actual changes)
bosun daemon -p 9090                  # Custom HTTP port
bosun daemon --poll-interval 1800     # Poll every 30 minutes
bosun daemon --poll-interval 0        # Disable polling (webhook-only)
```

| Flag | Default | Description |
|------|---------|-------------|
| `-n`, `--dry-run` | `false` | Dry run mode (no actual changes) |
| `-p`, `--port` | `8080` | HTTP server port |
| `-i`, `--poll-interval` | `3600` | Poll interval in seconds (0 = disabled) |

**Exposes:**
- Unix socket API at `/var/run/bosun.sock`
- HTTP endpoints for webhooks and health
- Optional TCP API with bearer token auth

For full daemon architecture details, see `@gitops.md`.

### `bosun reconcile`

One-shot GitOps workflow. Clone repo, decrypt secrets, template configs, deploy, compose up.

```bash
bosun reconcile                # Full reconciliation
bosun reconcile -n             # Dry run (show what would change)
bosun reconcile -f             # Force deploy even if no changes
bosun reconcile -l             # Force local deployment mode
bosun reconcile -r user@host   # Deploy to remote host
```

| Flag | Description |
|------|-------------|
| `-n`, `--dry-run` | Show what would be done without changes |
| `-f`, `--force` | Force deployment even if no changes detected |
| `-l`, `--local` | Force local deployment mode |
| `-r`, `--remote` | Target host for remote deployment (SSH) |

### `bosun trigger`

Tell a running daemon to start reconciliation.

```bash
bosun trigger                           # Via Unix socket (default)
bosun trigger -s "manual"               # Set source identifier
bosun trigger -f                        # Force full reconciliation
bosun trigger --socket /tmp/bosun.sock  # Custom socket path
bosun trigger --tcp localhost:9090 --token mytoken  # Remote trigger
```

| Flag | Default | Description |
|------|---------|-------------|
| `-f`, `--force` | `false` | Force full reconciliation |
| `-s`, `--source` | `cli` | Source identifier |
| `--socket` | `/var/run/bosun.sock` | Daemon socket path |
| `--tcp` | | TCP address for remote daemon |
| `--token` | | Bearer token for TCP auth |
| `-t`, `--timeout` | `30` | Timeout in seconds |

### `bosun drift`

Show drift between declared services and actual running containers.

```bash
bosun drift                    # Show last cached drift result
bosun drift --live             # Fresh check against Docker right now
bosun drift --json             # Machine-readable output
bosun drift --live --json      # Live check + JSON
bosun drift --project core     # Filter to a compose project
```

| Flag | Default | Description |
|------|---------|-------------|
| `--live` | `false` | Perform a live drift check against Docker |
| `--json` | `false` | Output as JSON |
| `--state-file` | `/var/lib/bosun/deploy-state.json` | Path to deploy state file |
| `--project` | | Docker Compose project name for filtering |

**Drift types detected:**

| Type | Severity | Meaning |
|------|----------|---------|
| `missing` | Critical | Declared service is not running (or exited) |
| `unhealthy` | Critical | Service is running but health check is failing |
| `image_mismatch` | Warning | Running image differs from declared image |

By default reads from the daemon's cached drift state. Use `--live` for a fresh Docker check.

---

## Diagnostics Commands

### `bosun status`

Health dashboard showing overall yacht status.

```bash
bosun status
```

**Alias:** `bridge`

**Displays:**
- Crew status (running/total containers, health)
- Infrastructure services (traefik, authelia, gatus)
- Application containers
- Resources (memory, CPU, volumes)
- Recent activity

### `bosun log [n]`

Show release history. Displays recent manifest changes (git log), last provisions, and deploy tags.

```bash
bosun log                      # Last 10 entries
bosun log 20                   # Last 20 entries
```

**Alias:** `ledger`

### `bosun daemon-status`

Show daemon health and state.

```bash
bosun daemon-status                    # Human-readable
bosun daemon-status --json             # Machine-readable
bosun daemon-status --socket /tmp/bosun.sock  # Custom socket
```

### `bosun validate`

Validate configuration and daemon connectivity.

```bash
bosun validate                         # Basic validation
bosun validate --full                  # Include full dry-run reconciliation
```

| Flag | Description |
|------|-------------|
| `--full` | Run full dry-run reconciliation |
| `--socket` | Path to daemon socket |
| `-t`, `--timeout` | Timeout in seconds (default: 30) |

---

## Radio Commands (Connectivity)

### `bosun radio test`

Test webhook endpoint. Sends a GET request to `http://localhost:8080/health`.

```bash
bosun radio test
```

**Alias:** `parrot test`

### `bosun radio status`

Check Tailscale/tunnel connectivity.

```bash
bosun radio status
```

**Displays:** Connection state, device info (hostname, IP, DNS), network info (tailnet, peer count), and online peers.

---

## Emergency Commands

### `bosun mayday`

Error triage and rollback.

```bash
bosun mayday                   # Show recent errors across all containers
bosun mayday -l                # List available snapshots
bosun mayday -r interactive    # Interactive rollback menu
bosun mayday -r 2024-01-15_143022  # Rollback to specific snapshot
```

**Alias:** `mutiny`

| Flag | Description |
|------|-------------|
| `-l`, `--list` | List available snapshots |
| `-r`, `--rollback` | Rollback to a snapshot (name or "interactive") |

### `bosun overboard <name>`

Force-remove a problematic container. Use with caution.

```bash
bosun overboard myapp
```

**Alias:** `plank`

---

## Webhook Server

### `bosun webhook`

Run a standalone webhook receiver (separate from the daemon).

```bash
bosun webhook                          # Default port 8080
bosun webhook -p 9000                  # Custom port
bosun webhook --fetch-secret           # Fetch secret from daemon
bosun webhook --secret mysecret        # Set secret directly
```

| Flag | Description |
|------|-------------|
| `-p`, `--port` | HTTP port (default: 8080) |
| `--socket` | Path to daemon socket |
| `--secret` | Webhook secret for signature validation |
| `--fetch-secret` | Fetch secret from daemon at startup (never stored on disk) |

Validates webhook signatures and forwards valid requests to the daemon's trigger endpoint. Supports GitHub, GitLab, Gitea, and Bitbucket formats.

# Bosun CLI Commands

Complete reference for every bosun command. All commands support `--help` for inline help.

**Global flags:** `--help`/`-h` (help), `--version` (version), `--verbose` (debug output), `--log-level` (debug/info/warn/error), `--log-format` (json/console/auto). Exit codes: `0` = success, `1` = error.

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

Pre-flight checks. Validates Docker, Compose v2, Git, SOPS, Age key, project root, manifest directory, webhook status, and Traefik configuration (HTTPS redirect, exposedByDefault, security headers, Docker socket proxy).

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

```bash
bosun yacht up                 # Start all services (validates compose, checks Traefik first)
bosun yacht up nginx redis     # Start specific services
bosun yacht down               # Stop and remove all services
bosun yacht restart             # Restart all services
bosun yacht restart myapp      # Restart specific service
bosun yacht status             # Show status of all compose services
```

---

## Crew Commands (Container Management)

Manage individual containers. **Parent alias:** `scallywags`

```bash
bosun crew list                # List running containers (name, status, ports)
bosun crew list -a             # Include stopped containers
bosun crew logs <name>         # Last 100 lines of container logs
bosun crew logs <name> -f      # Stream logs (follow)
bosun crew logs <name> -n 20   # Last 20 lines
bosun crew inspect <name>      # Detailed container info as formatted JSON
bosun crew restart <name>      # Restart a specific container
```

| Flag | Applies To | Default | Description |
|------|-----------|---------|-------------|
| `-a`, `--all` | `list` | `false` | Show all containers (including stopped) |
| `-f`, `--follow` | `logs` | `false` | Follow log output |
| `-n`, `--tail` | `logs` | `100` | Number of lines to show |

---

## Manifest Commands

Render service manifests into Docker Compose, Traefik, and Gatus configurations.

### `bosun provision <stack>`

Render a stack or service manifest into output files.

```bash
bosun provision core                        # Render the 'core' stack
bosun provision core -n                     # Dry run -- preview without writing
bosun provision core -d                     # Show diff against existing files
bosun provision core -f prod.yaml           # Apply values overlay file
bosun provision core --set db.host=localhost # Override individual values
```

**Aliases:** `plunder`, `loot`, `forge`. Supports both legacy (provisions) and Helm-aligned (charts) formats, auto-detected from directory structure.

| Flag | Description |
|------|-------------|
| `-n`, `--dry-run` | Show output without writing files |
| `-d`, `--diff` | Show diff against existing files |
| `-f`, `--values` | Apply values overlay file |
| `--set` | Override individual values (repeatable, dot notation: `--set key=value`) |

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

```text
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

### Helm-Aligned Charts

```bash
bosun chart list               # List available charts (name, version, description)
bosun chart show <name>        # Detailed chart info (templates, deps, version)
bosun template list            # List available templates (alias: templates)
```

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

## Upgrade Commands

### `bosun upgrade traefik`

Check Traefik configuration against Bosun's recommended security and performance defaults and interactively apply improvements.

```bash
bosun upgrade traefik                # Show recommendations (dry-run by default)
bosun upgrade traefik --yes          # Apply all recommendations
bosun upgrade traefik --dry-run      # Explicit dry-run
bosun upgrade traefik --compose ./compose/core.yml  # Specify compose file
bosun upgrade traefik --dynamic ./traefik/conf.d     # Specify dynamic config dir
```

| Flag | Description |
|------|-------------|
| `--dry-run` | Show recommendations without applying |
| `-y`, `--yes` | Apply all recommendations without prompting |
| `--compose` | Path to compose file containing Traefik service |
| `--dynamic` | Path to Traefik dynamic config directory |

**6 checks:** HTTPS redirect, exposedByDefault, defaultRule, security headers, compression, ACME resolver. Additive only — never removes existing config. Template files (`.tmpl`) get display-only output.

---

## Radio Commands (Connectivity)

**Parent alias:** `parrot`

```bash
bosun radio test               # Test webhook endpoint (GET /health)
bosun radio status             # Check Tailscale/tunnel connectivity, peers, network info
```

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

---

## Alert Commands

**Parent alias:** `horn`

### `bosun alert status`

Show configured alert providers (Discord, SendGrid, Twilio) with masked credentials and settings.

```bash
bosun alert status
```

### `bosun alert test`

Send a test alert to configured providers.

```bash
bosun alert test                       # Test all configured providers
bosun alert test -p discord            # Test specific provider
bosun alert test -m "Custom message"   # Custom test message
bosun alert test -s error              # Set severity (info/warning/error)
```

| Flag | Description |
|------|-------------|
| `-p`, `--provider` | Test specific provider (discord, sendgrid, twilio) |
| `-m`, `--message` | Custom test message |
| `-s`, `--severity` | Alert severity (info, warning, error) |

---

## Backup & Restore

### `bosun restore [backup-name]`

Restore infrastructure configs from a backup created during reconciliation.

```bash
bosun restore                  # Restore latest backup
bosun restore -l               # List available backups
bosun restore 2024-01-15_143022  # Restore specific backup
```

| Flag | Description |
|------|-------------|
| `-l`, `--list` | List available backups |

After restoring, automatically runs `docker compose up -d` if a compose file is present.

---

## Shell Completion

### `bosun completion [shell]`

Generate shell completion scripts.

```bash
bosun completion bash | sudo tee /etc/bash_completion.d/bosun
bosun completion zsh | sudo tee "${fpath[1]}/_bosun"
bosun completion fish | source
bosun completion powershell | Out-String | Invoke-Expression
```

Supports: `bash`, `zsh`, `fish`, `powershell`.

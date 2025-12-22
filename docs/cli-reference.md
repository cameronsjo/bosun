# Bosun CLI Reference

Bosun is a nautical-themed GitOps toolkit for managing Docker Compose deployments with Traefik, Gatus, and Homepage integration.

## Global Options

These options are available for all commands:

| Flag | Description |
|------|-------------|
| `--help`, `-h` | Display help for any command |
| `--version` | Display version information |

## Exit Codes

All bosun commands use standard exit codes:

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | General error (configuration, runtime, or validation failure) |

---

## Setup Commands

### bosun

The root command. When called without subcommands, displays help.

**Synopsis:**

Helm for home - GitOps for Docker Compose

**Usage:**

```bash
bosun [command]
```

**Description:**

Bosun provides a comprehensive set of commands for managing Docker Compose deployments in a GitOps workflow. Commands are organized into logical groups: setup, yacht (stack management), crew (container management), manifests, communications, diagnostics, and emergency operations.

**Related Commands:**

- [init](#bosun-init) - Initialize a new project
- [doctor](#bosun-doctor) - Check system prerequisites

---

### bosun init

Initialize a new bosun project with the required directory structure.

**Synopsis:**

Christen your yacht (interactive setup wizard)

**Usage:**

```bash
bosun init [directory]
```

**Aliases:**

- `christen` (pirate mode)

**Description:**

Creates a new bosun project with the required directory structure, encryption keys, and starter files. If no directory is specified, the current directory is used.

Creates the following structure:

```
.
├── bosun/               # Webhook receiver compose file
│   └── docker-compose.yml
├── manifest/            # Service definitions
│   ├── provisions/      # Reusable templates
│   ├── services/        # Individual services
│   └── stacks/          # Service groups
├── .sops.yaml           # SOPS encryption config
├── .gitignore           # Git ignore file
└── README.md            # Project documentation
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--yes`, `-y` | `false` | Skip all interactive prompts (assume yes for all questions) |

**Examples:**

```bash
# Initialize in current directory
bosun init

# Initialize in a new directory
bosun init my-homelab

# Non-interactive initialization (useful for CI/CD)
bosun init --yes

# Pirate mode
bosun christen my-homelab
```

**Exit Codes:**

| Code | Meaning |
|------|---------|
| `0` | Project initialized successfully |
| `1` | Failed to create directory structure or files |

**Related Commands:**

- [doctor](#bosun-doctor) - Verify setup after initialization
- [yacht up](#bosun-yacht-up) - Start the webhook receiver

---

## Yacht Commands (Stack Management)

### bosun yacht

Parent command for Docker Compose stack management.

**Synopsis:**

Manage Docker Compose services

**Usage:**

```bash
bosun yacht [command]
```

**Aliases:**

- `hoist` (pirate mode)

**Description:**

Yacht commands provide high-level management of Docker Compose services. These commands operate on the compose file defined in your project configuration.

**Subcommands:**

| Command | Description |
|---------|-------------|
| `up` | Start the yacht (docker compose up -d) |
| `down` | Dock the yacht (docker compose down) |
| `restart` | Quick turnaround (docker compose restart) |
| `status` | Check if we're seaworthy |

---

### bosun yacht up

Start Docker Compose services.

**Synopsis:**

Start the yacht (docker compose up -d)

**Usage:**

```bash
bosun yacht up [services...]
```

**Description:**

Starts all services defined in the compose file, or specific services if names are provided. Before starting, validates the compose file syntax and checks that Traefik is running (attempting to start it if needed).

**Flags:**

None

**Examples:**

```bash
# Start all services
bosun yacht up

# Start specific services
bosun yacht up nginx redis

# Using pirate mode
bosun hoist up
```

**Exit Codes:**

| Code | Meaning |
|------|---------|
| `0` | Services started successfully |
| `1` | Configuration error, invalid compose file, or Docker error |

**Related Commands:**

- [yacht down](#bosun-yacht-down) - Stop services
- [yacht status](#bosun-yacht-status) - Check service status

---

### bosun yacht down

Stop and remove Docker Compose services.

**Synopsis:**

Dock the yacht (docker compose down)

**Usage:**

```bash
bosun yacht down
```

**Description:**

Stops and removes all services defined in the compose file. Validates the compose file before performing the operation.

**Flags:**

None

**Examples:**

```bash
# Stop all services
bosun yacht down

# Using pirate mode
bosun hoist down
```

**Exit Codes:**

| Code | Meaning |
|------|---------|
| `0` | Services stopped successfully |
| `1` | Configuration error or Docker error |

**Related Commands:**

- [yacht up](#bosun-yacht-up) - Start services
- [overboard](#bosun-overboard) - Force remove a container

---

### bosun yacht restart

Restart Docker Compose services.

**Synopsis:**

Quick turnaround (docker compose restart)

**Usage:**

```bash
bosun yacht restart [services...]
```

**Description:**

Restarts all services or specific named services. Validates the compose file and service names before restarting.

**Flags:**

None

**Examples:**

```bash
# Restart all services
bosun yacht restart

# Restart specific services
bosun yacht restart nginx redis

# Using pirate mode
bosun hoist restart
```

**Exit Codes:**

| Code | Meaning |
|------|---------|
| `0` | Services restarted successfully |
| `1` | Configuration error, invalid service names, or Docker error |

**Related Commands:**

- [crew restart](#bosun-crew-restart) - Restart individual container

---

### bosun yacht status

Show the status of Docker Compose services.

**Synopsis:**

Check if we're seaworthy

**Usage:**

```bash
bosun yacht status
```

**Description:**

Displays the status of all services defined in the compose file using `docker compose ps`.

**Flags:**

None

**Examples:**

```bash
bosun yacht status

# Using pirate mode
bosun hoist status
```

**Exit Codes:**

| Code | Meaning |
|------|---------|
| `0` | Status displayed successfully |
| `1` | Configuration error or Docker error |

**Related Commands:**

- [status](#bosun-status) - Show comprehensive health dashboard
- [crew list](#bosun-crew-list) - List all containers

---

## Crew Commands (Container Management)

### bosun crew

Parent command for individual container management.

**Synopsis:**

Manage containers

**Usage:**

```bash
bosun crew [command]
```

**Aliases:**

- `scallywags` (pirate mode)

**Description:**

Crew commands provide direct management of individual Docker containers, including listing, log viewing, inspection, and restart operations.

**Subcommands:**

| Command | Description |
|---------|-------------|
| `list` | Show all hands on deck (docker ps) |
| `logs` | Tail crew member logs |
| `inspect` | Detailed crew info |
| `restart` | Send crew member for coffee break |

---

### bosun crew list

List all containers.

**Synopsis:**

Show all hands on deck (docker ps)

**Usage:**

```bash
bosun crew list
```

**Description:**

Lists all containers with their name, status, and exposed ports. By default, shows only running containers.

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--all`, `-a` | `false` | Show all containers (including stopped) |

**Examples:**

```bash
# List running containers
bosun crew list

# List all containers including stopped
bosun crew list --all

# Using pirate mode
bosun scallywags list -a
```

**Exit Codes:**

| Code | Meaning |
|------|---------|
| `0` | Containers listed successfully |
| `1` | Docker connection error |

**Related Commands:**

- [yacht status](#bosun-yacht-status) - Show compose service status
- [status](#bosun-status) - Show health dashboard

---

### bosun crew logs

View container logs.

**Synopsis:**

Tail crew member logs

**Usage:**

```bash
bosun crew logs <name>
```

**Description:**

Shows logs from a specific container. Supports following logs in real-time and limiting the number of lines displayed.

**Arguments:**

| Argument | Required | Description |
|----------|----------|-------------|
| `name` | Yes | Container name |

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--tail`, `-n` | `100` | Number of lines to show |
| `--follow`, `-f` | `false` | Follow log output |

**Examples:**

```bash
# Show last 100 lines
bosun crew logs nginx

# Follow logs in real-time
bosun crew logs -f nginx

# Show last 50 lines
bosun crew logs -n 50 nginx

# Using pirate mode
bosun scallywags logs -f nginx
```

**Exit Codes:**

| Code | Meaning |
|------|---------|
| `0` | Logs displayed successfully |
| `1` | Container not found or Docker error |

**Related Commands:**

- [mayday](#bosun-mayday) - Show errors across all containers

---

### bosun crew inspect

Show detailed container information.

**Synopsis:**

Detailed crew info

**Usage:**

```bash
bosun crew inspect <name>
```

**Description:**

Displays detailed JSON-formatted information about a container, including configuration, network settings, mounts, and state.

**Arguments:**

| Argument | Required | Description |
|----------|----------|-------------|
| `name` | Yes | Container name |

**Flags:**

None

**Examples:**

```bash
# Inspect container
bosun crew inspect nginx

# Pipe to jq for filtering
bosun crew inspect nginx | jq '.NetworkSettings.Networks'

# Using pirate mode
bosun scallywags inspect nginx
```

**Exit Codes:**

| Code | Meaning |
|------|---------|
| `0` | Container inspected successfully |
| `1` | Container not found or Docker error |

**Related Commands:**

- [crew list](#bosun-crew-list) - List containers

---

### bosun crew restart

Restart a specific container.

**Synopsis:**

Send crew member for coffee break

**Usage:**

```bash
bosun crew restart <name>
```

**Description:**

Restarts a specific container by name.

**Arguments:**

| Argument | Required | Description |
|----------|----------|-------------|
| `name` | Yes | Container name |

**Flags:**

None

**Examples:**

```bash
# Restart container
bosun crew restart nginx

# Using pirate mode
bosun scallywags restart nginx
```

**Exit Codes:**

| Code | Meaning |
|------|---------|
| `0` | Container restarted successfully |
| `1` | Container not found or Docker error |

**Related Commands:**

- [yacht restart](#bosun-yacht-restart) - Restart compose services
- [overboard](#bosun-overboard) - Force remove container

---

## Manifest Commands

### bosun provision

Render manifest to compose/traefik/gatus outputs.

**Synopsis:**

Render manifest to compose/traefik/gatus

**Usage:**

```bash
bosun provision [stack]
```

**Aliases:**

- `plunder` (pirate mode)
- `loot` (pirate mode)
- `forge` (pirate mode)

**Description:**

Renders a stack or service manifest into Docker Compose, Traefik, and Gatus configuration files. Supports dry-run mode to preview output, diff mode to compare against existing files, and values overlays for environment-specific configuration.

**Arguments:**

| Argument | Required | Description |
|----------|----------|-------------|
| `stack` | Yes | Name of stack or service to render |

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--dry-run`, `-n` | `false` | Show what would be generated without writing |
| `--diff`, `-d` | `false` | Show diff against existing output files |
| `--values`, `-f` | `""` | Apply values overlay file (YAML) |

**Examples:**

```bash
# Render the 'core' stack
bosun provision core

# Dry run - show output without writing
bosun provision -n core

# Show diff against existing files
bosun provision -d core

# Apply production values overlay
bosun provision -f prod.yaml core

# Using pirate mode
bosun plunder core
```

**Exit Codes:**

| Code | Meaning |
|------|---------|
| `0` | Manifest rendered successfully |
| `1` | Stack/service not found, render error, or write error |

**Related Commands:**

- [provisions](#bosun-provisions) - List available provisions
- [create](#bosun-create) - Scaffold new service

---

### bosun provisions

List available provision templates.

**Synopsis:**

List available provisions

**Usage:**

```bash
bosun provisions
```

**Description:**

Lists all available provision templates in the provisions directory. Provisions are reusable templates that define common service patterns.

**Flags:**

None

**Examples:**

```bash
bosun provisions
```

**Exit Codes:**

| Code | Meaning |
|------|---------|
| `0` | Provisions listed successfully |
| `1` | Configuration error or directory not found |

**Related Commands:**

- [provision](#bosun-provision) - Render a manifest
- [create](#bosun-create) - Scaffold new service

---

### bosun create

Scaffold a new service from a template.

**Synopsis:**

Scaffold new service from template

**Usage:**

```bash
bosun create <template> <name>
```

**Description:**

Creates a new service manifest file from a predefined template. The service file is created in the services directory.

**Arguments:**

| Argument | Required | Description |
|----------|----------|-------------|
| `template` | Yes | Template type (webapp, api, worker, static) |
| `name` | Yes | Name for the new service |

**Available Templates:**

| Template | Description |
|----------|-------------|
| `webapp` | Web application with Traefik routing |
| `api` | API service with health checks |
| `worker` | Background worker service |
| `static` | Static file server |

**Flags:**

None

**Examples:**

```bash
# Create a webapp service
bosun create webapp my-app

# Create an API service
bosun create api my-api

# Create a background worker
bosun create worker my-worker

# Create a static file server
bosun create static my-site
```

**Exit Codes:**

| Code | Meaning |
|------|---------|
| `0` | Service created successfully |
| `1` | Unknown template, service already exists, or write error |

**Related Commands:**

- [provision](#bosun-provision) - Render the new service
- [provisions](#bosun-provisions) - List available provisions

---

## Communications Commands

### bosun radio

Parent command for connectivity and communication operations.

**Synopsis:**

Communication and connectivity commands

**Usage:**

```bash
bosun radio [command]
```

**Aliases:**

- `parrot` (pirate mode)

**Description:**

Radio commands for testing webhook connectivity and checking tunnel status.

**Subcommands:**

| Command | Description |
|---------|-------------|
| `test` | Test webhook endpoint |
| `status` | Check Tailscale/tunnel status |

---

### bosun radio test

Test the webhook endpoint.

**Synopsis:**

Test webhook endpoint

**Usage:**

```bash
bosun radio test
```

**Description:**

Sends a GET request to `http://localhost:8080/health` to verify the webhook receiver is running and responding.

**Flags:**

None

**Examples:**

```bash
bosun radio test

# Using pirate mode
bosun parrot test
```

**Exit Codes:**

| Code | Meaning |
|------|---------|
| `0` | Webhook endpoint responding with HTTP 200 |
| `1` | Connection failed or non-200 response |

**Related Commands:**

- [radio status](#bosun-radio-status) - Check network status
- [doctor](#bosun-doctor) - Full system check

---

### bosun radio status

Check Tailscale/tunnel status.

**Synopsis:**

Check Tailscale/tunnel status

**Usage:**

```bash
bosun radio status
```

**Description:**

Displays Tailscale connection status including the current device, network information, and online peers. Shows structured output if Tailscale JSON status is available, otherwise falls back to plain text output.

**Flags:**

None

**Examples:**

```bash
bosun radio status

# Using pirate mode
bosun parrot status
```

**Exit Codes:**

| Code | Meaning |
|------|---------|
| `0` | Status displayed successfully |
| `1` | Tailscale not installed or status command failed |

**Related Commands:**

- [radio test](#bosun-radio-test) - Test webhook endpoint

---

## Diagnostics Commands

### bosun status

Display the yacht health dashboard.

**Synopsis:**

Show yacht health dashboard

**Usage:**

```bash
bosun status
```

**Aliases:**

- `bridge` (pirate mode)

**Description:**

Shows a comprehensive health dashboard including:

- **Crew Status**: Container counts and health
- **Infrastructure**: Status of core services (Traefik, Authelia, Gatus)
- **Applications**: Status of non-infrastructure containers
- **Resources**: Memory and CPU usage, volume sizes
- **Recent Activity**: Latest container activity

**Flags:**

None

**Examples:**

```bash
bosun status

# Using pirate mode
bosun bridge
```

**Exit Codes:**

| Code | Meaning |
|------|---------|
| `0` | Dashboard displayed (always succeeds if Docker is available) |

**Related Commands:**

- [crew list](#bosun-crew-list) - List all containers
- [doctor](#bosun-doctor) - Run diagnostic checks

---

### bosun log

Display release history.

**Synopsis:**

Show release history

**Usage:**

```bash
bosun log [n]
```

**Aliases:**

- `ledger` (pirate mode)

**Description:**

Shows release history including recent manifest changes (from git log), last provisions timestamps, and deploy tags.

**Arguments:**

| Argument | Required | Description |
|----------|----------|-------------|
| `n` | No | Number of entries to show (default: 10) |

**Flags:**

None

**Examples:**

```bash
# Show last 10 entries
bosun log

# Show last 20 entries
bosun log 20

# Using pirate mode
bosun ledger 5
```

**Exit Codes:**

| Code | Meaning |
|------|---------|
| `0` | History displayed successfully |

**Related Commands:**

- [drift](#bosun-drift) - Check for configuration drift

---

### bosun drift

Detect configuration drift between manifests and running state.

**Synopsis:**

Detect config drift - git vs running state

**Usage:**

```bash
bosun drift
```

**Aliases:**

- `compass` (pirate mode)

**Description:**

Compares the expected state from manifest files against the actual running state of containers. Detects:

- **Image drift**: Running containers using different images than specified
- **Missing services**: Services defined in manifests but not running
- **Orphaned containers**: Running containers not defined in any manifest

**Flags:**

None

**Examples:**

```bash
bosun drift

# Using pirate mode
bosun compass
```

**Exit Codes:**

| Code | Meaning |
|------|---------|
| `0` | No drift detected - running state matches manifests |
| `1` | Drift detected or error occurred |

**Related Commands:**

- [yacht up](#bosun-yacht-up) - Reconcile drift by redeploying
- [reconcile](#bosun-reconcile) - Run full GitOps reconciliation

---

### bosun doctor

Run pre-flight diagnostic checks.

**Synopsis:**

Pre-flight checks - is the ship seaworthy?

**Usage:**

```bash
bosun doctor
```

**Aliases:**

- `checkup` (pirate mode)

**Description:**

Runs comprehensive diagnostic checks to verify the system is ready for bosun operations. Checks include:

- Docker daemon status and Compose v2
- Git installation
- Project root and manifest directory
- Age encryption key
- SOPS installation
- uv (Python package manager)
- Webhook endpoint responsiveness

Each check reports passed, warned, or failed status with remediation instructions.

**Flags:**

None

**Examples:**

```bash
bosun doctor

# Using pirate mode
bosun checkup
```

**Exit Codes:**

| Code | Meaning |
|------|---------|
| `0` | All checks passed or only warnings |
| `1` | One or more checks failed |

**Related Commands:**

- [init](#bosun-init) - Initialize project
- [lint](#bosun-lint) - Validate manifests

---

### bosun lint

Validate all manifests before deployment.

**Synopsis:**

Validate all manifests before deploy

**Usage:**

```bash
bosun lint [target]
```

**Aliases:**

- `inspect` (pirate mode)

**Description:**

Validates all manifest files including provisions, services, and stacks. Checks for:

- Valid YAML syntax
- Required fields (name, provisions)
- Dependency issues
- Port conflicts
- Dependency cycles

**Arguments:**

| Argument | Required | Description |
|----------|----------|-------------|
| `target` | No | Specific manifest to validate |

**Flags:**

None

**Examples:**

```bash
# Validate all manifests
bosun lint

# Using pirate mode
bosun inspect
```

**Exit Codes:**

| Code | Meaning |
|------|---------|
| `0` | All manifests valid |
| `1` | Validation errors found |

**Related Commands:**

- [provision](#bosun-provision) - Render validated manifests
- [doctor](#bosun-doctor) - System diagnostics

---

## Emergency Commands

### bosun mayday

Show recent errors across all containers.

**Synopsis:**

Show recent errors across all crew

**Usage:**

```bash
bosun mayday
```

**Aliases:**

- `mutiny` (pirate mode)

**Description:**

Emergency command to quickly identify problems across the fleet. By default, shows recent errors from all running container logs. Also supports listing and restoring from snapshots.

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--list`, `-l` | `false` | List available snapshots |
| `--rollback`, `-r` | `""` | Rollback to a snapshot (use 'interactive' for menu) |

**Examples:**

```bash
# Show recent errors across all containers
bosun mayday

# List available snapshots
bosun mayday --list

# Interactive rollback selection
bosun mayday --rollback interactive

# Rollback to specific snapshot
bosun mayday --rollback snapshot-2024-01-15-120000

# Using pirate mode
bosun mutiny --list
```

**Exit Codes:**

| Code | Meaning |
|------|---------|
| `0` | Command completed successfully |
| `1` | Rollback failed or error displaying errors |

**Related Commands:**

- [crew logs](#bosun-crew-logs) - View specific container logs
- [restore](#bosun-restore) - Restore from reconcile backup

---

### bosun overboard

Force remove a problematic container.

**Synopsis:**

Force remove a problematic container

**Usage:**

```bash
bosun overboard <name>
```

**Aliases:**

- `plank` (pirate mode)

**Description:**

Forcefully removes a container by name. Use with caution as this bypasses normal shutdown procedures.

**Arguments:**

| Argument | Required | Description |
|----------|----------|-------------|
| `name` | Yes | Container name to remove |

**Flags:**

None

**Examples:**

```bash
# Force remove container
bosun overboard broken-container

# Using pirate mode
bosun plank broken-container
```

**Exit Codes:**

| Code | Meaning |
|------|---------|
| `0` | Container removed successfully |
| `1` | Container not found or Docker error |

**Related Commands:**

- [crew restart](#bosun-crew-restart) - Restart instead of remove
- [yacht down](#bosun-yacht-down) - Stop all services gracefully

---

### bosun restore

Restore from a reconcile backup.

**Synopsis:**

Restore from a reconcile backup

**Usage:**

```bash
bosun restore [backup-name]
```

**Description:**

Restores infrastructure configs from a previous backup created by the reconcile command. Backups contain tarball archives of configuration files.

**Arguments:**

| Argument | Required | Description |
|----------|----------|-------------|
| `backup-name` | Conditional | Backup name (required unless using --list) |

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--list`, `-l` | `false` | List available backups |

**Environment Variables:**

| Variable | Description |
|----------|-------------|
| `BACKUP_DIR` | Custom backup directory location |
| `LOCAL_APPDATA` | Local appdata path for restore target |

**Examples:**

```bash
# List available backups
bosun restore --list

# Restore from specific backup
bosun restore backup-20240115-120000
```

**Exit Codes:**

| Code | Meaning |
|------|---------|
| `0` | Restore completed successfully |
| `1` | Backup not found, incomplete, or restore failed |

**Related Commands:**

- [mayday](#bosun-mayday) - Snapshot rollback
- [reconcile](#bosun-reconcile) - GitOps sync (creates backups)

---

## GitOps Commands

### bosun reconcile

Run the GitOps reconciliation workflow.

**Synopsis:**

Run GitOps reconciliation workflow

**Usage:**

```bash
bosun reconcile
```

**Description:**

Executes the complete GitOps reconciliation workflow:

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

| Variable | Default | Description |
|----------|---------|-------------|
| `REPO_URL` | (required) | Git repository URL |
| `REPO_BRANCH` | `main` | Git branch to track |
| `DEPLOY_TARGET` | `""` | Target host for remote deployment (e.g., root@192.168.1.8) |
| `SECRETS_FILES` | `""` | Comma-separated list of SOPS secret files |
| `REPO_DIR` | `/app/repo` | Local repo directory |
| `STAGING_DIR` | `/app/staging` | Staging directory |
| `BACKUP_DIR` | `/app/backups` | Backup directory |
| `LOG_DIR` | `/app/logs` | Log directory |
| `LOCAL_APPDATA` | `/mnt/appdata` | Local appdata path |
| `REMOTE_APPDATA` | `/mnt/user/appdata` | Remote appdata path |
| `DRY_RUN` | `false` | Dry run mode |
| `FORCE` | `false` | Force deployment |

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--dry-run`, `-n` | `false` | Show what would be done without making changes |
| `--force`, `-f` | `false` | Force deployment even if no changes detected |
| `--local`, `-l` | `false` | Force local deployment mode |
| `--remote`, `-r` | `""` | Target host for remote deployment |

**Examples:**

```bash
# Run reconciliation
REPO_URL=https://github.com/user/homelab.git bosun reconcile

# Dry run
bosun reconcile --dry-run

# Force deployment
bosun reconcile --force

# Local deployment mode
bosun reconcile --local

# Remote deployment
bosun reconcile --remote root@192.168.1.8
```

**Exit Codes:**

| Code | Meaning |
|------|---------|
| `0` | Reconciliation completed successfully |
| `1` | Configuration error, lock acquisition failed, or deployment error |

**Related Commands:**

- [drift](#bosun-drift) - Check for configuration drift
- [restore](#bosun-restore) - Restore from backup

---

## Utility Commands

### bosun completion

Generate shell completion scripts.

**Synopsis:**

Generate shell completion scripts

**Usage:**

```bash
bosun completion [bash|zsh|fish|powershell]
```

**Arguments:**

| Argument | Required | Description |
|----------|----------|-------------|
| `shell` | Yes | Shell type (bash, zsh, fish, powershell) |

**Description:**

Generates shell completion scripts for bosun commands. Follow the shell-specific instructions to enable completions.

**Examples:**

```bash
# Bash (Linux)
bosun completion bash > /etc/bash_completion.d/bosun

# Bash (macOS with Homebrew)
bosun completion bash > $(brew --prefix)/etc/bash_completion.d/bosun

# Zsh
bosun completion zsh > "${fpath[1]}/_bosun"

# Fish
bosun completion fish > ~/.config/fish/completions/bosun.fish

# PowerShell
bosun completion powershell > bosun.ps1
```

**Exit Codes:**

| Code | Meaning |
|------|---------|
| `0` | Completion script generated |
| `1` | Invalid shell argument |

---

## Pirate Mode (Easter Egg)

Bosun includes a hidden "pirate mode" with nautical command aliases. Access the pirate mode help with:

```bash
bosun yarr
```

### Pirate Aliases

| Standard Command | Pirate Alias |
|------------------|--------------|
| `init` | `christen` |
| `yacht` | `hoist` |
| `crew` | `scallywags` |
| `provision` | `plunder` |
| `provisions` | `loot` |
| `create` | `forge` |
| `radio` | `parrot` |
| `status` | `bridge` |
| `log` | `ledger` |
| `drift` | `compass` |
| `doctor` | `checkup` |
| `lint` | `inspect` |
| `mayday` | `mutiny` |
| `overboard` | `plank` |

**Examples:**

```bash
# Start the fleet
bosun hoist up

# Check the crew
bosun scallywags list

# View the bridge
bosun bridge

# Prepare the loot
bosun plunder core

# Man the parrot
bosun parrot test
```

---

## Quick Reference

### Common Workflows

**Initial Setup:**

```bash
bosun init my-homelab
cd my-homelab
bosun doctor
bosun yacht up
```

**Deploy a Service:**

```bash
bosun create webapp my-app
# Edit manifest/services/my-app.yml
bosun lint
bosun provision my-app
bosun yacht up
```

**Troubleshooting:**

```bash
bosun status          # Health dashboard
bosun mayday          # Recent errors
bosun drift           # Configuration drift
bosun crew logs nginx # Specific container logs
```

**Emergency Recovery:**

```bash
bosun mayday --list           # List snapshots
bosun mayday --rollback NAME  # Restore snapshot
bosun restore --list          # List backups
bosun restore backup-NAME     # Restore backup
```

---

## See Also

- [README.md](../README.md) - Project overview
- [CONTRIBUTING.md](../CONTRIBUTING.md) - Contribution guidelines
- [CHANGELOG.md](../CHANGELOG.md) - Version history

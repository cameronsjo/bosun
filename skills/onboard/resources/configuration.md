# Bosun Configuration

How bosun finds its project, what the config file looks like, environment variables, and directory layout.

## Project Discovery

Bosun searches **upward** from the current directory to find the project root. It looks for (in order):

1. `bosun.yaml` or `bosun.yml` config file
2. `bosun/` directory containing `docker-compose.yml`
3. `manifest/` or `manifests/` directory

If none are found, you get: `project root not found`. Fix by running commands from inside your project, or using `bosun --root /path/to/project`.

## Config File (bosun.yaml)

Optional but recommended. Controls project-level settings.

```yaml
# bosun.yaml

# Base domain for Traefik routing (used by defaultRule and upgrade traefik)
domain: example.com

# Override the manifest directory (default: manifest/)
manifest_dir: manifest

# Override the provisions directory (default: manifest/provisions/)
provisions_dir: manifest/provisions

# Docker Compose project name for all stacks.
# Ensures all containers share a namespace and --remove-orphans works correctly.
# Defaults to the project root directory name.
project_name: my-homelab

# Infrastructure containers (shown separately in status output)
infrastructure:
  containers:
    - traefik
    - authelia
    - gatus

# Tunnel configuration for webhook delivery
tunnel:
  provider: tailscale          # or "cloudflare"
  # Cloudflare-specific:
  hostname: bosun.example.com
  tunnel_name: my-tunnel
  health_endpoint: https://bosun.example.com/health

# Alert configuration for deploy notifications
alerts:
  on_success: false            # Alert on successful deploys
  on_failure: true             # Alert on failed deploys (default)

  # Discord
  discord_webhook_url: "https://discord.com/api/webhooks/..."

  # SendGrid email
  sendgrid_api_key: "SG.xxx"
  sendgrid_from_email: "bosun@example.com"
  sendgrid_from_name: "Bosun"
  sendgrid_to_emails:
    - admin@example.com

  # Twilio SMS
  twilio_account_sid: "ACxxx"
  twilio_auth_token: "xxx"
  twilio_from_number: "+1234567890"
  twilio_to_numbers:
    - "+1987654321"

# Whether to pass --remove-orphans to docker compose up.
# Set to false in shared environments where Bosun doesn't own all containers.
# Default: true (orphan containers from removed services are cleaned up).
remove_orphans: true

# Global pause after deploy, before any post-sync hooks run.
# Lets FUSE filesystems (e.g., Unraid's shfs) propagate writes.
hook_settle_delay: "2s"

# Deploy path allowlist: only run the full pipeline when changed files
# match these globs. Commits touching only non-matching files (docs, beads)
# are recorded as deployed and skipped. Omit to deploy on every commit.
deploy_paths:
  - "unraid/**"
  - "compose/**"
  - "traefik/**"
  - "bosun.yaml"

# Drift alert debounce: suppress alerts for transient drift that self-resolves.
# Items must persist past this window before the first alert fires.
# Default: 0 (disabled, alerts fire immediately). Recommended: 5m.
drift_alert_debounce: "5m"

# Drift ignore rules: suppress known drift noise from reports and alerts.
# Each rule matches a service name (glob) and drift type.
# type can be: missing, image_mismatch, unhealthy, or * for all types.
# Rules are validated at config load: an unknown type or invalid service glob
# fails startup, and a "*"/"*" rule (suppresses all drift) errors in
# `bosun validate` and warns at daemon startup.
drift_ignore:
  - service: "traefik"
    type: "unhealthy"
  - service: "monitoring-*"
    type: "*"

# Critical containers: must be healthy after compose up for deploy to succeed.
# When any container is unhealthy or missing after the health gate timeout,
# rollback is triggered. Empty list (default) skips the health gate.
critical_containers:
  - traefik
  - authelia

# Health gate scope: what the post-compose-up gate polls and rolls back on.
#   critical (default) - only critical_containers members (above)
#   declared           - all declared services, exempting ones already unhealthy
#                        before this deploy (opt-in: adds flapping-healthcheck
#                        rollback churn on top of the fail+retry+alert path)
#   off                - no health gate
health_gate_scope: critical

# Post-sync hooks: act on containers when specific config files change.
# Solves services (like Traefik) not picking up config changes on FUSE mounts.
post_sync_hooks:
  - paths: ["traefik/conf.d/**"]
    action: restart
    container: traefik
    delay: "5s"                  # Per-hook pause before this container restarts
  - paths: ["authelia/configuration.yml"]
    action: exec
    container: authelia
    command: ["authelia", "config", "validate"]
  - paths: ["authelia/config.yml", "authelia/users.yml"]
    action: restart
    container: authelia
```

### Config Field Reference

| Field | Default | Description |
|-------|---------|-------------|
| `manifest_dir` | `manifest` | Path to manifest directory (relative to project root) |
| `provisions_dir` | `manifest/provisions` | Path to provisions directory |
| `project_name` | *(directory name)* | Docker Compose project name for all stacks |
| `domain` | *(empty)* | Base domain for Traefik routing (e.g., `example.com`). Used by `defaultRule` and `upgrade traefik` |
| `infrastructure.containers` | `[traefik, authelia, gatus]` | Infrastructure container names |
| `tunnel.provider` | `tailscale` | Tunnel provider: `tailscale` or `cloudflare` |
| `alerts.on_success` | `false` | Send alerts on successful deploys |
| `alerts.on_failure` | `true` | Send alerts on failed deploys |
| `remove_orphans` | `true` | Pass `--remove-orphans` to docker compose up |
| `post_sync_hooks` | `[]` | Container restart or exec hooks triggered by file changes |
| `hook_settle_delay` | `0` (disabled) | Global pause after deploy before hooks run (e.g., `"2s"`) |
| `deploy_paths` | `[]` (deploy all) | Glob allowlist — skip pipeline when no changed files match |
| `deploy_sync_paths` | `[]` (sync all) | Glob allowlist — only sync staging entries matching these patterns |
| `deploy_sync_exclude` | `[]` (exclude none) | Glob blocklist — exclude matching staging entries from sync (wins over include) |
| `critical_containers` | `[]` (disabled) | Container names that must be healthy after deploy — triggers rollback on failure |
| `health_gate_scope` | `critical` | Health gate target set: `critical` (only `critical_containers`), `declared` (all declared services, exempting pre-existing casualties), or `off`. Overridden by `BOSUN_HEALTH_GATE_SCOPE` |
| `template_include_dir` | `templates` | Subtree that reconcile and `bosun render` template `include`/`fromJsonFile` reads are confined to (allowlist), relative to the infra dir (absolute values used as-is). Default `<infraDir>/templates` keeps sibling SOPS files and `bosun.yaml` unreachable from templates. Overridden by `BOSUN_TEMPLATE_INCLUDE_DIR` |
| `drift_ignore` | `[]` (disabled) | Suppress known drift noise by service glob + type (`missing`, `image_mismatch`, `unhealthy`, or `*`). Validated at load: unknown types and invalid globs fail startup. Overridden by `BOSUN_DRIFT_IGNORE` env var (JSON array), validated identically |
| `drift_alert_debounce` | `0` (disabled) | Debounce window before first drift alert fires (e.g., `"5m"`) |
| `targets` | `[]` (implicit default) | Named deployment targets with per-target overrides. Overridden by `BOSUN_TARGETS` env var (JSON array) |

## Multi-Target Deployment

Define multiple deployment targets when a single repo deploys to more than one host or appdata layout. When `targets:` is absent, bosun uses a single implicit default target (backwards compatible).

```yaml
# bosun.yaml
targets:
  - name: nas
    target_host: ""                      # Local deployment
    local_appdata_path: /mnt/appdata
    remote_appdata_path: /mnt/user/appdata
    project_name: homelab
    state_file: /var/lib/bosun/nas-state.json # Optional exact state-file override
    staging_dir: /app/nas-staging            # Optional exact staging-dir override
    secrets_scope: nas                   # Decrypt targets.nas.* from secrets
    critical_containers: [traefik, authelia]
    post_sync_hooks:
      - paths: ["traefik/conf.d/**"]
        action: restart
        container: traefik
    deploy_sync_paths: ["compose/**", "appdata/**"]
    deploy_sync_exclude: ["appdata/temp/**"]

  - name: media
    target_host: user@media-server
    local_appdata_path: /mnt/appdata
    remote_appdata_path: /mnt/user/appdata
    project_name: media-stack
    secrets_scope: media
```

Each named target gets isolated defaults: `deploy-state-<name>.json`, `<staging>/<name>/`, and `reconcile-<name>.lock`. Set `state_file` or `staging_dir` when a target needs an exact path instead. A lone `name: default` target may also override these paths while retaining the legacy default lock; a `default` entry in a multi-target list is rejected. Targets are reconciled sequentially; failure on one does not block others.

The daemon resolves target identity, host, and paths from its startup configuration. Restart the daemon after adding or removing targets or changing `target_host`, appdata paths, `state_file`, or `staging_dir`; only per-target operational overrides such as hooks, sync paths, critical containers, and `project_name` hot-reload from the pulled repository.

Per-target secrets scoping: when `secrets_scope` is set, keys under `targets.<scope>.*` in the decrypted secrets override top-level keys for that target.

Override via environment: `BOSUN_TARGETS` (JSON array) completely replaces the config file targets. It accepts the same snake_case fields as YAML, including `state_file` and `staging_dir`: `[{"name":"nas","target_host":"user@host","project_name":"homelab","state_file":"/var/lib/bosun/nas-state.json","staging_dir":"/app/nas-staging"}]`. Setting `BOSUN_TARGETS=[]` explicitly clears all targets (falls back to implicit default).

**Constraints:** Target names must be alphanumeric with hyphens/underscores (no dots, spaces, or path separators). The name `"default"` is reserved for a lone explicit default target; it cannot appear in a multi-target list. Duplicate names (case-insensitive) are rejected. Two targets may intentionally use the same host only when their effective `project_name` and deploy path are both distinct. Bosun rejects targets that resolve to the same host plus Compose project or the same host plus local/remote appdata path; an omitted project name uses Compose's derived `compose` namespace, and equivalent paths with `.` segments or trailing slashes count as the same destination.

## Directory Structure

A standard bosun project:

```
my-homelab/
├── bosun.yaml               # Project configuration
├── .sops.yaml               # SOPS encryption config
│
├── manifest/                # Service definitions
│   ├── provisions/          # Reusable templates
│   │   ├── container.yml
│   │   ├── healthcheck.yml
│   │   ├── homepage.yml
│   │   ├── monitoring.yml
│   │   ├── postgres.yml
│   │   ├── redis.yml
│   │   └── reverse-proxy.yml
│   ├── services/            # Individual service manifests
│   │   ├── myapp.yml
│   │   ├── norish.yml
│   │   └── ...
│   └── stacks/              # Service groups
│       ├── core.yml
│       └── apps.yml
│
├── compose/                 # Generated compose files (output)
│   ├── core.yml
│   └── apps.yml
├── traefik/                 # Generated Traefik config (output)
│   └── dynamic.yml
├── gatus/                   # Generated Gatus endpoints (output)
│   └── endpoints.yml
│
├── bosun/                   # Webhook receiver (if using container mode)
│   └── docker-compose.yml
│
└── secrets.sops.yaml        # Encrypted secrets (SOPS + Age)
```

Bosun infers the encrypted secrets format from the filename. Supported formats are YAML (`.yaml`, `.yml`), JSON (`.json`), dotenv (`.env`), and INI (`.ini`); a trailing `.sops` after the format extension is also accepted for compatibility. Binary SOPS files are rejected because template secrets must decode to a key/value map.

### Key Directories

| Directory | Purpose | Managed By |
|-----------|---------|------------|
| `manifest/provisions/` | Reusable templates | You (or `bosun init`) |
| `manifest/services/` | Service definitions | You |
| `manifest/stacks/` | Service groups | You |
| `compose/` | Generated Docker Compose files | `bosun provision` |
| `traefik/` | Generated Traefik dynamic config | `bosun provision` |
| `gatus/` | Generated Gatus monitoring endpoints | `bosun provision` |

## Secrets (SOPS + Age)

Bosun uses SOPS with Age encryption for secrets management.

### Setup

1. **Generate an Age key:**
   ```bash
   age-keygen -o ~/.config/sops/age/keys.txt
   ```

2. **Create `.sops.yaml`** in project root:
   ```yaml
   creation_rules:
     - age: "age1xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
   ```

3. **Create and encrypt a secrets file:**
   ```bash
   sops secrets.sops.yaml
   ```

### Using Secrets in Templates

During reconciliation, SOPS files are decrypted and available to Go templates:

```yaml
# In a .tmpl file:
db_password: "{{ .apps.myapp.db_password }}"
```

In service manifests, reference secrets with the template syntax:

```yaml
services:
  postgres:
    db_password: "{{ $secrets.apps.myapp.db_password }}"
```

### Preview Rendered Templates

```bash
bosun render config.yml.tmpl                    # Single file to stdout
bosun render -s secrets.sops.yaml *.tmpl        # Specify secrets file
bosun render -o /tmp/rendered unraid/           # Render to directory
```

The `BOSUN_SECRETS_FILE` environment variable can set the default secrets file path.

## Environment Variables

### Reconciliation Variables

Used by `bosun daemon` and `bosun reconcile`:

| Variable | Default | Description |
|----------|---------|-------------|
| `REPO_URL` | *required* | Git repository URL |
| `REPO_BRANCH` | `main` | Branch to track |
| `REPO_DIR` | `/app/repo` | Local clone directory |
| `STAGING_DIR` | `/app/staging` | Staging directory |
| `BACKUP_DIR` | `/app/backups` | Backup directory |
| `LOG_DIR` | `/app/logs` | Log directory |
| `LOCAL_APPDATA` | `/mnt/appdata` | Local appdata path |
| `REMOTE_APPDATA` | `/mnt/user/appdata` | Remote appdata path |
| `DEPLOY_TARGET` | *(local)* | Target host (user@host for SSH) |
| `SECRETS_FILES` | | Comma-separated SOPS files to decrypt |
| `DRY_RUN` | `false` | Dry run mode |
| `FORCE` | `false` | Force deployment |

### Other Environment Variables

| Variable | Description |
|----------|-------------|
| `BOSUN_REMOVE_ORPHANS` | Pass `--remove-orphans` to docker compose up (default: `true`; overrides config file). Set to `false` in shared environments where Bosun doesn't own all containers |
| `BOSUN_CRITICAL_CONTAINERS` | JSON array of container names that must be healthy after deploy (overrides config file) |
| `BOSUN_HEALTH_GATE_TIMEOUT` | Health gate polling timeout (default: `60s`; `0` disables the gate). Accepts Go duration strings or bare seconds |
| `BOSUN_BACKUP_TIMEOUT` | Pre-deploy backup creation + verification timeout (default: `5m`). Accepts Go duration strings or bare seconds. On timeout the backup is treated as a failure but the deploy continues |
| `BOSUN_SECRETS_FILE` | Default secrets file for `bosun render` |
| `BOSUN_ALLOW_EMPTY_DECLARED_STATE` | Allow reconcile to continue when the staging compose dir contains no declared services (default: `false` — strict). Set to `true` for genuinely empty repos. The dir-missing case is always fatal. |
| `BOSUN_SKIP_DEPLOY_INVARIANT` | Bypass the post-deploy mtime + WrittenFiles invariant check (default: `false`). Set to `true` for diagnostic deploys where silent-sync failures are acceptable. Logged at `Warn` with `override=true` when enabled. |
| `WEBHOOK_SECRET` | HMAC secret for daemon webhook endpoints. Required for webhook triggers — with no secret the endpoints fail closed (reject with `403`) |
| `BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK` | Opt out of fail-closed webhook auth (default: `false`; strict `== "true"` match). Accepts unauthenticated triggers on trusted networks; logs a security warning at startup and per accepted request |
| `BOSUN_LISTEN_ADDR` | Host/IP the daemon HTTP server binds to (default: empty = all interfaces, so container-side callers like Traefik and Prometheus can reach it over the docker bridge) |
| `SOPS_AGE_KEY_FILE` | Path to Age key file (default: `~/.config/sops/age/keys.txt`) |

## Tunnel Configuration

Bosun needs a way to receive webhooks from GitHub. Two tunnel options:

### Tailscale Funnel (Recommended for Simplicity)

Zero-config tunnel using your Tailscale network:

```yaml
# bosun.yaml
tunnel:
  provider: tailscale
```

```bash
bosun radio status             # Check Tailscale connectivity
bosun radio test               # Test webhook endpoint
```

### Cloudflare Tunnel

For custom domains and DDoS protection:

```yaml
# bosun.yaml
tunnel:
  provider: cloudflare
  hostname: bosun.example.com
  tunnel_name: my-tunnel
  health_endpoint: https://bosun.example.com/health
```

## Alert Configuration

Bosun can notify you when deployments succeed or fail.

### Supported Providers

| Provider | Config Key | What You Need |
|----------|-----------|---------------|
| **Discord** | `discord_webhook_url` | Discord webhook URL |
| **SendGrid** | `sendgrid_api_key` | API key, from/to emails |
| **Twilio** | `twilio_account_sid` | Account SID, auth token, phone numbers |

### Configuration

```yaml
# bosun.yaml
alerts:
  on_failure: true             # Alert on failed deploys (default)
  on_success: false            # Alert on successful deploys

  discord_webhook_url: "https://discord.com/api/webhooks/..."
```

Alerts include: deploy status, commit hash, changed files, error details (on failure), and duration.

Discord embeds are bounded to the provider's component limits and 6000-unit
aggregate so verbose deploy errors remain deliverable. Twilio sends only error
and critical alerts, truncating each formatted message to one SMS segment (160
GSM-7 septets or 70 UTF-16 units for non-GSM content) to bound delivery cost.

## Infrastructure Containers

Bosun treats certain containers as "infrastructure" -- they're shown separately in `bosun status` and get priority during `bosun yacht up` (e.g., Traefik is started first).

Default infrastructure containers: `traefik`, `authelia`, `gatus`.

Override in config:

```yaml
# bosun.yaml
infrastructure:
  containers:
    - traefik
    - authelia
    - gatus
    - homepage
```

## Post-Sync Hooks

Restart containers automatically when specific files change during deployment. Useful for services that don't detect config changes on certain filesystems (e.g., Traefik on Unraid's FUSE mount).

```yaml
# bosun.yaml
post_sync_hooks:
  - paths: ["traefik/conf.d/**"]
    action: restart
    container: traefik
```

### Hook Fields

| Field | Required | Description |
|-------|----------|-------------|
| `paths` | Yes | Glob patterns matched against changed files (relative to the staging root). Supports `**` for recursive matching |
| `action` | Yes | Action to perform: `restart` or `exec` |
| `container` | Yes | Container name to act on |
| `command` | For `exec` | Command and arguments to run inside the container. An `exec` hook without a non-empty command is rejected during configuration validation |
| `delay` | No | Pause before executing this hook (e.g., `"5s"`). Default: `0` (no delay) |

### Behavior

- After a successful deploy, bosun diffs the previous and current commits to find changed files
- Each changed file is matched against hook glob patterns
- Each container is restarted at most once per deploy, even if multiple patterns match
- Hooks only run when dry run is disabled and a previous commit exists (skipped on first deploy)
- Invalid `exec` hooks fail configuration validation before any target deploys; this applies to root and per-target hooks from `bosun.yaml`, `BOSUN_POST_SYNC_HOOKS`, and `BOSUN_TARGETS`. A newly pulled invalid hook also aborts the current reconciliation instead of falling back to stale hook configuration.

### Environment Variable Override

The `BOSUN_POST_SYNC_HOOKS` environment variable (JSON array) overrides hooks from the config file. The `BOSUN_HOOK_SETTLE_DELAY` variable overrides the settle delay. Both apply to daemon and CLI modes:

```bash
export BOSUN_POST_SYNC_HOOKS='[{"paths":["traefik/**"],"action":"restart","container":"traefik","delay":"5s"}]'
export BOSUN_HOOK_SETTLE_DELAY=2s
```

## Docker Compose Project Name

By default, bosun uses the project root directory name as the Docker Compose project name. This ensures all containers share a namespace and `--remove-orphans` works correctly.

Override if needed:

```yaml
# bosun.yaml
project_name: my-homelab
```

This affects container naming (`my-homelab-traefik-1`), network naming, and volume naming.

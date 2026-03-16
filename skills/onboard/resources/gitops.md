# Bosun GitOps System

Bosun automates deployment through a GitOps workflow: push configuration changes to git, and bosun reconciles your server to match. This document covers the daemon, reconciliation pipeline, webhooks, polling, and drift detection.

## Two Deployment Modes

| Mode | Command | Use Case |
|------|---------|----------|
| **Daemon** | `bosun daemon` | Production. Long-running service with webhooks and polling |
| **One-shot** | `bosun reconcile` | Manual deploys, testing, CI/CD pipelines |

Both follow the same reconciliation pipeline. The daemon just runs it automatically on triggers.

## Reconciliation Pipeline

Every reconciliation follows this 14-stage sequence:

```text
 1. Acquire lock (prevent concurrent runs)
        |
 2. Git repository sync (clone/pull via go-git)
        |
 3. Load deploy state and evaluate skip/circuit-breaker
        |
 4. Decrypt secrets (SOPS)
        |
 5. Render templates (Go text/template + Sprig)
        |
 6. Extract declared state from rendered compose
        |
 7. Create configuration backup
        |
 8. Deploy files (local copy or tar-over-SSH)
        |
 9. Run docker compose up (per-file isolated, with rollback)
        |
10. Clean up staging directory
        |
11. Critical container health gate (if configured)
        |
12. Execute post-sync hooks
        |
13. Post-deploy health verification (poll containers until healthy or timeout)
        |
14. Post-deploy drift check
        |
15. Record successful deployment in state file
        |
16. Release lock
```

### Failure Alerting

Specific pipeline stages send throttled failure alerts to all configured alert providers (Discord, SendGrid, Twilio) when they fail. This behavior is controlled by the `on_failure` config flag (default: `true`).

- **Git sync (stage 2)**: sends a throttled failure alert (loads state file first so throttle state is available)
- **Circuit breaker trip (stage 3)**: sends a throttled failure alert
- **Decrypt failure (stage 4)**: sends a throttled failure alert
- **Template failure (stage 5)**: sends a throttled failure alert
- **Deploy failure (stages 8-9)**: sends a throttled failure alert
- **Health gate failure (stage 11)**: sends a throttled failure alert; triggers rollback to backup compose files
- **Health verification failure (stage 13)**: sends a throttled failure alert; counts toward circuit breaker
- **Lock acquisition (stage 1)**: logged as a warning only, no alert (transient condition)
- **Backup, cleanup, post-sync hooks, state save, drift check**: logged as warnings only, no failure alert

Success and recovery alerts are controlled by the `on_success` flag (default: `false`). When enabled, a success alert is sent after a successful deployment, and a recovery alert is sent when a deploy succeeds after prior failures.

Configure these flags in `bosun.yaml` under `alerts`:

```yaml
alerts:
  on_failure: true   # Send alerts on pipeline failures (default)
  on_success: false  # Send alerts on successful deploys and recoveries (opt-in)
```

Both flags are re-read from `bosun.yaml` after each git pull, so changes take effect on the next reconciliation without restarting the daemon. Other reload-eligible fields include hooks, settle delay, deploy paths, and remove_orphans.

### Orphan Container Cleanup

During `docker compose up`, bosun passes `--remove-orphans` by default. This removes containers belonging to services that have been deleted from the compose file. In shared environments where Bosun does not own all containers on the Docker host, set `remove_orphans: false` in `bosun.yaml` or `BOSUN_REMOVE_ORPHANS=false` to disable this behavior. The environment variable takes precedence over the config file. Emergency restore (`bosun mayday`) always uses `--remove-orphans` regardless of this setting.

### Critical Container Health Gate

When `critical_containers` is configured, bosun polls each listed container via Docker API after compose up. Containers must be running and healthy (or have no healthcheck defined) within the `HealthGateTimeout` (default 60s, configurable via `BOSUN_HEALTH_GATE_TIMEOUT`). If any container fails the gate, rollback is triggered using the backup compose files.

The health gate is skipped when: dry run is active, the deploy is remote (`TargetHost` set — Docker API is local-only), no Docker client is available, or the critical containers list is empty.

Configure in `bosun.yaml` or via `BOSUN_CRITICAL_CONTAINERS` (JSON array, completely replaces config file value):

```yaml
critical_containers:
  - traefik
  - authelia
```

### Post-Sync Hooks

After `docker compose up`, bosun determines which files changed and matches them against configured hook patterns. When content-hash sync is enabled (default), hooks use the list of files actually written to disk — skipping files whose content didn't change. When disabled or in remote mode, hooks fall back to git diff. This solves services like Traefik that don't detect config file changes on certain filesystems (e.g., Unraid's FUSE mount).

Two timing controls are available:
- **`hook_settle_delay`** — global pause after deploy, before any hooks run (filesystem propagation)
- **`delay`** — per-hook pause before restarting a specific container

Hooks are configured in `bosun.yaml` under `post_sync_hooks`. See the [Configuration guide](configuration.md#post-sync-hooks) for schema and examples.

### Path-Aware Deploy Skipping

When `deploy_paths` is configured in `bosun.yaml`, bosun diffs the previous and current commits after pulling and checks whether any changed files match the glob allowlist. If no files match, the commit is recorded as deployed and the rest of the pipeline is skipped (~80s savings). This avoids unnecessary file rewrites on FUSE filesystems and prevents stale file handles.

- **Allowlist model** — only listed paths trigger a deploy; new directories require explicit opt-in
- **`--force` bypasses** — `bosun reconcile -f` always runs the full pipeline regardless of path matching
- **DiffFiles failure = full deploy** — if the git diff fails (e.g., shallow clone), the safe default is to run everything
- **State updated on skip** — the commit is recorded as deployed so it isn't re-evaluated on the next poll

### Project Config Reload

After pulling the repository (step 2), bosun re-reads `bosun.yaml` from the repo and updates `PostSyncHooks`, `HookSettleDelay`, `DeployPaths`, `on_failure`, and `on_success` if the file has changed. This means config changes pushed to the repo take effect without a daemon restart.

Environment variable overrides (`BOSUN_POST_SYNC_HOOKS`, `BOSUN_HOOK_SETTLE_DELAY`, `BOSUN_DEPLOY_PATHS`) still take precedence -- if set, the corresponding fields from `bosun.yaml` are ignored during reload. If the repo has no `bosun.yaml` or the file fails to parse, the existing config values are retained.

### Lock Behavior

Only one reconciliation runs at a time. If a trigger arrives during reconciliation, it sets a "pending" flag. After the current run completes, it checks the flag and runs again if set. This coalesces rapid-fire triggers into a single run.

### One-Shot Mode

```bash
bosun reconcile                # Full pipeline
bosun reconcile -n             # Dry run (preview changes)
bosun reconcile -f             # Force deploy even if no changes
bosun reconcile -l             # Force local deployment
bosun reconcile -r user@host   # Deploy to remote host via SSH
```

## Daemon Mode

The daemon is a long-running service that watches for changes and auto-deploys.

```bash
bosun daemon                          # Start with defaults
bosun daemon -p 9090                  # Custom HTTP port
bosun daemon --poll-interval 1800     # Poll every 30 minutes
bosun daemon --poll-interval 0        # Webhooks only (no polling)
bosun daemon -n                       # Dry run mode
```

### What the Daemon Provides

- **Unix socket API** at `/var/run/bosun.sock` -- primary control interface
- **HTTP server** for webhooks and health checks
- **Optional TCP API** with bearer token auth for remote access
- **Polling** -- periodic reconciliation on a configurable interval (default: 1 hour)
- **Drift detection** -- periodic checks comparing declared state vs running containers
- **Deploy state tracking** with circuit breaker (stops retrying after 3 consecutive failures)
- **Graceful shutdown** on SIGTERM/SIGINT

### Unix Socket API

The daemon's primary interface. All `bosun trigger`, `bosun daemon-status`, and `bosun validate` commands communicate through this socket.

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/trigger` | POST | Trigger reconciliation |
| `/status` | GET | Daemon status |
| `/health` | GET | Health check (JSON) |
| `/ready` | GET | Readiness check |
| `/config` | GET | Current config |
| `/ping` | GET | Simple ping |
| `/api/drift` | GET | Drift status |
| `/api/containers` | GET | Container list with summary |
| `/api/trigger` | POST | Trigger reconciliation (WebUI) |
| `/api/status` | GET | Extended status (WebUI) |

**Direct access via curl:**

```bash
curl --unix-socket /var/run/bosun.sock http://localhost/status
curl --unix-socket /var/run/bosun.sock http://localhost/trigger -X POST
curl --unix-socket /var/run/bosun.sock http://localhost/api/drift
```

**Via CLI (preferred):**

```bash
bosun trigger                  # Trigger reconciliation
bosun daemon-status            # Get daemon status
bosun validate                 # Validate config and connectivity
```

### Concurrency Model

Single-flight with dirty flag coalescing:

1. Trigger arrives -> if idle, start reconciliation
2. Trigger arrives during reconciliation -> set `pending = true`, return HTTP 202
3. Reconciliation completes -> check `pending` flag -> if set, run again
4. This prevents duplicate work while ensuring no trigger is lost

## Webhooks

The daemon accepts webhooks from Git providers to trigger reconciliation on push.

### Webhook Endpoints

| Provider | Path | Signature Header |
|----------|------|------------------|
| GitHub | `/webhook/github` | `X-Hub-Signature-256` |
| GitLab | `/webhook/gitlab` | `X-Gitlab-Token` |
| Gitea | `/webhook/gitea` | `X-Gitea-Signature` |
| Bitbucket | `/webhook/bitbucket` | `X-Hub-Signature` |
| Generic | `/webhook` | None |

All signatures are validated using HMAC-SHA256 (or SHA1 for legacy) with constant-time comparison.

### Webhook Setup

1. **Configure a webhook secret** in your bosun environment
2. **Set up the webhook** in your Git provider pointing to `https://your-server/webhook/github` (or appropriate provider)
3. **Expose the endpoint** via Tailscale Funnel or Cloudflare Tunnel (see Radio commands)

### Standalone Webhook Server

If you need the webhook receiver separate from the daemon:

```bash
bosun webhook                          # Default port
bosun webhook -p 9000                  # Custom port
bosun webhook --fetch-secret           # Fetch secret from daemon
```

The standalone server validates signatures and forwards valid requests to the daemon's trigger endpoint via the Unix socket.

## Polling

Enable periodic reconciliation as a fallback (or alternative) to webhooks:

```bash
bosun daemon --poll-interval 3600      # Every hour (default)
bosun daemon --poll-interval 1800      # Every 30 minutes
bosun daemon --poll-interval 0         # Disable polling
```

Polling pulls the Git repository and checks for changes. If changes are detected, it triggers a reconciliation.

## Drift Detection

Drift is the difference between what you declared (in your deploy state) and what's actually running in Docker.

### How It Works

1. The daemon periodically checks running containers against the declared deploy state
2. It uses Docker Compose v2 labels (`com.docker.compose.project`, `com.docker.compose.service`) for authoritative matching
3. Falls back to name-based parsing (`<project>-<service>-<replica>`) for containers without labels
4. Results are cached in the deploy state file

### Drift Types

| Type | Severity | Meaning |
|------|----------|---------|
| `missing` | Critical | Declared service is not running (or has exited) |
| `unhealthy` | Critical | Service is running but health check is failing |
| `image_mismatch` | Warning | Running image differs from declared image |

### Drift Alert Debounce

Transient drift (I/O pressure, image pulls, daemon restarts) self-resolves within minutes. The debounce layer suppresses alerts until drift persists beyond a configurable window, eliminating noise from self-healing transients.

Alert pipeline: **detect -> debounce filter -> dedup (per-item cooldown) -> send**

- **`BOSUN_DRIFT_ALERT_DEBOUNCE`** or **`drift_alert_debounce`** in `bosun.yaml`: Duration before first alert fires. Default `0` (disabled). Recommended: `5m`.
- Drift that resolves before the window expires is silently suppressed.
- Drift that persists past the window enters the normal dedup/cooldown pipeline.
- Resolution alerts bypass debounce (fire immediately for previously alerted items).
- Debounce state persists across daemon restarts.

### Checking Drift

```bash
bosun drift                    # Cached result from last daemon check
bosun drift --live             # Fresh check against Docker right now
bosun drift --json             # Machine-readable output
bosun drift --project core     # Filter to one compose project
```

## Deploy State and Circuit Breaker

The daemon tracks deploy state in a JSON file (default: `/var/lib/bosun/deploy-state.json`).

**State tracking includes:**
- Last successful deploy timestamp and commit
- Last failed deploy details
- Consecutive failure count
- Declared services (what should be running)
- Drift check results

**Deploy circuit breaker:** After 3 consecutive deployment failures, the daemon stops retrying automatically. A manual `bosun trigger -f` (force) resets the circuit breaker and tries again.

**Restart circuit breaker:** Detects containers in restart loops by tracking restart count deltas within a sliding window. When a container accumulates `BOSUN_RESTART_THRESHOLD` (default: 5) restarts within `BOSUN_RESTART_WINDOW` (default: 10m), the breaker trips and stops the container to prevent resource exhaustion. Runs during each drift check cycle. Sends critical alerts on trip and info alerts on resolution. Disabled with `BOSUN_RESTART_BREAKER=false`.

## Deployment Targets

Deploy targets are **auto-discovered** from the staging directory after template rendering. The staging directory structure determines what gets synced:

- `appdata/` children are expanded one level (per-service granularity)
- `compose/` is handled specially (per-file isolated `docker compose up` with per-file rollback)
- All other top-level entries are synced as-is

Use `deploy_sync_paths` (allowlist) and `deploy_sync_exclude` (blocklist) in `bosun.yaml` or via `BOSUN_DEPLOY_SYNC_PATHS`/`BOSUN_DEPLOY_SYNC_EXCLUDE` env vars to filter which targets are deployed. Exclude wins over include.

### Local Deployment

Default mode. Copies rendered files directly to the local filesystem and runs `docker compose up` per-file with isolated rollback. Each compose file is deployed independently — if one file fails (e.g., bad image tag), only that file is rolled back from backup while other files continue. A final orphan-reconciliation pass runs `--remove-orphans` across all files to clean up stale containers.

```bash
bosun reconcile -l             # Force local mode
```

### Remote Deployment

Deploy to a remote host via SSH (tar-over-SSH for efficient transfer).

```bash
bosun reconcile -r user@host
```

Requires SSH key authentication. Test connectivity first: `ssh user@host exit`.

## Environment Variables

These configure the reconciliation pipeline (used by daemon and one-shot modes):

| Variable | Default | Description |
|----------|---------|-------------|
| `REPO_URL` | *required* | Git repository URL |
| `REPO_BRANCH` | `main` | Branch to track |
| `REPO_DIR` | `/app/repo` | Local clone directory |
| `STAGING_DIR` | `/app/staging` | Staging directory for rendered files |
| `BACKUP_DIR` | `/app/backups` | Backup directory |
| `LOG_DIR` | `/app/logs` | Log directory |
| `LOCAL_APPDATA` | `/mnt/appdata` | Local appdata path |
| `REMOTE_APPDATA` | `/mnt/user/appdata` | Remote appdata path |
| `DEPLOY_TARGET` | *(local)* | Target host for remote deployment |
| `SECRETS_FILES` | | Comma-separated SOPS encrypted files |
| `DRY_RUN` | `false` | Enable dry run |
| `FORCE` | `false` | Force deployment |
| `BOSUN_POST_SYNC_HOOKS` | | JSON array of post-sync hooks (overrides config file) |
| `BOSUN_HOOK_SETTLE_DELAY` | `0` | Global pause before post-sync hooks run (e.g., `2s`) |
| `BOSUN_DEPLOY_PATHS` | | JSON array of glob patterns for deploy-relevant paths (overrides config file) |
| `BOSUN_DEPLOY_SYNC_PATHS` | | JSON array of glob patterns for deploy sync target allowlist (overrides config file) |
| `BOSUN_DEPLOY_SYNC_EXCLUDE` | | JSON array of glob patterns for deploy sync target blocklist (overrides config file; exclude wins over include) |
| `BOSUN_CRITICAL_CONTAINERS` | | JSON array of container names that must be healthy after deploy (overrides config file) |
| `BOSUN_HEALTH_GATE_TIMEOUT` | `60s` | Health gate polling timeout (accepts Go duration strings or bare seconds) |
| `BOSUN_OTEL_ENDPOINT` | *(disabled)* | OpenTelemetry OTLP HTTP endpoint (e.g., `http://localhost:4318`). When set, spans are exported for each reconciliation pipeline phase. When empty, a noop provider is used (zero overhead) |

## OpenTelemetry Tracing

Bosun supports distributed tracing via OpenTelemetry. Set `BOSUN_OTEL_ENDPOINT` to the OTLP HTTP collector endpoint (e.g., `http://localhost:4318`) to enable span export.

### Instrumented Spans

The reconciliation pipeline emits these spans:

| Span Name | Scope | Description |
|-----------|-------|-------------|
| `reconcile` | Root | Entire reconciliation run |
| `reconcile.git_sync` | Child | Git clone/pull |
| `reconcile.decrypt` | Child | SOPS secret decryption |
| `reconcile.template` | Child | Go template rendering |
| `reconcile.backup` | Child | Configuration backup |
| `reconcile.deploy` | Child | File deployment + compose up |
| `reconcile.health_gate` | Child | Critical container health gate |
| `reconcile.post_sync_hooks` | Child | Post-sync hook execution |
| `reconcile.drift_check` | Child | Post-deploy health verification |
| `daemon.reconcile` | Daemon | Daemon-level reconcile orchestration |
| `daemon.drift_check` | Daemon | Periodic drift check |
| `daemon.webhook` | Daemon | Webhook-triggered reconciliation |

Span attributes include `source`, `force`, `reconcile_id`, and `hook_count` where applicable. When `BOSUN_OTEL_ENDPOINT` is empty, a noop provider is used with zero overhead.

## Systemd Deployment

For production, run the daemon as a systemd service:

```bash
bosun init --systemd
```

This generates:
- `systemd/bosund.service` -- systemd service unit
- `systemd/bosund.socket` -- socket activation unit
- `systemd/bosund.env.example` -- environment variable template
- `systemd/install.sh` -- installation script

Install and start:

```bash
cd systemd
sudo ./install.sh
sudo systemctl start bosund
sudo systemctl status bosund
```

## Typical GitOps Setup

1. **Initialize project:** `bosun init`
2. **Write manifests** in `manifest/services/`
3. **Render and test locally:** `bosun provision mystack -n`
4. **Set up daemon:** `bosun init --systemd` or run `bosun daemon` directly
5. **Configure webhook** in GitHub/GitLab/Gitea pointing to your server
6. **Set up tunnel** if needed: Tailscale Funnel or Cloudflare Tunnel
7. **Push changes** to git -- daemon auto-deploys
8. **Monitor:** `bosun drift`, `bosun daemon-status`, `bosun status`

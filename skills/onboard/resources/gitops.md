# Bosun GitOps System

Bosun automates deployment through a GitOps workflow: push configuration changes to git, and bosun reconciles your server to match. This document covers the daemon, reconciliation pipeline, webhooks, polling, and drift detection.

## Two Deployment Modes

| Mode | Command | Use Case |
|------|---------|----------|
| **Daemon** | `bosun daemon` | Production. Long-running service with webhooks and polling |
| **One-shot** | `bosun reconcile` | Manual deploys, testing, CI/CD pipelines |

Both follow the same reconciliation pipeline. The daemon just runs it automatically on triggers.

## Reconciliation Pipeline

Every reconciliation follows this sequence:

```
1. Acquire lock (prevent concurrent runs)
       |
2. Clone/pull repository (go-git, in-process)
       |
3. Reload project config from repo (hooks, settle delay, deploy paths)
       |
4. Path-aware skip (if deploy_paths configured, skip when no files match)
       |
5. Decrypt secrets (go-sops, in-process)
       |
6. Render templates (Go text/template + Sprig)
       |
7. Create backup of current configs
       |
8. Deploy files (local copy or tar-over-SSH)
       |
9. Docker compose up
       |
10. Post-sync hooks (restart containers on config changes)
       |
11. SIGHUP to agentgateway (if configured)
       |
12. Release lock
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

After pulling the repository (step 2), bosun re-reads `bosun.yaml` from the repo and updates `PostSyncHooks`, `HookSettleDelay`, and `DeployPaths` if the file has changed. This means config changes pushed to the repo take effect without a daemon restart.

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

**Circuit breaker:** After 3 consecutive deployment failures, the daemon stops retrying automatically. A manual `bosun trigger -f` (force) resets the circuit breaker and tries again.

## Deployment Targets

### Local Deployment

Default mode. Copies rendered files directly to the local filesystem and runs `docker compose up`.

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

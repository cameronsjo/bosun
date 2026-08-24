# Bosun GitOps System

Bosun automates deployment through a GitOps workflow: push configuration changes to git, and bosun reconciles your server to match. This document covers the daemon, reconciliation pipeline, webhooks, polling, and drift detection.

## Two Deployment Modes

| Mode | Command | Use Case |
|------|---------|----------|
| **Daemon** | `bosun daemon` | Production. Long-running service with webhooks and polling |
| **One-shot** | `bosun reconcile` | Manual deploys, testing, CI/CD pipelines |

Both follow the same reconciliation pipeline. The daemon just runs it automatically on triggers.

Before any target starts, Bosun canonicalizes every effective staging path,
rejects equal or nested slots, and hardens or deletes pre-existing evidence. If
one slot can be neither protected nor deleted, no target reaches Git sync or
secret decryption.

## Reconciliation Pipeline

Every reconciliation follows this 16-stage sequence:

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
 6. Extract declared state from rendered compose (FATAL if dir missing;
    fatal on zero services unless BOSUN_ALLOW_EMPTY_DECLARED_STATE=true)
        |
 7. Create configuration backup
        |
 8. Deploy files (local copy or tar-over-SSH). Remote deploys use a
    locally round-tripped archive, verify its SHA-256 after transport,
    and verify the extracted entry set, types, contents, symlink targets,
    and hard-link topology before promotion. Integrity failure preserves the
    live target. The remote host needs POSIX `sh` plus `tar`, `find`,
    `readlink`, `sha256sum`, and a `test` implementation with `-ef` support.
    Successful remote deploys then use a
    retain-old rename-swap (move live target aside, move new tree in,
    remove the retained copy on success; restore it on failure) so an
    interrupted deploy never leaves an empty target; the next deploy
    self-heals a missing target from the newest retained copy
        |
 9. Verify deploy-sync invariants — every WrittenFiles entry must exist
    with fresh mtime; empty WrittenFiles against a non-empty source fails
    when any source file is missing OR byte-differs at the destination (a
    no-op sync whose destination already content-matches passes; symlinks
    are skipped). Skipped if BOSUN_SKIP_DEPLOY_INVARIANT=true.
        |
10. Run docker compose up (per-file isolated, with rollback)
        |
11. Critical container health gate (if configured; rollback on failure)
        |
12. Execute post-sync hooks
        |
13. Post-deploy health verification (local targets)
        |
14. Clean up verified staging directory
        |
15. Record successful deployment in state file
        |
16. Release lock
```

If a local Docker Compose operation exceeds its context deadline or the
reconcile is cancelled, Bosun actively asks the Docker CLI to stop instead of
immediately killing only that thin client. It isolates the Docker CLI and its
Compose plugin in a platform process group, delivers SIGTERM on Unix or a
targeted Ctrl-Break on Windows, and allows up to five seconds for them to cancel
the daemon request before forceful escalation. This prevents a timed-out
`compose up`, rollback, orphan pass, or health inspection from remaining in
flight after Bosun returns.

Template rendering is strict: a missing map key stops stage 5 with the template
and key named in the error. Optional values must use an explicit lookup such as
`get . "key" | default "value"`.

### Failed Staging Evidence

Rendered staging can contain plaintext secrets. Bosun creates the effective
staging root as `0700` before rendering and keeps payload temp files private
until atomic rename. Active descendant modes remain compatible with deployment;
if the pipeline fails after rendering begins, the one retained evidence slot is
hardened recursively to `0700` directories and `0600` regular files.

The evidence remains at that target's effective `StagingDir`, including after a
failed health gate, successful or failed rollback, invariant failure, compose
failure, or post-deploy verification failure. Dry runs also retain it. A later
attempt preserves the slot through sync, config reload, decryption, and deploy
mode resolution, then replaces it only when the next render begins; timestamped
evidence archives are not accumulated.

Symlinks, FIFOs, sockets, devices, path overlap, or entry-replacement races fail
closed. Bosun never follows or logs a link target or rendered content: it deletes
an unsafe slot, and aborts the cycle if it can neither harden nor delete it.
Verified real deployments remove staging only after the health gate, hooks, and
post-deploy verification. A cleanup failure is warning-only when the remaining
tree is proven owner-only; otherwise it prevents deployment success.

Before stage 4 looks up the Age key, Bosun rejects malformed SOPS files. The
file must be valid YAML with a `sops` metadata mapping, a non-empty MAC, a valid
RFC3339 `lastmodified` timestamp, and at least one key recipient containing a
non-empty encrypted data key. This keeps incomplete SOPS files from being
misreported as Age key configuration failures.

Decryption errors use sanitized operator categories: integrity verification
failed, key unavailable, malformed encrypted data, or an unclassified
decryption failure. An integrity error means restore the encrypted file from a
trusted source or re-encrypt it; rotating the Age key does not repair corrupted
ciphertext. `BOSUN_LOG_LEVEL=debug` adds sanitized category and file context,
but Bosun never logs raw SOPS library errors or the decrypted MACs, encrypted
values, key identifiers, and additional local paths they may contain. The
requested secrets-file path remains in the surrounding error context.

### Deploy-Sync Invariants (stage 6 + stage 9)

Bosun enforces two invariant gates that turn the GH#214 silent-success failure mode into a loud error:

- **Declared-state invariant (stage 6)** — if `ExtractDeclaredState` returns `ErrComposeDirMissing` the reconcile fails unconditionally (no override). When the infra dir has no `compose/` but a sibling directory does, the error names the candidate and suggests the `BOSUN_INFRA_DIR` value to set (e.g. `did you mean BOSUN_INFRA_DIR=unraid?`) — the GH#214 misconfiguration. If it returns `ErrNoDeclaredServices` the reconcile fails unless `BOSUN_ALLOW_EMPTY_DECLARED_STATE=true` is set; the override logs at `Warn` level with `override=true`.
- **Post-deploy invariant (stage 9)** — for every file in `DeployResult.WrittenFiles`, the destination must exist at `mtime >= reconcileStartTime`. When a deploy target records zero writes against a non-empty source, the gate inspects the destination directly: every regular source file must be present **and byte-identical** at the destination (SHA-256, the same comparison `CopyFileIfChanged` uses to decide a write is skippable; symlinks are skipped to match the copy path). If every file is content-equal it is a legitimate no-op sync and passes; if any file is missing **or holds stale bytes** it is the GH#214 silent-sync failure and fails the reconcile before `docker compose up` runs, naming the first mismatching destination path. (The earlier behavior — treating *any* zero-write target as a failure — caused the GH#330 outage, where one byte-identical config aborted the entire deploy. A subsequent existence-only check fixed that but let a stale file occupying the right path pass; content-equality closes that gap.)

Operators can bypass the post-deploy invariant for diagnostic deploys via `BOSUN_SKIP_DEPLOY_INVARIANT=true`. The skip is logged at `Warn` level with `override=true` so it shows up in monitoring; the declared-state invariant is not affected by this flag.

Per-file write decisions are observable at `Debug` level: `CopyDirIfChanged` and `CopyFileIfChanged` emit `wrote src=… dst=… bytes=N` on every write and `skipped src=… dst=… reason=hash_match` on every hash-match skip. Use `BOSUN_LOG_LEVEL=debug` to see them.

### Configuration Backup (stage 7)

Before deploying new configs, bosun tars the **deployed config footprint** —
the files it renders into staging, mapped onto their appdata destinations — into
a timestamped `backup-YYYYMMDD-HHMMSS/configs.tar.gz` under `BackupDir`, verifies
it by listing the archive, and prunes to the most recent `BackupsToKeep` **valid**
backups (default 5). When no managed file exists yet (a fresh host's first deploy),
no archive is written and no rollback anchor is recorded — a content-free backup is
never reported as a real one (#360). Retention counts only backups that pass the
same verification used to pick a rollback anchor: a corrupt or partial dir (missing,
unlistable, or truncated archive) is removed rather than occupying a keep slot and
evicting an older good backup (#353). Three guards keep the backup from wedging the
reconcile (GH#319, bosun-5qx):

- **Footprint scoping.** The backup captures only the files bosun manages (its
  rendered staging footprint), not whole appdata target directories — those
  co-locate large runtime data (media, databases, caches) that made the archive
  grow without bound and burn the full timeout every reconcile. Symlinks are
  skipped, matching the deploy path. The path list is fed to `tar` via stdin
  (`-T -`) so a large footprint cannot overflow the argument list.
- **Self-exclusion.** Bosun is itself a deploy target, and `BackupDir`
  (default `/app/backups` → host `/mnt/appdata/bosun/backups`) lives *inside* a
  backed-up path. The creation tar passes `--exclude` for the backup
  destination so it can never archive its own growing output or any prior
  backup nested under it. Applies to both local and remote (`tar -czf -`) paths.
- **Bounded deadline.** Backup creation *and* verification run under
  `BackupTimeout` (default 5m, overridable via `BOSUN_BACKUP_TIMEOUT` — accepts a
  Go duration or plain seconds). Verification shares the same context, so the
  deadline kills a stuck `tar -tzf` rather than blocking. On timeout the backup
  is treated as a failure.

Backup failures, **including timeouts**, are logged as warnings and never abort
the deploy (see Failure Alerting below).

### Failure Alerting

Specific pipeline stages send throttled failure alerts to all configured alert providers (Discord, SendGrid, Twilio) when they fail. This behavior is controlled by the `on_failure` config flag (default: `true`).

- **Git sync (stage 2)**: sends a throttled failure alert (loads state file first so throttle state is available)
- **Circuit breaker trip (stage 3)**: sends a throttled failure alert
- **Decrypt failure (stage 4)**: sends a throttled failure alert
- **Template failure (stage 5)**: sends a throttled failure alert
- **Deploy failure (stages 8-9)**: sends a throttled failure alert
- **Health gate failure (stage 11)**: sends a throttled failure alert; triggers rollback to the managed tree
- **Health verification failure (stage 13)**: sends a throttled failure alert; counts toward circuit breaker
- **Lock acquisition (stage 1)**: logged as a warning only, no alert (transient condition)
- **Backup, cleanup, post-sync hooks, and state save**: logged without a dedicated failure alert; backup or cleanup still aborts when rollback safety or staging confidentiality cannot be established

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

### Health Gate

After compose up, bosun polls container health via the Docker API and, on a failure this deploy caused, restores the backup compose files before post-sync hooks run. `health_gate_scope` (config, or `BOSUN_HEALTH_GATE_SCOPE`) selects the target set:

- **`critical`** (default): polls only `critical_containers` members. An empty list skips the gate. A declared-but-non-critical service coming up unhealthy does NOT trigger rollback.
- **`declared`**: polls all declared services, exempting any service that was already unhealthy *before* this deploy (a pre-existing casualty, per #392) — only a service this deploy made unhealthy triggers rollback. Opt-in, because it adds flapping-healthcheck rollback churn on top of the fail-retry-alert path a failed post-deploy verification already provides.
- **`off`**: no health gate.

Containers must be running and healthy (or have no healthcheck defined) within the `HealthGateTimeout` (default 60s, `BOSUN_HEALTH_GATE_TIMEOUT`; set it to `0` to disable the gate). On failure, rollback reverts the **full managed tree** — every file this deploy wrote (appdata configs and compose files, from the deploy's `ManagedFiles`) is restored to its backed-up content, not just the compose files (#445) — then re-applies the restored compose files with `docker compose up`. Files the failed deploy *added* are removed only against a fresh backup anchor (this deploy's own pre-deploy backup), never a stale fallback anchor. A per-file restore failure does not abort the rest (errors are joined). Post-sync hooks are still skipped when a rollback ran. A throttled failure alert fires on the `1/3/10/30` attempt-count schedule under every scope; **under `declared` scope only**, a rollback alert fires alongside it on the same window (critical mode sends no rollback alert — its alert surface is unchanged).

The health gate is skipped regardless of scope when: dry run is active, the deploy is remote (`TargetHost` set — Docker API is local-only), or no Docker client is available.

Configure in `bosun.yaml` or via `BOSUN_CRITICAL_CONTAINERS` (JSON array, completely replaces config file value):

```yaml
critical_containers:
  - traefik
  - authelia
```

### Post-Sync Hooks

After `docker compose up`, bosun determines which files changed and matches them against configured hook patterns. When content-hash sync is enabled (default), hooks use the list of files actually written to disk — an empty result is authoritative no-change. When content-hash sync is disabled, local hooks fall back to git diff because standard-copy mode does not populate per-file writes. Remote deploys have no file-level tracking and fire every configured hook. This solves services like Traefik that don't detect config file changes on certain filesystems (e.g., Unraid's FUSE mount).

Written/deleted paths and fallback git-diff paths use one canonical staging-relative namespace (for example, `appdata/traefik/**`). The fallback strips `BOSUN_INFRA_DIR` from repo-relative paths and diffs from `DeployState.LastDeployedCommit`, so a failed attempt never advances the hook diff base or loses changes from the next successful retry.

If files changed but none match any configured hook pattern, bosun warns with
distinct/duplicate/empty pattern counts, at most five pattern samples, the
evaluated-file count, an explicit zero matched-file count, and at most five
staging-relative path samples. Absolute or traversal paths are redacted. A
deploy with no changed files is logged separately at info level and is not
treated as a likely pattern mistake. Hook diagnostics never include file
contents or hook command arguments.

Two timing controls are available:
- **`hook_settle_delay`** — global pause after deploy, before any hooks run (filesystem propagation)
- **`delay`** — per-hook pause before restarting a specific container

Hooks are configured in `bosun.yaml` under `post_sync_hooks`. See the [Configuration guide](configuration.md#post-sync-hooks) for schema and examples.
`exec` hooks must provide a non-empty `command`; bosun rejects invalid root or per-target hook configuration before deployment instead of silently skipping it.

### Path-Aware Deploy Skipping

When `deploy_paths` is configured in `bosun.yaml`, bosun diffs the last *successfully deployed* commit (`state.LastDeployedCommit`) against the current commit and checks whether any changed files match the glob allowlist. If no files match, the commit is recorded as deployed and the rest of the pipeline is skipped (~80s savings). This avoids unnecessary file rewrites on FUSE filesystems and prevents stale file handles.

- **Diff base is the last successful deploy** — uses `state.LastDeployedCommit`, not the git pull's `commit_before`. After a failed reconciliation, the next diff covers all files since the last successful deploy, ensuring files from failed attempts are re-evaluated
- **First deploy runs full pipeline** — when there is no prior deploy state, the path-aware check is skipped entirely (everything is deploy-relevant)
- **Allowlist model** — only listed paths trigger a deploy; new directories require explicit opt-in
- **`--force` bypasses** — `bosun reconcile -f` always runs the full pipeline regardless of path matching
- **Configurable shallow history** — clone and fetch use depth 1 by default; set `BOSUN_GIT_FETCH_DEPTH` to a larger positive integer when deploy diffs routinely span multiple commits
- **Unavailable DiffFiles base = full deploy** — if the last deployed commit is absent from shallow history, bosun reports that condition explicitly and safely runs everything; post-sync hooks likewise all fire
- **State updated on skip** — the commit is recorded as deployed so it isn't re-evaluated on the next poll

### Project Config Reload

After pulling the repository (step 2), bosun re-reads `bosun.yaml` from the repo and updates `PostSyncHooks`, `HookSettleDelay`, `DeployPaths`, `on_failure`, and `on_success` if the file has changed. This means config changes pushed to the repo take effect without a daemon restart.

Hook fields use presence-aware snapshots. A successfully loaded file with an omitted or empty `post_sync_hooks` clears file hooks, while an omitted `hook_settle_delay` retains its previous effective value and explicit `0s` clears it. A valid empty file is successful; a missing, unreadable, malformed, unknown-field, or invalid-hook file retains the prior hook/delay state, and hook validation finishes before either value changes. Target hooks inherit root when omitted, clear inheritance with explicit `[]`, and fall back to root when their target descriptor disappears pending the required daemon restart for topology changes.

Environment variable overrides (`BOSUN_POST_SYNC_HOOKS`, `BOSUN_HOOK_SETTLE_DELAY`, `BOSUN_DEPLOY_PATHS`) still take precedence -- if set, the corresponding fields from `bosun.yaml` are ignored during reload. Target hook lists supplied through `BOSUN_TARGETS` likewise remain environment-owned. Reload logs report source, target, outcome, and hook count without printing executable command arguments.

### Lock Behavior

Only one reconciliation runs at a time. Triggers that arrive during a run increment a pending counter, retain every distinct source, and make `force` sticky for that batch. After the current run completes, bosun atomically drains the batch into one follow-up reconciliation. Triggers arriving during that follow-up form another batch, so the cycle-boundary reset cannot erase them.

### Multi-Target Reconciliation

When `targets:` is defined in `bosun.yaml`, the pipeline runs sequentially for each target. Each target gets its own lock file (`reconcile-<name>.lock`), state file (`deploy-state-<name>.json`), and staging subdirectory (`<staging>/<name>/`). Failure on one target does not block the others.

Use `--target=NAME` to reconcile a single target instead of all targets. When `targets:` is absent, behavior is identical to before (a single implicit default target).

Alert titles include `[targetName]` for named targets so notifications identify which target was affected.

### One-Shot Mode

```bash
bosun reconcile                # Full pipeline (all targets)
bosun reconcile -n             # Dry run (preview changes)
bosun reconcile -f             # Force deploy even if no changes
bosun reconcile -l             # Force local deployment
bosun reconcile -r user@host   # Deploy to remote host via SSH
bosun reconcile --target=nas   # Reconcile only the "nas" target
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
- **Graceful shutdown** on SIGTERM/SIGINT: the daemon cancels reconciles
  accepted through webhooks, the Unix socket, TCP, and `/api/trigger`, then
  waits up to `BOSUN_SHUTDOWN_TIMEOUT` for every tracked trigger goroutine to
  unwind. Request completion does not cancel accepted work; daemon shutdown
  does. New triggers are rejected with `503` once shutdown begins.

The webhook, Unix socket, and optional TCP HTTP servers all allow at most 5
seconds to receive request headers and set a 32 KiB request-header parsing
limit. These transport limits are fixed security defaults and do not change
the existing per-operation `BOSUN_API_TIMEOUT` behavior.

The Unix socket is `0660` by default. Bosun creates it behind a private `0700`
staging directory, applies the configured `SocketMode`, and atomically publishes
the already-restricted socket at its final path. This avoids a permissive
`listen`-then-`chmod` window without changing the process-global umask. Bosun
also refuses to replace a stale-path symlink or non-socket entry and removes the
socket at shutdown only if the path still refers to the inode it created.

On Linux, mutating socket requests are independently authorized with
`SO_PEERCRED`: the daemon's effective UID is always allowed, and
`BOSUN_SOCKET_ALLOWED_UIDS` adds comma-separated numeric UIDs. An unauthorized
UID or a connection without available peer credentials receives `403` and
cannot trigger reconciliation. This also means non-Linux platforms reject
socket mutations by default. `BOSUN_ALLOW_UNAUTHENTICATED_SOCKET=true` is the
strict, loudly logged opt-out for deployments that intentionally rely only on
socket filesystem permissions.

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

Single-flight with counted trigger coalescing:

1. Trigger arrives -> if idle, start reconciliation
2. Trigger arrives during reconciliation -> increment the pending count, retain its source, and return HTTP 202
3. Reconciliation completes -> atomically drain the pending batch into one run with sorted distinct-source attribution and sticky `force`
4. Triggers received during that coalesced run form the next batch
5. This prevents duplicate work while ensuring no trigger batch or source attribution is lost

## Webhooks

The daemon accepts webhooks to trigger reconciliation on push.

### Daemon Webhook Endpoints

The daemon serves GitHub and a generic HMAC endpoint:

| Endpoint | Path | Signature Header |
|----------|------|------------------|
| GitHub | `/webhook/github` | `X-Hub-Signature-256` |
| Generic | `/webhook` | `X-Signature`, or `X-Hub-Signature-256` |
| Manual | `/webhook/manual` | `X-Signature` |

**Webhook auth fails closed.** With no `WEBHOOK_SECRET` configured, all three
endpoints reject every request with `403` and no reconcile runs — a missing
secret is not an open door. On trusted networks, opt out explicitly with
`BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK=true` (logs a security warning at startup
and per accepted request). The Unix socket trigger (`bosun trigger`) is not
affected. `BOSUN_LISTEN_ADDR` narrows the HTTP bind; the default stays
all-interfaces so container-side callers reach the daemon over the docker bridge.

GitHub pusher attribution is sanitized by both the daemon endpoint and the
standalone receiver, whether a request has a valid signature or uses the direct
daemon endpoint's explicit unauthenticated opt-out. Control, formatting, and
line separator characters are removed and the printable name is capped at 256
Unicode code points before it reaches logs, reconcile state, tracing, or Sentry.

**Read-endpoint auth fails closed too.** `/metrics` (Prometheus) and
`/api/widget` (Homepage) disclose the deployed commit and daemon stats, so they
reject every request with `403` unless a token is configured. Give scrapers a
**read-scope** `BOSUN_METRICS_TOKEN` (sent as `Authorization: Bearer <token>`) —
deliberately separate from `BOSUN_BEARER_TOKEN`, which also authorizes control
operations like `/trigger` and must not be handed to a scraper. The control
bearer, being strictly more privileged, is also accepted on these read
endpoints. Opt out on trusted networks with
`BOSUN_ALLOW_UNAUTHENTICATED_METRICS=true` (logs a security warning at startup
and per accepted request).

GitLab, Gitea, and Bitbucket are **not** handled by the daemon directly. Point them
at the standalone `bosun webhook` receiver (below), which understands each provider
and forwards normalized triggers to the daemon:

| Provider | Path (receiver) | Signature Header |
|----------|-----------------|------------------|
| GitHub | `/webhook/github` | `X-Hub-Signature-256` |
| GitLab | `/webhook/gitlab` | `X-Gitlab-Token` |
| Gitea | `/webhook/gitea` | `X-Gitea-Signature` |
| Bitbucket | `/webhook/bitbucket` | `X-Hub-Signature` |

All signatures are validated using HMAC-SHA256 (or SHA1 for legacy) with constant-time comparison.

### Webhook Setup

1. **Configure a webhook secret** in your bosun environment (`WEBHOOK_SECRET` — required unless you explicitly opt out; see above)
2. **Set up the webhook** in your Git provider. For GitHub, point it at `https://your-server/webhook/github`. For GitLab/Gitea/Bitbucket, run the standalone `bosun webhook` receiver and point the provider at its provider-specific path
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
- A critical type transition (`unhealthy` to `missing`, or the reverse) remains active drift for that service. The replacement type follows normal debounce/dedup handling, and Bosun does not send a resolution until the service has no critical drift.
- Adding an ignore rule for active drift removes its prior alert state silently; Bosun sends a resolution only when the service's critical drift is absent from the unfiltered Docker comparison.
- Debounce state persists across daemon restarts.

### Drift Self-Healing

When drift is detected, bosun normally only sends alerts. With self-healing enabled, the daemon automatically triggers a reconciliation to resolve drift.

- **`BOSUN_DRIFT_SELF_HEAL`** or **`drift_self_heal`** in `bosun.yaml`: Enable self-healing on drift detection. Default `false`.
- **`BOSUN_DRIFT_SELF_HEAL_COOLDOWN`** or **`drift_self_heal_cooldown`** in `bosun.yaml`: Minimum interval between self-heal reconciliations. Default `15m`.
- Self-heal skips when a reconciliation is already in progress (prevents infinite loops).
- Self-heal fires asynchronously so it does not block the drift check loop.
- The cooldown timer resets each time a self-heal triggers, preventing rapid-fire reconciliations.

```yaml
drift_self_heal: true
drift_self_heal_cooldown: "15m"
```

### Drift Ignore Rules

Some containers produce known drift noise (labels that change at runtime, environment variables injected by orchestrators). Drift ignore rules suppress these items from reports and alerts.

Configure in `bosun.yaml`:

```yaml
drift_ignore:
  - service: "traefik"
    type: "unhealthy"          # Ignore unhealthy drift for traefik
  - service: "monitoring-*"
    type: "*"                  # Ignore all drift for monitoring services
```

Or via `BOSUN_DRIFT_IGNORE` environment variable (JSON array, completely replaces config file value):

```bash
BOSUN_DRIFT_IGNORE='[{"service":"traefik","type":"unhealthy"}]'
```

- **`service`** — glob pattern matching service name (`filepath.Match` syntax: `*`, `?`, `[chars]`)
- **`type`** — drift type to ignore: `missing`, `image_mismatch`, `unhealthy`, or `*` for all types
- Ignored items are filtered out before alerting and before display in `bosun drift`
- The ignore rules are reloaded from `bosun.yaml` after each git pull (like other config fields)
- Environment variable takes precedence over config file
- Rules are validated at config load (config file and `BOSUN_DRIFT_IGNORE` alike): an unknown `type` or an invalid `service` glob fails startup and `bosun validate`, rather than silently never matching. A rule with both `service: "*"` and `type: "*"` (suppresses all drift) fails `bosun validate` and logs a loud warning at daemon startup

### Checking Drift

```bash
bosun drift                    # Cached result from last daemon check
bosun drift --live             # Fresh check against Docker right now
bosun drift --json             # Machine-readable output
bosun drift --project core     # Filter to one compose project
bosun drift --target=nas       # Show drift for a specific target
```

**Multi-target behavior:** When no `--target` is specified and the configuration defines multiple targets, the CLI MUST report drift for all targets. When `--json` is specified in this multi-target mode, the CLI MUST wrap the output in `{"targets": [...]}` instead of a single drift object.

## Deploy State and Circuit Breaker

The daemon and one-shot CLI track deploy state in a JSON file (default: `/var/lib/bosun/deploy-state.json`). Before a real reconciliation, each mode creates the state file's parent directory. Multi-target one-shot runs prepare and write-probe each target's state directory independently and fail a target before deployment if its directory cannot be written. A one-shot dry run uses a temporary copy of existing state, so it evaluates skip and circuit-breaker decisions without creating or updating the configured state path.

**State tracking includes:**
- Last successful deploy timestamp and commit
- Last failed deploy details
- Consecutive failure count
- Declared services (what should be running)
- Drift check results

**Deploy circuit breaker:** After 3 consecutive deployment failures, the daemon stops retrying automatically. A manual `bosun trigger -f` (force) resets the circuit breaker and tries again.

**Restart circuit breaker:** Detects containers in restart loops by tracking container identity and restart count increases across drift checks. Once restarts begin accumulating, Bosun preserves the earliest unresolved baseline until a clean sample observes no new restarts, so a slow loop still trips when the drift interval is longer than the nominal restart window. When a container accumulates `BOSUN_RESTART_THRESHOLD` (default: 5) restarts, the breaker trips and stops the container to prevent resource exhaustion. Docker resets the count when a deploy recreates a container, so an identity change does not resolve an existing trip; the same identity must remain free of additional restarts through the next drift-check interval before Bosun sends the resolution alert. Missing containers preserve the trip but restart the stability grace when they return, while inspect failures preserve the last persisted observation and cannot count as recovery. Runs during each drift check cycle. Sends critical alerts on trip and info alerts on resolution. Disabled with `BOSUN_RESTART_BREAKER=false`. Keep `BOSUN_DRIFT_INTERVAL` at or below `BOSUN_RESTART_WINDOW` for timely detection; configuration load and `bosun doctor` warn when the sampling interval is longer.

## Deployment Targets

Deploy targets are **auto-discovered** from the staging directory after template rendering. The staging directory structure determines what gets synced:

- `appdata/` children are expanded one level (per-service granularity)
- `compose/` is handled specially (per-file isolated `docker compose up` with per-file rollback)
- All other top-level entries are synced as-is

Use `deploy_sync_paths` (allowlist) and `deploy_sync_exclude` (blocklist) in `bosun.yaml` or via `BOSUN_DEPLOY_SYNC_PATHS`/`BOSUN_DEPLOY_SYNC_EXCLUDE` env vars to filter which targets are deployed. Exclude wins over include.

### Local Deployment

Default mode. Copies rendered files directly to the local filesystem and runs `docker compose up` per-file with isolated rollback. Each compose file is deployed independently — if one file fails (e.g., bad image tag), only that file is rolled back from backup while other files continue. A final orphan-reconciliation pass runs `--remove-orphans` across each successful new file and each verified rollback's backup copy, preserving input order; failed files without a successful rollback are excluded so the cleanup pass never retries known-bad input.

**Stale-file pruning is managed-set scoped.** When content-hash sync is on (default), bosun removes a target file only if it was in the *previous* deploy's manifest (`state.deployed_files`) **and** is absent from the current rendered source. Files bosun never wrote — container runtime data like `db.sqlite3`, `grafana.db`, or a service's `data/` dir living alongside config in the same appdata dir — are never in the manifest, so they are never deleted. A render that produces zero files also skips pruning entirely, so a templating failure can't wipe a populated target. The first deploy after upgrading (empty manifest) prunes nothing and simply seeds the manifest.

```bash
bosun reconcile -l             # Force local mode
```

### Remote Deployment

Deploy to a remote host via SSH (tar-over-SSH for efficient transfer).

```bash
bosun reconcile -r user@host
```

Requires SSH key authentication and POSIX `sh` plus `tar`, `find`, `readlink`,
`sha256sum`, and a `test` implementation with `-ef` support on the remote host.
Missing `sha256sum` fails the first attempt without retrying because integrity
verification cannot succeed until the host capability changes. Test connectivity
first: `ssh user@host exit`.

Bosun creates the tar archive locally, round-trips it against a source snapshot,
then checks the archive SHA-256 and the complete extracted tree on the remote
before promotion. Empty trees are valid, while a missing, extra, changed, or
wrong-type entry aborts the deploy and leaves the existing target untouched.
Filenames with spaces, quotes, backslashes, newlines, and other control
characters are verified without using a line-oriented filename manifest.
The portable integrity manifest covers paths, entry types, regular-file bytes,
symlink targets, and hard-link topology. Tar remains responsible for preserving
permission bits and ownership; Bosun does not separately compare ownership,
ACLs, or extended attributes across hosts.

Devices, sockets, FIFOs, and other special source entries fail closed before
transfer because Bosun cannot portably verify them after extraction. Local
verification uses a process-owned `bosun-deploy-*` workspace in the platform
temp directory (on Unix, `${TMPDIR:-/tmp}`; on Windows, `%TEMP%`), containing
the archive and a full extracted round trip. Unix creates the workspace at mode
0700 and its archive at mode 0600; Windows uses the temp directory's platform
ACL semantics. Plan for temporary free space of roughly twice the source-tree
size in addition to the existing staging tree. Normal success and error paths
remove the workspace; after an unclean Bosun or host crash, operators may
remove stale `bosun-deploy-*` workspaces owned by the Bosun service account once
no reconcile process is running.

The staged tree is promoted with a retain-old rename-swap: the live target is moved aside (never deleted first), the new tree is moved in, and the retained copy is removed only on success — so an interrupted deploy leaves the old or the new tree, never an empty target. A missing target left by a prior interrupted deploy is self-healed on the next run from the newest retained copy.

#### SSH Host Key Verification

Bosun verifies SSH host keys using only config-controlled paths:

1. `BOSUN_SSH_KNOWN_HOSTS` (explicit override)
2. `/config/known_hosts` (container convention)

`~/.ssh/known_hosts` is intentionally excluded — ephemeral entries from manual `ssh` commands inside a container can cause go-git key mismatches. If neither path exists, verification falls back to insecure mode with a warning. Set `BOSUN_SSH_INSECURE_HOST_KEY=true` to disable verification entirely.

## Environment Variables

These configure the reconciliation pipeline (used by daemon and one-shot modes):

| Variable | Default | Description |
|----------|---------|-------------|
| `REPO_URL` | *required* | Git repository URL |
| `REPO_BRANCH` | `main` | Branch to track |
| `BOSUN_GIT_USERNAME` | | Private HTTPS Git Basic-auth username; set with `BOSUN_GIT_TOKEN` |
| `BOSUN_GIT_TOKEN` | | Private HTTPS Git Basic-auth password/token; set with `BOSUN_GIT_USERNAME` |
| `REPO_DIR` | `/app/repo` | Local clone directory |
| `STAGING_DIR` | `/app/staging` | Secret-bearing rendered staging and single-slot failure evidence directory |
| `BACKUP_DIR` | `/app/backups` | Backup directory |
| `LOG_DIR` | `/app/logs` | Log directory |
| `LOCAL_APPDATA` | `/mnt/appdata` | Local appdata path |
| `REMOTE_APPDATA` | `/mnt/user/appdata` | Remote appdata path |
| `DEPLOY_TARGET` | *(local)* | Target host for remote deployment |
| `SECRETS_FILES` | | Comma-separated SOPS encrypted files |
| `DRY_RUN` | `false` | Enable dry run |
| `FORCE` | `false` | Force deployment |
| `BOSUN_POST_SYNC_HOOKS` | | JSON array of post-sync hooks (overrides config file) |
| `BOSUN_HOOK_SETTLE_DELAY` | safe FUSE fallback when unset | Global pause before post-sync hooks run (e.g., `2s`); explicit `0s` disables it |
| `BOSUN_DEPLOY_PATHS` | | JSON array of glob patterns for deploy-relevant paths (overrides config file) |
| `BOSUN_DEPLOY_SYNC_PATHS` | | JSON array of glob patterns for deploy sync target allowlist (overrides config file) |
| `BOSUN_DEPLOY_SYNC_EXCLUDE` | | JSON array of glob patterns for deploy sync target blocklist (overrides config file; exclude wins over include) |
| `BOSUN_CRITICAL_CONTAINERS` | | JSON array of container names that must be healthy after deploy (overrides config file) |
| `BOSUN_DRIFT_IGNORE` | | JSON array of `{"service","type"}` rules to suppress known drift noise (overrides config file) |
| `BOSUN_TARGETS` | | JSON array of target definitions (overrides `targets:` in config file) |
| `BOSUN_HEALTH_GATE_TIMEOUT` | `60s` | Health gate polling timeout (`0` disables; accepts Go duration strings or bare seconds) |
| `BOSUN_OTEL_ENDPOINT` | *(disabled)* | OpenTelemetry OTLP HTTP endpoint (e.g., `http://localhost:4318`). When set, spans are exported for each reconciliation pipeline phase. When empty, a noop provider is used (zero overhead) |

Private HTTPS clone and fetch share one credential pair. Set both variables or
leave both unset for anonymous HTTPS. Bosun fails before network access for a
partial pair, a non-HTTPS or hostless URL, or any URL userinfo. Authenticated
redirects must stay HTTPS on the configured host and effective port. The pair
has no legacy aliases or YAML fields, applies after `BOSUN_REPO_URL` takes
precedence over `REPO_URL`, and rotates only after restarting the process.

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

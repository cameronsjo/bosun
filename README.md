# Bosun

<p align="center"><img src="docs/mascot/bosun-reference-nobg.png" width="200" alt="Bosun mascot"></p>

**GitOps for Docker Compose. No Kubernetes required.**

Push to git. Bosun receives orders. Containers deploy. Smooth sailing.

<!-- DIAGRAM:pipeline-overview -->
```text
┌──────────┐     ┌───────────────────────┐     ┌─────────────────┐     ┌──────────────────┐     ┌──────────────────┐     ┌───────────────────┐     ┌────────────────────┐
│          │     │                       │     │                 │     │                  │     │                  │     │                   │     │                    │
│ git push ├────►│ Bosun receives orders ├────►│ Clone & decrypt ├────►│ Template configs ├────►│ Deploy to target ├────►│ docker compose up ├────►│ Drift verification │
│          │     │                       │     │                 │     │                  │     │                  │     │                   │     │                    │
└──────────┘     └───────────────────────┘     └─────────────────┘     └──────────────────┘     └──────────────────┘     └───────────────────┘     └────────────────────┘
```
<!-- /DIAGRAM:pipeline-overview -->

## Why Bosun?

You run 40 containers on bare metal. Traefik routes traffic. Secrets are everywhere.
You want GitOps -- push a change, everything updates -- but Kubernetes is overkill for a homelab.

Bosun is **Helm for home**: a single binary that brings GitOps workflows to Docker Compose.

| What you get | How it works |
|---|---|
| **Push-to-deploy** | Webhooks or polling trigger reconciliation |
| **Secret management** | SOPS + Age encryption, decrypted at deploy time |
| **Config templating** | Go templates + Sprig functions, DRY service definitions |
| **Drift detection** | Periodic checks: is what's running what you declared? |
| **Multi-provider alerts** | Discord, SendGrid, Twilio notifications on deploy events |
| **Single binary** | No Python, no Node, no bash scripts on target |

## Architecture

<!-- DIAGRAM:architecture -->
```text
┌─────────────────────────────────────────────────────────────────────────────────┐
│                               Your Yacht (Server)                               │
│                                                                                 │
│                                                                                 │
│ ┌─────────────────────────────────────────────────────────────────────────────┐ │
│ │                                    Bosun                                    │ │
│ │                                                                             │ │
│ │                                                                             │ │
│ │ ┌────────────────────────────────────┐     ┌──────────────────────────────┐ │ │
│ │ │                                    │     │                              │ │ │
│ │ │              git push              │  ┌┄┄┤ Drift Watch / Periodic check │ │ │
│ │ │                                    │  ┆  │                              │ │ │
│ │ └──────────────────┬─────────────────┘  ┆  └──────────────────────────────┘ │ │
│ │                    │                    ┆                                   │ │
│ │                    │                    ┆                                   │ │
│ │                    │                    ┆                                   │ │
│ │                    │                    ┆                                   │ │
│ │                    │                    ┆                                   │ │
│ │                    │                    ┆                                   │ │
│ │                    │                    ┆                                   │ │
│ │                    │                    ┆                                   │ │
│ │                    ▼                    ┆                                   │ │
│ │ ┌────────────────────────────────────┐  ┆                                   │ │
│ │ │                                    │  ┆                                   │ │
│ │ │        Radio / Webhook/Poll        │  ┆                                   │ │
│ │ │                                    │  ┆                                   │ │
│ │ └──────────────────┬─────────────────┘  ┆                                   │ │
│ │                    │                    ┆                                   │ │
│ │                    │                    ┆                                   │ │
│ │                    │                    ┆                                   │ │
│ │                    │                    ┆                                   │ │
│ │                    ▼                    ┆                                   │ │
│ │ ┌────────────────────────────────────┐  ┆                                   │ │
│ │ │                                    │  ┆                                   │ │
│ │ │   Fetch Orders / git clone/pull    │  ┆                                   │ │
│ │ │                                    │  ┆                                   │ │
│ │ └──────────────────┬─────────────────┘  ┆                                   │ │
│ │                    │                    ┆                                   │ │
│ │                    │                    ┆                                   │ │
│ │                    │                    ┆                                   │ │
│ │                    │                    ┆                                   │ │
│ │                    ▼                    ┆                                   │ │
│ │ ┌────────────────────────────────────┐  ┆                                   │ │
│ │ │                                    │  ┆                                   │ │
│ │ │    Decrypt Secrets / SOPS + Age    │  ┆                                   │ │
│ │ │                                    │  ┆                                   │ │
│ │ └──────────────────┬─────────────────┘  ┆                                   │ │
│ │                    │                    ┆                                   │ │
│ │                    │                    ┆                                   │ │
│ │                    │                    ┆                                   │ │
│ │                    │                    ┆                                   │ │
│ │                    ▼                    ┆                                   │ │
│ │ ┌────────────────────────────────────┐  ┆                                   │ │
│ │ │                                    │  ┆                                   │ │
│ │ │    Prep Configs / Go Templates     │  ┆                                   │ │
│ │ │                                    │  ┆                                   │ │
│ │ └──────────────────┬─────────────────┘  ┆                                   │ │
│ │                    │                    ┆                                   │ │
│ │                    │                    ┆                                   │ │
│ │                    │                    ┆                                   │ │
│ │                    │                    ┆                                   │ │
│ │                    ▼                    ┆                                   │ │
│ │ ┌────────────────────────────────────┐  ┆                                   │ │
│ │ │                                    │  ┆                                   │ │
│ │ │ Deploy / tar-over-SSH / local copy │  ┆                                   │ │
│ │ │                                    │  ┆                                   │ │
│ │ └──────────────────┬─────────────────┘  ┆                                   │ │
│ │                    │                    ┆                                   │ │
│ │                    │                    ┆                                   │ │
│ │                    │                    ┆                                   │ │
│ │                    │                    ┆                                   │ │
│ │                    ▼                    ┆                                   │ │
│ │ ┌────────────────────────────────────┐  ┆                                   │ │
│ │ │                                    │  ┆                                   │ │
│ │ │      Crew Up / docker compose      │  ┆                                   │ │
│ │ │                                    │  ┆                                   │ │
│ │ └──────────────────┬─────────────────┘  ┆                                   │ │
│ │                    │                    ┆                                   │ │
│ └────────────────────┼────────────────────┆───────────────────────────────────┘ │
│                   verify┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┘                                     │
│                      ┆                                                          │
│                      ▼                                                          │
│   ┌────────────────────────────────────┐                                        │
│   │                                    │                                        │
│   │       Your Crew / Containers       │                                        │
│   │                                    │                                        │
│   └────────────────────────────────────┘                                        │
│                                                                                 │
└─────────────────────────────────────────────────────────────────────────────────┘
```
<!-- /DIAGRAM:architecture -->

## Installation

### Quick Install (Recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/cameronsjo/bosun/main/scripts/install.sh | bash
```

Downloads the latest release, verifies the SHA256 checksum, and installs to `/usr/local/bin`.

### Other Methods

```bash
# Go install
go install github.com/cameronsjo/bosun/cmd/bosun@latest

# From source
git clone https://github.com/cameronsjo/bosun.git
cd bosun && make build
./build/bosun --version
```

### Update

```bash
bosun update          # Download and install latest
bosun update --check  # Check without installing
```

`bosun update` requires `checksums.txt` from the same GitHub release and verifies
the selected compressed archive before extraction or executable replacement. A
missing checksum asset, invalid selected entry, download failure, or digest
mismatch aborts the update and leaves the installed executable unchanged.
`bosun update --check` checks release metadata only and downloads neither asset.

### Checksum Trust Boundary

The same-release SHA-256 manifest detects corruption and mismatched assets, but
it is an integrity check rather than independent publisher authentication. An
attacker who can replace both the archive and `checksums.txt` is outside this
control. Releases do not currently publish `checksums.txt.pem` or
`checksums.txt.sig`; Bosun does not claim checksum signature verification.

## Quick Start

```bash
# 1. Generate encryption key
age-keygen -o ~/.config/sops/age/keys.txt

# 2. Create .sops.yaml with your public key
cat > .sops.yaml << 'EOF'
creation_rules:
  - path_regex: .*\.yaml$
    age: <your-public-key>
EOF

# 3. Initialize your yacht
bosun init

# 4. Check if everything is seaworthy
bosun doctor

# 5. Start the yacht
bosun yacht up
```

## Commands

### Setup & Diagnostics

| Command | Description |
|---------|-------------|
| `bosun init` | Interactive setup wizard (`--systemd` for unit files) |
| `bosun doctor` | Pre-flight checks |
| `bosun validate` | Validate config and daemon connectivity |
| `bosun status` | Health dashboard |

### GitOps

| Command | Description |
|---------|-------------|
| `bosun daemon` | Run the GitOps daemon |
| `bosun reconcile` | One-shot GitOps workflow |
| `bosun trigger` | Trigger reconciliation via daemon |
| `bosun daemon-status` | Show daemon health and state |
| `bosun drift` | Detect config drift (`--live` for fresh check) |

### Docker Management

| Command | Description |
|---------|-------------|
| `bosun yacht up/down/restart/status` | Manage Docker Compose services |
| `bosun crew list/logs/inspect/restart` | Manage individual containers |

### Manifest & Provisioning

| Command | Description |
|---------|-------------|
| `bosun provision [stack]` | Render manifest to compose/traefik/gatus |
| `bosun provisions` | List available provisions |
| `bosun create <template> <name>` | Scaffold new service |
| `bosun lint` | Validate manifests |

### Operations

| Command | Description |
|---------|-------------|
| `bosun radio test/status` | Test webhook and Tailscale |
| `bosun mayday` | Show errors, rollback snapshots |
| `bosun webhook` | Run standalone webhook receiver |

See **[Commands Reference](docs/commands.md)** for full documentation.

## Daemon Mode

Run bosun as a long-running daemon for production GitOps:

```bash
# Generate systemd unit files
bosun init --systemd

# Install and start
cd systemd && sudo ./install.sh

# Or run directly
bosun daemon
```

The daemon provides:

- **Unix socket API** at `/var/run/bosun.sock`
- **GitHub webhooks** (`/webhook/github`) and a generic HMAC endpoint (`/webhook`). GitLab, Gitea, and Bitbucket run through the separate `bosun webhook` receiver, which normalizes each provider and forwards to the daemon
- **Configurable polling** with interval-based reconciliation
- **Health endpoints** (`/health`, `/ready`) for orchestrators
- **Drift detection** with periodic declared-vs-actual state checks
- **Circuit breaker** stops retrying after 3 consecutive failures

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `BOSUN_REPO_URL` | Git repository URL | Required |
| `BOSUN_REPO_BRANCH` | Branch to track | `main` |
| `BOSUN_GIT_USERNAME` | Private HTTPS Git Basic-auth username; requires `BOSUN_GIT_TOKEN` | Unset |
| `BOSUN_GIT_TOKEN` | Private HTTPS Git Basic-auth password/token; requires `BOSUN_GIT_USERNAME` | Unset |
| `BOSUN_GIT_FETCH_DEPTH` | Shallow clone/fetch history depth; increase when deploy diffs span multiple commits | `1` |
| `BOSUN_POLL_INTERVAL` | Poll interval in seconds | `3600` |
| `BOSUN_SOCKET_PATH` | Unix socket path | `/var/run/bosun.sock` |
| `BOSUN_SOCKET_ALLOWED_UIDS` | Additional numeric UIDs allowed to trigger through the Unix socket (comma-separated) | Daemon UID only |
| `BOSUN_ALLOW_UNAUTHENTICATED_SOCKET` | Disable Unix socket peer-credential authorization (`true` only; logs security warnings) | `false` |
| `WEBHOOK_SECRET` | Webhook signature validation | Required for webhook triggers (fail-closed) |
| `BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK` | Accept unauthenticated webhook triggers (`true` only; logs a security warning per request) | `false` |
| `BOSUN_LISTEN_ADDR` | Host/IP the HTTP server binds to | All interfaces |

> **Webhook auth fails closed.** With no `WEBHOOK_SECRET` set, the daemon's
> HTTP trigger endpoints (`/webhook`, `/webhook/github`, `/webhook/manual`)
> reject every request with `403`. To restore the old accept-anything behavior
> on a trusted network, set `BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK=true`
> explicitly. The Unix socket trigger (`bosun trigger`) is unaffected.

> **Unix socket mutation auth also fails closed.** On Linux, the daemon UID and
> numeric UIDs in `BOSUN_SOCKET_ALLOWED_UIDS` can trigger reconciliation. A
> missing peer credential (including on non-Linux platforms) returns `403`.
> Set `BOSUN_ALLOW_UNAUTHENTICATED_SOCKET=true` only when socket permissions
> are intentionally the entire trust boundary.

For a private HTTPS repository, set `BOSUN_GIT_USERNAME` and
`BOSUN_GIT_TOKEN` together. Bosun sends them only to the configured HTTPS
origin and rejects plaintext, cross-origin, downgrade, partial-pair, and
URL-userinfo configurations before network access. Leave both unset for
anonymous HTTPS. These new variables have no unprefixed aliases and apply to
the effective URL after `BOSUN_REPO_URL` takes precedence over `REPO_URL`.
Remove credentials from the URL itself, and restart Bosun after rotation;
project configuration reload does not read or rotate the pair.

## Configuration

Bosun looks for configuration in order:

1. `bosun.yaml` in the current directory
2. `.bosun.yaml` in the current directory
3. `$HOME/.config/bosun/config.yaml`

```yaml
# bosun.yaml
root: .
manifest_dir: manifest
compose_file: docker-compose.yml
```

### Additional Environment Variables (Reconcile)

> These variables are used by `bosun reconcile` and the daemon's reconciliation pipeline. `REPO_URL` and `REPO_BRANCH` are legacy aliases for `BOSUN_REPO_URL` and `BOSUN_REPO_BRANCH` — if both are set, the `BOSUN_`-prefixed variable takes precedence.

| Variable | Description | Default |
|----------|-------------|---------|
| `REPO_URL` | Git repository URL | Required for reconcile |
| `REPO_BRANCH` | Git branch to track | `main` |
| `BOSUN_GIT_USERNAME` | Private HTTPS Git username; pair with `BOSUN_GIT_TOKEN` | Unset |
| `BOSUN_GIT_TOKEN` | Private HTTPS Git token; pair with `BOSUN_GIT_USERNAME` | Unset |
| `SOPS_AGE_KEY_FILE` | Path to age key file | `~/.config/sops/age/keys.txt` |
| `DEPLOY_TARGET` | Remote host for deployment | Local if unset |

## Reconciliation Pipeline

<!-- DIAGRAM:reconcile-pipeline -->
```text
┌────────────────────────────────────────────────────────────────┐
│                                                                │
│                        1. Acquire Lock                         │
│                                                                │
└────────────────────────────────┬───────────────────────────────┘
                                 │
                                 │
                                 │
                                 │
                                 ▼
┌────────────────────────────────────────────────────────────────┐
│                                                                │
│                     2. Git Repository Sync                     │
│                                                                │
└────────────────────────────────┬───────────────────────────────┘
                                 │
                                 │
                                 │
                                 │
                                 ▼
┌────────────────────────────────────────────────────────────────┐
│                                                                │
│ 3. Reload Config + Check State / Commit / Path Skips + Breaker │
│                                                                │
└────────────────────────────────┬───────────────────────────────┘
                                 │
                                 │
                                 │
                                 │
                                 ▼
◇────────────────────────────────────────────────────────────────◇
│                                                                │
│          Deploy required? / No: release lock + return          │
│                                                                │
◇────────────────────────────────┬───────────────────────────────◇
                                 │
                                yes
                                 │
                                 │
                                 ▼
┌────────────────────────────────────────────────────────────────┐
│                                                                │
│        4. Track Attempt + Decrypt / Resolve Deploy Mode        │
│                                                                │
└────────────────────────────────┬───────────────────────────────┘
                                 │
                                 │
                                 │
                                 │
                                 ▼
┌────────────────────────────────────────────────────────────────┐
│                                                                │
│                 5. Render Private Staging Tree                 │
│                                                                │
└────────────────────────────────┬───────────────────────────────┘
                                 │
                                 │
                                 │
                                 │
                                 ▼
┌────────────────────────────────────────────────────────────────┐
│                                                                │
│     6. Extract Declared Services / Fail Closed if Missing      │
│                                                                │
└────────────────────────────────┬───────────────────────────────┘
                                 │
                                 │
                                 │
                                 │
                                 ▼
┌────────────────────────────────────────────────────────────────┐
│                                                                │
│               7. Create + Verify Rollback Backup               │
│                                                                │
└────────────────────────────────┬───────────────────────────────┘
                                 │
                                 │
                                 │
                                 │
                                 ▼
┌────────────────────────────────────────────────────────────────┐
│                                                                │
│            8. Snapshot Health / Mark NeedsRedeploy             │
│                                                                │
└────────────────────────────────┬───────────────────────────────┘
                                 │
                                 │
                                 │
                                 │
                                 ▼
┌────────────────────────────────────────────────────────────────┐
│                                                                │
│       9. Deploy Managed Files / Verify Writes / Transfer       │
│                                                                │
└────────────────────────────────┬───────────────────────────────┘
                                 │
                                 │
                                 │
                                 │
                                 ▼
┌────────────────────────────────────────────────────────────────┐
│                                                                │
│             10. Docker Compose Up / Signal Reload              │
│                                                                │
└────────────────────────────────┬───────────────────────────────┘
                                 │
                                 │
                                 │
                                 │
                                 ▼
┌────────────────────────────────────────────────────────────────┐
│                                                                │
│       11. Optional Health Gate / Rollback Failed Rollout       │
│                                                                │
└────────────────────────────────┬───────────────────────────────┘
                                 │
                                 │
                                 │
                                 │
                                 ▼
┌────────────────────────────────────────────────────────────────┐
│                                                                │
│                  12. Execute Post-Sync Hooks                   │
│                                                                │
└────────────────────────────────┬───────────────────────────────┘
                                 │
                                 │
                                 │
                                 │
                                 ▼
┌────────────────────────────────────────────────────────────────┐
│                                                                │
│          13. Local Post-Deploy / Health + Drift Check          │
│                                                                │
└────────────────────────────────┬───────────────────────────────┘
                                 │
                                 │
                                 │
                                 │
                                 ▼
┌────────────────────────────────────────────────────────────────┐
│                                                                │
│                 14. Finalize Staging Evidence                  │
│                                                                │
└────────────────────────────────┬───────────────────────────────┘
                                 │
                                 │
                                 │
                                 │
                                 ▼
┌────────────────────────────────────────────────────────────────┐
│                                                                │
│          15. Retain Verified Backups / Record Success          │
│                                                                │
└────────────────────────────────┬───────────────────────────────┘
                                 │
                                 │
                                 │
                                 │
                                 ▼
┌────────────────────────────────────────────────────────────────┐
│                                                                │
│                        16. Release Lock                        │
│                                                                │
└────────────────────────────────────────────────────────────────┘
```
<!-- /DIAGRAM:reconcile-pipeline -->

### Trigger Coalescing and Reconcile Locking

<!-- DIAGRAM:locking-singleflight -->
```text
┌─────────────────────────────────────────────────────────┐
│                                                         │
│                         Trigger                         │
│                                                         │
└────────────────────────────┬────────────────────────────┘
                             │
                             │
                             │
                             │
                             ▼
◇─────────────────────────────────────────────────────────◇
│                                                         │
│                 Reconciliation running?                 ├──────────────────────────────────────┐
│                                                         │                                      │
◇────────────────────────────┬────────────────────────────◇                                     no
                             │                                                                   │
                            yes                                                                  │
                             │                                                                   │
                             │                                                                   │
                             ▼                                                                   ▼
┌─────────────────────────────────────────────────────────┐     ┌─────────────────────────────────────────────────────────────────┐
│                                                         │     │                                                                 │
│ Add to pendingTrigger batch / preserve source and force │     │ Acquire /var/run/bosun/reconcile.lock / flock LOCK_EX | LOCK_NB │
│                                                         │     │                                                                 │
└────────────────────────────┬────────────────────────────┘     └────────────────────────────────┬────────────────────────────────┘
                             │                                                                   │
                             │                                                                   │
                             │                                                                   │
                             │                                                                   │
                             ▼                                                                   ▼
┌─────────────────────────────────────────────────────────┐     ┌─────────────────────────────────────────────────────────────────┐
│                                                         │     │                                                                 │
│                   Return 202 Accepted                   │     │                        Run Reconciliation                       │
│                                                         │     │                                                                 │
└─────────────────────────────────────────────────────────┘     └────────────────────────────────┬────────────────────────────────┘
                                                                                                 │
                                                                                                 │
                             ┌───────────────────────────────┬───────────────────────────────────┘
                             │                               │
                             ▼                               │
◇─────────────────────────────────────────────────────────◇  │
│                                                         │  │
│               pendingTrigger batch queued?              ├──┼───────────────────────────────────┐
│                                                         │  │                                   │
◇────────────────────────────┬────────────────────────────◇  │                                  no
                             │                               │                                   │
                            yes                              │                                   │
                             │                               │                                   │
                             │                               │                                   │
                             ▼                               │                                   ▼
┌─────────────────────────────────────────────────────────┐  │  ┌─────────────────────────────────────────────────────────────────┐
│                                                         │  │  │                                                                 │
│             Run One Coalesced Reconciliation            │  │  │                              Finish                             │
│                                                         │  │  │                                                                 │
└────────────────────────────┬────────────────────────────┘  │  └─────────────────────────────────────────────────────────────────┘
                             │                               │
                             └───────────────────────────────┘
```
<!-- /DIAGRAM:locking-singleflight -->

## The Nautical Theme

Everything uses nautical terminology:

| Term | Meaning |
|------|---------|
| **Bosun** | The CLI tool (receives orders, deploys crew) |
| **Captain** | GitHub (gives the orders) |
| **Yacht** | Your server running Docker Compose |
| **Crew** | Containers |
| **Manifest** | Service definitions (crew manifest) |
| **Provisions** | Reusable config templates (supplies stocked aboard) |
| **Radio** | Webhook/tunnel connection (Tailscale Funnel) |

## Documentation

| Doc | Description |
|-----|-------------|
| **[Commands Reference](docs/commands.md)** | Full CLI documentation |
| **[Concepts](docs/concepts.md)** | Architecture and components |
| **[GitOps Workflow](docs/gitops.md)** | Reconciliation, polling, triggers |
| **[Manifest System](docs/manifest-system.md)** | DRY service definitions |
| **[Daemon Architecture](docs/architecture/daemon-split.md)** | Unix socket API, webhooks, security |
| **[Alerting](docs/alerting.md)** | Discord, SendGrid, Twilio notifications |
| **[CI Pipeline](docs/ci.md)** | Dagger-based CI/CD |
| **[Security](docs/security.md)** | Security considerations |
| **[Troubleshooting](docs/troubleshooting.md)** | Common issues and solutions |
| **[Migration Guide](docs/migration.md)** | From bash/Python version |
| **[GitOps Comparison](docs/gitops-comparison.md)** | Bosun vs Argo CD vs Flux CD |

### Architecture Decisions

| ADR | Summary |
|-----|---------|
| [Daemon Architecture](docs/architecture/daemon-split.md) | Unix socket API, webhook reception, standalone receiver split |
| [Council Review](docs/architecture/council-review.md) | Security-first daemon design (9/10) |
| [0001: Manifest System](docs/adr/0001-manifest-system.md) | DRY crew provisioning |
| [0008: Container vs Daemon](docs/adr/0008-container-vs-daemon.md) | When to use systemd |
| [0010: Go Rewrite](docs/adr/0010-go-rewrite.md) | Single-binary CLI |
| [0011: Helm Alignment](docs/adr/0011-helm-alignment.md) | Chart-based manifest format |

## Requirements

- **Go 1.25+** (building from source)
- **Docker + Docker Compose v2**
- **Git** (for reconcile workflow)
- **SOPS + Age** (for secret encryption)
- Linux or macOS (tested: Unraid, Debian, Ubuntu, macOS)

## Development

```bash
make build          # Build binary
make test           # Run tests
make test-cover     # Run with coverage
make dev            # Development build (no optimizations)
make build-all      # Build for all platforms
make ci             # Full Dagger CI pipeline
make lint           # Run linter
```

See [docs/ci.md](docs/ci.md) for the full CI pipeline.

## Support

[![Ko-fi](https://ko-fi.com/img/githubbutton_sm.svg)](https://ko-fi.com/cameronsjo)

## License

MIT

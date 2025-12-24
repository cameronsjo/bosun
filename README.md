# Bosun

**Helm for home.**

You're the captain of your homelab. 40 containers. Traefik. Secrets everywhere.
You shouldn't have to swab the deck yourself. That's what the bosun is for.

```
git push -> bosun receives orders -> crew deployed -> yacht runs smooth
```

No Kubernetes. No drama. Just smooth sailing.

```
+------------------------------------------------------------------------------+
|                              Your Yacht (Server)                             |
|                                                                              |
|  +------------------------------------------------------------------------+  |
|  |                              Bosun                                     |  |
|  |  +---------+  +---------+  +---------+  +---------+  +---------+       |  |
|  |  |  Radio  |->|  Fetch  |->| Decrypt |->|  Prep   |->| Deploy  |       |  |
|  |  |(Webhook)|  | Orders  |  | Secrets |  | Configs |  |  Crew   |       |  |
|  |  +---------+  +---------+  +---------+  +---------+  +---------+       |  |
|  +------------------------------------------------------------------------+  |
|        ^                                                   |                 |
|        |                                                   v                 |
|  +-----+-----+                                    +--------------+           |
|  | Tailscale |                                    |  Your Crew   |           |
|  |  Funnel   |                                    | (Containers) |           |
|  +-----+-----+                                    +--------------+           |
+--------|---------------------------------------------------------------------+
         |
         v
   +----------+
   | Captain  |
   | (GitHub) |
   +----------+
```

## Installation

### Quick Install (Recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/cameronsjo/bosun/main/scripts/install.sh | bash
```

This downloads the latest release, verifies the SHA256 checksum, and installs to `/usr/local/bin`.

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
bosun update --check  # Check for updates without installing
```

### Verify Release

Releases are signed with [cosign](https://github.com/sigstore/cosign) and include [SLSA provenance](https://slsa.dev/).

```bash
# Verify checksum signature
cosign verify-blob --certificate checksums.txt.pem \
  --signature checksums.txt.sig checksums.txt

# Verify build provenance
gh attestation verify bosun_*.tar.gz --owner cameronsjo
```

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
- **Unix socket API** - Primary interface at `/var/run/bosun.sock`
- **Multi-provider webhooks** - GitHub, GitLab, Gitea, Bitbucket
- **Polling** - Configurable interval reconciliation
- **Health endpoints** - `/health`, `/ready` for orchestrators

### Daemon Commands

```bash
bosun daemon              # Run the daemon
bosun trigger             # Trigger reconciliation
bosun daemon-status       # Show daemon health
bosun validate            # Validate configuration
bosun webhook             # Run standalone webhook receiver
```

### Environment Variables (Daemon)

| Variable | Description | Default |
|----------|-------------|---------|
| `BOSUN_REPO_URL` | Git repository URL | Required |
| `BOSUN_REPO_BRANCH` | Branch to track | `main` |
| `BOSUN_POLL_INTERVAL` | Poll interval in seconds | `3600` |
| `BOSUN_SOCKET_PATH` | Unix socket path | `/var/run/bosun.sock` |
| `WEBHOOK_SECRET` | Webhook signature validation | Optional |

See [docs/architecture/daemon-split.md](docs/architecture/daemon-split.md) for the full daemon architecture.

## Commands

### Setup & Diagnostics

| Command | Description |
|---------|-------------|
| `init` | Interactive setup wizard (`--systemd` for unit files) |
| `doctor` | Pre-flight checks |
| `validate` | Validate config and daemon connectivity |
| `status` | Health dashboard |

### Daemon

| Command | Description |
|---------|-------------|
| `daemon` | Run the GitOps daemon |
| `trigger` | Trigger reconciliation via daemon |
| `daemon-status` | Show daemon health and state |
| `webhook` | Run standalone webhook receiver |

### Yacht (Docker Compose)

| Command | Description |
|---------|-------------|
| `yacht up/down/restart/status` | Manage Docker Compose services |
| `crew list/logs/inspect/restart` | Manage individual containers |

### Manifest & Provisioning

| Command | Description |
|---------|-------------|
| `provision [stack]` | Render manifest to compose/traefik/gatus |
| `provisions` | List available provisions |
| `create <template> <name>` | Scaffold new service |
| `lint` | Validate manifests |
| `drift` | Detect config drift |

### Operations

| Command | Description |
|---------|-------------|
| `reconcile` | Run GitOps workflow (one-shot) |
| `radio test/status` | Test webhook and Tailscale |
| `mayday` | Show errors, rollback snapshots |

See [docs/commands.md](docs/commands.md) for the full command reference.

## Configuration

Bosun looks for configuration in the following locations:

1. `bosun.yaml` in the current directory
2. `.bosun.yaml` in the current directory
3. `$HOME/.config/bosun/config.yaml`

Example configuration:

```yaml
# bosun.yaml
root: .
manifest_dir: manifest
compose_file: docker-compose.yml
```

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `REPO_URL` | Git repository URL (for reconcile) | Required for reconcile |
| `REPO_BRANCH` | Git branch to track | `main` |
| `SOPS_AGE_KEY_FILE` | Path to age key file | `~/.config/sops/age/keys.txt` |
| `DEPLOY_TARGET` | Remote host for deployment | Local if unset |

## What's on Board

| Component | Role |
|-----------|------|
| **Bosun CLI** | Single binary managing Docker, manifests, and GitOps |
| **Manifest System** | Write 10 lines, generate compose + Traefik + Gatus configs |
| **Provisions** | Reusable config templates - batteries included, all swappable |

## Documentation

- **[Commands Reference](docs/commands.md)** - Full command documentation
- **[Daemon Architecture](docs/architecture/daemon-split.md)** - Unix socket API, webhooks, security
- **[GitOps Workflow](docs/gitops.md)** - Reconciliation, polling, triggers
- **[Migration Guide](docs/migration.md)** - Migrating from bash/Python version
- **[Concepts](docs/concepts.md)** - Architecture, components, diagrams

### Architecture Decisions

| ADR | Status | Summary |
|-----|--------|---------|
| [Daemon Architecture](docs/architecture/daemon-split.md) | Accepted | Unix socket API, multi-provider webhooks |
| [Council Review](docs/architecture/council-review.md) | Approved | Security-first daemon design (9/10) |
| [0001: Manifest System](docs/adr/0001-manifest-system.md) | Accepted | DRY crew provisioning |
| [0008: Container vs Daemon](docs/adr/0008-container-vs-daemon.md) | Accepted | When to use systemd |
| [0010: Go Rewrite](docs/adr/0010-go-rewrite.md) | Accepted | Single-binary CLI |

## Requirements

- Go 1.24+ (for building from source)
- Docker + Docker Compose v2
- Linux or macOS (tested: Unraid, Debian, Ubuntu, macOS)
- Git (for reconcile workflow)
- SOPS + Age (for secret encryption)

## Development

```bash
# Build
make build

# Run tests
make test

# Run with coverage
make test-cover

# Build for all platforms
make build-all

# Development build (no optimizations)
make dev
```

## Support

[![Ko-fi](https://ko-fi.com/img/githubbutton_sm.svg)](https://ko-fi.com/cameronsjo)

## License

MIT

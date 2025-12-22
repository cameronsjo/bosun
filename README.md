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

### From Source (Recommended)

```bash
# Clone and build
git clone https://github.com/cameronsjo/bosun.git
cd bosun
make build

# Binary is at ./build/bosun
./build/bosun --version
```

### Go Install

```bash
go install github.com/cameronsjo/bosun/cmd/bosun@latest
```

### Download Binary

Download the latest release for your platform from the [Releases](https://github.com/cameronsjo/bosun/releases) page.

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

| Command | Description |
|---------|-------------|
| `init` | Interactive setup wizard |
| `yacht up/down/restart/status` | Manage Docker Compose services |
| `crew list/logs/inspect/restart` | Manage individual containers |
| `provision [stack]` | Render manifest to compose/traefik/gatus |
| `provisions` | List available provisions |
| `create <template> <name>` | Scaffold new service |
| `radio test/status` | Test webhook and Tailscale |
| `status` | Health dashboard |
| `doctor` | Pre-flight checks |
| `drift` | Detect config drift |
| `lint` | Validate manifests |
| `mayday` | Show errors, rollback snapshots |
| `reconcile` | Run GitOps workflow |

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
- **[Migration Guide](docs/migration.md)** - Migrating from bash/Python version
- **[Concepts](docs/concepts.md)** - Architecture, components, diagrams
- **[Unraid Setup](docs/guides/unraid-setup.md)** - Complete walkthrough

### Architecture Decisions

| ADR | Status | Summary |
|-----|--------|---------|
| [0001: Manifest System](docs/adr/0001-service-composer.md) | Accepted | DRY crew provisioning |
| [0002: Watchtower Webhook](docs/adr/0002-watchtower-webhook-deploy.md) | Accepted | Crew rotation automation |
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

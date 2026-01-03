# Project Context

## Purpose

Bosun is a GitOps CLI tool for Docker Compose on bare metal - "Helm for home." It brings GitOps workflows to homelab Docker Compose deployments: push to git, bosun receives orders, containers deploy automatically. No Kubernetes required.

**Design Philosophy:**
- Captain gives orders, bosun executes - Push to git, everything updates
- Single binary - No Python, uv, or bash dependencies on target
- Every crew member has a backup - Batteries included, all swappable
- One yacht, many ports - Monorepo support for multi-server

## Tech Stack

- **Language:** Go 1.24+
- **CLI Framework:** Cobra (`github.com/spf13/cobra`)
- **Container Runtime:** Docker + Docker Compose v2 (`github.com/docker/docker`)
- **Secret Management:** SOPS + Age (`github.com/getsops/sops/v3`, `filippo.io/age`)
- **Templating:** Chezmoi, Sprig (`github.com/Masterminds/sprig/v3`)
- **Git Operations:** go-git (`github.com/go-git/go-git/v5`)
- **YAML:** gopkg.in/yaml.v3
- **Testing:** testify (`github.com/stretchr/testify`)
- **Console Output:** fatih/color

## Project Conventions

### Code Style

- Type annotations on all code
- Comments explain WHY, not what
- Defensive programming: validate inputs, fail fast
- No magic strings or numbers - use constants
- Prefer functional over imperative, immutable over mutable
- Use positive names (`enabled`, `visible`) over negative (`disabled`, `hidden`)

### Architecture Patterns

```
project/
├── cmd/bosun/main.go       # Entry point only
├── internal/
│   ├── cmd/                # Cobra CLI commands
│   ├── config/             # Configuration loading
│   ├── docker/             # Docker SDK wrapper
│   ├── daemon/             # Unix socket API, webhooks
│   ├── manifest/           # YAML rendering engine
│   ├── reconcile/          # GitOps engine
│   └── ui/                 # Colored console output
├── manifest/               # Service definitions
└── docs/                   # Documentation
```

**Command Pattern:**
```go
var exampleCmd = &cobra.Command{
    Use:     "example",
    Aliases: []string{"alias"},
    Short:   "Short description",
    Long:    "Long description...",
    Run:     runExample,
}

func init() {
    rootCmd.AddCommand(exampleCmd)
}
```

**GitOps Workflow:**
```
git push -> webhook/poll -> clone repo -> decrypt secrets (SOPS) ->
template configs (chezmoi) -> rsync to target -> docker compose up
```

### Testing Strategy

- Test behavior, not implementation
- Prefer integration tests over unit tests
- Run with `make test` or `make test-cover`
- Use testify for assertions
- Mock external dependencies (Docker SDK, filesystem)

### Git Workflow

- **Branch:** `main` (not master)
- **Commits:** Conventional Commits format: `type(scope): description`
- **Tense:** Present tense, imperative mood: "add feature" not "added feature"
- **Co-author:** Include `Co-Authored-By: Claude <noreply@anthropic.com>` on AI-assisted commits
- **Merges:** Use merge over rebase - preserves true history
- **PRs:** Use closing keywords: "Closes #123" or "Fixes #123"

## Domain Context

**Nautical/Below Deck Terminology:**
| Term | Meaning |
|------|---------|
| Bosun | CLI tool and orchestrator |
| Manifest | Service definitions (YAML) |
| Provisions | Reusable config templates |
| Captain | GitHub (gives orders) |
| Radio | Webhook/tunnel (Tailscale Funnel or Cloudflare Tunnel) |
| Crew | Containers |
| Yacht | Server/host running Docker Compose |

**Key Commands:**
- `bosun init` - Interactive setup wizard
- `bosun doctor` - Pre-flight checks
- `bosun daemon` - Run the GitOps daemon
- `bosun yacht up/down/restart` - Manage Docker Compose services
- `bosun crew list/logs/restart` - Manage individual containers
- `bosun provision` - Render manifest to compose configs

## Important Constraints

- Single binary deployment - no runtime dependencies on target systems
- Must work behind reverse proxy (configurable base paths, honor X-Forwarded-* headers)
- Daemon exposes Unix socket API at `/var/run/bosun.sock`
- Must support multi-server deployments from single monorepo
- Graceful shutdown handling (SIGTERM)
- Log to stdout (not files)

## External Dependencies

- **Docker Daemon:** Required for container operations
- **Git Repository:** Source of truth for configurations
- **SOPS/Age:** Optional secret encryption
- **Chezmoi:** Optional templating engine (external binary)
- **Webhook Providers:** GitHub, GitLab, Gitea, Bitbucket for push notifications

## Build Commands

```bash
make build              # Build -> build/bosun
make test               # Run tests
make test-cover         # Run with coverage
make dev                # Development build (no optimizations)
make build-all          # Build for all platforms
make release-dry-run    # Test release locally
```

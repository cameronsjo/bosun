# Bosun - AI Context

GitOps for Docker Compose on bare metal. "Helm for home."

## Nautical Theme

Everything uses nautical/Below Deck terminology:

- **Bosun** = CLI tool and orchestrator (receives orders, deploys containers)
- **Manifest** = service definitions (crew manifest)
- **Provisions** = reusable config templates (supplies stocked aboard)
- **Captain** = GitHub (gives orders)
- **Radio** = webhook/tunnel (Tailscale Funnel or Cloudflare Tunnel)
- **Crew** = containers

## Directory Structure

```
bosun/
├── cmd/bosun/
│   └── main.go               # Entry point
├── internal/
│   ├── cmd/                  # Cobra CLI commands
│   │   ├── root.go           # Root command, version
│   │   ├── yacht.go          # yacht up/down/restart/status
│   │   ├── crew.go           # crew list/logs/inspect/restart
│   │   ├── provision.go      # provision, provisions, create
│   │   ├── comms.go          # radio test/status
│   │   ├── diagnostics.go    # status, log, drift, doctor, lint
│   │   ├── emergency.go      # mayday, overboard
│   │   ├── reconcile.go      # GitOps reconcile command
│   │   └── init.go           # Interactive setup wizard
│   ├── config/
│   │   └── config.go         # Configuration loading
│   ├── docker/
│   │   ├── client.go         # Docker SDK wrapper
│   │   ├── compose.go        # Docker Compose operations
│   │   └── containers.go     # Container operations
│   ├── manifest/
│   │   ├── types.go          # Service/Stack types
│   │   ├── render.go         # Main rendering logic
│   │   ├── provision.go      # Provision loading
│   │   ├── merge.go          # Deep merge semantics
│   │   └── interpolate.go    # Variable interpolation
│   ├── reconcile/
│   │   ├── reconcile.go      # Main reconcile loop
│   │   ├── git.go            # Git operations
│   │   ├── sops.go           # SOPS decryption
│   │   ├── template.go       # Chezmoi templating
│   │   └── deploy.go         # Rsync/local deployment
│   ├── snapshot/
│   │   └── snapshot.go       # Rollback system
│   └── ui/
│       └── color.go          # Colored console output
├── manifest/                  # Service definitions (unchanged)
│   ├── provisions/            # Reusable config templates
│   ├── services/              # Service manifests
│   └── stacks/                # Stack definitions
├── docs/
│   ├── commands.md            # Full command reference
│   ├── migration.md           # Migration from bash/Python
│   ├── concepts.md            # Architecture diagrams
│   ├── adr/                   # Architecture Decision Records
│   └── guides/
├── build/                     # Compiled binaries (gitignored)
├── go.mod                     # Go module definition
├── go.sum                     # Dependency checksums
└── Makefile                   # Build targets
```

## Building and Running

```bash
# Build
make build              # -> build/bosun

# Run without building
make run ARGS="doctor"

# Development build (no optimizations)
make dev

# Install to GOPATH/bin
make install

# Build for all platforms
make build-all
```

## Testing

```bash
# Run tests
make test

# Run with coverage
make test-cover
# Creates coverage.out and coverage.html
```

## Key Packages

### internal/cmd

Cobra commands following the pattern:

```go
var exampleCmd = &cobra.Command{
    Use:     "example",
    Aliases: []string{"alias"},
    Short:   "Short description",
    Long:    "Long description...",
    Run:     runExample,
}

func runExample(cmd *cobra.Command, args []string) {
    // Implementation
}

func init() {
    rootCmd.AddCommand(exampleCmd)
}
```

### internal/docker

Docker SDK wrapper. Uses `github.com/docker/docker/client`.

```go
client, err := docker.NewClient()
defer client.Close()

containers, err := client.ListContainers(ctx, onlyRunning)
err := client.RestartContainer(ctx, name)
```

### internal/manifest

YAML rendering engine. Ported from Python.

- **Types**: `ServiceManifest`, `StackManifest`, `RenderOutput`
- **Rendering**: `RenderStack()`, `RenderService()`
- **Merge**: `DeepMerge()` with special handling for networks/depends_on
- **Interpolation**: `${var}` syntax resolved from service config

### internal/reconcile

GitOps engine. Ported from bash reconcile.sh.

Workflow:

1. Lock acquisition
2. Git clone/pull
3. SOPS decrypt
4. Chezmoi template
5. Backup
6. Deploy (rsync or local)
7. Docker compose up
8. Unlock

### internal/ui

Colored output helpers:

```go
ui.Success("Container started!")
ui.Warning("Traefik not running")
ui.Error("Failed to connect: %v", err)
ui.Fatal("Critical error: %v", err)  // Exits with code 1

ui.Green.Println("Text")
ui.Yellow.Printf("Value: %s", val)
```

## Design Principles

1. **Captain gives orders, bosun executes** - Push to git, everything updates
2. **Single binary** - No Python, uv, or bash dependencies on target
3. **Every crew member has a backup** - Batteries included, all swappable
4. **One yacht, many ports** - Monorepo support for multi-server

## Adding a New Command

1. Create file in `internal/cmd/<name>.go`
2. Define command and flags
3. Add to `rootCmd` in `init()`
4. Update `docs/commands.md`

Example:

```go
// internal/cmd/example.go
package cmd

import (
    "github.com/spf13/cobra"
    "github.com/cameronsjo/bosun/internal/ui"
)

var exampleCmd = &cobra.Command{
    Use:   "example",
    Short: "Example command",
    Run: func(cmd *cobra.Command, args []string) {
        ui.Success("Example ran!")
    },
}

func init() {
    rootCmd.AddCommand(exampleCmd)
}
```

## Dependencies

Core:

- `github.com/spf13/cobra` - CLI framework
- `github.com/docker/docker` - Docker SDK
- `gopkg.in/yaml.v3` - YAML parsing
- `github.com/fatih/color` - Colored output

## Version

Version is defined in `internal/cmd/root.go`:

```go
const version = "0.2.0"
```

Update this when releasing.

## Legacy Files (To Remove)

These files are from the bash/Python implementation and should be removed:

- `bin/bosun` - Original bash script
- `manifest/manifest.py` - Python renderer
- `manifest/pyproject.toml` - Python dependencies

See `docs/migration.md` for cleanup instructions.

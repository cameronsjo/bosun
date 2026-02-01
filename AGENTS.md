<!-- OPENSPEC:START -->
# OpenSpec Instructions

These instructions are for AI assistants working in this project.

Always open `@/openspec/AGENTS.md` when the request:
- Mentions planning or proposals (words like proposal, spec, change, plan)
- Introduces new capabilities, breaking changes, architecture shifts, or big performance/security work
- Sounds ambiguous and you need the authoritative spec before coding

Use `@/openspec/AGENTS.md` to learn:
- How to create and apply change proposals
- Spec format and conventions
- Project structure and guidelines

Keep this managed block so 'openspec update' can refresh the instructions.

<!-- OPENSPEC:END -->

# Bosun - AI Context

@llms.txt

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

## Landing the Plane (Session Completion)

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   bd sync
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds

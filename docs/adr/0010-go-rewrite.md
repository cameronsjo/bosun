# ADR-0010: Rewrite Bosun CLI in Go

## Status

Accepted

## Context

Bosun is currently implemented as:
- **bin/bosun** - Bash script (~1400 lines) handling CLI, Docker operations, snapshots
- **manifest/manifest.py** - Python module (~330 lines) for YAML rendering
- **bosun/scripts/reconcile.sh** - Bash script (~300 lines) for GitOps deployment

This architecture has served well for rapid prototyping, but we're hitting limitations:

1. **Distribution complexity** - Requires Python, uv, and bash on target systems
2. **Testing difficulty** - Bash is hard to unit test; we rely on manual testing
3. **Argument parsing** - Manual, repetitive, easy to miss edge cases
4. **Data structures** - Limited to arrays; complex YAML manipulation is awkward
5. **Error handling** - `set -e` helps but proper error types would be cleaner
6. **Future features** - Filesystem watching, rolling updates, plugin system are painful in bash

The remaining roadmap items (local dev mode, rolling updates, plugin system) would be significantly easier in a compiled language with proper data structures and concurrency primitives.

## Decision

Rewrite bosun in **Go** as a single-binary CLI tool.

### Architecture

```
cmd/bosun/main.go           # Entry point
internal/
  cmd/                      # Cobra commands (yacht, crew, provision, etc.)
  manifest/                 # YAML rendering engine (port from Python)
  docker/                   # Docker SDK wrapper
  snapshot/                 # Rollback system
  reconcile/                # GitOps engine (port from bash)
  ui/                       # Colored console output
```

### Key Libraries

- `github.com/spf13/cobra` - CLI framework (used by kubectl, hugo, gh)
- `github.com/docker/docker` - Native Docker SDK
- `gopkg.in/yaml.v3` - YAML parsing
- `github.com/fatih/color` - Colored output

### Migration Strategy

1. Build Go version as `bosun-go` alongside existing bash
2. Verify output parity using golden file tests
3. Replace `bin/bosun` symlink once verified
4. Remove legacy Python/bash after monitoring period

## Consequences

### Pros

- **Single binary** - No runtime dependencies (Python, uv, bash)
- **Native Docker SDK** - Docker is written in Go; first-party SDK
- **Testable** - Unit tests, golden file tests from day 1
- **Type safety** - Catch errors at compile time
- **Concurrency** - Goroutines for watch mode, parallel health checks
- **Cross-compilation** - `GOOS=linux GOARCH=amd64 go build` produces Linux binary on Mac
- **Fast startup** - No interpreter overhead

### Cons

- **Rewrite effort** - ~2200 lines to port across ~8 phases
- **Learning curve** - Go idioms differ from bash/Python
- **Build step** - Must compile before running (vs editing bash directly)
- **Binary size** - Go binaries are larger than scripts (~10-20MB)

## Alternatives Considered

| Alternative | Why Not |
|-------------|---------|
| **Keep Bash** | Works now, but painful for P3+ features (watch, plugins) |
| **Python (typer+rich)** | Already a dependency, but still needs Python runtime |
| **Rust** | Steeper learning curve, overkill for this use case |
| **TypeScript/Deno** | Could work, but Go has better Docker ecosystem |

## References

- [Migration Plan](../../.claude/plans/keen-waddling-swan.md)
- [Cobra CLI Framework](https://cobra.dev/)
- [Docker SDK for Go](https://pkg.go.dev/github.com/docker/docker/client)

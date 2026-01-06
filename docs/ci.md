# CI Pipeline

Bosun uses [Dagger](https://dagger.io/) for CI/CD pipelines. This provides:

- **Local reproducibility**: Run the exact same pipeline locally and in CI
- **Content-addressed caching**: Faster builds with persistent caching
- **Portability**: Same pipeline works on GitHub Actions, GitLab CI, or any environment

## Quick Start

```bash
# Install Dagger (if not already installed)
curl -fsSL https://dl.dagger.io/dagger/install.sh | sh

# Run full CI pipeline
make ci

# Or use dagger directly
dagger call ci --source .
```

## Available Functions

| Function | Description | Command |
|----------|-------------|---------|
| `ci` | Full pipeline (test + lint + build) | `dagger call ci --source .` |
| `test` | Run tests with race detector and coverage | `dagger call test --source .` |
| `lint` | Run golangci-lint | `dagger call lint --source .` |
| `build` | Multi-platform binary builds | `dagger call build --source .` |
| `release` | Run GoReleaser for releases | `dagger call release --source . --github-token env:GITHUB_TOKEN` |
| `release-dry-run` | Test GoReleaser without publishing | `dagger call release-dry-run --source .` |
| `coverage` | Extract coverage file | `dagger call coverage --source . -o coverage.out` |

## Make Targets

For convenience, these Makefile targets wrap Dagger commands:

```bash
make ci                    # Full CI pipeline
make dagger-test           # Run tests in container
make dagger-lint           # Run linter in container
make dagger-build          # Build all platforms
make dagger-release-dry-run # Test goreleaser
```

## Pipeline Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        CI Pipeline                          │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ┌─────────┐    ┌─────────┐                                │
│  │  Test   │    │  Lint   │   (parallel execution)         │
│  └────┬────┘    └────┬────┘                                │
│       │              │                                      │
│       └──────┬───────┘                                      │
│              ▼                                              │
│         ┌────────┐                                          │
│         │ Build  │   (multi-platform: linux/darwin x86/arm)│
│         └────┬───┘                                          │
│              │                                              │
│              ▼                                              │
│         ┌────────┐                                          │
│         │Artifacts│  (4 binaries)                          │
│         └────────┘                                          │
└─────────────────────────────────────────────────────────────┘
```

## Caching

Dagger uses content-addressed caching for:

- **Go modules**: Cached at `/go/pkg/mod`
- **Go build cache**: Cached at `/root/.cache/go-build`
- **Lint cache**: Cached at `/root/.cache/golangci-lint`

Caches persist across runs, significantly speeding up subsequent builds.

## Build Outputs

The `build` function creates binaries for all target platforms:

| Platform | Binary |
|----------|--------|
| Linux x86_64 | `bosun-linux-amd64` |
| Linux ARM64 | `bosun-linux-arm64` |
| macOS x86_64 | `bosun-darwin-amd64` |
| macOS ARM64 | `bosun-darwin-arm64` |

## GitHub Actions Integration

The CI workflow (`.github/workflows/ci.yml`) uses Dagger:

```yaml
jobs:
  ci:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: dagger/dagger-for-github@v8.2.0
        with:
          version: "latest"
          call: ci --source .
```

## Release Workflow

Releases are triggered by Release Please and use GoReleaser via Dagger:

1. Release Please creates a release PR
2. On merge, GoReleaser runs via `dagger call release`
3. Binaries, checksums, and container images are published
4. SLSA provenance attestations are generated

## Debugging

### Interactive Shell

Drop into a container at any stage:

```bash
dagger call test --source . terminal
```

### Verbose Output

See detailed execution logs:

```bash
dagger call ci --source . --debug
```

### Check Dagger Version

```bash
dagger version
```

## Local Development vs CI

| Aspect | Local (`make ci`) | CI (GitHub Actions) |
|--------|-------------------|---------------------|
| Cache location | Docker volumes | GitHub Actions cache |
| Execution | Docker daemon | GitHub-hosted runner |
| Output | Terminal | Workflow logs |
| Secrets | Environment variables | GitHub Secrets |

Both environments run identical Dagger functions, ensuring consistent results.

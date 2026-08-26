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
| `all` | Full pipeline (Go + WebUI) | `dagger call all --source .` |
| `ci` | Go pipeline (test + lint + build) | `dagger call ci --source .` |
| `test` | Run Go tests with race detector and coverage | `dagger call test --source .` |
| `lint` | Run golangci-lint | `dagger call lint --source .` |
| `build` | Multi-platform binary builds | `dagger call build --source .` |
| `web-ui` | WebUI pipeline (install + typecheck + lint + bundle) | `dagger call web-ui --source .` |
| `web-ui-build` | Get WebUI dist directory | `dagger call web-ui-build --source .` |
| `release` | Run GoReleaser for releases | `dagger call release --source . --github-token env:GITHUB_TOKEN` |
| `release-dry-run` | Test GoReleaser without publishing | `dagger call release-dry-run --source .` |
| `coverage` | Extract coverage file | `dagger call coverage --source . -o coverage.out` |

## Make Targets

For convenience, these Makefile targets wrap Dagger commands:

```bash
make all                   # Full CI pipeline (Go + WebUI)
make ci                    # Go CI pipeline
make dagger-test           # Run Go tests in container
make dagger-lint           # Run Go linter in container
make dagger-build          # Build all platforms
make dagger-webui          # Build WebUI in container
make dagger-release-dry-run # Test goreleaser
```

## Pipeline Architecture

```
CI-All Pipeline
├── Go Pipeline
│   ├── Test
│   ├── Lint
│   └── Build → four platform binaries
└── WebUI Pipeline
    └── npm ci
        └── npm run typecheck
            └── npm run lint
                └── npm run build:bundle → dist/
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
2. A GitHub App installation token queues that PR for auto-merge
3. On merge, GoReleaser runs via `dagger call release`
4. Binaries, checksums, and container images are published
5. SLSA provenance attestations are generated

Auto-merge is fail-closed: it runs only when `RELEASE_APP_ID` and
`RELEASE_APP_PRIVATE_KEY` produce a valid installation token. Missing or
invalid credentials emit a warning and leave the generated release PR open for
manual merge; the workflow never falls back to `GITHUB_TOKEN`, whose merge
would not trigger the follow-up release workflow.

To rotate an invalid key without exposing it in shell history or logs, download
a new private key from the Release App settings, validate the PEM locally, and
pipe it to GitHub CLI:

```bash
openssl pkey -in /path/to/release-app.private-key.pem -check -noout
gh secret set RELEASE_APP_PRIVATE_KEY < /path/to/release-app.private-key.pem
```

Delete the downloaded PEM after a subsequent Release Please run shows
`Generate App Token` succeeded.

If a release already exists but its publishing job did not finish, recover its
assets through the same GoReleaser, container, signing, and provenance job by
dispatching the workflow with that existing tag:

```bash
gh workflow run release-please.yml -f tag=v0.40.6
```

The dispatch rejects tags without a `v`-prefixed semantic version and tags that
do not already have both a Git ref and GitHub Release. It does not run Release
Please or recreate either object. Inspect the run before treating recovery as
complete; do not delete and recreate the tag or release.

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

## Context

Bosun uses GitHub Actions for CI/CD with two workflows:
1. `ci.yml` - Test, lint, and build on PRs/pushes
2. `release-please.yml` - Automated releases with GoReleaser

Current pain points:
- Cannot reproduce CI failures locally without mimicking GitHub's environment
- Build matrix creates 4 separate jobs (linux/darwin × amd64/arm64) with duplicated setup
- No caching between workflow runs beyond Go module cache

Dagger provides a container-based pipeline runtime that solves these issues by running the same code locally and in CI.

## Goals / Non-Goals

**Goals:**
- Run identical CI pipeline locally with `dagger call` or `make ci`
- Leverage Dagger's content-addressed caching for faster builds
- Maintain existing functionality: test, lint, multi-platform build
- Keep goreleaser for release artifact creation
- Preserve Cosign signing and SLSA provenance

**Non-Goals:**
- Replace Release Please (GitHub-specific, works well)
- Replace Codecov integration (keep existing action)
- Support non-GitHub CI systems (future benefit, not current goal)
- Container image building (goreleaser handles this)

## Decisions

### Decision: Use Go SDK for Dagger module

**Rationale:** Bosun is a Go project. Using the Go SDK keeps the toolchain unified and allows leveraging existing Go tooling (golangci-lint, go test) as library calls rather than shell commands.

**Alternatives considered:**
- TypeScript SDK: Better for polyglot teams, but adds Node.js dependency
- Python SDK: Popular but adds Python dependency
- CLI-only (Daggerfile): Less type safety, harder to debug

### Decision: Keep GoReleaser for releases

**Rationale:** GoReleaser is battle-tested for Go releases with excellent GitHub integration (release notes, checksums, Homebrew taps). Dagger can orchestrate GoReleaser rather than replace it.

**Alternatives considered:**
- Pure Dagger builds: Would need to reimplement changelog, signing, GHCR push
- Ko: Good for containers but less feature-rich for binary releases

### Decision: Dagger module in `dagger/` directory

**Rationale:** Following Dagger conventions, the module lives at project root in a `dagger/` subdirectory. This keeps CI code separate from application code while remaining discoverable.

**Alternatives considered:**
- `ci/` directory: Conflicts with common convention for CI configs
- `.dagger/` hidden directory: Less discoverable, harder to navigate

### Decision: Phased migration with parallel running

**Rationale:** Run Dagger alongside existing Actions initially to validate behavior matches. Only remove Actions steps after confirming Dagger produces identical results.

## Dagger Module Structure

```
dagger/
├── dagger.json          # Module configuration
├── main.go              # Dagger functions
└── go.mod               # Go module for SDK dependencies
```

### Proposed Functions

```go
// Test runs go test with race detector and coverage
func (m *Bosun) Test(ctx context.Context, source *dagger.Directory) *dagger.Container

// Lint runs golangci-lint
func (m *Bosun) Lint(ctx context.Context, source *dagger.Directory) *dagger.Container

// Build creates binaries for all target platforms
func (m *Bosun) Build(ctx context.Context, source *dagger.Directory) *dagger.Directory

// CI runs the full CI pipeline (test + lint + build)
func (m *Bosun) CI(ctx context.Context, source *dagger.Directory) *dagger.Directory

// Release runs goreleaser (called from release workflow)
func (m *Bosun) Release(ctx context.Context, source *dagger.Directory, githubToken *dagger.Secret) error
```

## Workflow Changes

### ci.yml (After)

```yaml
jobs:
  ci:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: dagger/dagger-action@v5
      - run: dagger call ci --source .
```

### release-please.yml (After)

```yaml
goreleaser:
  steps:
    - uses: actions/checkout@v4
    - uses: dagger/dagger-action@v5
    - run: dagger call release --source . --github-token env:GITHUB_TOKEN
```

## Risks / Trade-offs

| Risk | Impact | Mitigation |
|------|--------|------------|
| Dagger version drift | Pipeline may break on upgrades | Pin Dagger version in workflow |
| Learning curve | Team unfamiliar with Dagger | Provide `make ci` for familiar interface |
| Debug complexity | Container layers add indirection | Use `dagger shell` for interactive debugging |
| Cache invalidation | Unexpected rebuilds | Document caching behavior in README |

## Migration Plan

1. Add Dagger module with test/lint/build functions
2. Add `make ci` target that calls `dagger call ci`
3. Update `ci.yml` to use Dagger (parallel with existing jobs initially)
4. Validate outputs match between Dagger and Actions
5. Remove redundant Actions steps
6. Update `release-please.yml` goreleaser job
7. Update documentation

## Open Questions

1. Should we expose individual functions (`dagger call test`) or only the combined `ci` function?
   - **Recommendation:** Expose both for flexibility

2. Should coverage upload remain in Actions or move to Dagger?
   - **Recommendation:** Keep in Actions (Codecov action handles auth)

3. Should we add Dagger Cloud for distributed caching?
   - **Recommendation:** Not initially, evaluate after adoption

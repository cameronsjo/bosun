# Change: Convert GitHub Actions to Dagger Pipelines

## Why

GitHub Actions workflows are tightly coupled to GitHub's infrastructure, making local testing difficult and creating vendor lock-in. Dagger provides pipelines-as-code that run identically on developer machines and in CI, improving debuggability and portability.

## What Changes

- Add Dagger Go SDK module for CI/CD pipelines
- Replace `ci.yml` workflow jobs (test, lint, build) with Dagger functions
- Update `release-please.yml` goreleaser job to use Dagger for build/publish
- Retain Release Please action for version management (it's GitHub-specific by design)
- Add `make ci` target to run the full CI pipeline locally

## Impact

- Affected specs: `ci` (new capability)
- Affected code:
  - `.github/workflows/ci.yml` - Simplified to call Dagger
  - `.github/workflows/release-please.yml` - Goreleaser job updated
  - `dagger/` - New Dagger module with Go SDK
  - `Makefile` - New `ci` target
- Dependencies: Dagger CLI installed in CI runners

## Benefits

1. **Local reproducibility**: Run `dagger call test` or `make ci` to execute the exact same pipeline locally
2. **Caching**: Dagger's content-addressed caching persists across runs
3. **Debuggability**: Step into any pipeline stage with `dagger shell`
4. **Portability**: Same pipeline works on GitHub Actions, GitLab CI, Jenkins, or bare metal
5. **Type safety**: Go SDK provides compile-time checking of pipeline logic

## Migration Strategy

Phased approach to minimize risk:

1. **Phase 1**: Add Dagger module alongside existing workflows
2. **Phase 2**: Update CI workflow to call Dagger functions
3. **Phase 3**: Update release workflow to use Dagger for builds
4. **Phase 4**: Remove redundant GitHub Actions steps

## Out of Scope

- Release Please action migration (GitHub-specific, no Dagger equivalent)
- Codecov integration (keep existing action for now)
- SLSA provenance attestation (keep existing action)

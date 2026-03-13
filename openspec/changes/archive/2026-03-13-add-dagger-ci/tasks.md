## 1. Dagger Module Setup

- [x] 1.1 Initialize Dagger module with `dagger init --sdk=go --name=bosun --source=./dagger`
- [x] 1.2 Configure `dagger.json` with appropriate settings
- [x] 1.3 Add `dagger/` to `.gitignore` for generated files (if any)

## 2. Implement Core Functions

- [x] 2.1 Implement `Base()` helper function for Go container setup (Go version from go.mod, module caching)
- [x] 2.2 Implement `Test()` function with race detection and coverage output
- [x] 2.3 Implement `Lint()` function using golangci-lint container
- [x] 2.4 Implement `Build()` function for multi-platform binaries with ldflags

## 3. Implement Composite Functions

- [x] 3.1 Implement `CI()` function that orchestrates test, lint, and build
- [x] 3.2 Implement `Release()` function that runs GoReleaser with GitHub token

## 4. Local Development Integration

- [x] 4.1 Add `make ci` target to Makefile
- [x] 4.2 Add `make dagger-test`, `make dagger-lint`, `make dagger-build` targets for individual stages
- [ ] 4.3 Verify local execution with `dagger call ci --source .`

## 5. Update GitHub Actions

- [x] 5.1 Update `ci.yml` to install Dagger and call CI function
- [x] 5.2 Keep Codecov upload step (runs after Dagger)
- [x] 5.3 Update `release-please.yml` goreleaser job to use Dagger release function
- [x] 5.4 Pin Dagger action version for reproducibility

## 6. Validation

- [ ] 6.1 Run Dagger CI locally and verify all stages pass
- [ ] 6.2 Push to branch and verify GitHub Actions workflow succeeds
- [ ] 6.3 Compare build artifacts between Dagger and previous Actions builds
- [ ] 6.4 Verify caching behavior on subsequent runs

## 7. Documentation

- [x] 7.1 Update README.md with Dagger usage instructions
- [x] 7.2 Add `docs/ci.md` explaining the CI pipeline architecture
- [x] 7.3 Document `make ci` and individual stage commands in Makefile help

## Dependencies

- Task 2.x depends on 1.x (module must be initialized first)
- Task 3.x depends on 2.x (composite functions use core functions)
- Task 5.x depends on 3.x (workflows call implemented functions)
- Task 6.x depends on 4.x and 5.x (validation requires both local and CI setup)

## Parallelizable Work

- Tasks 2.2, 2.3, 2.4 can run in parallel after 2.1
- Tasks 4.1, 4.2, 4.3 can run in parallel
- Tasks 5.1, 5.3 can run in parallel after 3.x
- Tasks 7.1, 7.2, 7.3 can run in parallel after 6.x

## Notes

- Task 4.3 and 6.x require Dagger CLI installed locally to verify
- Task 6.2-6.4 require pushing to GitHub and observing CI runs

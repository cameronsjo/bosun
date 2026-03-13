## 1. Config Layer

- [ ] 1.1 Add `DeployPaths []string` to `configFile` YAML DTO
- [ ] 1.2 Add `deployPaths []string` private field to `Config` struct
- [ ] 1.3 Add `DeployPaths()` getter method on `Config`
- [ ] 1.4 Add `extractDeployPaths()` helper
- [ ] 1.5 Wire into `Load()` and `LoadFrom()`
- [ ] 1.6 Write config parsing tests (parses from YAML, empty/missing returns nil)

## 2. Reconcile Config

- [ ] 2.1 Add `DeployPaths []string` to reconcile `Config` struct
- [ ] 2.2 Add `DeployPathsFromEnv bool` to reconcile `Config` struct
- [ ] 2.3 Add `DeployPaths []string` to `ReloadedConfig` struct
- [ ] 2.4 Update `reloadProjectConfig()` to apply DeployPaths (skip if from env)
- [ ] 2.5 Write reloadProjectConfig tests for deploy_paths

## 3. Path Matching

- [ ] 3.1 Add `MatchAnyPath(files, patterns []string) bool` to hooks.go (reuses `matchGlob`)
- [ ] 3.2 Write table-driven tests for MatchAnyPath

## 4. Pipeline Skip Logic

- [ ] 4.1 Insert path-aware skip in `Run()` between state-based skip and circuit breaker
- [ ] 4.2 On skip: record commit as deployed in state file, return nil
- [ ] 4.3 DiffFiles failure falls through to full deploy with warning
- [ ] 4.4 `--force` bypasses path check
- [ ] 4.5 Write pipeline tests: skip, match, diff-fails, force-override

## 5. Daemon + CLI Wiring

- [ ] 5.1 Wire `DeployPaths` in `ConfigFromEnv()` (load from project config)
- [ ] 5.2 Add `BOSUN_DEPLOY_PATHS` env var override (JSON array) + set `DeployPathsFromEnv`
- [ ] 5.3 Update ConfigReloader closure in daemon to include DeployPaths
- [ ] 5.4 Wire `DeployPaths` in `runReconcile()` (load from project config)
- [ ] 5.5 Add `BOSUN_DEPLOY_PATHS` env var override in CLI
- [ ] 5.6 Update ConfigReloader closure in CLI to include DeployPaths

## 6. Documentation

- [ ] 6.1 Add `BOSUN_DEPLOY_PATHS` to AGENTS.md env var table
- [ ] 6.2 Update `skills/onboard/resources/configuration.md` with `deploy_paths` field
- [ ] 6.3 Update `skills/onboard/resources/gitops.md` with path-aware skip behavior

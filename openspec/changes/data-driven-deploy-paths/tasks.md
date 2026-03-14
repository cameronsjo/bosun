## 1. Discovery and Config

- [ ] 1.1 Add `deploy_sync_paths` and `deploy_sync_exclude` fields to `configFile` struct in `internal/config/config.go`
- [ ] 1.2 Add `extractDeploySyncPaths()` and `extractDeploySyncExclude()` extractors
- [ ] 1.3 Add `DeploySyncPaths()` and `DeploySyncExclude()` getters to `Config`
- [ ] 1.4 Wire extractors into `loadConfigFile()` and `loadConfigDir()`
- [ ] 1.5 Parse `BOSUN_DEPLOY_SYNC_PATHS` and `BOSUN_DEPLOY_SYNC_EXCLUDE` env vars in `daemon.go:ConfigFromEnv()`
- [ ] 1.6 Add `DeploySyncPaths` and `DeploySyncExclude` fields to reconcile `Config` struct
- [ ] 1.7 Wire config fields from daemon config to reconcile config

## 2. Deploy Path Discovery

- [ ] 2.1 Implement `discoverDeployTargets()` that scans staging directory and returns a list of deploy target descriptors (path, type: dir or file)
- [ ] 2.2 Apply allowlist filtering when `DeploySyncPaths` is non-empty
- [ ] 2.3 Apply blocklist filtering when `DeploySyncExclude` is non-empty
- [ ] 2.4 Identify `compose/` directory for special handling (collect compose files for `docker compose up`)

## 3. Refactor Deploy Functions

- [ ] 3.1 Replace hardcoded sync calls in `deployLocal()` with discovery-driven loop
- [ ] 3.2 Replace hardcoded sync calls in `deployRemote()` with discovery-driven loop
- [ ] 3.3 Replace hardcoded paths in `createBackup()` with targets derived from discovery
- [ ] 3.4 Remove hardcoded `"unraid"` staging subdirectory assumption

## 4. Tests

- [ ] 4.1 Unit tests for `discoverDeployTargets()` with various staging layouts
- [ ] 4.2 Unit tests for allowlist/blocklist filtering
- [ ] 4.3 Config tests for `deploy_sync_paths` and `deploy_sync_exclude` parsing
- [ ] 4.4 Integration test: end-to-end local deploy with discovered paths
- [ ] 4.5 Integration test: verify compose directory special handling

## 5. Documentation

- [ ] 5.1 Update `skills/onboard/resources/configuration.md` with new config fields
- [ ] 5.2 Update `skills/onboard/resources/gitops.md` with deploy path discovery behavior
- [ ] 5.3 Update `AGENTS.md` env var table with new env vars
- [ ] 5.4 Update `skills/onboard/resources/commands.md` with deploy-sync env/config usage examples
- [ ] 5.5 Update `skills/onboard/resources/manifests.md` to reflect staging/discovery path conventions

# Change: Make deploy paths data-driven instead of hardcoded

## Why

`deployLocal()` and `deployRemote()` in `internal/reconcile/reconcile.go` have 6 hardcoded sync targets (traefik, authelia, agentgateway, gatus, tailscale-gateway, compose). Adding a new service requires a code change to Bosun. The manifest/provision system is generic but the deploy step collapses to an explicit list, violating the "batteries included, but swappable" principle.

## What Changes

- **Auto-discovery of deploy targets** -- after template rendering, the reconciler scans the staging directory for subdirectories and files to sync, rather than relying on a hardcoded list
- **Optional allowlist/blocklist config** -- `deploy_sync_paths` in `bosun.yaml` provides an allowlist of relative paths to sync (when set, only listed paths are synced). `deploy_sync_exclude` provides a blocklist of glob patterns to skip
- **Env var overrides** -- `BOSUN_DEPLOY_SYNC_PATHS` (JSON array) and `BOSUN_DEPLOY_SYNC_EXCLUDE` (JSON array) override their config file counterparts
- **Backup paths derived from deploy targets** -- `createBackup()` derives backup paths from the same discovered targets instead of its own hardcoded list
- **Compose directory convention** -- the `compose/` subdirectory retains special handling (compose files are collected for `docker compose up` after sync), but is discovered rather than hardcoded
- **Hardcoded `"unraid"` subdirectory removed** -- the staging subdirectory name becomes configurable via `InfraSubDir` (already exists) or derived from the repo structure

## Impact

- Affected specs: reconcile (modified)
- Affected code:
  - `internal/reconcile/reconcile.go` -- replace hardcoded paths in `deployLocal()`, `deployRemote()`, and `createBackup()` with discovery loop
  - `internal/reconcile/deploy.go` -- no changes to `DeployOps` interface; existing `DeployLocal`/`DeployRemote`/`DeployLocalFile`/`DeployRemoteFile` methods are reused
  - `internal/config/config.go` -- add `deploy_sync_paths` and `deploy_sync_exclude` fields, extractors, and getters
  - `internal/daemon/daemon.go` -- parse `BOSUN_DEPLOY_SYNC_PATHS` and `BOSUN_DEPLOY_SYNC_EXCLUDE` env vars
- All consumers:
  - `internal/reconcile/reconcile.go:deployLocal()` -- primary: hardcoded sync list
  - `internal/reconcile/reconcile.go:deployRemote()` -- primary: hardcoded sync list
  - `internal/reconcile/reconcile.go:createBackup()` -- derives backup paths from same hardcoded list
  - `internal/daemon/daemon.go:ConfigFromEnv()` -- env var parsing for new config fields
  - `internal/config/config.go:loadConfigFile()` -- YAML config parsing
  - `internal/config/config.go:loadConfigDir()` -- directory-based config parsing

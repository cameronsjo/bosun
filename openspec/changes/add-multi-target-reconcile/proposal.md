# Change: Add multi-target reconciliation to support deploying to multiple servers

## Why

Bosun's design principle is "one yacht, many ports" — a single monorepo can describe infrastructure for multiple servers. Today, the reconciler is structurally limited to one target per instance: `Config` has one `TargetHost`, one `StateFile`, one `StagingDir`, one `ProjectName`. Running multi-server deployments requires N independent daemon processes with no coordination, no shared state visibility, and no unified alerting. This defeats the monorepo advantage and makes operational visibility fragmented.

## What Changes

- **Target descriptors** — introduce a `Target` struct that captures per-target configuration: host, appdata path, project name, state file, staging directory, secrets scope, critical containers, post-sync hooks, and deploy sync filters
- **Config targets list** — `reconcile.Config` gains a `Targets []Target` field; the existing flat fields become the "default target" for backwards compatibility (single-target configs still work unchanged)
- **Per-target staging directories** — each target gets an isolated staging directory for execution isolation and future parallelization
- **Per-target state files** — each target tracks its own deploy state (last commit, attempt count, drift) independently
- **Namespaced secrets** — secrets can be scoped per-target via a naming convention (e.g., `targets.<name>.db_password`) alongside shared secrets accessible to all targets
- **Sequential target reconciliation** — the daemon iterates targets in order, running the full pipeline for each; a failure on one target does not block others
- **Per-target alerting context** — alerts include the target name so operators know which server is affected
- **Config file schema** — `bosun.yaml` gains an optional `targets:` section; when absent, behavior is identical to today
- **Env var mapping** — `BOSUN_TARGETS` (JSON) defines targets; individual `BOSUN_TARGET_<NAME>_*` vars override per-target fields
- **Target validation and safety** (folded from the Cluster C bug hunt) — target descriptors are validated at config-load for path traversal, colliding state/lock/staging paths, colliding Docker namespaces, case-insensitive reserved `default`, and YAML/env field parity; per-target configs deep-copy all slice/map fields on derivation and hot-reload; empty-field inheritance is explicit so a dev target cannot silently inherit a production host (GHSA-jxw8, GHSA-57r2, #228, #260, #270, #271, #272, #273, #287)

## Impact

- Affected specs: `reconcile` (modified — pipeline orchestration, state persistence, locking, deployment, drift), `alerting` (modified — per-target context in alerts)
- Affected code:
  - `internal/reconcile/reconcile.go` — `Config`, `DefaultConfig()`, pipeline orchestration, state load/save, deploy, drift
  - `internal/reconcile/deploy.go` — `DeployOps` gains target context
  - `internal/reconcile/state.go` — state file paths become per-target
  - `internal/daemon/daemon.go` — `ConfigFromEnv()`, daemon loop iterates targets
  - `internal/config/config.go` — `targets:` YAML section, extractors, getters
  - `internal/alert/manager.go` — alert messages include target name
  - `internal/cmd/reconcile.go` — CLI `--target` flag to reconcile a single target
  - `internal/cmd/drift.go` — CLI `--target` flag to check drift for a specific target
  - `internal/cmd/status.go` — status shows per-target state
- All consumers:
  - `internal/reconcile/reconcile.go:35` — `Config` struct definition (all singular target fields)
  - `internal/reconcile/reconcile.go:184` — `DefaultConfig()` (singular defaults)
  - `internal/reconcile/reconcile.go:251` — `New()` creates one `DeployOps` with one `ProjectName`
  - `internal/reconcile/reconcile.go:359-486` — state load/save uses `r.config.StateFile`
  - `internal/reconcile/reconcile.go:583-738` — deploy/drift uses `r.config.TargetHost`, `r.config.ProjectName`
  - `internal/daemon/daemon.go` — `ConfigFromEnv()` populates singular fields, daemon holds one `Reconciler`
  - `internal/config/config.go` — `Config.TargetHost()`, `Config.ProjectName()`, etc. (singular getters)
  - `internal/alert/manager.go` — `SendDeploySuccess()`, `SendDeployFailure()` don't include target context
  - `internal/cmd/reconcile.go` — `runReconcile()` builds one `reconcile.Config`
  - `internal/cmd/drift.go` — `runDrift()` reads one state file
  - `internal/cmd/status.go` — `runStatus()` reads one state file

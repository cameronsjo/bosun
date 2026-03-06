# Change: Handle slow image pulls gracefully

## Why

When `docker compose up` encounters a first-time image pull from a slow registry (e.g., GHCR), the entire deploy can fail due to the single `ComposeUpTimeout` (10m) covering both the pull and the container startup. Bosun cannot distinguish "compose failed because image doesn't exist" from "compose failed because pull is slow." Separating `docker compose pull` from `docker compose up` with independent timeouts provides better observability, actionable error messages, and resilience for large or multi-image deployments.

## What Changes

- Add a new **Image Pre-Pull** pipeline stage between "Extract declared state" and "Create backup"
- Run `docker compose pull` as an explicit step before `docker compose up`
- Introduce per-phase timeouts (`BOSUN_IMAGE_PULL_TIMEOUT`, `BOSUN_COMPOSE_UP_TIMEOUT`) to replace the single `ComposeUpTimeout`
- Log pull progress with structured fields (image count, duration) so operators can see what's slow
- Emit a clear, actionable error when pull fails (distinguishing auth errors, missing images, and timeouts)
- Skip pre-pull in dry-run mode (consistent with compose up behavior)

## Impact

- Affected specs: `reconcile`
- Affected code:
  - `internal/reconcile/deploy.go` — new `ComposePull` / `ComposePullMultiple` methods, timeout constants
  - `internal/reconcile/reconcile.go` — new pipeline stage call between extract-declared-state and backup
  - `internal/reconcile/interfaces.go` — no change (pre-pull is an internal DeployOps concern, not exposed via Deployer interface)
  - `internal/daemon/daemon.go` — parse new env vars (`BOSUN_IMAGE_PULL_TIMEOUT`, `BOSUN_COMPOSE_UP_TIMEOUT`)
  - `internal/cmd/reconcile.go` — no change (uses Reconciler.Run which gains the stage automatically)
- All consumers of `ComposeUpTimeout`:
  - `internal/reconcile/deploy.go:36` — constant definition
  - `internal/reconcile/deploy.go:866` — applied in `ComposeUpMultiple`
  - `internal/reconcile/deploy.go:966` — rollback timeout
  - `docs/error-handling.md:261` — documentation table
  - `docs/workflows.md:474` — documentation table
- All consumers of `docker compose up` invocations:
  - `internal/reconcile/reconcile.go:1004` — local pipeline (`ComposeUpMultipleWithRollback`)
  - `internal/reconcile/reconcile.go:1088` — remote pipeline (`ComposeUpRemote`)
  - `internal/cmd/yacht.go` — interactive CLI (not affected, no pre-pull needed)
  - `internal/cmd/emergency.go:730` — emergency restore (not affected)

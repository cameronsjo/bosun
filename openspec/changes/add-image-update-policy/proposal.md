# Change: Per-service image update policy

## Why

Two update mechanisms (Watchtower and Bosun) don't know about each other. Watchtower pulls `:latest` tags on a schedule, which can restart containers between Bosun reconciliations, creating drift that Bosun detects but didn't cause. Bosun needs per-service image update policies so operators can declare which services should auto-pull mutable tags (replacing Watchtower) and which should stay pinned to exactly what Git declares.

## What Changes

- Add `image_policies` map to `bosun.yaml` with per-service `pinned` or `auto` policy (default: `pinned`)
- Add `BOSUN_IMAGE_POLICY` env var for a global default override
- Before compose up, run `docker compose pull <services...>` for services tagged `auto`
- The `add-image-prepull` change handles the blanket pre-pull mechanism; this change adds the per-service filtering layer on top
- Extend `ReloadedConfig` to carry image policies so the repo's `bosun.yaml` is re-read after each git pull
- Record per-service policy in deploy state for drift reporting context

## Impact

- Affected specs: `reconcile`
- Affected code:
  - `internal/config/config.go` -- new `image_policies` field in `configFile`, `extractImagePolicies()`, `ImagePolicies()` getter
  - `internal/reconcile/reconcile.go` -- `ReloadedConfig` gains `ImagePolicies` field; pipeline passes policies to pre-pull stage
  - `internal/reconcile/deploy.go` -- `ComposePullMultiple` (from `add-image-prepull`) gains optional service filter; new `ComposePullServices` variant
  - `internal/daemon/daemon.go` -- parse `BOSUN_IMAGE_POLICY` env var
  - `internal/reconcile/drift.go` -- `DeclaredService` gains `ImagePolicy` field for context in drift reports
- All consumers of `ReloadedConfig`:
  - `internal/reconcile/reconcile.go:18` -- struct definition
  - `internal/reconcile/reconcile.go:30` -- `ConfigReloaderFunc` signature
  - `internal/config/config.go:309` -- `LoadFrom` populates reloaded fields
  - `internal/daemon/daemon.go` -- daemon builds reconcile config from env + reloaded config
- All consumers of `DeclaredService`:
  - `internal/reconcile/drift.go:70` -- `ExtractDeclaredState`
  - `internal/reconcile/drift.go:113` -- `extractServicesFromCompose`
  - `internal/reconcile/reconcile.go:439` -- pipeline stores declared services
  - `internal/reconcile/reconcile.go:505` -- post-deploy verification
  - `internal/reconcile/state.go` -- deploy state persistence

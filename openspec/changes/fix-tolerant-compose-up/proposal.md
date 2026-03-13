# Change: Fix tolerant compose up implementation gap

## Why

The reconcile spec already requires tolerant compose up behavior (unhealthy containers
produce warnings, not deployment failures), but the implementation treats ANY non-zero
exit from `docker compose up` as a hard failure that triggers rollback. This created a
real-world outage: the `obsidian` container had 21,553 consecutive healthcheck failures
over 7.5 days, blocking ALL deployments with no alert sent.

Docker Compose v2 can exit non-zero without `--wait` in two scenarios:
1. A container genuinely fails to start (bad image, port conflict, missing volume)
2. A container with `depends_on: condition: service_healthy` blocks because its
   dependency is unhealthy -- even though the unhealthy container is still running

The current code (`ComposeUpMultiple`) does not distinguish between these cases. Both
trigger rollback and mark the deployment as failed.

## What Changes

- **MODIFIED**: Service Orchestration requirement gains a new scenario for compose up
  exiting non-zero due to unhealthy dependencies, where the reconciler SHALL inspect
  container state to determine if the exit was caused by unhealthy containers vs genuine
  start failures
- **MODIFIED**: Tolerant Compose Up requirement adds a scenario for the unhealthy
  dependency edge case and specifies that compose up exit code 1 SHALL trigger container
  state inspection rather than immediate rollback
- Adds a new "Compose Exit Classification" requirement that defines how to inspect
  Docker container state after a compose up failure to classify the failure

## Impact

- Affected specs: `reconcile/spec.md` (Service Orchestration, Tolerant Compose Up)
- Affected code: `internal/reconcile/deploy.go` (`ComposeUpMultiple`, `ComposeUpMultipleWithRollback`),
  `internal/reconcile/reconcile.go` (`deployLocal`), `internal/reconcile/drift.go` (`CollectActualState`)
- All consumers:
  - `internal/reconcile/deploy.go:842` — `ComposeUpMultiple` (builds and runs the compose command)
  - `internal/reconcile/deploy.go:923` — `ComposeUpMultipleWithRollback` (calls ComposeUpMultiple, handles rollback)
  - `internal/reconcile/deploy.go:1019` — `ComposeUpRemote` (remote compose up via SSH)
  - `internal/reconcile/reconcile.go:1004` — `deployLocal` (calls ComposeUpMultipleWithRollback)
  - `internal/reconcile/reconcile.go:1088` — `deployRemote` (calls ComposeUpRemote)
  - `internal/reconcile/reconcile.go:561` — `verifyPostDeploy` (post-deploy health inspection)

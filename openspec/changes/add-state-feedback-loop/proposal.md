# Change: Add declared-vs-actual state feedback loop

## Why

Bosun's reconciliation pipeline is currently fire-and-forget: it deploys a commit and records success, but never verifies that the declared services are actually running, healthy, and matching their manifest definitions. If a container crashes after deployment, gets manually stopped, or fails its health check, bosun has no visibility into the drift between declared and actual state. This is the missing closed-loop in bosun's GitOps model.

## What Changes

- Extract declared state (expected services, images, health expectations) from rendered manifests after provisioning
- Collect actual state from Docker (running containers, images, health status) after deployment
- Compare declared vs actual to produce a drift report
- Verify post-deploy state: after `docker compose up`, confirm all declared services are running and healthy
- Surface drift through daemon status API (`/api/drift`), CLI (`bosun drift`), structured logging, and alerts
- Add periodic drift checks in the daemon poll loop (not just after deploys)
- Extend `DeployState` to record declared service count and drift status

## Impact

- Affected specs: reconcile (state tracking, pipeline stages)
- Affected code: `internal/reconcile/` (state, pipeline), `internal/daemon/` (API, polling), `internal/cmd/` (new drift command), `internal/docker/` (state collection), `internal/manifest/` (declared state extraction)

# Change: Health gate scope (critical | declared | off)

## Why

A health-gate failure only triggers rollback when `critical_containers` is configured and a listed container is unhealthy. An operator with no critical-containers list gets no auto-rollback protection at all: a declared-but-non-critical service coming up unhealthy is a no-op for rollback, because the per-file isolated compose-up path treats an unhealthy container as a successful start. The health gate needs an opt-in mode that gates on all declared services, so any deploy that makes a declared service unhealthy rolls back.

## What Changes

- Add `health_gate_scope` to `bosun.yaml` with three values — `critical` (default), `declared`, `off` — and a `BOSUN_HEALTH_GATE_SCOPE` environment override.
- **critical** (default): unchanged. Gate only on `critical_containers` members; an empty list skips the gate. A declared-but-non-critical service coming up unhealthy does NOT trigger rollback.
- **declared**: gate on ALL declared services, exempting any service that was already unhealthy before this deploy (a pre-existing casualty). Only a service this deploy made unhealthy triggers the existing rollback branch.
- **off**: no health gate.
- An unknown config-file scope falls back to `critical` at gate time with an
  error naming the valid set rather than failing the deploy. An invalid
  `BOSUN_HEALTH_GATE_SCOPE` override is ignored with a warning, leaving the
  config-file scope or default in effect.

## Impact

- Affected specs: `reconcile`
- Affected code:
  - `internal/reconcile/reconcile.go` — `Config.HealthGateScope`, `ResolveHealthGateScope`, three-way `runHealthGate`, `checkDeclaredHealth`
  - `internal/config/config.go` — `health_gate_scope` DTO field, `extractHealthGateScope`, `HealthGateScope()` getter, build wiring
  - `internal/daemon/daemon.go` — `BOSUN_HEALTH_GATE_SCOPE` env parse (validated via `ResolveHealthGateScope`), config-file wiring
- Dead code removed: `ComposeUpWithRollback` / `ComposeUpMultipleWithRollback` (zero non-test callers; the rollback sentinels they referenced are now produced by the remote rollback path)
- Alerting by scope: critical sends only the existing throttled failure alert (byte-for-byte, no rollback alert); declared additionally sends a throttled rollback alert on the same attempt window, so a flapping healthcheck alerts on the established 1/3/10/30 cadence rather than once per cycle.

## 1. Configuration Surface

- [x] 1.1 Add `HealthGateScope string` field to reconcile `Config` with the scope doc comment
- [x] 1.2 Add scope constants (`HealthGateScopeCritical`/`Declared`/`Off`) and exported `ResolveHealthGateScope` (empty → critical, unknown → error naming the valid set)
- [x] 1.3 Add `health_gate_scope` DTO field to `configFile` in `internal/config/config.go`
- [x] 1.4 Implement `extractHealthGateScope` helper and `HealthGateScope()` getter
- [x] 1.5 Wire `healthGateScope` through both `LoadFrom` and `Load` build blocks

## 2. Environment Variable Override

- [x] 2.1 Parse `BOSUN_HEALTH_GATE_SCOPE` in `internal/daemon/daemon.go`, validated via `ResolveHealthGateScope` (warn + ignore on invalid)
- [x] 2.2 Set `rcfg.HealthGateScope` from project config (`HealthGateScope()`); env override wins
- [x] 2.3 Document `BOSUN_HEALTH_GATE_SCOPE` in the AGENTS.md environment variable table

## 3. Gate Behavior

- [x] 3.1 Resolve scope at the top of `runHealthGate`; fall back to `critical` on an invalid value
- [x] 3.2 `off` skips the gate; `critical` preserves the existing critical-container path unchanged
- [x] 3.3 `declared` polls all declared services via `pollContainerHealth` and applies the #392 `blockingUnhealthy` pre-existing-casualty exemption (`checkDeclaredHealth`)
- [x] 3.4 Route a declared-scope failure through the SAME rollback branch (rollback-before-hooks; `rolledBack` → hooks-skip preserved); reuse the existing throttled failure alert (no new rollback alert)

## 4. Dead Code Cleanup

- [x] 4.1 Delete `ComposeUpWithRollback` / `ComposeUpMultipleWithRollback` and their exclusive tests; keep the sentinel vars and `composeUpFn`

## 5. Consumer Parity

- [x] 5.1 Update `skills/onboard/resources/configuration.md` (field + values + example) and `gitops.md` (scope behavior)

## 6. Tests

- [x] 6.1 Scope matrix: declared→rollback (exact throttled alert count), pre-existing-unhealthy exempt→no rollback, critical regression pin, off→no gate
- [x] 6.2 `ResolveHealthGateScope` unit (valid / empty / unknown) and config parse test

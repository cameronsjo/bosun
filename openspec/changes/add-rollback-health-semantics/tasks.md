## 1. Rollback restores prior good state (#229)

- [ ] 1.1 Add a distinct "restore from backup" rollback path that redeploys the backed-up prior compose files (mirror the `ComposeUpIsolated` rollback at `compose.go:~325`), instead of reusing `ComposeUpMultipleWithRollback` for the already-deployed-unhealthy case
- [ ] 1.2 Update the health-gate failure path in `reconcile.go:872-877` to call the restore path with `r.lastBackupPath`, not `ComposeUpMultipleWithRollback(ctx, r.lastComposeFiles, ...)`
- [ ] 1.3 Ensure the restore path does not short-circuit when containers already exist and merely report unhealthy
- [ ] 1.4 Tests: critical-container health failure re-deploys the backup compose files (assert the backup file set is used), not the new rendered output

## 2. Bounded post-deploy health polling (#230)

- [ ] 2.1 Move the deadline check in `pollContainerHealth` (`health.go:41-92`) out of the `else` branch so it runs every iteration regardless of `CollectActualState` success
- [ ] 2.2 Add a deadline/timeout case to the select wait loop (alongside `ctx.Done()` and the interval tick)
- [ ] 2.3 Distinguish "container unhealthy" (keep polling within the timeout) from "cannot query health" (Docker API error → bounded, fail at deadline) and return a failed `HealthCheckResult` on persistent API errors
- [ ] 2.4 Tests: `pollContainerHealth` returns a failed result after `HealthCheckTimeout` when `CollectActualState` errors every iteration; also returns on `ctx.Done()`

## 3. Surface rollback failure distinctly (#229)

- [ ] 3.1 Ensure the health-gate rollback path preserves the `ErrRollbackSucceeded` / `ErrRollbackFailed` sentinel contract and logs a failed rollback at ERROR as a critical state
- [ ] 3.2 Ensure a successful rollback is not logged as a generic deploy success and the deployment is not recorded as successful
- [ ] 3.3 Tests: failed rollback surfaces `ErrRollbackFailed`; successful rollback surfaces `ErrRollbackSucceeded` and the deploy is not marked successful

## 4. Docs

- [ ] 4.1 Update `skills/onboard/resources/gitops.md` to describe restore-from-backup rollback and bounded health polling

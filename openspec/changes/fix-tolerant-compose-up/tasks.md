## 1. Compose Exit Classification

- [ ] 1.1 Add `classifyComposeFailure` function to `internal/reconcile/deploy.go` that inspects container state after compose up failure
- [ ] 1.2 Use `docker compose ps --format json` to list container states after failure
- [ ] 1.3 Classify containers as: started-healthy, started-unhealthy, failed-to-start (exited/restarting/not-found)
- [ ] 1.4 Return structured result distinguishing "all containers started but some unhealthy" from "containers failed to start"

## 2. Update ComposeUpMultiple to Return Rich Errors

- [ ] 2.1 Define `ErrComposeUnhealthy` sentinel error for "compose exited non-zero but all containers are running (some unhealthy)"
- [ ] 2.2 When `ComposeUpMultiple` gets a non-zero exit, call `classifyComposeFailure` before returning
- [ ] 2.3 If classification shows only unhealthy containers (no start failures), return `ErrComposeUnhealthy` instead of generic error
- [ ] 2.4 If classification shows genuine start failures, return original error (triggers rollback)

## 3. Update ComposeUpMultipleWithRollback

- [ ] 3.1 When `ComposeUpMultiple` returns `ErrComposeUnhealthy`, skip rollback and return the error as a warning
- [ ] 3.2 Only trigger rollback for non-unhealthy compose failures

## 4. Update Reconcile Pipeline

- [ ] 4.1 In `deployLocal`, handle `ErrComposeUnhealthy` as a warning (log, alert, but do not fail the pipeline)
- [ ] 4.2 Ensure unhealthy containers from compose failure are included in post-deploy verification alert
- [ ] 4.3 In `deployRemote`, apply equivalent classification logic for remote compose up failures

## 5. Tests

- [ ] 5.1 Unit test `classifyComposeFailure` with mock `docker compose ps` output for healthy, unhealthy, and exited containers
- [ ] 5.2 Test `ComposeUpMultipleWithRollback` skips rollback on `ErrComposeUnhealthy`
- [ ] 5.3 Test `ComposeUpMultipleWithRollback` still rolls back on genuine start failures
- [ ] 5.4 Integration test: pipeline completes successfully when compose exits non-zero due to unhealthy container
- [ ] 5.5 Integration test: pipeline triggers rollback when compose exits non-zero due to container start failure

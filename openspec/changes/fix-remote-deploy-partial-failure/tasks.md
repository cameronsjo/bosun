## 1. Fix remote deploy error propagation

- [x] 1.1 In `deployRemote()` (`internal/reconcile/reconcile.go`), change `ComposeUpRemote` error handling from `ui.Warning` to `return fmt.Errorf`
- [x] 1.2 Verify `SignalContainerRemote` remains as `ui.Warning` (best-effort, no change needed)

## 2. Tests

- [x] 2.1 Add test: `deployRemote` returns error when first SSH sync fails (proxy for ComposeUpRemote path)
- [x] 2.2 Add test: `deployRemote` returns nil when all ops succeed (signal is best-effort)
- [x] 2.3 Add test: full `Run()` pipeline does NOT update `LastDeployedCommit` when remote deploy fails
- [ ] 2.4 Add test: full `Run()` pipeline re-runs on next reconcile when previous remote compose up failed (state mismatch triggers pipeline) — deferred, covered by existing `shouldSkipDeploy` tests

## 3. Verification

- [x] 3.1 Run `make test` -- all tests pass
- [x] 3.2 Run `make build` -- binary builds cleanly
- [x] 3.3 Confirm existing local deploy tests still pass (no regression)

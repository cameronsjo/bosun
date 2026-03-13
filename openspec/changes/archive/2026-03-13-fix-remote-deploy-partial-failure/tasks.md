## 1. Fix remote deploy error propagation

- [ ] 1.1 In `deployRemote()` (`internal/reconcile/reconcile.go`), change `ComposeUpRemote` error handling from `ui.Warning` to `return err`
- [ ] 1.2 Verify `SignalContainerRemote` remains as `ui.Warning` (best-effort, no change needed)

## 2. Tests

- [ ] 2.1 Add test: `deployRemote` returns error when `ComposeUpRemote` fails
- [ ] 2.2 Add test: `deployRemote` returns nil when `ComposeUpRemote` succeeds but `SignalContainerRemote` fails
- [ ] 2.3 Add test: full `Run()` pipeline does NOT update `LastDeployedCommit` when remote compose up fails
- [ ] 2.4 Add test: full `Run()` pipeline re-runs on next reconcile when previous remote compose up failed (state mismatch triggers pipeline)

## 3. Verification

- [ ] 3.1 Run `make test` -- all tests pass
- [ ] 3.2 Run `make build` -- binary builds cleanly
- [ ] 3.3 Confirm existing local deploy tests still pass (no regression)

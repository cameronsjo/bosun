## 1. Bounded reconcile execution

- [ ] 1.1 Move the `context.WithTimeout(.., ReconcileTimeout)` bound into `executeReconcile` (single chokepoint)
- [ ] 1.2 Remove now-redundant per-entry-point timeout wrapping (webhook/socket/TCP/api) or confirm it composes safely
- [ ] 1.3 Wrap the startup trigger (daemon.go:318) so it inherits the bound
- [ ] 1.4 Wrap the poll trigger (daemon.go:674) so it inherits the bound
- [ ] 1.5 Ensure `reconciling` is cleared on timeout/cancel (defer)
- [ ] 1.6 Tests: induced hang on poll and startup errors at timeout; subsequent trigger runs

## 2. Graceful shutdown of in-flight reconciles

- [ ] 2.1 Add a daemon-wide cancellation context that Shutdown cancels
- [ ] 2.2 Socket trigger goroutine joins `d.wg` and derives ctx from daemon ctx (socket.go:175)
- [ ] 2.3 TCP trigger goroutine joins `d.wg` and derives ctx from daemon ctx (tcp.go:169)
- [ ] 2.4 API trigger goroutine joins `d.wg` and derives ctx from daemon ctx (api.go:309)
- [ ] 2.5 Shutdown waits on `d.wg` within `ShutdownTimeout`
- [ ] 2.6 Tests: SIGTERM cancels in-flight socket/TCP/API reconciles

## 3. Lossless trigger coalescing

- [x] 3.1 Replace the dirty-bit with a counter or small queue capturing source per trigger (daemon.go:451-523)
- [x] 3.2 After each reconcile, atomically re-check the counter before clearing `reconciling`
- [x] 3.3 Preserve/aggregate source attribution across coalesced triggers
- [x] 3.4 Stress test: many concurrent triggers during a long reconcile; assert no trigger lost and sources recorded

## 4. Cancellation aborts external work

- [x] 4.1 Set `cmd.Cancel` (SIGTERM, grace, then SIGKILL) or compose-down on the `exec.CommandContext` sites (compose.go:77,235,326,360)
- [x] 4.2 Verify the docker operation stops, not just the CLI client
- [x] 4.3 Tests: forced compose-up timeout no longer leaves background container startup

## 5. Documentation

- [ ] 5.1 Update `skills/onboard/resources/gitops.md` (daemon reconcile lifecycle guarantees)
- [ ] 5.2 Update `docs/troubleshooting.md` (wedged-reconcile recovery now automatic at timeout)

## 1. Make the git network timeouts real

- [ ] 1.1 Move `GitCloneTimeout`, `GitFetchTimeout` and a new `GitSSHDialTimeout` (30s) from package constants (`git.go:77-83`) to `GitOps` fields, each defaulting to its constant when zero or negative. Keep the constants as the default values. `NewGitOps` (`git.go:106`) keeps its signature; the fields are set directly by the caller or a test
- [ ] 1.2 Wrap the resolved `transport.AuthMethod` so its `ClientConfig()` returns the auth's own config with `Timeout` filled in — preserving `User`, `Auth`, `HostKeyCallback`, `HostKeyAlgorithms`. Do **not** use `client.InstallProtocol` with a bare timeout config (see design.md). Safe because `overrideConfig` returns early on a nil client config (`ssh/common.go:291-293`)
- [ ] 1.3 Test: assert directly on the wrapper's returned `ClientConfig` — all four auth fields survive and `Timeout` is set. Assert on the config, **not** via a fetch: `internal/reconcile` has no live-SSH fixture, and a local-path or `file://` remote never enters the SSH transport, so a fetch-shaped test stays green under the declined `InstallProtocol` implementation and catches nothing
- [ ] 1.4 Bound the handshake and packfile-transfer phases: register a custom `ssh` transport that dials the TCP connection itself, wraps it in a `net.Conn` refreshing a read deadline on every `Read`, and builds the client via `xssh.NewClientConn`, replicating the auth config faithfully. **Not optional hardening** — `dial` applies `config.Timeout` only to the dial context (`common.go:192`) and `ssh.NewClientConn` (`:198`) honors no deadline, so 1.2 alone leaves the handshake unbounded. Carry 1.3's assertion down to this layer: the replacement transport is the same auth-blanking trap one level lower
- [ ] 1.5 If 1.4 proves unworkable inside its time box: ship 1.1-1.3, state the residual (TCP dial bounded, handshake and transfer not) in the PR body and `docs/troubleshooting.md`, **and move the transfer clause and its scenario out of the reconcile delta into a follow-up change** — landing a spec that asserts a bound the code lacks is worse than landing neither. Do not ship an abandon-in-a-goroutine design
- [ ] 1.6 Apply the same bounds to `Clone`, and fix its **conditional application**: `git.go:362-367` applies `GitCloneTimeout` only when `ctx.Deadline()` reports none, while `daemon.go:1008` sets a `ReconcileTimeout` deadline on every cycle — so on the daemon path the constant is never applied and the error names it anyway. Effective bound is the earlier of the two
- [ ] 1.7 Put the **actual elapsed time** in the timeout error text, replacing the configured-bound text at `git.go:486-488` (fetch) and `git.go:404` (clone). Name the bound that actually expired
- [ ] 1.8 Add the missing `logger.Error()` at the fetch timeout throw site carrying `operation`, sanitized URL, `branch`, `elapsed_ms`, `timeout_ms`. There is **no existing pattern to copy**: `git.go:394-400` is the partial-clone `os.RemoveAll` cleanup, and the clone timeout log at `:401-407` carries only `operation` and `duration_ms`. Bring the clone log up to the same field set in this task
- [ ] 1.9 Test fixture: a real initialized repo with a commit and an `origin` remote, since `Pull` runs `validateBranch`, `IsDirty`, `GetLatestCommit` and `PlainOpen` before the fetch (`git.go:433-472`). This fixture serves 1.10-1.12; it does **not** serve 1.3
- [ ] 1.10 Test: blackholed address (no TCP accept, no reset) bounded by the dial timeout
- [ ] 1.11 Test: listener accepts TCP and never speaks — a stalled *handshake*, bounded by the connection read deadline. Different layer from 1.10, and the test that proves 1.4 landed
- [ ] 1.12 Test: handshake completes, transfer stalls mid-packfile — bounded by the read deadline
- [ ] 1.13 Test: clone under a caller deadline **longer** than `GitCloneTimeout` still fails at `GitCloneTimeout`. This is the daemon's actual shape and the one a deadline-free fixture cannot catch
- [ ] 1.14 Red-proof against `origin/main`: the timeout fields do not exist there, so a test constructing `GitOps{FetchTimeout: ...}` is a **build failure** — which proves the field is new and nothing about enforcement. Run the red-proof against the *default* bound instead: stall the remote, set `-timeout` above `GitFetchTimeout`, and show the test exceeds 2m on `origin/main` and passes in seconds on the branch. Explicit `-timeout` on that single package; the expected red is a bounded panic dump, not a run the gate script scores as aborted

## 2. Make recovery actually fire

- [ ] 2.1 Add `on_recovery` to `alertConfigRaw` as a pointer bool and to `AlertConfig` (`config.go:162`) as a plain bool. Add an `OnRecovery` field to `reconcile.Config` (`reconcile.go:219-221` neighbourhood) — a different struct from `config.AlertConfig`, and 2.3/2.4 assume it exists
- [ ] 2.2 Default `on_recovery` to true in all three construction sites: `extractAlertConfig` (`config.go:1188-1193`), `AlertConfigFromEnv` (exported, `config.go:1139`), `DefaultConfig()` (`target.go:233`). **Do not copy `OnFailure`'s pattern literally** — its default is coupled (`else if raw.OnSuccess == nil`), so following it yields `on_recovery` false whenever `on_success` is explicitly set. `on_recovery` defaults true unconditionally
- [ ] 2.3 Copy `on_recovery` into the reconciler config at `daemon.go:2349-2350`, **and** into the hot-reload path — DTO field (`reconcile.go:35-36`), builder (`config.go:458-459`), applier (`config_reload.go:175-183`), reload log (`:234-235`). Startup-only wiring makes it the sole gate needing a restart, and the reload line would report two of three
- [ ] 2.4 Gate `sendRecoveryAlert` on `OnRecovery` instead of `OnSuccess` (`alerts.go:174`), and update its doc comment at `alerts.go:167-168`
- [ ] 2.5 Drop the `AttemptCount > 1` condition at `reconcile.go:944` — failure alerts fire at attempt 1 (`state.go:167`) — **and drop the `-1`** at `reconcile.go:945`. The subtraction exists only because the call was gated on `> 1`; keeping it reports 0 prior failures in exactly the single-failure case this change serves
- [ ] 2.6 Dispatch recovery from the deploy-path skip (`reconcile.go:637-655`) **before** it zeroes `AttemptCount`, `LastAttemptedCommit` and `LastAlertedAttempt`
- [ ] 2.7 Dispatch recovery from the already-deployed skip (`reconcile.go:608-616`). **Not "the same terms" as 2.6** — this branch never resets `LastAlertedAttempt`, and its reset of the other two is conditional. Its defect is the early return; it also needs `LastAlertedAttempt` cleared after dispatch, or every subsequent already-deployed run re-alerts
- [ ] 2.8 Use `LastAlertedAttempt > 0` as the retraction-owed predicate throughout. `AttemptCount > 0` is wrong: a failure below the alert threshold never alerted
- [ ] 2.9 Clear failure tracking state on every clean-run path whether or not dispatch happened, so `on_recovery: false` cannot bank a stale retraction for a later flip to true
- [ ] 2.10 Test the incident's exact sequence: one sync failure that alerts, then a success whose changed files are all deploy-irrelevant so it takes the skip branch. If this passes before 2.4-2.6, it is testing the wrong thing. Note it exercises 2.4/2.5/2.6 only — **not** 2.7
- [ ] 2.11 Test the already-deployed branch separately (2.7): recovery fires once, and a second already-deployed run does not re-alert
- [ ] 2.12 Test: recovery fires with `on_success: false`; suppressed with `on_recovery: false`; a clean run with no prior *alert* sends nothing; `on_success: true` alone still leaves `on_failure` false and `on_recovery` true; the alert reports 1 prior failure in the single-failure case
- [ ] 2.13 Correct the doc comment at `reconcile.go:219-221` — after 2.4 `OnSuccess` gates success alerts **only**
- [ ] 2.14 Update the gate reporting at `internal/cmd/alert.go` (`on_success` at `:166-170`, `on_failure` at `:171-175`) to report `on_recovery` too
- [ ] 2.15 Update `skills/onboard/resources/configuration.md` (mandatory per CLAUDE.md § Skill Maintenance). No `BOSUN_ALERT_ON_*` env var is added, so `CLAUDE.md`'s env-var table is unchanged

## 3. Log the client address, unforgeably

- [ ] 3.1 Add a `FieldRemoteAddr` constant to `internal/log/fields.go` and converge the six sites that hardcode the string (`server.go:587`, `:593`, `:608`, `:658`, `:664`, `:695`)
- [ ] 3.2 Add `remote_addr` from `r.RemoteAddr` to the `HTTP request completed` entry (`server.go:155-187`) — always present, never derived from a header
- [ ] 3.3 Add a trusted-proxy CIDR list, defaulting empty, rejecting unparseable entries at load
- [ ] 3.4 Add `forwarded_for` — the validated first `X-Forwarded-For` entry — emitted only when the **host portion** of `r.RemoteAddr` falls in that list. `r.RemoteAddr` is `host:port`; comparing the raw value never matches and fails silently in the safe direction, so an absence-only test cannot catch it. Never collapse the two fields; never prefer the header
- [ ] 3.5 Confirm tailscale `serve` actually sets `X-Forwarded-For` on the `/hooks/` path before depending on it
- [ ] 3.6 Tests: direct request omits `forwarded_for`; untrusted peer sending the header is not believed; trusted proxy contributes the first entry; port is stripped before the membership test; malformed header dropped; unparseable config entry rejected; 404 on an unmatched path carries path, status and `remote_addr`

## 4. Documentation and loose ends

- [ ] 4.1 Correct the stale webhook paths — `docs/guides/unraid-setup.md:236` (`/hooks/github-push` → `/hooks`, gateway-fronted), `:315` (`localhost:8080/hooks/github-push` → `/webhook/github`, direct), `unraid-templates/README.md:80` (→ `/webhook/github`), `:169` (`/hooks/test` → `/webhook`), `docs/adr/0005-tunnel-providers.md:70` (`"Proxy": "http://bosun:8080"` needs the `/webhook` suffix)
- [ ] 4.2 Add a `docs/troubleshooting.md` section: how to tell git sync is wedged, what the log line and elapsed time look like, and what clears it
- [ ] 4.3 Capture the post-deploy checks as `scripts/verify-git-timeout.sh` rather than prose that dies with the session. 5.5 and 5.6 both run through it
- [ ] 4.4 `[Unreleased]` entry in `CHANGELOG.md` with a `fix:` subject so release-please cuts a patch
- [ ] 4.5 Link the already-filed follow-ups in the PR body: #652 (`on_failure` contradiction), #653 (`GitLocalTimeout`), #654 (HTTPS transport). They are filed — do not re-file

## 5. Verification

- [ ] 5.1 `scripts/agent-go-gate.sh go vet ./...`
- [ ] 5.2 `scripts/agent-go-gate.sh go test -race -timeout 120s ./internal/reconcile ./internal/daemon ./internal/config` — timeout tests use short configured bounds, so they fit inside this budget
- [ ] 5.3 `openspec validate update-git-timeouts-and-alert-recovery --strict`
- [ ] 5.4 Timeout enforcement is proven in the Go tests, not on `unraid` — a blackholed address and a local stalling listener measure the same thing with no blast radius
- [ ] 5.5 After deploy, via `scripts/verify-git-timeout.sh`: recovery retracts (fail a sync, restore, push a docs-only commit; expect a Discord retraction with `on_success` still false), with the script's output captured rather than asserted by eye
- [ ] 5.6 After deploy, via the same script: `docker logs --since 1h bosun | grep github-push` — grep the 404, not the field name, which matches every request line once the field exists

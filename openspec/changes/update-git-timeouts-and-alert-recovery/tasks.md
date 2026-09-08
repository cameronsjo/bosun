## 1. Make the git network timeouts real

- [ ] 1.1 Move `GitCloneTimeout`, `GitFetchTimeout` and a new `GitSSHDialTimeout` (30s) from package constants (`git.go:77-83`) to `GitOps` fields, each defaulting to its constant when zero or negative
- [ ] 1.2 Wrap the resolved `transport.AuthMethod` so its `ClientConfig()` returns the auth's own config with `Timeout` filled in — preserving `User`, `Auth`, `HostKeyCallback`, `HostKeyAlgorithms`. Do **not** use `client.InstallProtocol` (see design.md)
- [ ] 1.3 Test: a fetch against a reachable remote still authenticates after the wrap. This is the test that catches the declined `InstallProtocol` approach
- [ ] 1.4 Bound the packfile-transfer phase: register a custom `ssh` transport that dials the TCP connection itself, wraps it in a `net.Conn` refreshing a read deadline on every `Read`, and builds the client via `xssh.NewClientConn` replicating the auth config faithfully
- [ ] 1.5 If 1.4 proves unworkable inside its time box, ship 1.1-1.3 alone and state the residual (dial and handshake bounded, transfer not) in the PR body and `docs/troubleshooting.md`. Do not ship an abandon-in-a-goroutine design
- [ ] 1.6 Apply the same bounds to `Clone` (`git.go:366`, `:406`)
- [ ] 1.7 Put the **actual elapsed time** in the timeout error text, replacing the configured-bound text at `git.go:486-488`
- [ ] 1.8 Add the missing `logger.Error()` at the fetch timeout throw site carrying `operation`, sanitized URL, `branch`, `elapsed_ms`, `timeout_ms` — matching what `Clone` already logs at `:394-400`
- [ ] 1.9 Test fixture: a real initialized repo with a commit and an `origin` remote, since `Pull` runs `validateBranch`, `IsDirty`, `GetLatestCommit` and `PlainOpen` before the fetch (`git.go:433-472`)
- [ ] 1.10 Test: stalled handshake (listener accepts TCP, never speaks) is bounded by the dial timeout
- [ ] 1.11 Test: handshake completes, transfer stalls mid-packfile — bounded by the fetch timeout. This is a different layer from 1.10 and proves nothing unless 1.4 landed
- [ ] 1.12 Show both tests red against `origin/main` with an explicit `-timeout 30s` on that single package; the expected red is a bounded panic dump, not a run the gate script scores as aborted

## 2. Make recovery actually fire

- [ ] 2.1 Add `on_recovery` to the raw config DTO and to `AlertConfig` (`config.go:162`), defaulting **true**
- [ ] 2.2 Set the default explicitly in all three construction sites, following how `OnFailure` does it: `extractAlertConfig` (`config.go:1188-1193`), `alertConfigFromEnv` (`config.go:1157-1158`), `DefaultConfig()` (`target.go:233`). `AlertConfig.OnSuccess` is a plain `bool`, so mirroring it yields default-*false*
- [ ] 2.3 Copy `on_recovery` into the reconciler config at `daemon.go:2349-2350` — omitting this leaves the daemon path, the only path the incident took, without the gate
- [ ] 2.4 Gate `sendRecoveryAlert` on `OnRecovery` instead of `OnSuccess` (`alerts.go:174`)
- [ ] 2.5 Drop the `AttemptCount > 1` condition at `reconcile.go:944` — failure alerts fire at attempt 1 (`state.go:167`)
- [ ] 2.6 Dispatch recovery from the deploy-path skip branch (`reconcile.go:637-655`) before it resets `AttemptCount`/`LastAlertedAttempt`
- [ ] 2.7 Dispatch recovery from the already-deployed skip branch (`reconcile.go:608`) on the same terms
- [ ] 2.8 Test the incident's exact sequence: one sync failure that alerts, then a success whose changed files are all deploy-irrelevant so it takes the skip branch. If this passes before 2.5-2.7, it is testing the wrong thing
- [ ] 2.9 Test: recovery fires with `on_success: false`; recovery is suppressed with `on_recovery: false`; a clean run with no prior alert sends nothing; a second consecutive clean run does not re-send
- [ ] 2.10 Correct the doc comment at `reconcile.go:219-221` ("OnSuccess gates success **and recovery** alert dispatch")
- [ ] 2.11 Update the gate reporting at `internal/cmd/alert.go:166-171`
- [ ] 2.12 Update `skills/onboard/resources/configuration.md` (mandatory per CLAUDE.md § Skill Maintenance)
- [ ] 2.13 If `on_recovery` gains a `BOSUN_ALERT_ON_*` env var, add it to `alertConfigFromEnv` and to the env-var table in `CLAUDE.md`; if not, leave `CLAUDE.md` alone

## 3. Log the client address, unforgeably

- [ ] 3.1 Add a `FieldRemoteAddr` constant to `internal/log/fields.go`, matching the middleware's existing field-constant style (the string `remote_addr` is currently hardcoded at `server.go:587` and five other sites)
- [ ] 3.2 Add `remote_addr` from `r.RemoteAddr` to the `HTTP request completed` entry (`server.go:155-187`) — always present, never derived from a header
- [ ] 3.3 Add an operator-configured trusted-proxy set, defaulting empty
- [ ] 3.4 Add `forwarded_for` — the validated first `X-Forwarded-For` entry — emitted only when `r.RemoteAddr` is in that set. Never collapse the two fields; never prefer the header
- [ ] 3.5 Confirm tailscale `serve` actually sets `X-Forwarded-For` on the `/hooks/` path before depending on it
- [ ] 3.6 Tests: direct request omits `forwarded_for`; untrusted peer sending the header is not believed; trusted proxy contributes the first entry; malformed header is dropped; 404 on an unmatched path carries path, status and `remote_addr`

## 4. Documentation and loose ends

- [ ] 4.1 Correct the stale webhook paths — `docs/guides/unraid-setup.md:236` (`/hooks/github-push` → `/hooks`, gateway-fronted), `:315` (`localhost:8080/hooks/github-push` → `/webhook/github`, direct), `unraid-templates/README.md:80` (→ `/webhook/github`), `:169` (`/hooks/test` → `/webhook`), `docs/adr/0005-tunnel-providers.md:70` (`"Proxy": "http://bosun:8080"` needs the `/webhook` suffix)
- [ ] 4.2 Add a `docs/troubleshooting.md` section: how to tell git sync is wedged, what the log line and elapsed time look like, and what clears it
- [ ] 4.3 Capture the post-deploy checks as `scripts/verify-git-timeout.sh` rather than leaving them as prose that dies with the session
- [ ] 4.4 `[Unreleased]` entry in `CHANGELOG.md` with a `fix:` subject so release-please cuts a patch
- [ ] 4.5 File separate issues: the `on_failure: false`-yet-alert-delivered contradiction; `GitLocalTimeout`'s unenforced label; a timeout-bearing HTTP transport for the private-HTTPS git path

## 5. Verification

- [ ] 5.1 `scripts/agent-go-gate.sh go vet ./...`
- [ ] 5.2 `scripts/agent-go-gate.sh go test -race -timeout 120s ./internal/reconcile ./internal/daemon ./internal/config`
- [ ] 5.3 `openspec validate update-git-timeouts-and-alert-recovery --strict`
- [ ] 5.4 Timeout enforcement is proven in the Go tests, not on `unraid` — a blackholed address in the fixture measures the same thing with no blast radius
- [ ] 5.5 After deploy: recovery retracts (fail a sync, restore, push a docs-only commit; expect a Discord retraction with `on_success` still false)
- [ ] 5.6 After deploy: `docker logs --since 1h bosun | grep github-push` — grep the 404, not the field name, which matches every request line once the field exists

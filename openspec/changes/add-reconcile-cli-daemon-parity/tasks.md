## 1. Configuration parity

- [ ] 1.1 Move the environment-to-config construction out of `runReconcile` (`internal/cmd/reconcile.go:169-360`) into a function that returns `(*reconcile.Config, error)` instead of calling `ui.Fatal`. `runReconcile` keeps the startup validation, telemetry, signal handling and target loop
- [ ] 1.2 Read `BOSUN_INFRA_DIR` into `InfraSubDir`, matching `internal/daemon/daemon.go:2154-2156`
- [ ] 1.3 Parity test: one `bosun.yaml` in a temp dir, `chdir` into it; build through the CLI function and through `daemon.ConfigFromEnv().ReconcileConfig`; walk every `reconcile.Config` field by reflection. Each field is either compared or on an allowed-difference list with a one-line reason. Fail on an unlisted difference (naming the field and both values) and on a field that is in neither set. Two cases, table-driven:
  - **override**: an environment map covering every variable either path reads, where each variable that `bosun.yaml` can also set (deploy paths, template include dir, post-sync hooks, hook settle delay, targets) carries a value *different* from the file's
  - **fallback**: the same `bosun.yaml` with those variables unset
- [ ] 1.3a Precedence assertion, independent of the reflection walk: in the override case both paths hold the environment value, and in the fallback case both hold the file value. Equality alone would pass if both paths picked the same wrong source
- [ ] 1.4 Stage the break: remove the `BOSUN_INFRA_DIR` read and confirm the test fails naming `InfraSubDir`; restore it and confirm green
- [ ] 1.5 File one follow-up issue listing each allowed difference that is a gap rather than a design choice

## 2. `--no-alerts`

- [ ] 2.1 Add the `--no-alerts` flag. When set, pass no `reconcile.WithAlerter` option and print that alerts are disabled
- [ ] 2.2 Test with an `httptest` receiver and a repository URL that cannot be cloned, table-driven over two alert sources: `DISCORD_WEBHOOK_URL` in the environment, and `alerts.discord_webhook_url` in a `bosun.yaml` in the test's working directory (no alert environment). For each source: with the flag, zero requests; without it (control), one failure alert. The control proves each case can fail

## 3. Docs

- [ ] 3.1 `docs/commands.md`: `--no-alerts`, and `BOSUN_INFRA_DIR` in the reconcile environment list
- [ ] 3.2 `skills/onboard/resources/commands.md`: the same
- [ ] 3.3 Update the `reconcile` command's long help, which lists environment variables

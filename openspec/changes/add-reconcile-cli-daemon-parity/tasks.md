## 1. Configuration parity

- [ ] 1.1 Move the environment-to-config construction out of `runReconcile` (`internal/cmd/reconcile.go:169-360`) into a function that returns `(*reconcile.Config, error)` instead of calling `ui.Fatal`. `runReconcile` keeps the startup validation, telemetry, signal handling and target loop
- [ ] 1.2 Read `BOSUN_INFRA_DIR` into `InfraSubDir`, matching `internal/daemon/daemon.go:2154-2156`
- [ ] 1.3 Parity test: one environment map covering every variable either path reads, one `bosun.yaml` in a temp dir, `chdir` into it; build through the CLI function and through `daemon.ConfigFromEnv().ReconcileConfig`; walk every `reconcile.Config` field by reflection. Each field is either compared or on an allowed-difference list with a one-line reason. Fail on an unlisted difference (naming the field and both values) and on a field that is in neither set
- [ ] 1.4 Stage the break: remove the `BOSUN_INFRA_DIR` read and confirm the test fails naming `InfraSubDir`; restore it and confirm green
- [ ] 1.5 File one follow-up issue listing each allowed difference that is a gap rather than a design choice

## 2. `--no-alerts`

- [ ] 2.1 Add the `--no-alerts` flag. When set, pass no `reconcile.WithAlerter` option and print that alerts are disabled
- [ ] 2.2 Test with an `httptest` receiver as `DISCORD_WEBHOOK_URL` and a repository URL that cannot be cloned: with the flag, zero requests; without it (control), one failure alert. The control proves the test can fail

## 3. Docs

- [ ] 3.1 `docs/commands.md`: `--no-alerts`, and `BOSUN_INFRA_DIR` in the reconcile environment list
- [ ] 3.2 `skills/onboard/resources/commands.md`: the same
- [ ] 3.3 Update the `reconcile` command's long help, which lists environment variables

## 1. Configuration parity

- [ ] 1.1 Move the environment-to-config construction out of `runReconcile` (`internal/cmd/reconcile.go:169-360`) into a function that returns `(*reconcile.Config, error)` instead of calling `ui.Fatal`. `runReconcile` keeps the startup validation, telemetry, signal handling and target loop
- [ ] 1.2 Read `BOSUN_INFRA_DIR` into `InfraSubDir`, matching `internal/daemon/daemon.go:2154-2156`
- [ ] 1.2a Use one parser for secrets files in both paths. The CLI keeps empty entries (`reconcile.go:201-206`) and does not split `BOSUN_SECRETS_FILE` at all (`:207-209`); the daemon's `splitAndTrim` (`daemon.go:2525`) drops empties and splits both. Export or share that helper rather than copying it
- [ ] 1.2b Use one boolean parser for `DRY_RUN`. The CLI is strict `== "true"` (`reconcile.go:229`); the daemon's `parseBoolVal` (`daemon.go:2504`) accepts `1/yes/on/TRUE`. A daemon environment carrying `DRY_RUN=yes` currently makes the CLI deploy for real
- [ ] 1.3 Parity test: one `bosun.yaml` in a temp dir, `chdir` into it (so no `t.Parallel()`, and `t.Setenv` throughout); build through the CLI function and through `daemon.ConfigFromEnv().ReconcileConfig`; walk every `reconcile.Config` field by reflection. Two table-driven cases:
  - **override**: an environment map covering every variable either path reads, where each variable `bosun.yaml` can also set carries a different value from the file's
  - **fallback**: the same `bosun.yaml` with those variables unset
- [ ] 1.3a Classify every field in exactly one of three sets, declared as data in the test: **compared**, **allowed difference** (with a reason string), **unset by both**. Fail on a field in none of them, on an unlisted difference, and on an "unset by both" field that either path did set. Known members at the time of writing: allowed difference — `RepoDir`, `StagingDir`, `BackupDir`, `LogDir`, `LocalAppdataPath`, `RemoteAppdataPath` (the one-shot points at its own directories; the daemon's are fixed by its image), `Source` (`"cli"` vs the daemon's trigger), `Force` (the CLI's `FORCE`/`--force` is per-invocation), `ConfigReloader` (both assign the same function; `reflect.DeepEqual` on two non-nil funcs is always false and `Interface()` comparison panics, so compare `reflect.Value.Pointer()`); unset by both — `SecretsScope`, `TargetName`, `LockFile`, `BackupsToKeep`, `ProjectName`, `ForceRedeployUnchanged`
- [ ] 1.3b Precedence assertion, independent of the reflection walk: in the override case both paths hold the environment value, in the fallback case both hold the file value. Equality alone passes when both paths pick the same wrong source
- [ ] 1.4 Stage the break: remove the `BOSUN_INFRA_DIR` read and confirm the test fails naming `InfraSubDir`; restore it and confirm green. Repeat for one of 1.2a/1.2b
- [ ] 1.5 File one follow-up issue for the divergences this change does not close (deploy mode, the sync-path/critical-container/drift-ignore environment overrides, health gate, compose/backup/health-check timeouts, restart breaker, content-hash sync, orphan removal, the safety overrides, alert gates from project config). For each, state what an operator observes when it bites. These stay out of the fixed set above, so the spec does not claim they are fixed

## 2. `--no-alerts`

- [ ] 2.1 Add the `--no-alerts` flag. When set, skip `createAlertManager()` entirely — it prints `Alert providers: …` (`reconcile.go:462-469`), which would contradict the flag — pass no `reconcile.WithAlerter`, and print that alerts are disabled
- [ ] 2.2 Test with an `httptest` receiver and a repository URL that **passes** `ValidateGitAuthentication` but cannot be cloned (for example `https://127.0.0.1:<closed port>/x.git`): a URL that fails validation exits before the pipeline and sends nothing in either arm, which would make the control vacuous. Install `ui.SetExitFn` so `ui.Fatal` cannot kill the test binary. Table-driven over two alert sources — `DISCORD_WEBHOOK_URL` in the environment, and `alerts.discord_webhook_url` in a `bosun.yaml` in the working directory. For each: assert the control arm (no flag) received exactly one request, then that the flag arm received none
- [ ] 2.3 Test that `--no-alerts` output states alerts are disabled and does not list providers

## 3. Docs

- [ ] 3.1 `docs/commands.md`: `--no-alerts`, and `BOSUN_INFRA_DIR` in the reconcile environment list
- [ ] 3.2 `skills/onboard/resources/commands.md`: the same
- [ ] 3.3 Update the `reconcile` command's long help, which lists environment variables

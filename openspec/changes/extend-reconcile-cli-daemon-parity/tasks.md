## 1. Shared loader

- [x] 1.1 Add `config.ApplyProjectOperationalConfig` and `config.ApplyReconcileEnv`
- [x] 1.2 Call both from `daemon.ConfigFromEnv` in place of its inline reads
- [x] 1.3 Call both from `buildReconcileConfigFromEnv`; apply `--local` after the environment

## 2. Tests

- [x] 2.1 Move the bosun#676 fields from `allowedDifferences` to `comparedFields` in `reconcile_parity_test.go`, with env and file values that differ from defaults
- [x] 2.2 Assert each variable changes the CLI value, and that `--local` outranks `BOSUN_DEPLOY_MODE`
- [x] 2.3 Stage the break (skip `ApplyReconcileEnv` in the CLI) and confirm the tests go red

## 3. Docs

- [x] 3.1 `docs/commands.md` and `skills/onboard/resources/commands.md`: reconcile honors the daemon's operational variables

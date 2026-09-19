# Change: Make `bosun reconcile` a faithful dry run of the daemon

## Why

`bosun reconcile --dry-run` is the only way to see what a given bosun binary would render for a commit without deploying it. The upgrade canary (#672) depends on that: it dry-runs the running version and the candidate against the same commit and compares their staging output before cutover. Two gaps make the one-shot command unfit for that job.

- **The CLI and the daemon build their configuration separately, and they have drifted.** The daemon reads `BOSUN_INFRA_DIR` into `InfraSubDir` (`internal/daemon/daemon.go:2154-2156`); `runReconcile` never reads it (`internal/cmd/reconcile.go:167-360`). The live homelab daemon sets `BOSUN_INFRA_DIR=unraid`, so a CLI dry run with the same environment renders from the repo root, not `unraid/`, and produces a different staging tree from the one the daemon deploys. No test compares the two builders, so this went unnoticed, and nothing stops the next difference either.
- **A dry run sends real alerts.** Alert dispatch has no dry-run gate (`internal/reconcile/alerts.go:79-125`). A dry run that fails with `DISCORD_WEBHOOK_URL` set posts a `Deployment Failed` alert, just like a real failure. A canary that runs two dry runs per upgrade must be able to rule that out explicitly.

## What Changes

- `bosun reconcile` reads `BOSUN_INFRA_DIR` into `InfraSubDir`, with the daemon's semantics (non-empty value overrides the default).
- The CLI's environment-to-config construction moves out of `runReconcile` into a function that returns the config, so a test can call it.
- A parity test builds the reconciler config through the CLI path and through `daemon.ConfigFromEnv` from one environment and one project config, then compares **every** `reconcile.Config` field. Each field on which the two paths may differ is listed in the test with its reason. A difference on an unlisted field, or a new field that is not classified, fails the test.
- `bosun reconcile --no-alerts` builds the reconciler with no alert manager. No alert of any kind is sent, whatever the alert environment or config says. `--dry-run` does not imply `--no-alerts`; a dry run keeps today's alert behavior unless the flag is given.

The differences the parity test lists but this change does not fix (deploy mode, sync-path and critical-container environment overrides, health gate, compose/backup/health-check timeouts, restart breaker, content-hash sync, orphan removal, safety overrides, alert gates from the project config) are tracked in one follow-up issue. They do not change which files a dry run renders into staging, which is what the canary compares.

## Impact

- Affected specs: `reconcile` (two ADDED requirements).
- Affected code: `internal/cmd/reconcile.go`, a new `internal/cmd/reconcile_parity_test.go`, a `--no-alerts` test in `internal/cmd`.
- Affected docs: `docs/commands.md`, `skills/onboard/resources/commands.md`.
- Behavior change for existing users: a `bosun reconcile` run with `BOSUN_INFRA_DIR` set now renders from that directory. Before, it silently ignored the variable. Anyone relying on the old behavior was rendering a tree the daemon would never deploy.

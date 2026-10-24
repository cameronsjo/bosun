# Change: Close the operational environment gaps between `bosun reconcile` and the daemon

## Why

`add-reconcile-cli-daemon-parity` aligned the fields that decide what a reconciliation renders. About twenty operational fields stayed daemon-only: `bosun reconcile` ignored their environment variables, so a one-shot run behaved differently from the daemon under the same environment (bosun#676). Examples: a run synced trees the daemon would not, gated health on a different container set, and aborted on safety checks the daemon had overridden.

## What Changes

- One shared loader in `internal/config` (`ApplyProjectOperationalConfig`, then `ApplyReconcileEnv`) builds these fields. The daemon and the CLI both call it, so a field cannot be read by one path only.
- `bosun reconcile` now honors: `BOSUN_DEPLOY_MODE`, `BOSUN_DEPLOY_SYNC_PATHS`, `BOSUN_DEPLOY_SYNC_EXCLUDE`, `BOSUN_CRITICAL_CONTAINERS`, `BOSUN_DRIFT_IGNORE`, `BOSUN_HEALTH_GATE_TIMEOUT`, `BOSUN_HEALTH_GATE_SCOPE`, `BOSUN_HEALTH_CHECK_TIMEOUT`, `BOSUN_HEALTH_CHECK_INTERVAL`, `BOSUN_COMPOSE_UP_TIMEOUT`, `BOSUN_BACKUP_TIMEOUT`, `BOSUN_RESTART_BREAKER`, `BOSUN_RESTART_THRESHOLD`, `BOSUN_RESTART_WINDOW`, `BOSUN_CONTENT_HASH_SYNC`, `BOSUN_REMOVE_ORPHANS`, `BOSUN_ALLOW_EMPTY_DECLARED_STATE`, `BOSUN_SKIP_DEPLOY_INVARIANT`. `--local` still outranks `BOSUN_DEPLOY_MODE`.
- Startup file reads change in two places. The CLI now reads `health_gate_scope` and `remove_orphans` from `bosun.yaml` at startup. Before, the CLI never applied `health_gate_scope` at all (the reload does not carry it), and `remove_orphans` only arrived through the post-clone reload. The daemon now reads `deploy_sync_paths` and `deploy_sync_exclude` from `bosun.yaml` at startup; before, only the post-clone reload filled them.
- Flags outrank `BOSUN_DEPLOY_MODE` in both directions: `--remote` forces remote mode and `--local` forces local mode. `--remote` is refused when named targets are configured (a target carries its own host and would override the flag's), and `--remote ""` is rejected.
- Follow-up, not fixed here: the reload does not carry `health_gate_scope`, so a daemon with no project file in its working directory never applies the repo's value.
- Content-hash sync defaults to on for the CLI, matching the daemon.
- The parity test moves these fields from allowed differences to compared fields. Alert gates (`OnFailure`, `OnSuccess`, `OnRecovery`) were already compared.

## Impact

- Affected specs: `reconcile` (one ADDED requirement).
- Affected code: `internal/config/reconcile_env.go`, `internal/daemon/daemon.go`, `internal/cmd/reconcile.go`, `internal/cmd/reconcile_parity_test.go`.
- Behavior change: a one-shot run in an environment that sets these variables now follows them. A one-shot run also writes only changed files by default (content-hash sync on).

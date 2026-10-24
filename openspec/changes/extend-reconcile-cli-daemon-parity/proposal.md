# Change: Close the operational environment gaps between `bosun reconcile` and the daemon

## Why

`add-reconcile-cli-daemon-parity` aligned the fields that decide what a reconciliation renders. About twenty operational fields stayed daemon-only: `bosun reconcile` ignored their environment variables, so a one-shot run behaved differently from the daemon under the same environment (bosun#676). Examples: a run synced trees the daemon would not, gated health on a different container set, and aborted on safety checks the daemon had overridden.

## What Changes

- One shared loader in `internal/config` (`ApplyProjectOperationalConfig`, then `ApplyReconcileEnv`) builds these fields. The daemon and the CLI both call it, so a field cannot be read by one path only.
- `bosun reconcile` now honors: `BOSUN_DEPLOY_MODE`, `BOSUN_DEPLOY_SYNC_PATHS`, `BOSUN_DEPLOY_SYNC_EXCLUDE`, `BOSUN_CRITICAL_CONTAINERS`, `BOSUN_DRIFT_IGNORE`, `BOSUN_HEALTH_GATE_TIMEOUT`, `BOSUN_HEALTH_GATE_SCOPE`, `BOSUN_HEALTH_CHECK_TIMEOUT`, `BOSUN_HEALTH_CHECK_INTERVAL`, `BOSUN_COMPOSE_UP_TIMEOUT`, `BOSUN_BACKUP_TIMEOUT`, `BOSUN_RESTART_BREAKER`, `BOSUN_RESTART_THRESHOLD`, `BOSUN_RESTART_WINDOW`, `BOSUN_CONTENT_HASH_SYNC`, `BOSUN_REMOVE_ORPHANS`, `BOSUN_ALLOW_EMPTY_DECLARED_STATE`, `BOSUN_SKIP_DEPLOY_INVARIANT`. `--local` still outranks `BOSUN_DEPLOY_MODE`.
- The CLI now also reads `health_gate_scope` from `bosun.yaml` at startup, and the daemon now reads `deploy_sync_paths` and `deploy_sync_exclude` from `bosun.yaml` at startup (each path previously waited for the post-clone reload).
- Content-hash sync defaults to on for the CLI, matching the daemon.
- The parity test moves these fields from allowed differences to compared fields. Alert gates (`OnFailure`, `OnSuccess`, `OnRecovery`) were already compared.

## Impact

- Affected specs: `reconcile` (one ADDED requirement).
- Affected code: `internal/config/reconcile_env.go`, `internal/daemon/daemon.go`, `internal/cmd/reconcile.go`, `internal/cmd/reconcile_parity_test.go`.
- Behavior change: a one-shot run in an environment that sets these variables now follows them. A one-shot run also writes only changed files by default (content-hash sync on).

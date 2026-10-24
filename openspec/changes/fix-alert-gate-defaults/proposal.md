# Change: Alert gates default independently

## Why

`alerts.on_success: true` with no `on_failure` key resolved `on_failure` to false, because the default applied only when neither key was set. An operator who adds success alerts loses the failure alerts they already had, with no warning. For a monitoring control the surprising direction is silence.

## What Changes

- `on_failure` defaults to true whenever the key is unset, regardless of `on_success`.
- `on_recovery` keeps its unconditional default of true; `on_success` keeps its default of false.
- An explicit `false` on any gate is honored. Each gate resolves from its own key only.

The gates are read from `bosun.yaml` only; there is no environment variable for them, so no env path changes.

## Impact

- Affected specs: `alerting` (Alert Configuration modified)
- Affected code: `internal/config/config.go` (`extractAlertConfig`), tests in `internal/config`
- Behavior change: a config that sets `on_success: true` and omits `on_failure` now sends failure alerts. Set `on_failure: false` to restore the old result.

Note: `update-git-timeouts-and-alert-recovery` also modifies the Alert Configuration requirement. Its delta carries the same independent-gate wording, so archive order cannot restore the coupling.

# Change: A dry run changes nothing it can persist or stop

## Why

`bosun reconcile --dry-run` and `bosun daemon -n` are documented as making no changes (#792). Two paths break that.

- `Reconciler.Run` persists deploy state on a dry run: the attempt counters, the pre-deploy redeploy marker, the throttle marker, the interruption outcome, and finally `LastDeployedCommit`, `DeployCount`, and `DeclaredServices`. The one-shot CLI hides this behind a temporary copy of the state file. The daemon does not. A dry-run daemon marks an undeployed commit as deployed, and the next real daemon skips it.
- The daemon's periodic drift check runs under `-n` with no dry-run check. It writes drift, self-heal, and restart-breaker data into the real state file, and the restart breaker stops real containers.

## What Changes

- `Reconciler.Run` writes no state on a dry run. Decisions inside the run use the state loaded at its start.
- A dry run sends no recovery alert, because it cannot record that the retraction was delivered, so each run would repeat it. Failure, success, and interruption alerts stay as specified by Standalone Reconcile Alert Suppression: `--dry-run` does not imply `--no-alerts`.
- A dry-run daemon's periodic drift check observes, logs, and updates in-memory runtime status only. It saves no state, runs no restart breaker, consumes no self-heal attempt, and sends no drift alert.

## Impact

- Affected specs: `reconcile` (three ADDED requirements). No MODIFIED block: Periodic Drift Checks and Drift Status Persistence already exceed the 500-character requirement limit, so restating them whole would fail strict validation. The new daemon requirement states its precedence over them.
- Order: `add-reconcile-cli-daemon-parity` (shipped in #677) is archived in the same pull request, so Standalone Reconcile Alert Suppression is in `openspec/specs/` before this change narrows it.
- Unarchived changes that overlap: `add-interruption-recovery-alert` (ADDED "Interruption Alert Retraction": every clean run after a delivered interruption alert sends a recovery) and `update-git-timeouts-and-alert-recovery` (ADDED "Deploy Recovery Dispatch") have no dry-run exemption. The recovery requirement here names both with "Notwithstanding", so the exemption holds in whichever order they archive. `add-drift-alert-delivery` (ADDED "Drift Alert Delivery Confirmation") is named by the daemon requirement the same way.
- A failure alert sent by a dry run is never retracted: no state records it, and a dry run withholds recovery alerts. A real run that later fails and recovers retracts its own alerts only.
- Affected code: `internal/reconcile/{reconcile,alerts,interruption,predeploy}.go`, `internal/daemon/daemon.go`.
- Behavior change for a polling dry-run daemon: with state never advancing, every poll runs the whole pipeline, including pre-deploy hooks, instead of skipping an already-deployed commit. Success and failure alerts repeat every poll while enabled. The daemon has no `--no-alerts`.
- Affected docs: `docs/gitops.md`, `docs/alerting.md`, `skills/onboard/resources/{gitops,commands}.md`.

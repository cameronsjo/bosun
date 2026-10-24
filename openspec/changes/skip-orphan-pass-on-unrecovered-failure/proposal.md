# Change: Skip the orphan pass when a compose file failed without rollback

## Why

Isolated compose up runs each file on its own, then one `up --remove-orphans`
pass over every file that succeeded or was rolled back (#729). A file that
failed and could not be rolled back is left out of that pass. Its containers
are often still running (compose up failed partway, or the old containers were
never replaced), and to the pass they are services absent from every `-f`
file, so compose removes them as orphans. A failed deploy of one stack could
take down that stack's running containers.

## What Changes

- When any compose file in the run failed without a verified rollback, the
  orphan pass SHALL NOT run. A file marked rolled back with no extracted backup
  tree counts as unrecovered (its state is uncertain).
- The skip SHALL be logged at warn level with the unrecovered file names, and
  the partial-deploy (or all-failed) error SHALL name the skip and the files,
  so the existing deploy-failure alert carries it.
- Orphans left by the skipped pass are removed by the next full run. A partial
  deploy keeps `NeedsRedeploy` set, which forces a full run next time.
- Runs where every failed file was rolled back keep the orphan pass, using the
  rolled-back files' backup copies, as before.

## Impact

- Affected specs: `reconcile` — ADDED: Orphan Pass Safety After Unrecovered
  Failure.
- Affected code: `internal/reconcile/pure.go` (`orphanPassSkipReason`,
  `ComposeUpSummary.OrphanPassSkipped`, `partialDeployError`),
  `internal/reconcile/compose.go` (skip the pass, carry the reason).
- Builds on `compose-up-changed-only`: the orphan pass already runs only on
  full runs; this adds a second condition.
- No new env vars or config.

## Scope

Spec delta and implementation ship together in one PR.

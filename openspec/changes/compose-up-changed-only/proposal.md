# Change: Run compose up only on compose files that changed

## Why

A local deploy runs `docker compose up -d` on every live `compose/*.yml`, one
file at a time, then an orphan pass over all of them (#726). Compose leaves
services with unchanged config alone, but every call still inspects every
container and starts any stopped one. On a loaded host each file took one to
four minutes, so a one-file commit paid for every file, the shared reconcile
budget ran out, and stacks the commit never touched were marked failed and
rolled back. A deploy that changes one stack should not act on the others.

## What Changes

- Local deploys SHALL run compose up only on the compose files whose effective
  input changed this run, plus files declaring a service with no present container
  (`ServiceIsPresent`, shared with the missing-service check from #725).
- The change signal SHALL be content-hash sync's written and deleted file lists:
  a compose file is written only when its rendered bytes (templates plus
  secrets) differ from the deployed bytes. A compose file is also selected when
  a local file it reads (`env_file`, `label_file`, top-level `configs`/`secrets`
  `file:`) changed, and always selected when it reads an input bosun cannot map
  to a deployed path (`include`, `extends.file`, an absolute, out-of-tree, or
  interpolated path), an in-tree input outside the deploy manifest, or a
  service with a `pull_policy` that can change the image on its own.
- Not detected, by design: a floating tag pulled outside bosun and `${VAR}`
  from bosun's process environment. `bosun trigger -f` applies both.
- Every file SHALL run, as before, whenever bosun cannot tell what changed:
  force, drift self-heal, a previous deploy that did not record success
  (`NeedsRedeploy`), no previous successful deploy, no deploy manifest,
  content-hash sync off, compose not synced this run, a non-`*.yml` entry or a
  deletion in the compose directory, an unparseable compose file, unknown live
  container state, a container that belongs to no declared service, or a
  service declared at the last deploy that no file declares now.
- The orphan pass (`--remove-orphans`) SHALL run only on full runs. Over a
  subset it would remove every omitted file's services.
- The health-gate rollback keeps re-applying the full compose file list.

## Impact

- Affected specs: `reconcile` — MODIFIED: Service Orchestration (orphan
  removal only on full runs); ADDED: Changed-Only Compose Up.
- Affected code: `internal/reconcile/compose_select.go` (new: selection rules,
  compose input inspection), `internal/reconcile/compose.go` (orphan pass made
  explicit), `internal/reconcile/reconcile.go` (inputs captured before the
  pre-deploy marker, selection applied in `deployLocal`).
- Remote deploys are unchanged: they have no per-path change tracking.
- No new env vars or config.

## Scope

Spec delta and implementation ship together in one PR.

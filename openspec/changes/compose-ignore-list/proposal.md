# Change: Explicit ignore list for unmanaged compose-directory entries

## Why

Since changed-only compose up (#726, #732), a local deploy narrows compose up
to the changed files only when every entry in the live compose directory is
in bosun's deploy manifest. Any unmanaged entry makes every file run with the
orphan pass. Failing closed is the right default, but an operator can have a
legitimate unmanaged entry there, such as a separate compose project checked
out by hand. That entry then turns narrowing off on every deploy,
permanently and silently (#744).

## What Changes

- A new `compose_ignore` config field (root level and per target, also
  accepted in `BOSUN_TARGETS`) SHALL list exact names of entries directly in
  the live compose directory that local deploys leave alone.
- A listed entry that bosun does not deploy SHALL NOT force a full run, SHALL
  be left out of the recorded unmanaged-entry list and its comparison with
  the last deploy's record (on both sides, so adding a name takes effect on
  the next deploy), and SHALL never be read as compose input: a listed
  `*.yml` is not inspected, not passed to compose up, and not part of the
  health-gate rollback scope.
- An entry bosun deploys (in this run's manifest or the last deploy's) SHALL
  never be ignored, even when listed; bosun logs a warning for it.
- An entry not on the list SHALL keep today's fail-closed behavior; removing a
  name from the list makes that entry force a full run again.
- Entries SHALL be exact names: config load rejects an empty name, `.` or
  `..`, a path separator, glob characters (`*`, `?`, `[`, `]`), leading or
  trailing whitespace, a NUL byte, and `.env` (compose reads `.env` from the
  compose directory on its own, so ignoring it would hide changes compose
  acts on). An invalid list reaching the reconciler some other way is dropped
  whole, so selection fails closed.
- Each local deploy with a non-empty list SHALL log the configured names and
  the entries it skipped.
- A config file without the key reloads as an empty list, so removing
  `compose_ignore` from `bosun.yaml` clears it without a restart.
- The orphan pass is unchanged. It acts on containers in bosun's compose
  project, not on directory entries. Containers an ignored file starts under
  bosun's project name still count as orphans (forcing a full run that
  removes them), so an ignored compose project must use its own project name.

## Impact

- Affected specs: `reconcile` — ADDED: Compose Directory Ignore List.
  (Changed-Only Compose Up is still an open change, so this adds a sibling
  requirement instead of modifying it.)
- Affected code: `internal/reconcile/compose_select.go` (validation,
  filtering, selection, logging), `internal/reconcile/reconcile.go` (config
  field, glob filter in the local deploy), `internal/reconcile/target.go` and
  `config_reload.go` (per-target override, hot reload),
  `internal/config/config.go` (YAML field, load-time validation),
  `internal/cmd/reconcile.go` and `internal/daemon/daemon.go` (hydration).
- Remote deploys are unchanged: they have no changed-only selection.
- No new env vars.

## Scope

Spec delta and implementation ship together in one PR.

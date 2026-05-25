## Context

The pre-deploy backup (`internal/reconcile/backup.go`) shells out to `tar`. Issue
#319 showed it can wedge a reconcile forever. The fix is small, but two decisions
warrant recording: how the self-exclusion is expressed to `tar`, and how this
change coexists with the in-flight backup-integrity spec PR #312.

## Decision: exclude by archived-member path, not by container path

`tar` `--exclude` matches against the *member names* tar generates, which are the
absolute source paths it is told to archive (`/mnt/appdata/bosun`, …). The backup
destination is configured as `/app/backups` inside the Bosun container, but that is
a *different mount* than the appdata path tar walks; the same host directory appears
to tar as `/mnt/appdata/bosun/backups`. So a naive `--exclude=/app/backups` would
not match anything tar archives.

The implementation MUST derive the exclude from the destination *as it appears under
the backed-up appdata path*, plus a defensive pattern (e.g. `*/backups/backup-*` and
the `configs.tar.gz` output name). The `backup_test.go` case that asserts "archive
does not contain the backups subtree" is the guard that this matched correctly.

## Decision: timeout is a non-fatal failure, not a new abort path

The existing requirement already says backup failures warn and continue. Rather than
introduce a new fatal timeout behavior, a timeout is funneled into the *same*
non-fatal failure path. This keeps the blast radius minimal and consistent: a slow
backup degrades to "no backup this run" with a warning, never a wedged or aborted
deploy. Default 5m is generous for config-sized backups and well under any operator's
patience threshold.

## Decision: coordinate with PR #312 rather than fold into it

PR #312 (`add-backup-integrity-semantics`) MODIFIES the same `Configuration Backup`
requirement, covering integrity and rollback. This change is kept separate so each
stays independently reviewable (#312 is mid CodeRabbit cycle). The two sets of
modifications are additive and non-contradictory:

- This change: archive must self-exclude; backup runs under a bounded deadline;
  verification respects cancellation.
- #312: deep verification, fail-closed on unverifiable backup, surfaced remote
  errors, retention deferred until after deploy verification.

Whichever merges second rebases its `MODIFIED Configuration Backup` delta onto the
first so the resulting requirement text contains both sets of clauses. The
config-only backup-scope narrowing (issue #319's other suggestion) is deliberately
left to #312's rollback-integrity work, since it changes what rollback restores from.

# Change: Bound the pre-deploy backup with a deadline and a self-exclusion invariant

## Why

On a cold/empty-state reconcile (GitHub #319, hit during the GH#214 cutover after
recreating the Bosun container), the pre-deploy backup step wedges indefinitely and
nothing downstream deploys. Three intersecting causes, all confirmed in code:

1. **Recursive self-inclusion.** Backup paths come from `backupPathsFromTargets`
   (`reconcile.go:1125`) as `LocalAppdataPath/<target>`. Bosun is itself a deploy
   target, so `/mnt/appdata/bosun` is in the list. The tar output lives at
   `BackupDir` (default `/app/backups` → host `/mnt/appdata/bosun/backups/...`),
   *inside* a backed-up path, and `tar` (`backup.go:89`) has no `--exclude`. tar
   archives its own growing output — observed 8 GB and climbing, never terminating.

2. **Context-ignoring verification.** `VerifyBackup` (`backup.go:37`) shells
   `exec.Command("tar","-tzf",...)` — not `CommandContext` — so no caller deadline
   can interrupt it. Listing a multi-GB, concurrently-growing archive hangs.

3. **No per-step deadline.** `createBackup` is the only `Run()` pipeline step not
   wrapped in `context.WithTimeout`; git, ssh, and compose all derive per-step
   deadlines. The creation tar uses `CommandContext` but inherits an unbounded ctx,
   so there is no deadline to honor.

The current spec already says *"Backup failures SHALL log a warning but SHALL NOT
abort the deployment pipeline"* and the call site is already non-fatal. The gap is
that an unbounded hang is neither success nor failure — it never reaches the
non-fatal path. Bounding the backup converts the hang into a loud, non-fatal
failure, which is the behavior the spec already intends.

## What Changes

- **Self-exclusion invariant (MODIFIED Configuration Backup).** The backup archive
  SHALL NOT include the backup destination directory or any prior backup it
  contains. When the destination is nested within a backed-up path, the reconciler
  excludes it.
- **Bounded deadline (MODIFIED Configuration Backup).** Backup creation and
  verification run under a configurable `BackupTimeout` (default 5m, env
  `BOSUN_BACKUP_TIMEOUT`, accepting a Go duration or plain seconds). Timeout is
  treated as a (non-fatal) backup failure.
- **Verification respects cancellation (MODIFIED Configuration Backup).**
  `VerifyBackup` runs under the same context as creation so a deadline aborts it.
- **New configuration surface:** `BackupTimeout` field + `BOSUN_BACKUP_TIMEOUT`.

Out of scope (deferred to coordinate with the in-flight backup-integrity spec PR
#312, which MODIFIES this same requirement): narrowing the backup to config-only
files rather than whole appdata trees. That changes what rollback can restore from,
which is #312's "Backup-Backed Rollback Integrity" territory.

## Impact

- **Affected spec:** `reconcile` — `Configuration Backup` requirement (MODIFIED).
- **Coordination:** PR #312 (`add-backup-integrity-semantics`) also MODIFIES
  `Configuration Backup`. These two changes must be merge-ordered; whichever lands
  second rebases its delta onto the first. The two sets of modifications are
  additive (self-exclusion + deadline here; integrity + rollback there) and do not
  contradict.
- **Accompanying non-spec fixes shipping in the same implementation PR** (no spec
  delta — they do not touch spec'd behavior): thread context into the
  `emergency.go` recovery `runComposeUp`, and cap request-body size on the
  standalone `bosun webhook` receiver (parity with the daemon's existing 1 MB cap).
- **Backwards compatibility:** `BackupTimeout` defaults preserve current behavior
  for any backup that completes within 5 minutes.

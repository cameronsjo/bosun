## 1. Verified, fail-closed backups (#240, #244)

- [ ] 1.1 Strengthen `VerifyBackup` to confirm deep integrity (archive lists AND extracts/round-trips), not merely non-empty + `tar -tzf` exit 0
- [ ] 1.2 In `BackupRemote`, stop discarding `sshErr` and stop piping remote `tar` stderr to `/dev/null`; return the transport error
- [ ] 1.3 Make backup failure fail-closed: abort the reconcile before any target mutation when a required backup cannot be created/verified
- [ ] 1.4 When there is genuinely nothing to back up, return `lastBackupPath == ""` (no empty directory) so rollback skips with a clear "no backup available" message
- [ ] 1.5 Tests: SSH failure mid-stream fails the backup; truncated archive fails verification; empty-paths host yields no rollback target and a clean skip
- [ ] 1.6 Preserve #319's already-merged behavior when reworking backup: keep `BackupTimeout`/`BOSUN_BACKUP_TIMEOUT`, context-bound verification, and the backup self-exclusion; route a timed-out *required* backup through the new fail-closed abort (not warn+continue), while a cold-state reconcile with nothing to back up is unaffected

## 2. Retention preserves last-known-good (#243)

- [ ] 2.1 Defer `CleanupBackups` until after the current deploy passes verification (or keep N+1 internally through the deploy window)
- [ ] 2.2 Tests: with `backups_to_keep == 1`, the cycle T-1 backup survives through cycle T's deploy

## 3. Propagated deploy-prep errors (#250)

- [ ] 3.1 Propagate `EnsureRemoteDir` and `MkdirAll` errors at `reconcile.go:1218,1233,1330,1340,1351`, wrapped to name the failing path/layer
- [ ] 3.2 Tests: with the target path unwritable, the error message includes the path and the underlying cause (e.g. "permission denied")

## 4. Deploy archive integrity (#252)

- [x] 4.1 In the tar-over-SSH path (`ssh.go:224-262`), verify the extracted archive (e.g. file-count parity) before the atomic move; run cleanup on integrity failure, not only on `tarErr != nil`
- [x] 4.2 Tests: a simulated partial/empty archive aborts the move and preserves the existing target

## 5. Backup-backed rollback (#229)

- [ ] 5.1 Route the health-gate / deploy-failure rollback through a distinct path that re-applies the backed-up previous-good compose files, not `ComposeUpMultipleWithRollback` over the new files
- [ ] 5.2 Ensure an idempotent `docker compose up` exit 0 on the failing files is not treated as a successful rollback
- [ ] 5.3 Tests: a critical-container health failure re-deploys the previous-good files from backup; logs reflect actual rollback outcome

## 6. Documentation

- [ ] 6.1 Update `skills/onboard/resources/gitops.md` with backup/rollback fail-closed semantics
- [ ] 6.2 Update `docs/troubleshooting.md` (backup-failure abort, no-backup-available skip)
- [ ] 6.3 Update `CLAUDE.md` reconcile-pipeline notes if backup ordering changes

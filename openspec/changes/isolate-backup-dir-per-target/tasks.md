## 1. Per-target directory

- [x] 1.1 Add `TargetBackupDir` and `Config.BackupRoot` in `internal/reconcile/target.go` and `reconcile.go`; set both in `ConfigForTarget` for named targets
- [x] 1.2 Pass the shared root to the local and remote backup writers so the archive excludes it (`internal/reconcile/backup.go`)

## 2. Legacy backups

- [x] 2.1 Count legacy `backup-YYYYMMDD-HHMMSS` directories holding an archive directly under the root and log one warning per run for named targets
- [x] 2.2 Match backups by the exact name `backup-YYYYMMDD-HHMMSS` in the anchor, retention, and legacy scans, and reject a target named that way (`ValidateTargetName`)

## 3. Tests

- [x] 3.1 `TargetBackupDir` and `ConfigForTarget` table tests
- [x] 3.2 Reconciler-level tests for per-target creation, anchor recovery, and retention
- [x] 3.3 Local and remote shared-root exclusion tests, and a fault-injection test for the path-resolution failure

## 4. Docs

- [x] 4.1 Update `skills/onboard/resources/{gitops,configuration,commands}.md` and `docs/{gitops,cli-reference,commands,error-handling}.md`: layout, restore with `BACKUP_DIR=<root>/<target>`, upgrade note and cleanup command

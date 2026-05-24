## Tasks

Implementation begins only after this proposal reaches the `ready-to-build` label.
Each numbered group is a distinct commit.

## 1. Self-exclusion in the backup tar

- [x] `internal/reconcile/backup.go` — `Backup()`: compute and pass `--exclude` for the
      backup destination so the creation tar cannot archive its own output tree
- [x] Confirm the archived-member path form (absolute `/mnt/appdata/...`) and make the
      exclude pattern match that form, not the container-local `/app/backups`
- [x] `internal/reconcile/backup.go` — `BackupRemote()`: apply the same exclude to the
      `tar -czf -` over SSH
- [x] `internal/reconcile/backup_test.go` — backup dir nested under a backed-up path
      produces an archive that does NOT contain the backups subtree

## 2. Context-aware verification

- [x] `internal/reconcile/backup.go` — `VerifyBackup(ctx, backupPath)` using
      `exec.CommandContext`
- [x] Update callers (`Backup`, `BackupRemote`) and the `DeployOps` / interface
      signature in `internal/reconcile/interfaces.go` (VerifyBackup is an internal
      helper, not part of the `DeployOps` interface — only the concrete method and
      its 6 callers needed updating)
- [x] `internal/reconcile/backup_test.go` — `VerifyBackup` honors a cancelled ctx

## 3. Bounded backup deadline

- [ ] `internal/reconcile/reconcile.go` — add `BackupTimeout time.Duration` to `Config`
- [ ] `internal/reconcile/target.go` — default `BackupTimeout` to 5m in config defaults
- [ ] Parse `BOSUN_BACKUP_TIMEOUT` (Go duration or plain seconds) mirroring the
      `BOSUN_COMPOSE_UP_TIMEOUT` pattern
- [ ] `internal/reconcile/reconcile.go` — `createBackup` wraps ctx in
      `context.WithTimeout(ctx, BackupTimeout)`; timeout → warn + continue
- [ ] `internal/reconcile/backup_test.go` — `createBackup` returns within `BackupTimeout`
      when tar would hang (inject a slow path / fake `DeployOps`)

## 4. Accompanying non-spec Tier-1 fixes (same PR)

- [ ] `internal/cmd/emergency.go` — `runComposeUp(ctx, composeFile)` using
      `exec.CommandContext`; derive ctx from `cmd.Context()` + a sensible timeout
- [ ] `internal/cmd/webhook.go` — wrap the 5 `io.ReadAll(r.Body)` calls in
      `io.LimitReader(r.Body, maxWebhookBodySize)` and set `Server.MaxHeaderBytes`
- [ ] Tests: `runComposeUp` honors a cancelled ctx; webhook receiver truncates a body
      over `maxWebhookBodySize` (table-driven over the 5 handlers)

## 5. Docs + validation

- [ ] `skills/onboard/resources/gitops.md` — backup deadline + self-exclusion behavior
- [ ] `AGENTS.md`/`CLAUDE.md` env-var table — `BOSUN_BACKUP_TIMEOUT`
- [ ] `make test`
- [ ] `openspec validate add-backup-deadline-and-self-exclusion --strict`

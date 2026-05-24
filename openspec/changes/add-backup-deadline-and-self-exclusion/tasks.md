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

- [x] `internal/reconcile/reconcile.go` — add `BackupTimeout time.Duration` to `Config`
- [x] `internal/reconcile/target.go` — default `BackupTimeout` to 5m in config defaults
      (via `DefaultBackupTimeout` const in `backup.go`)
- [x] Parse `BOSUN_BACKUP_TIMEOUT` (Go duration or plain seconds) mirroring the
      `BOSUN_COMPOSE_UP_TIMEOUT` pattern (in `internal/daemon/daemon.go`)
- [x] `internal/reconcile/reconcile.go` — `createBackup` wraps ctx in
      `context.WithTimeout(ctx, BackupTimeout)`; timeout → warn + continue
- [x] `internal/reconcile/backup_test.go` — `createBackup` returns within `BackupTimeout`
      when tar would hang (expired-budget timeout, real `*DeployOps`); plus
      `TestConfigFromEnv_BackupTimeout` covering the env-parse branch

## 4. Accompanying non-spec Tier-1 fixes (same PR)

- [x] `internal/cmd/emergency.go` — `runComposeUp(ctx, composeFile)` using
      `exec.CommandContext`; derive ctx from `cmd.Context()` + a sensible timeout
      (shipped via PR #321 / bosun-9nm)
- [x] `internal/cmd/webhook.go` — wrap the 5 `io.ReadAll(r.Body)` calls in
      `http.MaxBytesReader(w, r.Body, maxWebhookBodySize)` and set `Server.MaxHeaderBytes`
      (shipped via PR #321 / bosun-9nm)
- [x] Tests: `runComposeUp` honors a cancelled ctx; webhook receiver rejects a body
      over `maxWebhookBodySize` (table-driven over the 5 handlers) — `wedge_class_test.go`
      (shipped via PR #321 / bosun-9nm)

## 5. Docs + validation

- [x] `skills/onboard/resources/gitops.md` — backup deadline + self-exclusion behavior
- [x] `AGENTS.md`/`CLAUDE.md` env-var table — `BOSUN_BACKUP_TIMEOUT`
- [x] `make test` — new tests green. One pre-existing failure,
      `TestReconcilerRunFullSuccess/full_non-dry-run…`, is environment-specific:
      it requires a live Docker daemon and failed only because the local daemon
      was down. The backup stage logged success in that run; the failure was the
      downstream `docker compose up` reaching an unreachable daemon. CI provides
      Docker, so it passes there. Not a regression and not a follow-up item.
- [x] `openspec validate add-backup-deadline-and-self-exclusion --strict` — valid

# Change: Isolate BackupDir per named target

## Why

Named targets share one `BackupDir`, although state file, staging directory, and lock file are already per target. One target can therefore pick another target's backup as its rollback anchor, prune or delete another target's backups (an in-progress archive reads as corrupt and is removed), and collide on a same-second `backup-YYYYMMDD-HHMMSS` name (cameronsjo/bosun#791).

## What Changes

- A named target derives its backup directory as `<BackupDir>/<target-name>`; the implicit and lone `default` target keep `BackupDir` unchanged.
- Anchor recovery, retention, and corrupt-backup removal act on the target's own directory only.
- Archive self-exclusion covers the shared backup root, not only the target's own directory, so a full-appdata fallback never archives a sibling's backups.
- Backups that earlier releases wrote directly under the shared root are neither used nor pruned by a named target. A warning is logged; the operator removes them.
- Scans of a backup directory treat only the exact name `backup-YYYYMMDD-HHMMSS` as a backup, so a target directory such as `backup-pi` is never pruned as a corrupt backup. A target named exactly that shape is rejected.
- No per-target override, migration, or new environment variable.

## Impact

- Affected specs: `reconcile` (ADDED only).
- Affected code: `internal/reconcile/target.go`, `reconcile.go`, `backup.go`; docs and `skills/onboard/resources/*`.
- Upgrade: pre-existing `<BackupDir>/backup-YYYYMMDD-HHMMSS` directories of a multi-target deployment are orphaned until removed. A target already named exactly like one (very unlikely) fails target resolution and must be renamed. Per current code (`applyBackupFailurePolicy`), the first failed backup of a named target aborts its deploy, because no verified anchor exists in its new directory.

## Relationship to other changes

The requirement "Configuration Backup" is already modified by two unarchived changes and by open draft PR cameronsjo/bosun#824:

- `add-backup-integrity-semantics` modifies "Configuration Backup" (fail-closed backup, deep verification).
- `data-driven-deploy-paths` modifies "Configuration Backup" (deploy-path-driven backup scope).
- Draft PR cameronsjo/bosun#824 edits both of those deltas and `openspec/specs/reconcile/spec.md`.
- `add-multi-target-reconcile` (ADDED only) already says canonical backup rules apply independently to each target (Per-Target State Persistence). It defines no backup layout, so this change agrees with it and adds the layout.

This change adds new requirements and modifies none of them. Archive replaces a modified requirement as a whole, so a third whole-block modification would be overwritten by, or would overwrite, the others. The new requirements read against whatever text "Configuration Backup" has when they archive: they scope its create, anchor, prune, and exclude operations to one target's directory and widen the exclusion to the shared root. Archive order does not matter.

The existing "Backup failure does not block deploy" scenario disagrees with the code, which aborts when no verified anchor exists (`applyBackupFailurePolicy`). `add-backup-integrity-semantics` resolves that; this change neither depends on nor changes it.

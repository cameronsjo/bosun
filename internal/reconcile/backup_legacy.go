package reconcile

import (
	"context"
	"os"
	"path/filepath"

	"github.com/cameronsjo/bosun/internal/log"
)

// legacySharedBackupCount counts backups an earlier release wrote directly under
// the shared backup root: a backup-YYYYMMDD-HHMMSS directory that itself holds
// configs.tar.gz, the exact shape Backup creates. A named target's own
// directory (<root>/backup-pi) is not a backup name, and ValidateTargetName
// refuses a target named like one.
//
// An unreadable or missing root counts as zero. A missing root is the normal
// first-run state; a root that cannot be read also fails the backup's own
// MkdirAll, which reports it.
func legacySharedBackupCount(root string) int {
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() || !isBackupDirName(e.Name()) {
			continue
		}
		info, statErr := os.Lstat(filepath.Join(root, e.Name(), "configs.tar.gz"))
		if statErr == nil && info.Mode().IsRegular() {
			count++
		}
	}
	return count
}

// warnLegacySharedBackups logs one warning per call when backups from before
// per-target backup directories remain under root. Named targets never select,
// count, or prune them (#791), so they stay on disk until the operator removes
// them.
func warnLegacySharedBackups(ctx context.Context, root string) {
	count := legacySharedBackupCount(root)
	if count == 0 {
		return
	}
	logger := log.ComponentCtx(ctx, log.ComponentDeploy)
	logger.Warn().
		Str(log.FieldPath, root).
		Int("legacy_backups", count).
		Msg("Legacy shared backups remain under the backup root and are not used by named targets; remove them once each target has a fresh verified backup")
}

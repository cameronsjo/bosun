package reconcile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cameronsjo/bosun/internal/fileutil"
	"github.com/cameronsjo/bosun/internal/log"
)

// RollbackSet describes a full managed-tree restore from a backup archive — the
// single chokepoint the health-gate rollback funnels through (#445). It widens
// rollback beyond the compose files: a failed deploy's appdata config writes are
// reverted too, so a rollback no longer leaves a hybrid tree (old compose paired
// with the failed deploy's new config).
//
// ComposeUpIsolated keeps its OWN compose-only per-file rollback and does NOT go
// through this set — its rollback is a targeted restart of a single failed
// stack, not a whole-tree revert.
type RollbackSet struct {
	// Files are the LIVE absolute paths of every managed file this deploy wrote
	// (DeployResult.ManagedFiles joined under the appdata root). Each is restored
	// from its backed-up copy. A file ABSENT from the backup was added by the
	// failed deploy; it is removed only when DeleteMissing is set.
	Files []string

	// ComposeFiles are the LIVE compose file paths to `docker compose up` after
	// the tree is restored — the restart, run LAST so it acts on the fully
	// reverted config.
	ComposeFiles []string

	// Root is the appdata directory every entry in Files must resolve within. It
	// backstops the DELETE and restore paths: even though Files are derived from
	// ManagedFiles (always inside appdata today), an explicit containment check
	// means a future absolute-path-target regression can't silently reopen the
	// filepath.Join prefix-drop footgun on a path we os.Remove.
	Root string

	// DeleteMissing removes live managed files absent from the backup (files the
	// failed deploy added). Default false, and safe to enable ONLY against a
	// fresh anchor — this deploy's own pre-deploy backup, tracked by the
	// Reconciler's lastBackupIsFresh bool. A stale fallback anchor predates files
	// a later legitimate deploy added, so deleting "missing" files against it
	// would destroy real data (the #331 incident class).
	DeleteMissing bool
}

// RollbackFromBackupSet restores a full managed tree from the backup archive at
// backupPath, then re-applies the restored compose files. Order:
//  1. copy every backed-up managed file over its live counterpart
//  2. (opt-in) delete live managed files absent from the backup
//  3. `docker compose up` on the restored compose files (LAST)
//
// A per-file failure does NOT abort the restore — failures are collected and
// returned via errors.Join, so one unreadable file cannot strand the rest of the
// tree half-restored. Returns errRollbackNotAttempted when there is no usable
// backup, mirroring the prior compose-only path.
func (d *DeployOps) RollbackFromBackupSet(ctx context.Context, set RollbackSet, backupPath string) error {
	logger := log.ComponentCtx(ctx, log.ComponentDeploy)

	if backupPath == "" {
		logger.Warn().Msg("No backup available for rollback")
		return fmt.Errorf("%w: no backup available for rollback", errRollbackNotAttempted)
	}

	// Independent timeout so the restore runs even if ctx was cancelled by the
	// gate failure; the enriched logger flows reconcile_id into rollback logs.
	rollbackCtx, cancel := context.WithTimeout(
		log.WithContext(context.Background(), log.Ctx(ctx)),
		d.composeUpTimeout(),
	)
	defer cancel()

	root, cleanup, extractErr := extractBackupArchive(rollbackCtx, backupPath)
	if extractErr != nil {
		logger.Warn().Err(extractErr).Str(log.FieldPath, backupPath).Msg("No backup archive available for rollback")
		return fmt.Errorf("%w: no backup available for rollback: %v", errRollbackNotAttempted, extractErr)
	}
	defer cleanup()

	if d.DryRun {
		logger.Debug().Int("files", len(set.Files)).Msg("Dry run: would restore managed tree from backup")
		return nil
	}

	var errs []error
	restored, deleted := 0, 0

	// Steps 1 + 2: restore each managed file from its backed-up copy, or (opt-in)
	// delete a file the failed deploy added that the backup does not contain.
	for _, live := range set.Files {
		// Defense-in-depth on the DELETE and restore paths: refuse any target
		// that escapes the appdata root before we os.Remove or CopyFile it. One
		// guard at the loop head gates both branches — every Remove and every
		// CopyFile below is preceded by it.
		if !withinAppdata(set.Root, live) {
			errs = append(errs, fmt.Errorf("refuse to touch %q: outside appdata root %q", live, set.Root))
			continue
		}
		backupFile, ok := resolveBackupFile(root, live)
		if !ok {
			if set.DeleteMissing {
				if err := os.Remove(live); err != nil && !os.IsNotExist(err) {
					errs = append(errs, fmt.Errorf("delete added file %q: %w", live, err))
				} else if err == nil {
					deleted++
				}
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(live), 0o755); err != nil {
			errs = append(errs, fmt.Errorf("create parent for %q: %w", live, err))
			continue
		}
		if err := fileutil.CopyFile(backupFile, live); err != nil {
			errs = append(errs, fmt.Errorf("restore %q: %w", live, err))
			continue
		}
		restored++
	}

	// Step 3: re-apply the restored compose files LAST — they now hold the
	// backed-up config, so compose up brings services back to the prior state.
	if len(set.ComposeFiles) > 0 {
		args := d.composeUpArgs(set.ComposeFiles)
		cmd := exec.CommandContext(rollbackCtx, "docker", args...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			errs = append(errs, fmt.Errorf("rollback compose up: %w: %s", err, stderr.String()))
		}
	}

	if len(errs) > 0 {
		joined := errors.Join(errs...)
		logger.Error().
			Err(joined).
			Int("restored", restored).
			Int("deleted", deleted).
			Str(log.FieldPath, backupPath).
			Msg("CRITICAL: rollback completed with errors")
		return fmt.Errorf("rollback failed: %w", joined)
	}

	logger.Info().
		Int("restored", restored).
		Int("deleted", deleted).
		Bool("delete_missing", set.DeleteMissing).
		Str(log.FieldPath, backupPath).
		Msg("Rollback restored managed tree from backup")
	return nil
}

// withinAppdata reports whether target resolves inside root — the containment
// guard the DELETE and restore paths run before touching any file. Files are
// derived from ManagedFiles (always inside appdata today), so this never trips
// legitimately; it exists so a future absolute-path-target regression can't
// silently reopen the filepath.Join prefix-drop footgun on a path we os.Remove
// (filepath.Join("appdata", "/abs") == "/abs" — a target that escapes root).
func withinAppdata(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

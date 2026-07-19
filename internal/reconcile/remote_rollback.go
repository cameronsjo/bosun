package reconcile

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cameronsjo/bosun/internal/log"
)

// RollbackRemoteCompose restores the remote compose dir from the backup anchor
// after a failed remote `docker compose up`, then re-deploys and restarts (#340).
// It is the remote counterpart to the local rollback path. The re-push goes
// through the SAME hardened DeployRemote, so the transfer integrity check (#334)
// covers the restored tree automatically.
//
// deployErr is the original compose-up failure being recovered from; it is
// wrapped into the returned sentinel. RollbackRemoteCompose ALWAYS returns a
// non-nil error:
//   - ErrRollbackSucceeded (wrapping deployErr) when the backup was restored and
//     re-applied cleanly, and
//   - ErrRollbackFailed (wrapping both errors) when any rollback step failed.
//
// remoteComposeDir is the live remote compose dir the failed deploy just wrote —
// passed by the caller where it is in scope, never derived from r.lastComposeFiles
// (only deployLocal sets that).
func (d *DeployOps) RollbackRemoteCompose(ctx context.Context, host, remoteComposeDir, backupPath string, deployErr error) error {
	logger := log.ComponentCtx(ctx, log.ComponentDeploy)

	failed := func(rollbackErr error) error {
		logger.Error().
			Err(rollbackErr).
			Str(log.FieldTarget, host).
			Str(log.FieldPath, remoteComposeDir).
			Msg("CRITICAL: remote rollback failed after compose-up failure")
		return fmt.Errorf("%w: deployment error: %v, rollback error: %v", ErrRollbackFailed, deployErr, rollbackErr)
	}

	if backupPath == "" {
		return failed(errors.New("no backup anchor available for rollback"))
	}

	// Interim stale-anchor observability (#340): surface the anchor's age before
	// restoring. Full freshness gating lands in a later PR; here we only log it.
	logBackupAnchorAge(ctx, backupPath)

	// The anchor must be trustworthy before we restore from it.
	if err := d.VerifyBackup(ctx, backupPath); err != nil {
		return failed(fmt.Errorf("backup anchor failed verification: %w", err))
	}

	// Path-traversal guard: validate every archive member stays within the
	// extraction root BEFORE extracting, so a crafted `..`/absolute name cannot
	// escape the temp dir when tar writes it.
	tarFile := filepath.Join(backupPath, "configs.tar.gz")
	if err := assertArchiveEntriesWithinRoot(ctx, tarFile); err != nil {
		return failed(fmt.Errorf("backup archive failed path-traversal check: %w", err))
	}

	root, cleanup, err := extractBackupArchive(ctx, backupPath)
	if err != nil {
		return failed(fmt.Errorf("extract backup archive: %w", err))
	}
	defer cleanup()

	// resolveBackupFile accounts for tar's leading-'/' strip, mapping the live
	// remote compose dir to its backed-up copy inside the extracted tree.
	restoredComposeDir, ok := resolveBackupFile(root, remoteComposeDir)
	if !ok {
		return failed(fmt.Errorf("backed-up compose dir %q not found in anchor", remoteComposeDir))
	}

	logger.Warn().
		Str(log.FieldTarget, host).
		Str(log.FieldPath, remoteComposeDir).
		Str("restored_from", restoredComposeDir).
		Msg("Restoring remote compose dir from backup anchor")

	// Re-push through the SAME hardened remote deploy path so the transfer
	// integrity check (#334) applies to the restored tree. The rollback path is
	// independent of the main deploy's probe, so re-probe sha256sum here.
	verifyChecksums := d.remoteHasSha256sum(ctx, host)
	if err := d.DeployRemote(ctx, restoredComposeDir, host, remoteComposeDir, verifyChecksums); err != nil {
		return failed(fmt.Errorf("re-deploy restored compose dir: %w", err))
	}

	// Bring the restored compose dir back up. Unhealthy-only is acceptable here:
	// the previous config is restored, so an unhealthy container is a pre-existing
	// condition rather than a rollback failure.
	if err := d.ComposeUpRemote(ctx, host, remoteComposeDir); err != nil && !errors.Is(err, ErrComposeUnhealthy) {
		return failed(fmt.Errorf("compose up after restore: %w", err))
	}

	logger.Info().
		Str(log.FieldTarget, host).
		Str(log.FieldPath, remoteComposeDir).
		Msg("Remote rollback succeeded: restored and re-applied backup anchor")
	return fmt.Errorf("%w: %v", ErrRollbackSucceeded, deployErr)
}

// logBackupAnchorAge logs the mtime-based age of a backup anchor's archive.
// Best-effort observability: a stat failure is warned, not fatal.
func logBackupAnchorAge(ctx context.Context, backupPath string) {
	logger := log.ComponentCtx(ctx, log.ComponentDeploy)
	tarFile := filepath.Join(backupPath, "configs.tar.gz")
	info, err := os.Stat(tarFile)
	if err != nil {
		logger.Warn().Err(err).Str(log.FieldPath, tarFile).
			Msg("Could not stat backup anchor to report its age before rollback")
		return
	}
	logger.Warn().
		Str(log.FieldPath, backupPath).
		Time("anchor_mtime", info.ModTime()).
		Dur("anchor_age", time.Since(info.ModTime())).
		Msg("Rolling back to backup anchor (interim age observability; freshness gating arrives in a later PR)")
}

// assertArchiveEntriesWithinRoot reads a gzip-compressed tar archive's member
// headers and fails if any entry would escape the extraction root once tar's
// leading-'/' strip is applied. Runs under ctx so a deadline/cancellation aborts
// the scan. It reads only headers, not member bodies — decompression-bomb bounds
// are the concern of verifyArchiveIntegrity, which VerifyBackup already ran.
func assertArchiveEntriesWithinRoot(ctx context.Context, tarFile string) error {
	f, err := os.Open(tarFile)
	if err != nil {
		return fmt.Errorf("cannot open archive: %w", err)
	}
	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("cannot read gzip header: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("cannot read archive entry header: %w", err)
		}
		if !archiveNameWithinRoot(hdr.Name) {
			return fmt.Errorf("archive entry escapes extraction root: %q", hdr.Name)
		}
	}
	return nil
}

// archiveNameWithinRoot reports whether a tar member name stays within the
// extraction root once tar's leading-'/' strip is applied. Rejects absolute
// paths and any name whose cleaned form climbs above root via '..'.
func archiveNameWithinRoot(name string) bool {
	// Mirror the leading-slash strip extractBackupArchive/resolveBackupFile rely on.
	stripped := strings.TrimPrefix(filepath.ToSlash(name), "/")
	if stripped == "" {
		return true
	}
	cleaned := filepath.ToSlash(filepath.Clean(stripped))
	if filepath.IsAbs(cleaned) {
		return false
	}
	return cleaned != ".." && !strings.HasPrefix(cleaned, "../")
}

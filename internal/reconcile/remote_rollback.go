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

	// The anchor must be trustworthy before we restore from it (truncation and
	// decompression-bomb bounds).
	if err := d.VerifyBackup(ctx, backupPath); err != nil {
		return failed(fmt.Errorf("backup anchor failed verification: %w", err))
	}

	// Extract in-process with a path-safe extractor that validates each entry's
	// REALIZED path — name AND, for links, target — at write time. One reader
	// does both validation and extraction, so there is no scan-then-`tar -xzf`
	// divergence where two tar parsers disagree on where an entry lands. It
	// rejects any symlink/hardlink whose target escapes the extraction root, so
	// the classic "symlink to /etc/cron.d then write a file through it" escape
	// cannot land on the root-owned remote FS after the re-push (#448).
	tarFile := filepath.Join(backupPath, "configs.tar.gz")
	root, cleanup, err := safeExtractBackup(ctx, tarFile)
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
	// integrity checks (#252, #334) apply to the restored tree too.
	if err := d.DeployRemote(ctx, restoredComposeDir, host, remoteComposeDir); err != nil {
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

// safeExtractBackup extracts a gzip-compressed tar into a fresh temp dir with
// Go's archive/tar for local and remote rollback consumers. It validates each
// entry's realized path at write time so no member can escape the extraction
// root — via its name, or via a symlink / hardlink target. It is the single
// reader for both validation and extraction,
// which avoids the divergence a header pre-scan followed by external `tar -xzf`
// would leave (two independent parsers, a PAX/GNU-longname or Linkname mismatch
// landing where the scan blessed something else). The extracted layout matches
// what resolveBackupFile expects (leading-'/' stripped names under root).
//
// Total decompressed bytes are re-bounded here (ErrBackupTooLarge) even though
// VerifyBackup already ran, because this is a second, independent read. Returns
// the root, an always-safe cleanup func, and any error.
func safeExtractBackup(ctx context.Context, tarFile string) (root string, cleanup func(), err error) {
	return safeExtractBackupBounded(ctx, tarFile, MaxVerifyDecompressedBytes)
}

// safeExtractBackupBounded is safeExtractBackup with an explicit total
// decompressed-byte budget, so the bomb-bound branch is exercisable in tests.
func safeExtractBackupBounded(ctx context.Context, tarFile string, maxBytes int64) (root string, cleanup func(), err error) {
	return safeExtractBackupBoundedWithWriter(ctx, tarFile, maxBytes, writeRegularEntry)
}

func safeExtractBackupBoundedWithWriter(
	ctx context.Context,
	tarFile string,
	maxBytes int64,
	writeEntry func(context.Context, string, io.Reader) (int64, error),
) (root string, cleanup func(), err error) {
	noop := func() {}
	if maxBytes < 0 {
		return "", noop, fmt.Errorf("%w: %s (limit %d bytes)", ErrBackupTooLarge, tarFile, maxBytes)
	}

	f, err := os.Open(tarFile)
	if err != nil {
		return "", noop, fmt.Errorf("cannot open archive: %w", err)
	}
	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", noop, fmt.Errorf("cannot read gzip header: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tmp, err := os.MkdirTemp("", "bosun-rollback-*")
	if err != nil {
		return "", noop, fmt.Errorf("cannot create rollback temp dir: %w", err)
	}
	cleanupTmp := func() { _ = os.RemoveAll(tmp) }

	// Bound and cancel the entire decompressed stream below tar.Reader. This is
	// load-bearing: tar.Reader may consume entry bodies itself while advancing to
	// the next header, including bodies for entry types this extractor skips.
	// Counting here includes headers, padding, every body, and trailing bytes.
	stream := &boundedContextReader{ctx: ctx, reader: gz, remaining: maxBytes}
	tr := tar.NewReader(stream)
	for {
		if ctxErr := ctx.Err(); ctxErr != nil {
			cleanupTmp()
			return "", noop, ctxErr
		}
		hdr, nextErr := tr.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			cleanupTmp()
			if errors.Is(nextErr, ErrBackupTooLarge) {
				return "", noop, fmt.Errorf("cannot read archive entry header: %w: %s (limit %d bytes)",
					ErrBackupTooLarge, tarFile, maxBytes)
			}
			return "", noop, fmt.Errorf("cannot read archive entry header: %w", nextErr)
		}

		dest, ok := resolveWithinRoot(tmp, hdr.Name)
		if !ok {
			cleanupTmp()
			return "", noop, fmt.Errorf("archive entry escapes extraction root: %q", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if mkErr := os.MkdirAll(dest, 0o755); mkErr != nil {
				cleanupTmp()
				return "", noop, fmt.Errorf("cannot create dir %q: %w", hdr.Name, mkErr)
			}
		case tar.TypeReg:
			if mkErr := os.MkdirAll(filepath.Dir(dest), 0o755); mkErr != nil {
				cleanupTmp()
				return "", noop, fmt.Errorf("cannot create parent for %q: %w", hdr.Name, mkErr)
			}
			_, wErr := writeEntry(ctx, dest, tr)
			if wErr != nil {
				cleanupTmp()
				if errors.Is(wErr, ErrBackupTooLarge) {
					return "", noop, fmt.Errorf("cannot extract %q: %w: %s (limit %d bytes)",
						hdr.Name, ErrBackupTooLarge, tarFile, maxBytes)
				}
				return "", noop, fmt.Errorf("cannot extract %q: %w", hdr.Name, wErr)
			}
		case tar.TypeSymlink, tar.TypeLink:
			// Reject any link whose target escapes root BEFORE creating it, so a
			// later entry cannot be written through it to an outside path.
			if !linkTargetWithinRoot(tmp, dest, hdr.Typeflag, hdr.Linkname) {
				cleanupTmp()
				return "", noop, fmt.Errorf("archive %s target escapes extraction root: %q -> %q",
					linkKind(hdr.Typeflag), hdr.Name, hdr.Linkname)
			}
			if mkErr := os.MkdirAll(filepath.Dir(dest), 0o755); mkErr != nil {
				cleanupTmp()
				return "", noop, fmt.Errorf("cannot create parent for %q: %w", hdr.Name, mkErr)
			}
			if lErr := writeLinkEntry(tmp, dest, hdr); lErr != nil {
				cleanupTmp()
				return "", noop, fmt.Errorf("cannot extract %s %q: %w", linkKind(hdr.Typeflag), hdr.Name, lErr)
			}
		default:
			// Devices, FIFOs, etc: a config backup never legitimately needs them.
			continue
		}
	}

	// tar.Reader's logical EOF only proves that it saw the tar end markers. Drain
	// the same bounded reader to the gzip stream's true EOF so gzip validates its
	// trailer checksum and size. The drain also bounds and observes cancellation
	// for trailing decompressed data that tar itself does not consume.
	if _, drainErr := copyCtx(ctx, io.Discard, stream); drainErr != nil {
		cleanupTmp()
		if errors.Is(drainErr, ErrBackupTooLarge) {
			return "", noop, fmt.Errorf("cannot finish archive stream: %w: %s (limit %d bytes)",
				ErrBackupTooLarge, tarFile, maxBytes)
		}
		return "", noop, fmt.Errorf("cannot finish archive stream: %w", drainErr)
	}

	return tmp, cleanupTmp, nil
}

// boundedContextReader enforces cancellation and a total byte budget at the
// decompressed-stream boundary. It permits one byte beyond the limit so callers
// can distinguish exact-boundary EOF from overflow without draining an
// attacker-controlled stream.
type boundedContextReader struct {
	ctx       context.Context
	reader    io.Reader
	remaining int64
}

func (r *boundedContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) == 0 {
		return 0, nil
	}
	if r.remaining < 0 {
		return 0, ErrBackupTooLarge
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining+1]
	}

	n, err := r.reader.Read(p)
	r.remaining -= int64(n)
	if r.remaining < 0 {
		return n, ErrBackupTooLarge
	}
	return n, err
}

// resolveWithinRoot maps a tar member name to its extraction path under root,
// mirroring tar's leading-'/' strip. It REJECTS (returns false) any name whose
// cleaned relative form climbs out via `..` or is absolute, rather than silently
// clamping it — bosun's own backups never contain such names, so one is a signal
// to fail loudly, not to remap.
func resolveWithinRoot(root, name string) (string, bool) {
	stripped := strings.TrimPrefix(filepath.ToSlash(name), "/")
	if stripped == "" {
		return root, true // a bare directory entry for the root itself
	}
	rel := filepath.Clean(filepath.FromSlash(stripped))
	if !relWithinRoot(rel) {
		return "", false
	}
	return filepath.Join(root, rel), true
}

// relWithinRoot reports whether a cleaned relative path stays under its base —
// not absolute and not climbing above it via `..`.
func relWithinRoot(rel string) bool {
	if filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

// withinRoot reports whether path is root itself or a descendant of it.
func withinRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return relWithinRoot(rel)
}

// linkTargetWithinRoot reports whether a symlink/hardlink entry's target stays
// within root. Symlink targets resolve against the link's own directory (an
// absolute target always escapes a temp extraction root); hardlink targets are
// archive-relative paths that must not climb above or point outside root.
func linkTargetWithinRoot(root, linkPath string, typeflag byte, linkname string) bool {
	if linkname == "" {
		return false
	}
	switch typeflag {
	case tar.TypeSymlink:
		if filepath.IsAbs(linkname) {
			return false
		}
		resolved := filepath.Join(filepath.Dir(linkPath), filepath.FromSlash(linkname))
		return withinRoot(root, resolved)
	case tar.TypeLink:
		stripped := strings.TrimPrefix(filepath.ToSlash(linkname), "/")
		rel := filepath.Clean(filepath.FromSlash(stripped))
		return relWithinRoot(rel)
	default:
		return false
	}
}

// writeRegularEntry writes one regular file from the already bounded and
// context-aware decompressed stream. Returns the number of bytes written.
func writeRegularEntry(ctx context.Context, dest string, r io.Reader) (int64, error) {
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, err
	}
	n, cErr := copyCtx(ctx, out, r)
	closeErr := out.Close()
	if cErr != nil {
		return n, cErr
	}
	return n, closeErr
}

// writeLinkEntry creates a symlink or hardlink entry. The caller has already
// validated the target stays within root.
func writeLinkEntry(root, dest string, hdr *tar.Header) error {
	_ = os.Remove(dest) // defensive: a well-formed backup won't collide
	if hdr.Typeflag == tar.TypeSymlink {
		return os.Symlink(filepath.FromSlash(hdr.Linkname), dest)
	}
	rel := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(filepath.ToSlash(hdr.Linkname), "/")))
	return os.Link(filepath.Join(root, rel), dest)
}

// linkKind names a link typeflag for error messages.
func linkKind(typeflag byte) string {
	if typeflag == tar.TypeSymlink {
		return "symlink"
	}
	return "hardlink"
}

package reconcile

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cameronsjo/bosun/internal/log"
	"github.com/kballard/go-shellquote"
)

// DefaultBackupTimeout bounds backup creation + verification when no
// BackupTimeout is configured. Prevents the pre-deploy backup step from
// wedging the reconcile indefinitely (#319).
const DefaultBackupTimeout = 5 * time.Minute

// MaxVerifyDecompressedBytes caps the TOTAL decompressed size that
// verifyArchiveIntegrity will read across an entire archive. A crafted gzip/tar
// member can decompress to tens of GB (a decompression bomb), spinning CPU past
// BackupTimeout during the integrity read-through (#240). The cap is a TOTAL
// across all members (not per-member) because a bomb can be split across many
// members each individually small — the total is the true bound on verify work.
// 10 GiB is far above any real bosun config backup (rendered compose/config
// files) yet small enough to reject a bomb well before it exhausts memory or CPU.
const MaxVerifyDecompressedBytes int64 = 10 << 30 // 10 GiB

// ErrBackupTooLarge is returned when a backup archive decompresses past
// MaxVerifyDecompressedBytes during verification — a likely decompression bomb.
var ErrBackupTooLarge = errors.New("backup archive exceeds maximum verifiable decompressed size")

// VerifyBackup checks that a backup archive is valid and non-empty.
// The archive listing runs under ctx so a caller deadline or cancellation
// aborts verification rather than blocking on a large/growing archive (#319).
func (d *DeployOps) VerifyBackup(ctx context.Context, backupPath string) error {
	logger := log.ComponentCtx(ctx, log.ComponentDeploy)
	tarFile := filepath.Join(backupPath, "configs.tar.gz")

	logger.Debug().Str(log.FieldPath, tarFile).Msg("Verifying backup archive")

	// Check file exists
	info, err := os.Stat(tarFile)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Error().Str(log.FieldPath, tarFile).Msg("Failed to verify backup. Reason: archive not found")
			return fmt.Errorf("backup archive not found: %s", tarFile)
		}
		logger.Error().Err(err).Str(log.FieldPath, tarFile).Msg("Failed to verify backup. Reason: cannot stat archive")
		return fmt.Errorf("failed to stat backup archive: %w", err)
	}

	// Check file is non-empty
	if info.Size() == 0 {
		logger.Error().Str(log.FieldPath, tarFile).Msg("Failed to verify backup. Reason: archive is empty")
		return fmt.Errorf("backup archive is empty: %s", tarFile)
	}

	// Fast pre-check: the archive lists. Runs under ctx so a caller deadline or
	// cancellation aborts here before the full read-through (#319).
	cmd := exec.CommandContext(ctx, "tar", "-tzf", tarFile)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		logger.Error().Err(err).Str(log.FieldPath, tarFile).Msg("Failed to verify backup. Reason: archive is corrupted")
		return fmt.Errorf("backup archive is corrupted: %w: %s", err, stderr.String())
	}

	// Check archive has at least one file
	if strings.TrimSpace(stdout.String()) == "" {
		logger.Error().Str(log.FieldPath, tarFile).Msg("Failed to verify backup. Reason: archive contains no files")
		return fmt.Errorf("backup archive contains no files: %s", tarFile)
	}

	// Integrity check: fully decompress and read every entry to EOF. `tar -tzf`
	// reads member headers but some tar implementations list a truncated archive
	// (all headers present, trailing data lost) without error — a truncated
	// stream a rollback would then trust. Reading through gzip+tar surfaces that
	// truncation (io.ErrUnexpectedEOF) the listing alone misses (#240).
	if err := verifyArchiveIntegrity(ctx, tarFile, MaxVerifyDecompressedBytes); err != nil {
		logger.Error().Err(err).Str(log.FieldPath, tarFile).Msg("Failed to verify backup. Reason: archive integrity check failed")
		return fmt.Errorf("backup archive failed integrity check: %w", err)
	}

	logger.Debug().Str(log.FieldPath, tarFile).Int64("size_bytes", info.Size()).Msg("Backup archive verified")
	return nil
}

// verifyArchiveIntegrity fully reads a gzip-compressed tar archive, decompressing
// and consuming every entry's data to EOF. Unlike `tar -tzf`, which only needs the
// member headers, this forces decompression of every byte so a truncated data
// stream fails with io.ErrUnexpectedEOF instead of being silently accepted (#240).
//
// Two bounds keep a malicious archive from wedging verification (#240):
//   - a TOTAL decompressed-byte budget (maxBytes): each entry is read through an
//     io.LimitReader sized to the remaining budget, so a decompression bomb is
//     stopped just past the cap rather than expanded in full. Overflow returns
//     ErrBackupTooLarge.
//   - context awareness DOWN TO the byte stream: the copy runs in bounded chunks
//     checking ctx between them (copyCtx), so a caller deadline/cancellation
//     interrupts a long member mid-read, not only at entry boundaries.
func verifyArchiveIntegrity(ctx context.Context, tarFile string, maxBytes int64) error {
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
	remaining := maxBytes
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("cannot read archive entry header: %w", err)
		}
		// Consume the entry body to force decompression; a truncated stream fails
		// here rather than being skipped over by a header-only listing. Cap the
		// read at remaining+1 so a bomb is halted one byte past budget instead of
		// decompressed whole, and copy in ctx-checked chunks so the deadline bites
		// mid-member.
		n, err := copyCtx(ctx, io.Discard, io.LimitReader(tr, remaining+1))
		if err != nil {
			return fmt.Errorf("cannot read archive entry body: %w", err)
		}
		remaining -= n
		if remaining < 0 {
			return fmt.Errorf("%w: %s (limit %d bytes)", ErrBackupTooLarge, tarFile, maxBytes)
		}
	}
	return nil
}

// copyCtx copies from src to dst in bounded chunks, checking ctx between chunks so
// a caller deadline or cancellation interrupts a long copy mid-stream rather than
// only at higher-level boundaries. Returns the number of bytes copied.
func copyCtx(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	const chunk = 1 << 20 // 1 MiB — ctx is re-checked at least this often
	buf := make([]byte, chunk)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, rerr := src.Read(buf)
		if n > 0 {
			w, werr := dst.Write(buf[:n])
			total += int64(w)
			if werr != nil {
				return total, werr
			}
		}
		if rerr == io.EOF {
			return total, nil
		}
		if rerr != nil {
			return total, rerr
		}
	}
}

// extractBackupArchive extracts a backup's configs.tar.gz into a fresh temp
// directory and returns its root, a cleanup func, and any error. tar strips the
// leading '/' from absolute member names, so a backed-up "/mnt/appdata/x.yml"
// lands at "<root>/mnt/appdata/x.yml". Resolve a specific backed-up file with
// resolveBackupFile, never filepath.Join(backupPath, base) — the loose-file
// layout that Backup() never produced (#332/#335).
//
// Returns an error if the archive is absent or cannot be extracted; the caller
// treats that as "no backup available". The returned cleanup func is always
// safe to call (a no-op on the error paths).
func extractBackupArchive(ctx context.Context, backupPath string) (root string, cleanup func(), err error) {
	noop := func() {}
	tarFile := filepath.Join(backupPath, "configs.tar.gz")
	if _, statErr := os.Stat(tarFile); statErr != nil {
		return "", noop, fmt.Errorf("backup archive not found: %s: %w", tarFile, statErr)
	}

	tmp, err := os.MkdirTemp("", "bosun-rollback-*")
	if err != nil {
		return "", noop, fmt.Errorf("failed to create rollback temp dir: %w", err)
	}
	cleanupTmp := func() { _ = os.RemoveAll(tmp) }

	cmd := exec.CommandContext(ctx, "tar", "-xzf", tarFile, "-C", tmp)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if runErr := cmd.Run(); runErr != nil {
		cleanupTmp()
		return "", noop, fmt.Errorf("failed to extract backup archive %s: %w: %s", tarFile, runErr, stderr.String())
	}

	return tmp, cleanupTmp, nil
}

// resolveBackupFile maps an original (deployed) file path to its backed-up copy
// inside an extracted backup root, accounting for tar's leading-'/' stripping.
// Returns the resolved path and whether it exists on disk.
func resolveBackupFile(root, originalPath string) (string, bool) {
	candidate := filepath.Join(root, strings.TrimPrefix(filepath.ToSlash(originalPath), "/"))
	if _, err := os.Stat(candidate); err != nil {
		return "", false
	}
	return candidate, true
}

// Backup creates a timestamped tar.gz backup of the specified paths.
func (d *DeployOps) Backup(ctx context.Context, backupDir string, paths []string) (string, error) {
	start := time.Now()
	logger := log.ComponentCtx(ctx, log.ComponentDeploy)

	timestamp := time.Now().Format("20060102-150405")
	backupName := fmt.Sprintf("backup-%s", timestamp)
	backupPath := filepath.Join(backupDir, backupName)

	logger.Info().
		Str(log.FieldOperation, "backup").
		Str(log.FieldPath, backupPath).
		Int("path_count", len(paths)).
		Msg("Creating backup")

	if err := os.MkdirAll(backupPath, 0755); err != nil {
		logger.Error().Err(err).Str(log.FieldPath, backupPath).Msg("Failed to create backup directory")
		return "", fmt.Errorf("failed to create backup directory: %w", err)
	}

	tarFile := filepath.Join(backupPath, "configs.tar.gz")

	// Filter to only existing paths.
	var existingPaths []string
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			existingPaths = append(existingPaths, p)
		}
	}

	if len(existingPaths) == 0 {
		logger.Debug().Str(log.FieldPath, backupPath).Msg("No existing paths to backup")
		return backupName, nil
	}

	// Exclude the backup destination so tar cannot recursively archive its own
	// growing output when backupDir is nested inside a backed-up path (#319).
	// --exclude is matched against the path as tar walks it (the absolute
	// argument), so the absolute form works on both GNU tar and bsdtar.
	args := []string{"-czf", tarFile}
	if absBackupDir, absErr := filepath.Abs(backupDir); absErr == nil {
		args = append(args, "--exclude", absBackupDir)
	} else {
		// Without the exclude, tar can recursively archive its own output —
		// surface the failed safeguard rather than swallowing it.
		logger.Warn().Err(absErr).Str(log.FieldPath, backupDir).
			Msg("Failed to resolve absolute backup path; self-exclusion skipped")
	}
	// Feed the path list to tar via stdin rather than argv: the deployed footprint
	// can be many files, which as an argument list would risk ARG_MAX. Use NUL
	// delimiters (--null) so a filename containing a newline cannot corrupt the
	// list — matching the shell-quoting safety the remote path already applies.
	// Both GNU tar and bsdtar read NUL-separated names from "-" under --null.
	args = append(args, "--null", "-T", "-")
	cmd := exec.CommandContext(ctx, "tar", args...)
	cmd.Stdin = strings.NewReader(strings.Join(existingPaths, "\x00") + "\x00")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	// tar returns non-zero if some files don't exist, which is OK.
	_ = cmd.Run()

	// Verify the backup was created successfully
	if err := d.VerifyBackup(ctx, backupPath); err != nil {
		logger.Error().Err(err).Str(log.FieldPath, backupPath).Msg("Backup verification failed")
		return "", fmt.Errorf("backup verification failed: %w", err)
	}

	logger.Info().
		Str(log.FieldOperation, "backup").
		Str(log.FieldPath, backupPath).
		Int("file_count", len(existingPaths)).
		Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
		Msg("Backup created successfully")

	return backupName, nil
}

// BackupRemote creates a backup from a remote host via SSH.
// Retries on transient SSH errors with exponential backoff.
func (d *DeployOps) BackupRemote(ctx context.Context, host, backupDir string, remotePaths []string) (string, error) {
	start := time.Now()
	logger := log.ComponentCtx(ctx, log.ComponentDeploy)

	logger.Debug().
		Str(log.FieldTarget, host).
		Int("path_count", len(remotePaths)).
		Msg("Preparing to create remote backup")

	if err := validateHost(host); err != nil {
		logger.Error().Err(err).Str(log.FieldTarget, host).Msg("Failed to create remote backup. Reason: invalid SSH host")
		return "", fmt.Errorf("invalid SSH host: %w", err)
	}

	timestamp := time.Now().Format("20060102-150405")
	backupName := fmt.Sprintf("backup-%s", timestamp)
	backupPath := filepath.Join(backupDir, backupName)

	if err := os.MkdirAll(backupPath, 0755); err != nil {
		logger.Error().Err(err).Str(log.FieldPath, backupPath).Msg("Failed to create remote backup. Reason: cannot create backup directory")
		return "", fmt.Errorf("failed to create backup directory: %w", err)
	}

	tarFile := filepath.Join(backupPath, "configs.tar.gz")

	// Build remote tar command with properly quoted paths to prevent shell injection.
	// Exclude the backup destination so the archive cannot recursively include a
	// nested backups subtree (#319), mirroring the local Backup() behavior.
	// A remote path can only be resolved on the remote host — filepath.Abs here
	// would resolve against the LOCAL cwd, which is wrong — so require an absolute
	// backupDir for the exclude to match the path tar walks; skip (with a warning)
	// otherwise rather than emit an exclude that silently matches nothing.
	var sshCmd string
	if filepath.IsAbs(backupDir) {
		sshCmd = fmt.Sprintf("tar -czf - --exclude %s %s",
			shellquote.Join(backupDir), shellquote.Join(remotePaths...))
	} else {
		logger.Warn().Str(log.FieldPath, backupDir).
			Msg("Remote backup destination is not absolute; self-exclusion skipped")
		sshCmd = fmt.Sprintf("tar -czf - %s", shellquote.Join(remotePaths...))
	}

	// Retry with backoff on transient SSH errors. The output file is created
	// (truncating) fresh INSIDE each attempt: a failed attempt that streamed a
	// partial archive must not have its bytes concatenated ahead of a later
	// attempt's stream — that produced a corrupt-but-listable archive. Each
	// attempt starts from an empty file, so on success the file holds exactly
	// one stream.
	sshErr := retryWithBackoff(ctx, DefaultMaxRetries, func() error {
		outFile, err := os.Create(tarFile)
		if err != nil {
			return fmt.Errorf("failed to create backup file: %w", err)
		}

		var stderr bytes.Buffer
		cmd := sshExecCommand(ctx, host, sshCmd)
		cmd.Stdout = outFile
		cmd.Stderr = &stderr
		runErr := cmd.Run()

		// Close before returning so the bytes are flushed regardless of outcome.
		closeErr := outFile.Close()
		if runErr != nil {
			return fmt.Errorf("remote tar failed: %w: %s", runErr, stderr.String())
		}
		if closeErr != nil {
			return fmt.Errorf("failed to close backup file: %w", closeErr)
		}
		return nil
	})

	// A non-nil SSH/tar error means the archive is untrustworthy even if it
	// happens to list — treat it as fatal rather than verifying a possibly
	// truncated stream. Remove the partial archive so no rollback trusts it (#240).
	if sshErr != nil {
		_ = os.RemoveAll(backupPath)
		logger.Error().Err(sshErr).Str(log.FieldTarget, host).Msg("Failed to create remote backup. Reason: remote tar command failed")
		return "", fmt.Errorf("remote backup failed: %w", sshErr)
	}

	// Verify the backup was created successfully
	if err := d.VerifyBackup(ctx, backupPath); err != nil {
		// Clean up invalid backup on verification failure
		_ = os.RemoveAll(backupPath)
		logger.Error().Err(err).Str(log.FieldPath, backupPath).Msg("Failed to create remote backup. Reason: verification failed")
		return "", fmt.Errorf("backup verification failed: %w", err)
	}

	logger.Info().
		Str(log.FieldTarget, host).
		Str(log.FieldPath, backupPath).
		Int64(log.FieldDurationMS, log.DurationMS(start)).
		Msg("Successfully created remote backup")

	return backupName, nil
}

// LatestVerifiedBackup returns the path to the most recent backup directory
// under backupDir whose archive passes VerifyBackup, scanning newest-first.
// It is the rollback-anchor fallback when a fresh backup fails: a deploy must
// never proceed without a verified anchor, so the caller uses this to recover a
// prior good backup (and aborts if none qualifies) (#240).
//
// Verification runs under ctx; a caller deadline/cancellation aborts the scan.
func (d *DeployOps) LatestVerifiedBackup(ctx context.Context, backupDir string) (string, error) {
	logger := log.ComponentCtx(ctx, log.ComponentDeploy)

	entries, err := os.ReadDir(backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("no backup directory at %s", backupDir)
		}
		return "", fmt.Errorf("cannot read backup directory %s: %w", backupDir, err)
	}

	var backups []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "backup-") {
			backups = append(backups, e.Name())
		}
	}
	// Names embed a sortable timestamp (backup-YYYYMMDD-HHMMSS); sort ascending
	// then walk from the end so the newest verified backup wins.
	sort.Strings(backups)
	for i := len(backups) - 1; i >= 0; i-- {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		path := filepath.Join(backupDir, backups[i])
		if verr := d.VerifyBackup(ctx, path); verr != nil {
			logger.Warn().Err(verr).Str(log.FieldPath, path).
				Msg("Skipping unverifiable prior backup while searching for a rollback anchor")
			continue
		}
		return path, nil
	}
	return "", fmt.Errorf("no verified backup found in %s", backupDir)
}

// CleanupBackups removes old backups, keeping only the most recent N.
func (d *DeployOps) CleanupBackups(backupDir string, keep int) error {
	logger := log.Component(log.ComponentDeploy)
	logger.Debug().
		Str(log.FieldPath, backupDir).
		Int("keep_count", keep).
		Msg("Preparing to cleanup old backups")

	entries, err := os.ReadDir(backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Debug().Str(log.FieldPath, backupDir).Msg("Skipping backup cleanup. Reason: backup directory does not exist")
			return nil
		}
		logger.Error().Err(err).Str(log.FieldPath, backupDir).Msg("Failed to cleanup backups. Reason: cannot read backup directory")
		return fmt.Errorf("failed to read backup directory: %w", err)
	}

	// Filter to backup directories.
	var backups []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "backup-") {
			backups = append(backups, e.Name())
		}
	}

	// Sort by name (which includes timestamp, so chronological).
	sort.Strings(backups)

	// Remove old backups.
	if len(backups) > keep {
		toRemove := backups[:len(backups)-keep]
		logger.Debug().
			Int("total_backups", len(backups)).
			Int("removing", len(toRemove)).
			Int("keeping", keep).
			Msg("Removing old backups")

		for _, name := range toRemove {
			path := filepath.Join(backupDir, name)
			if err := os.RemoveAll(path); err != nil {
				logger.Error().Err(err).Str(log.FieldPath, path).Msg("Failed to remove old backup")
				return fmt.Errorf("failed to remove backup %s: %w", name, err)
			}
			logger.Debug().Str(log.FieldPath, path).Msg("Removed old backup")
		}
		logger.Info().
			Int("removed", len(toRemove)).
			Int("kept", keep).
			Msg("Successfully cleaned up old backups")
	} else {
		logger.Debug().Int("total_backups", len(backups)).Msg("Backup cleanup: no old backups to remove")
	}

	return nil
}

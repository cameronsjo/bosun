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
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/cameronsjo/bosun/internal/log"
	"github.com/kballard/go-shellquote"
)

// DefaultBackupTimeout separately bounds pre-deploy backup creation +
// verification and post-success retention verification + cleanup when no
// BackupTimeout is configured. It prevents either backup phase from wedging
// the reconcile indefinitely (#319, #243).
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

// ErrBackupTooLarge is returned when backup verification or extraction reads
// past MaxVerifyDecompressedBytes — a likely decompression bomb.
var ErrBackupTooLarge = errors.New("backup archive exceeds maximum verifiable decompressed size")

// VerifyBackup checks that a backup archive is valid and non-empty.
// The archive read runs under ctx so a caller deadline or cancellation
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

	// One in-process pass: list every entry, read each body to EOF, then drain the
	// gzip stream to its end so its checksum and trailer are checked. A header-only
	// listing can accept a truncated archive whose headers are intact, so the body
	// read is what proves the stream complete (#240). Rollback extraction reads the
	// archive with the same Go gzip and tar packages and drains to the same gzip
	// EOF, but it also enforces entry-name and link containment, which verification
	// does not, so a verified archive can still be refused at extraction.
	if err := verifyArchiveIntegrity(ctx, tarFile, MaxVerifyDecompressedBytes); err != nil {
		switch {
		case errors.Is(err, errBackupNoEntries):
			logger.Error().Str(log.FieldPath, tarFile).Msg("Failed to verify backup. Reason: archive contains no files")
			return fmt.Errorf("backup archive contains no files: %s", tarFile)
		case errors.Is(err, ErrBackupTooLarge) || ctx.Err() != nil:
			logger.Error().Err(err).Str(log.FieldPath, tarFile).Msg("Failed to verify backup. Reason: archive integrity check failed")
			return fmt.Errorf("backup archive failed integrity check: %w", err)
		default:
			logger.Error().Err(err).Str(log.FieldPath, tarFile).Msg("Failed to verify backup. Reason: archive is corrupted")
			return fmt.Errorf("backup archive is corrupted: %w", err)
		}
	}

	logger.Debug().Str(log.FieldPath, tarFile).Int64("size_bytes", info.Size()).Msg("Backup archive verified")
	return nil
}

func (d *DeployOps) verifyBackup(ctx context.Context, backupPath string) error {
	if d.verifyBackupFn != nil {
		return d.verifyBackupFn(ctx, backupPath)
	}
	return d.VerifyBackup(ctx, backupPath)
}

// errBackupNoEntries is returned when a gzip-compressed tar archive reads
// cleanly but holds no entries.
var errBackupNoEntries = errors.New("backup archive contains no entries")

// verifyArchiveIntegrity fully reads a gzip-compressed tar archive, decompressing
// and consuming every entry's data to EOF, then draining the gzip stream so its
// CRC-32 and size trailer are validated. Unlike a header-only listing, this forces
// decompression of every byte so a truncated data stream fails with
// io.ErrUnexpectedEOF and a damaged trailer with gzip.ErrChecksum instead of being
// silently accepted (#240). An archive with no entries returns errBackupNoEntries.
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
	entries := 0
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
		entries++
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
	// tar.Reader stops at the two end-of-archive blocks, and gzip validates its
	// CRC-32 and size trailer only when it is read to its true EOF, an error the
	// tar reader drops when it arrives with a full block. Drain the rest of the
	// gzip stream so a bad checksum or a cut trailer fails here, as it does in
	// safeExtractBackup. The drain is charged to the same budget and checks ctx
	// per chunk.
	n, err := copyCtx(ctx, io.Discard, io.LimitReader(gz, 0))
	if err != nil {
		return fmt.Errorf("cannot finish archive stream: %w", err)
	}
	remaining -= n
	if remaining < 0 {
		return fmt.Errorf("%w: %s (limit %d bytes)", ErrBackupTooLarge, tarFile, maxBytes)
	}
	if entries == 0 {
		return errBackupNoEntries
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

// backupDirNamePattern is the exact name Backup and BackupRemote give a backup
// directory: backup-YYYYMMDD-HHMMSS. Scans of a backup root match on it, not on
// the bare "backup-" prefix, because a named target's own directory under the
// root (for example backup-pi) shares that prefix and is not a backup (#791).
var backupDirNamePattern = regexp.MustCompile(`^backup-[0-9]{8}-[0-9]{6}$`)

// isBackupDirName reports whether name is a backup directory name.
func isBackupDirName(name string) bool {
	return backupDirNamePattern.MatchString(name)
}

// backupAbs resolves the archive exclusion path. It is a package variable so a
// test can inject the failure filepath.Abs only returns when the working
// directory is unreadable.
var backupAbs = filepath.Abs

// Backup creates a timestamped tar.gz backup of the specified paths. The
// archive excludes backupDir itself.
func (d *DeployOps) Backup(ctx context.Context, backupDir string, paths []string) (string, error) {
	return d.backupExcluding(ctx, backupDir, backupDir, paths)
}

// backupExcluding creates the backup under backupDir and keeps excludeDir out of
// the archive. They differ for a named target: backupDir is <root>/<name> and
// excludeDir is the shared root, so the archive holds no sibling's backups (#791).
func (d *DeployOps) backupExcluding(ctx context.Context, excludeDir, backupDir string, paths []string) (string, error) {
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

	// Exclude the backup destination so the archive cannot recursively include
	// its own growing output when backupDir is nested inside a backed-up path
	// (#319). Resolving it is load-bearing for that safeguard, so failure is
	// fatal — and it resolves BEFORE MkdirAll so no partial directory is left
	// behind on this path.
	absBackupDir, absErr := backupAbs(excludeDir)
	if absErr != nil {
		logger.Error().Err(absErr).Str(log.FieldPath, excludeDir).
			Msg("Failed to create backup. Reason: cannot resolve backup destination for self-exclusion")
		return "", fmt.Errorf("failed to resolve backup destination: %w", absErr)
	}

	if err := os.MkdirAll(backupPath, 0755); err != nil {
		logger.Error().Err(err).Str(log.FieldPath, backupPath).Msg("Failed to create backup directory")
		return "", fmt.Errorf("failed to create backup directory: %w", err)
	}

	// A failed backup must not leave partial artifacts for a later rollback to
	// trust (#352); a cleanup failure is loud (logged), not silently discarded.
	removeFailedBackup := func() {
		if rmErr := os.RemoveAll(backupPath); rmErr != nil {
			logger.Error().Err(rmErr).Str(log.FieldPath, backupPath).
				Msg("Failed to remove partial backup after error; stale artifacts remain")
		}
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
		// No live paths existed to back up (e.g. a fresh host's first deploy). The
		// MkdirAll'd dir holds no archive, so it is NOT a valid rollback anchor —
		// returning it as one reported "Backup saved" and let a later rollback
		// trust an empty dir, skipping verification entirely (#360). Remove the
		// empty dir so it cannot masquerade as a backup, and signal "nothing
		// backed up" with an empty name so the caller records no anchor.
		removeFailedBackup()
		logger.Warn().Str(log.FieldPath, backupPath).
			Msg("No existing paths to back up; no rollback anchor created")
		return "", nil
	}

	// Write the archive with Go's archive/tar directly. The previous external
	// `tar --null -T -` invocation silently produced nothing on busybox tar
	// (no --null support) and its exit code was discarded — every backup was an
	// empty directory and the failure surfaced only at verification (#395).
	// Native archiving removes the GNU/bsd/busybox capability guessing, and a
	// creation failure is now fatal with the partial output removed (#352).
	if err := d.writeBackupArchive(ctx, tarFile, existingPaths, absBackupDir); err != nil {
		removeFailedBackup()
		logger.Error().Err(err).Str(log.FieldPath, tarFile).Msg("Failed to create backup. Reason: cannot write archive")
		return "", fmt.Errorf("failed to write backup archive: %w", err)
	}

	// Verify the backup was created successfully
	if err := d.VerifyBackup(ctx, backupPath); err != nil {
		removeFailedBackup()
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

type backupArchiveFS struct {
	walk func(root string, walkFn filepath.WalkFunc) error
	open func(name string) (*os.File, error)
}

func (d *DeployOps) backupFileSystem() backupArchiveFS {
	fs := backupArchiveFS{walk: filepath.Walk, open: os.Open}
	if d.backupFS == nil {
		return fs
	}
	if d.backupFS.walk != nil {
		fs.walk = d.backupFS.walk
	}
	if d.backupFS.open != nil {
		fs.open = d.backupFS.open
	}
	return fs
}

// writeBackupArchive creates a gzip-compressed tar archive at tarFile containing
// every path in paths (directories recursed), excluding the excludeDir subtree.
// Member names have the leading '/' stripped, matching what external tar produced,
// so safeExtractBackup/resolveBackupFile keep working unchanged (#332/#335).
//
// The backed-up footprint is LIVE container appdata, so the walk tolerates churn
// the way GNU tar does, instead of failing the deploy on it (#240 made a failed
// backup abort the deploy, so spurious failures are outages):
//   - a path that vanishes before its tar header is written is skipped (logged
//     debug); a regular file that vanishes after its header is written is
//     zero-padded to the promised size so the archive remains valid
//   - irregular files (sockets, devices, FIFOs) are skipped — archiving a live
//     unix socket is meaningless and tar.FileInfoHeader rejects it
//   - a file that shrinks mid-read is zero-padded to its header size so the
//     archive stays structurally valid (logged warn); growth past the header
//     size is truncated at the recorded length
//
// Everything else — permission errors, I/O errors, a failed write — is fatal:
// a backup that cannot be produced must fail loudly at creation, not be
// discovered missing at verification (#395).
func (d *DeployOps) writeBackupArchive(ctx context.Context, tarFile string, paths []string, excludeDir string) (err error) {
	logger := log.ComponentCtx(ctx, log.ComponentDeploy)
	fs := d.backupFileSystem()

	f, err := os.Create(tarFile)
	if err != nil {
		return fmt.Errorf("cannot create archive file: %w", err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("cannot close archive file: %w", closeErr)
		}
	}()

	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	for _, root := range paths {
		walkErr := fs.walk(root, func(path string, info os.FileInfo, werr error) error {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if werr != nil {
				// Vanished mid-walk: live appdata churn, not a broken backup.
				if os.IsNotExist(werr) {
					logger.Debug().Str(log.FieldPath, path).Msg("Skipping path that vanished during backup walk")
					return nil
				}
				return fmt.Errorf("cannot walk %s: %w", path, werr)
			}
			if info.IsDir() {
				// Compare in absolute coordinates: a relative walk root would
				// otherwise never match the absolute excludeDir, and the
				// archive would recursively include its own growing output.
				absPath, aerr := filepath.Abs(path)
				if aerr != nil {
					return fmt.Errorf("cannot resolve walked directory %s: %w", path, aerr)
				}
				if absPath == excludeDir {
					return filepath.SkipDir
				}
			}

			var linkTarget string
			if info.Mode()&os.ModeSymlink != 0 {
				linkTarget, werr = os.Readlink(path)
				if werr != nil {
					if os.IsNotExist(werr) {
						logger.Debug().Str(log.FieldPath, path).Msg("Skipping symlink that vanished during backup walk")
						return nil
					}
					return fmt.Errorf("cannot read symlink %s: %w", path, werr)
				}
			} else if !info.Mode().IsRegular() && !info.IsDir() {
				// Sockets, devices, FIFOs: meaningless in a config backup.
				logger.Debug().Str(log.FieldPath, path).Msg("Skipping irregular file during backup")
				return nil
			}

			hdr, herr := tar.FileInfoHeader(info, linkTarget)
			if herr != nil {
				return fmt.Errorf("cannot build header for %s: %w", path, herr)
			}
			// Full path with the leading '/' stripped — external tar's layout,
			// which resolveBackupFile depends on. fi.Name() alone would flatten
			// the tree into colliding basenames.
			hdr.Name = strings.TrimPrefix(filepath.ToSlash(path), "/")
			if info.IsDir() {
				hdr.Name += "/"
			}
			if werr := tw.WriteHeader(hdr); werr != nil {
				return fmt.Errorf("cannot write header for %s: %w", path, werr)
			}
			if !info.Mode().IsRegular() {
				return nil
			}

			src, oerr := fs.open(path)
			if oerr != nil {
				if os.IsNotExist(oerr) {
					// Header already written; pad the promised bytes so the
					// archive stays valid.
					if _, perr := copyCtx(ctx, tw, io.LimitReader(zeroReader{}, hdr.Size)); perr != nil {
						return fmt.Errorf("cannot pad vanished file %s: %w", path, perr)
					}
					logger.Debug().Str(log.FieldPath, path).Msg("Padded file that vanished during backup")
					return nil
				}
				return fmt.Errorf("cannot open %s: %w", path, oerr)
			}
			// TOCTOU guard: os.Open follows a symlink swapped in after the
			// walk's Lstat, which would archive bytes from an arbitrary
			// readable host path under this entry's header. Stat the opened
			// fd and require the same inode the walk saw; anything else is
			// treated as churn — the entry is zero-padded, never read.
			srcInfo, serr := src.Stat()
			if serr != nil {
				_ = src.Close()
				return fmt.Errorf("cannot stat opened file %s: %w", path, serr)
			}
			if !os.SameFile(info, srcInfo) {
				_ = src.Close()
				if _, perr := copyCtx(ctx, tw, io.LimitReader(zeroReader{}, hdr.Size)); perr != nil {
					return fmt.Errorf("cannot pad replaced file %s: %w", path, perr)
				}
				logger.Warn().Str(log.FieldPath, path).
					Msg("File replaced during backup; archive entry zero-padded")
				return nil
			}
			if srcInfo.Size() > hdr.Size {
				logger.Warn().Str(log.FieldPath, path).
					Int64("expected_bytes", hdr.Size).Int64("current_bytes", srcInfo.Size()).
					Msg("File grew during backup; archive entry truncated at header size")
			}
			// Copy exactly the header-recorded size: growth past it is
			// truncated, shrinkage below it is zero-padded — a structurally
			// valid archive either way, as GNU tar behaves on live files.
			n, cerr := copyCtx(ctx, tw, io.LimitReader(src, hdr.Size))
			closeErr := src.Close()
			if cerr != nil {
				return fmt.Errorf("cannot archive %s: %w", path, cerr)
			}
			if closeErr != nil {
				return fmt.Errorf("cannot close %s: %w", path, closeErr)
			}
			if n < hdr.Size {
				if _, perr := copyCtx(ctx, tw, io.LimitReader(zeroReader{}, hdr.Size-n)); perr != nil {
					return fmt.Errorf("cannot pad truncated file %s: %w", path, perr)
				}
				logger.Warn().Str(log.FieldPath, path).
					Int64("expected_bytes", hdr.Size).Int64("read_bytes", n).
					Msg("File shrank during backup; archive entry zero-padded")
			}
			return nil
		})
		if walkErr != nil {
			return walkErr
		}
	}

	if err := tw.Close(); err != nil {
		return fmt.Errorf("cannot finalize tar stream: %w", err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("cannot finalize gzip stream: %w", err)
	}
	return nil
}

// zeroReader yields an endless stream of zero bytes; used to pad archive
// entries whose source file shrank or vanished after its header was written.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

// BackupRemote creates a backup from a remote host via SSH.
// Retries on transient SSH errors with exponential backoff. The archive excludes
// backupDir itself.
func (d *DeployOps) BackupRemote(ctx context.Context, host, backupDir string, remotePaths []string) (string, error) {
	return d.backupRemoteExcluding(ctx, host, backupDir, backupDir, remotePaths)
}

// backupRemoteExcluding is BackupRemote with the tar exclusion decoupled from the
// destination: excludeDir is the shared backup root for a named target (#791).
func (d *DeployOps) backupRemoteExcluding(ctx context.Context, host, excludeDir, backupDir string, remotePaths []string) (string, error) {
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

	// An empty footprint is legitimate on a fresh host. It requires no SSH or
	// local artifact, so return the same empty-name signal as Backup after the
	// operation's host input has passed the common remote-boundary validation.
	if len(remotePaths) == 0 {
		logger.Warn().Str(log.FieldTarget, host).
			Msg("No remote paths to back up; no rollback anchor created")
		return "", nil
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
	// excludeDir for the exclude to match the path tar walks; skip (with a warning)
	// otherwise rather than emit an exclude that silently matches nothing.
	var sshCmd string
	if filepath.IsAbs(excludeDir) {
		sshCmd = fmt.Sprintf("tar -czf - --exclude %s %s",
			shellquote.Join(excludeDir), shellquote.Join(remotePaths...))
	} else {
		logger.Warn().Str(log.FieldPath, excludeDir).
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
		if e.IsDir() && isBackupDirName(e.Name()) {
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

// CleanupBackups removes old backups, keeping only the most recent N VALID ones.
// A candidate counts toward retention only if it passes the SAME VerifyBackup used
// to select a rollback anchor — reusing that helper rather than a private, weaker
// notion of "good" so the two can never drift (a truncated-but-nonempty archive is
// "good" to a mere existence check yet junk to VerifyBackup). A corrupt or partial
// dir (missing, unlistable, or truncated archive) does NOT occupy a keep slot and
// is removed outright; otherwise it could evict an older known-good backup (#353).
// Verification and removal share the caller's post-success retention context,
// which is independently bounded by BackupTimeout so a stuck tar/gzip read
// cannot wedge cleanup.
func (d *DeployOps) CleanupBackups(ctx context.Context, backupDir string, keep int) error {
	logger := log.ComponentCtx(ctx, log.ComponentDeploy)
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

	// Partition backup dirs into valid (passes VerifyBackup) and invalid (corrupt
	// or partial). Only VALID backups count toward retention: an invalid dir must
	// never occupy a keep slot and evict an older known-good backup (#353). Invalid
	// dirs are never usable rollback anchors, so they are removed outright rather
	// than left to litter the directory and re-trip the eviction.
	var valid, invalid []string
	for _, e := range entries {
		if !e.IsDir() || !isBackupDirName(e.Name()) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		verifyErr := d.verifyBackup(ctx, filepath.Join(backupDir, e.Name()))
		if err := ctx.Err(); err != nil {
			return err
		}
		if verifyErr == nil {
			valid = append(valid, e.Name())
		} else {
			invalid = append(invalid, e.Name())
		}
	}

	// Remove corrupt/partial dirs unconditionally — they are litter, never anchors.
	for _, name := range invalid {
		if err := ctx.Err(); err != nil {
			return err
		}
		path := filepath.Join(backupDir, name)
		if err := os.RemoveAll(path); err != nil {
			logger.Error().Err(err).Str(log.FieldPath, path).Msg("Failed to remove corrupt/partial backup")
			return fmt.Errorf("failed to remove corrupt backup %s: %w", name, err)
		}
		logger.Warn().Str(log.FieldPath, path).Msg("Removed corrupt or partial backup (no valid archive)")
	}

	// Sort valid backups by name (timestamp-embedded, so chronological).
	sort.Strings(valid)

	// Remove old VALID backups beyond the retention count.
	if len(valid) > keep {
		toRemove := valid[:len(valid)-keep]
		logger.Debug().
			Int("valid_backups", len(valid)).
			Int("removing", len(toRemove)).
			Int("keeping", keep).
			Msg("Removing old backups")

		for _, name := range toRemove {
			if err := ctx.Err(); err != nil {
				return err
			}
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
		logger.Debug().Int("valid_backups", len(valid)).Msg("Backup cleanup: no old backups to remove")
	}

	return nil
}

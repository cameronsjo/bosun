package reconcile

import (
	"bytes"
	"context"
	"fmt"
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

	// Verify archive integrity by listing contents
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

	logger.Debug().Str(log.FieldPath, tarFile).Int64("size_bytes", info.Size()).Msg("Backup archive verified")
	return nil
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
	args = append(args, existingPaths...)
	cmd := exec.CommandContext(ctx, "tar", args...)
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

	outFile, err := os.Create(tarFile)
	if err != nil {
		logger.Error().Err(err).Str(log.FieldPath, tarFile).Msg("Failed to create remote backup. Reason: cannot create backup file")
		return "", fmt.Errorf("failed to create backup file: %w", err)
	}

	// Retry with backoff on transient SSH errors.
	sshErr := retryWithBackoff(ctx, DefaultMaxRetries, func() error {
		var stderr bytes.Buffer
		cmd := exec.CommandContext(ctx, "ssh", host, sshCmd)
		cmd.Stdout = outFile
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("remote tar failed: %w: %s", err, stderr.String())
		}
		return nil
	})

	// Close the file before verification
	if closeErr := outFile.Close(); closeErr != nil {
		// Clean up on close failure
		_ = os.RemoveAll(backupPath)
		logger.Error().Err(closeErr).Str(log.FieldPath, tarFile).Msg("Failed to create remote backup. Reason: cannot close backup file")
		return "", fmt.Errorf("failed to close backup file: %w", closeErr)
	}

	// Log SSH/tar error but don't fail outright — the backup may still be usable.
	// We verify it below; that check will surface any real corruption.
	if sshErr != nil {
		logger.Warn().Err(sshErr).Str(log.FieldTarget, host).Msg("Remote tar command returned error, verifying backup integrity")
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

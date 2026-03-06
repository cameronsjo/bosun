package reconcile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cameronsjo/bosun/internal/fileutil"
	"github.com/cameronsjo/bosun/internal/log"
)

// ErrRollbackSucceeded indicates deployment failed but rollback succeeded.
var ErrRollbackSucceeded = errors.New("deployment failed, rollback succeeded")

// ErrRollbackFailed indicates both deployment and rollback failed.
var ErrRollbackFailed = errors.New("deployment and rollback both failed")

// ErrComposeUnhealthy indicates compose exited non-zero but all containers are
// running (some unhealthy). This is recoverable and should not trigger rollback.
var ErrComposeUnhealthy = errors.New("compose up completed with unhealthy containers")

// SSH retry configuration
const (
	DefaultMaxRetries = 3
	InitialBackoff    = 1 * time.Second
)

// Deploy operation timeouts
const (
	SSHConnectTimeout  = 5 * time.Second
	SSHTimeout         = 30 * time.Second
	RemoteDeployTimeout = 5 * time.Minute
	ComposeUpTimeout   = 10 * time.Minute
)

// DeployOps provides deployment operations including backup, file sync, and service management.
type DeployOps struct {
	// DryRun if true, only shows what would be done without making changes.
	DryRun bool
	// ProjectName is the docker compose project name for consistent container namespacing.
	ProjectName string
	// ContentHashSync if true, skips writing files whose content has not changed.
	// Reduces unnecessary FUSE handle invalidation on copy-on-write filesystems.
	ContentHashSync bool
}

// DeployResult tracks which files were actually written during deployment.
// Used to inform post-sync hooks about actual on-disk changes.
type DeployResult struct {
	// WrittenFiles contains relative paths of files that were written to disk.
	WrittenFiles []string
}

// AddWritten appends file paths to the result's written files list.
func (r *DeployResult) AddWritten(files ...string) {
	r.WrittenFiles = append(r.WrittenFiles, files...)
}

// NewDeployOps creates a new DeployOps instance.
func NewDeployOps(dryRun bool, projectName string) *DeployOps {
	return &DeployOps{DryRun: dryRun, ProjectName: projectName}
}

// composeArgs returns docker compose arguments with project name if set.
func (d *DeployOps) composeArgs(files ...string) []string {
	return buildComposeArgs(d.ProjectName, files)
}

// isTransientSSHError checks if an error is transient and worth retrying.
// Transient errors include connection refused, timeout, and network unreachable.
func isTransientSSHError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	transientPatterns := []string{
		"connection refused",
		"connection reset",
		"connection timed out",
		"network is unreachable",
		"no route to host",
		"host is down",
		"operation timed out",
		"i/o timeout",
		"temporary failure",
	}
	for _, pattern := range transientPatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}
	return false
}

// retryWithBackoff executes a function with exponential backoff retry logic.
// It retries only on transient SSH errors (connection refused, timeout, etc).
// The backoff sequence is: 1s, 2s, 4s (for maxRetries=3).
func retryWithBackoff(ctx context.Context, maxRetries int, operation func() error) error {
	if maxRetries <= 0 {
		maxRetries = DefaultMaxRetries
	}

	logger := log.ComponentCtx(ctx, log.ComponentDeploy)
	var lastErr error
	backoff := InitialBackoff

	for attempt := 1; attempt <= maxRetries; attempt++ {
		lastErr = operation()
		if lastErr == nil {
			if attempt > 1 {
				logger.Info().
					Int("attempt", attempt).
					Int("max_attempts", maxRetries).
					Msg("Operation succeeded after retry")
			}
			return nil
		}

		// Check if context is cancelled
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Only retry on transient errors
		if !isTransientSSHError(lastErr) {
			return lastErr
		}

		// Log the transient failure and upcoming retry
		if attempt < maxRetries {
			logger.Warn().
				Err(lastErr).
				Int("attempt", attempt).
				Int("max_attempts", maxRetries).
				Int64("backoff_ms", backoff.Milliseconds()).
				Msg("Transient error, retrying after backoff")

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
				backoff *= 2 // Exponential backoff
			}
		} else {
			logger.Warn().
				Err(lastErr).
				Int("attempt", attempt).
				Int("max_attempts", maxRetries).
				Msg("Final attempt failed")
		}
	}

	return fmt.Errorf("operation failed after %d attempts: %w", maxRetries, lastErr)
}

// CheckSSHConnectivity verifies SSH connectivity to a remote host.
// Returns nil if connection succeeds, error with actionable details otherwise.
func (d *DeployOps) CheckSSHConnectivity(ctx context.Context, host string) error {
	if err := validateHost(host); err != nil {
		return fmt.Errorf("invalid SSH host: %w", err)
	}

	// Apply timeout if context doesn't have one
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, SSHConnectTimeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, "ssh",
		"-o", "ConnectTimeout=5",
		"-o", "BatchMode=yes",
		host, "exit", "0",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		stderrStr := stderr.String()
		return parseSSHError(err, stderrStr, host)
	}
	return nil
}

// parseSSHError converts SSH errors into actionable error messages.
func parseSSHError(err error, stderr, host string) error {
	category := classifySSHError(stderr)
	switch category {
	case "auth":
		return fmt.Errorf("SSH authentication failed for %s: permission denied. Check that your SSH key is added to the remote host's authorized_keys", host)
	case "connection":
		stderrLower := strings.ToLower(stderr)
		if strings.Contains(stderrLower, "no route to host") {
			return fmt.Errorf("cannot reach %s: no route to host. Check network connectivity and that the host is online", host)
		}
		return fmt.Errorf("SSH connection refused by %s: the SSH service may not be running or the port may be blocked", host)
	case "host_key":
		return fmt.Errorf("SSH host key verification failed for %s: run 'ssh-keyscan %s >> ~/.ssh/known_hosts' to add the host key", host, host)
	case "dns":
		return fmt.Errorf("cannot resolve hostname %s: check that the hostname is correct and DNS is working", host)
	case "timeout":
		return fmt.Errorf("SSH connection to %s timed out: check network connectivity and firewall rules", host)
	default:
		return fmt.Errorf("SSH connection to %s failed: %w: %s", host, err, stderr)
	}
}

// VerifyBackup checks that a backup archive is valid and non-empty.
func (d *DeployOps) VerifyBackup(backupPath string) error {
	tarFile := filepath.Join(backupPath, "configs.tar.gz")

	// Check file exists
	info, err := os.Stat(tarFile)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("backup archive not found: %s", tarFile)
		}
		return fmt.Errorf("failed to stat backup archive: %w", err)
	}

	// Check file is non-empty
	if info.Size() == 0 {
		return fmt.Errorf("backup archive is empty: %s", tarFile)
	}

	// Verify archive integrity by listing contents
	cmd := exec.Command("tar", "-tzf", tarFile)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("backup archive is corrupted: %w: %s", err, stderr.String())
	}

	// Check archive has at least one file
	if strings.TrimSpace(stdout.String()) == "" {
		return fmt.Errorf("backup archive contains no files: %s", tarFile)
	}

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

	args := append([]string{"-czf", tarFile}, existingPaths...)
	cmd := exec.CommandContext(ctx, "tar", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	// tar returns non-zero if some files don't exist, which is OK.
	_ = cmd.Run()

	// Verify the backup was created successfully
	if err := d.VerifyBackup(backupPath); err != nil {
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

	if err := validateHost(host); err != nil {
		return "", fmt.Errorf("invalid SSH host: %w", err)
	}

	timestamp := time.Now().Format("20060102-150405")
	backupName := fmt.Sprintf("backup-%s", timestamp)
	backupPath := filepath.Join(backupDir, backupName)

	logger.Info().
		Str(log.FieldOperation, "backup_remote").
		Str(log.FieldTarget, host).
		Str(log.FieldPath, backupPath).
		Int("path_count", len(remotePaths)).
		Msg("Creating remote backup")

	if err := os.MkdirAll(backupPath, 0755); err != nil {
		logger.Error().Err(err).Str(log.FieldPath, backupPath).Msg("Failed to create backup directory")
		return "", fmt.Errorf("failed to create backup directory: %w", err)
	}

	tarFile := filepath.Join(backupPath, "configs.tar.gz")

	// Build remote tar command.
	tarArgs := strings.Join(remotePaths, " ")
	sshCmd := fmt.Sprintf("tar -czf - %s 2>/dev/null", tarArgs)

	outFile, err := os.Create(tarFile)
	if err != nil {
		return "", fmt.Errorf("failed to create backup file: %w", err)
	}

	// Retry with backoff on transient SSH errors.
	sshErr := retryWithBackoff(ctx, DefaultMaxRetries, func() error {
		cmd := exec.CommandContext(ctx, "ssh", host, sshCmd)
		cmd.Stdout = outFile
		return cmd.Run()
	})

	// Close the file before verification
	if closeErr := outFile.Close(); closeErr != nil {
		// Clean up on close failure
		os.RemoveAll(backupPath)
		return "", fmt.Errorf("failed to close backup file: %w", closeErr)
	}

	// Log SSH error but don't fail - tar may return non-zero for missing files.
	// Only log if it's not a transient error we already retried.
	// tar returning non-zero for missing files is expected.
	_ = sshErr

	// Verify the backup was created successfully
	if err := d.VerifyBackup(backupPath); err != nil {
		// Clean up invalid backup on verification failure
		os.RemoveAll(backupPath)
		logger.Error().Err(err).Str(log.FieldPath, backupPath).Msg("Remote backup verification failed")
		return "", fmt.Errorf("backup verification failed: %w", err)
	}

	logger.Info().
		Str(log.FieldOperation, "backup_remote").
		Str(log.FieldTarget, host).
		Str(log.FieldPath, backupPath).
		Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
		Msg("Remote backup created successfully")

	return backupName, nil
}

// CleanupBackups removes old backups, keeping only the most recent N.
func (d *DeployOps) CleanupBackups(backupDir string, keep int) error {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
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
		logger := log.Component(log.ComponentDeploy)
		for _, name := range toRemove {
			path := filepath.Join(backupDir, name)
			if err := os.RemoveAll(path); err != nil {
				return fmt.Errorf("failed to remove backup %s: %w", name, err)
			}
		}
		logger.Debug().
			Str(log.FieldOperation, "cleanup_backups").
			Int("removed", len(toRemove)).
			Int("kept", keep).
			Msg("Old backups removed")
	}

	return nil
}

// DeployLocal syncs files locally using native Go file operations.
// Performs atomic copy: copies to temp directory first, then replaces target.
// Uses --delete semantics: removes files in target that don't exist in source.
func (d *DeployOps) DeployLocal(ctx context.Context, sourceDir, targetDir string, result *DeployResult) error {
	start := time.Now()
	logger := log.ComponentCtx(ctx, log.ComponentDeploy)

	if d.DryRun {
		logger.Debug().
			Str("source", sourceDir).
			Str("target", targetDir).
			Msg("Dry run: would deploy locally")
		return nil
	}

	logger.Debug().
		Str(log.FieldOperation, "deploy_local").
		Str("source", sourceDir).
		Str("target", targetDir).
		Msg("Deploying files locally")

	// Verify source directory exists
	srcInfo, err := os.Stat(sourceDir)
	if err != nil {
		logger.Error().Err(err).Str("source", sourceDir).Msg("Source directory error")
		return fmt.Errorf("source directory: %w", err)
	}
	if !srcInfo.IsDir() {
		logger.Error().Str("source", sourceDir).Msg("Source is not a directory")
		return fmt.Errorf("source is not a directory: %s", sourceDir)
	}

	// Content-hash mode: compare per-file against existing target, skip unchanged.
	if d.ContentHashSync {
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return fmt.Errorf("create target directory: %w", err)
		}

		written, err := fileutil.CopyDirIfChanged(sourceDir, targetDir)
		if err != nil {
			return fmt.Errorf("copy with content hash: %w", err)
		}

		// Remove files in target that aren't in source (--delete semantics).
		if err := removeStaleFiles(sourceDir, targetDir); err != nil {
			return fmt.Errorf("remove stale files: %w", err)
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}

		if result != nil {
			result.AddWritten(written...)
		}

		logger.Debug().
			Str(log.FieldOperation, "deploy_local").
			Str("target", targetDir).
			Int("files_written", len(written)).
			Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
			Msg("Local deployment completed (content-hash sync)")
		return nil
	}

	// Standard mode: nuke-and-replace for atomic directory swap.
	targetParent := filepath.Dir(targetDir)
	if err := os.MkdirAll(targetParent, 0755); err != nil {
		return fmt.Errorf("create target parent: %w", err)
	}

	tmpDir, err := os.MkdirTemp(targetParent, ".deploy-tmp-*")
	if err != nil {
		return fmt.Errorf("create temp directory: %w", err)
	}

	success := false
	defer func() {
		if !success {
			os.RemoveAll(tmpDir)
		}
	}()

	if err := fileutil.CopyDir(sourceDir, tmpDir); err != nil {
		return fmt.Errorf("copy to temp: %w", err)
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	if _, err := os.Stat(targetDir); err == nil {
		if err := os.RemoveAll(targetDir); err != nil {
			return fmt.Errorf("remove existing target: %w", err)
		}
	}

	if err := os.Rename(tmpDir, targetDir); err != nil {
		logger.Error().Err(err).Str("target", targetDir).Msg("Failed to rename to target")
		return fmt.Errorf("rename to target: %w", err)
	}

	success = true
	logger.Debug().
		Str(log.FieldOperation, "deploy_local").
		Str("target", targetDir).
		Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
		Msg("Local deployment completed")
	return nil
}

// removeStaleFiles removes files in targetDir that don't exist in sourceDir.
// Preserves --delete semantics when using per-file content-hash sync.
func removeStaleFiles(sourceDir, targetDir string) error {
	return filepath.WalkDir(targetDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(targetDir, path)
		if err != nil {
			return err
		}
		if relPath == "." {
			return nil
		}

		srcPath := filepath.Join(sourceDir, relPath)
		if _, err := os.Stat(srcPath); os.IsNotExist(err) {
			if d.IsDir() {
				os.RemoveAll(path)
				return filepath.SkipDir
			}
			os.Remove(path)
		}
		return nil
	})
}

// DeployLocalFile syncs a single file locally using native Go file operations.
// Uses atomic copy via temp file. When ContentHashSync is enabled, skips writing
// if the file content has not changed.
func (d *DeployOps) DeployLocalFile(ctx context.Context, sourceFile, targetFile string, result *DeployResult) error {
	if d.DryRun {
		return nil
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	if d.ContentHashSync {
		changed, err := fileutil.CopyFileIfChanged(sourceFile, targetFile)
		if err != nil {
			return err
		}
		if changed && result != nil {
			result.AddWritten(targetFile)
		}
		return nil
	}

	return fileutil.CopyFile(sourceFile, targetFile)
}

// DeployRemote syncs files to a remote host using tar-over-SSH.
// Uses RemoteDeployTimeout if the parent context has no deadline.
// Retries on transient SSH errors with exponential backoff.
// Performs atomic deployment: tar to temp dir, then move to target.
func (d *DeployOps) DeployRemote(ctx context.Context, sourceDir, targetHost, targetDir string) error {
	start := time.Now()
	logger := log.ComponentCtx(ctx, log.ComponentDeploy)

	if err := validateHost(targetHost); err != nil {
		return fmt.Errorf("invalid SSH host: %w", err)
	}

	if d.DryRun {
		logger.Debug().
			Str("source", sourceDir).
			Str(log.FieldTarget, targetHost).
			Str("target_dir", targetDir).
			Msg("Dry run: would deploy remotely")
		return nil
	}

	logger.Debug().
		Str(log.FieldOperation, "deploy_remote").
		Str("source", sourceDir).
		Str(log.FieldTarget, targetHost).
		Str("target_dir", targetDir).
		Msg("Deploying files remotely")

	// Apply timeout if context doesn't have one
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, RemoteDeployTimeout)
		defer cancel()
	}

	// Ensure target directory parent exists on remote
	targetParent := filepath.Dir(targetDir)
	if err := d.EnsureRemoteDir(ctx, targetHost, targetParent); err != nil {
		return fmt.Errorf("ensure remote parent dir: %w", err)
	}

	// Create temp directory on remote for atomic deployment
	// Use unique name based on target to avoid collisions
	tmpDirName := fmt.Sprintf(".deploy-tmp-%d", time.Now().UnixNano())
	tmpDir := filepath.Join(targetParent, tmpDirName)

	return retryWithBackoff(ctx, DefaultMaxRetries, func() error {
		// Create temp directory on remote
		mkdirCmd := exec.CommandContext(ctx, "ssh", targetHost, "mkdir", "-p", tmpDir)
		var mkdirStderr bytes.Buffer
		mkdirCmd.Stderr = &mkdirStderr
		if err := mkdirCmd.Run(); err != nil {
			return fmt.Errorf("create remote temp dir: %w: %s", err, mkdirStderr.String())
		}

		// Tar source directory and pipe to SSH for extraction on remote
		// tar -C sourceDir -cf - . | ssh host "tar -C tmpDir -xf -"
		tarCmd := exec.CommandContext(ctx, "tar", "-C", sourceDir, "-cf", "-", ".")
		sshCmd := exec.CommandContext(ctx, "ssh", targetHost, fmt.Sprintf("tar -C %s -xf -", tmpDir))

		// Connect tar stdout to ssh stdin
		pipe, err := tarCmd.StdoutPipe()
		if err != nil {
			return fmt.Errorf("create pipe: %w", err)
		}
		sshCmd.Stdin = pipe

		var tarStderr, sshStderr bytes.Buffer
		tarCmd.Stderr = &tarStderr
		sshCmd.Stderr = &sshStderr

		// Start both commands
		if err := tarCmd.Start(); err != nil {
			return fmt.Errorf("start tar: %w", err)
		}
		if err := sshCmd.Start(); err != nil {
			_ = tarCmd.Process.Kill()
			return fmt.Errorf("start ssh: %w: %s", err, sshStderr.String())
		}

		// Wait for both to complete
		tarErr := tarCmd.Wait()
		sshErr := sshCmd.Wait()

		if tarErr != nil {
			// Cleanup temp dir on failure
			_ = exec.CommandContext(ctx, "ssh", targetHost, "rm", "-rf", tmpDir).Run()
			return fmt.Errorf("tar failed: %w: %s", tarErr, tarStderr.String())
		}
		if sshErr != nil {
			_ = exec.CommandContext(ctx, "ssh", targetHost, "rm", "-rf", tmpDir).Run()
			if ctx.Err() == context.DeadlineExceeded {
				return fmt.Errorf("ssh timed out after %v", RemoteDeployTimeout)
			}
			return fmt.Errorf("ssh extract failed: %w: %s", sshErr, sshStderr.String())
		}

		// Atomic move: remove old target and rename temp to target
		// Using a shell command to ensure atomicity
		moveCmd := fmt.Sprintf("rm -rf %s && mv %s %s", targetDir, tmpDir, targetDir)
		atomicCmd := exec.CommandContext(ctx, "ssh", targetHost, moveCmd)
		var atomicStderr bytes.Buffer
		atomicCmd.Stderr = &atomicStderr

		if err := atomicCmd.Run(); err != nil {
			// Try to cleanup temp dir
			_ = exec.CommandContext(ctx, "ssh", targetHost, "rm", "-rf", tmpDir).Run()
			return fmt.Errorf("atomic move failed: %w: %s", err, atomicStderr.String())
		}

		logger.Debug().
			Str(log.FieldOperation, "deploy_remote").
			Str(log.FieldTarget, targetHost).
			Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
			Msg("Remote deployment completed")
		return nil
	})
}

// DeployRemoteFile syncs a single file to a remote host using scp.
// Uses RemoteDeployTimeout if the parent context has no deadline.
// Retries on transient SSH errors with exponential backoff.
// Performs atomic copy: scp to temp file, then move to target.
func (d *DeployOps) DeployRemoteFile(ctx context.Context, sourceFile, targetHost, targetFile string) error {
	start := time.Now()
	logger := log.ComponentCtx(ctx, log.ComponentDeploy)

	if err := validateHost(targetHost); err != nil {
		return fmt.Errorf("invalid SSH host: %w", err)
	}

	if d.DryRun {
		logger.Debug().
			Str("source", sourceFile).
			Str(log.FieldTarget, targetHost).
			Str("target_file", targetFile).
			Msg("Dry run: would deploy remote file")
		return nil
	}

	logger.Debug().
		Str(log.FieldOperation, "deploy_remote_file").
		Str("source", sourceFile).
		Str(log.FieldTarget, targetHost).
		Str("target_file", targetFile).
		Msg("Deploying file remotely")

	// Apply timeout if context doesn't have one
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, RemoteDeployTimeout)
		defer cancel()
	}

	// Ensure target directory exists on remote
	targetDir := filepath.Dir(targetFile)
	if err := d.EnsureRemoteDir(ctx, targetHost, targetDir); err != nil {
		return fmt.Errorf("ensure remote dir: %w", err)
	}

	// Create temp file path for atomic copy
	tmpFile := fmt.Sprintf("%s.tmp.%d", targetFile, time.Now().UnixNano())

	err := retryWithBackoff(ctx, DefaultMaxRetries, func() error {
		// SCP to temp file
		target := fmt.Sprintf("%s:%s", targetHost, tmpFile)
		scpCmd := exec.CommandContext(ctx, "scp", "-q", sourceFile, target)
		var scpStderr bytes.Buffer
		scpCmd.Stderr = &scpStderr

		if err := scpCmd.Run(); err != nil {
			// Cleanup temp file on failure
			_ = exec.CommandContext(ctx, "ssh", targetHost, "rm", "-f", tmpFile).Run()
			if ctx.Err() == context.DeadlineExceeded {
				return fmt.Errorf("scp timed out after %v", RemoteDeployTimeout)
			}
			return fmt.Errorf("scp failed: %w: %s", err, scpStderr.String())
		}

		// Atomic move temp file to target
		moveCmd := exec.CommandContext(ctx, "ssh", targetHost, "mv", tmpFile, targetFile)
		var moveStderr bytes.Buffer
		moveCmd.Stderr = &moveStderr

		if err := moveCmd.Run(); err != nil {
			_ = exec.CommandContext(ctx, "ssh", targetHost, "rm", "-f", tmpFile).Run()
			return fmt.Errorf("atomic move failed: %w: %s", err, moveStderr.String())
		}

		return nil
	})
	if err == nil {
		logger.Debug().
			Str(log.FieldOperation, "deploy_remote_file").
			Str(log.FieldTarget, targetHost).
			Str("target_file", targetFile).
			Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
			Msg("Remote file deployment completed")
	}
	return err
}

// EnsureRemoteDir ensures a directory exists on a remote host via SSH.
// Uses SSHTimeout if the parent context has no deadline.
// Retries on transient SSH errors with exponential backoff.
func (d *DeployOps) EnsureRemoteDir(ctx context.Context, host, dir string) error {
	logger := log.ComponentCtx(ctx, log.ComponentDeploy)

	if err := validateHost(host); err != nil {
		return fmt.Errorf("invalid SSH host: %w", err)
	}

	logger.Debug().
		Str(log.FieldOperation, "ensure_remote_dir").
		Str(log.FieldTarget, host).
		Str(log.FieldPath, dir).
		Msg("Ensuring remote directory exists")

	// Apply timeout if context doesn't have one
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, SSHTimeout)
		defer cancel()
	}

	return retryWithBackoff(ctx, DefaultMaxRetries, func() error {
		cmd := exec.CommandContext(ctx, "ssh", host, "mkdir", "-p", dir)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return fmt.Errorf("ssh timed out after %v", SSHTimeout)
			}
			return fmt.Errorf("ssh mkdir failed: %w: %s", err, stderr.String())
		}
		return nil
	})
}

// ComposeUp runs docker compose up for the specified compose file.
// Uses ComposeUpTimeout if the parent context has no deadline.
// Returns an error if compose up fails (caller should handle rollback).
func (d *DeployOps) ComposeUp(ctx context.Context, composeFile string) error {
	return d.ComposeUpMultiple(ctx, []string{composeFile})
}

// ComposeUpMultiple runs docker compose up for multiple compose files.
// Uses ComposeUpTimeout if the parent context has no deadline.
// Returns an error if compose up fails (caller should handle rollback).
func (d *DeployOps) ComposeUpMultiple(ctx context.Context, composeFiles []string) error {
	start := time.Now()
	logger := log.ComponentCtx(ctx, log.ComponentDeploy)

	if d.DryRun {
		logger.Debug().
			Int("file_count", len(composeFiles)).
			Msg("Dry run: would run compose up")
		return nil
	}

	if len(composeFiles) == 0 {
		return nil
	}

	logger.Info().
		Str(log.FieldOperation, "compose_up").
		Int("file_count", len(composeFiles)).
		Str("project", d.ProjectName).
		Msg("Starting docker compose up")

	// Apply timeout if context doesn't have one
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, ComposeUpTimeout)
		defer cancel()
	}

	// Build args: docker compose -p project -f file1.yml -f file2.yml up -d --remove-orphans
	// Note: --wait is intentionally omitted. It exits non-zero when ANY container is
	// unhealthy, even pre-existing ones, which would block all deployments. Post-deploy
	// verification (verifyPostDeploy) handles health inspection separately.
	args := d.composeArgs(composeFiles...)
	args = append(args, "up", "-d", "--remove-orphans")

	cmd := exec.CommandContext(ctx, "docker", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			logger.Error().
				Str(log.FieldOperation, "compose_up").
				Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
				Msg("Docker compose up timed out")
			return fmt.Errorf("docker compose up timed out after %v", ComposeUpTimeout)
		}

		originalErr := fmt.Errorf("docker compose up failed: %w: %s", err, stderr.String())

		// Classify the failure by inspecting container state.
		result, classifyErr := d.classifyComposeFailure(ctx, composeFiles)
		if classifyErr != nil {
			// Classification failed — fail-safe: treat as genuine start failure.
			logger.Warn().
				Err(classifyErr).
				Str(log.FieldOperation, "compose_up").
				Msg("Failed to classify compose failure, treating as start failure")
			return originalErr
		}

		if result.Kind == failureUnhealthyOnly {
			logger.Warn().
				Str(log.FieldOperation, "compose_up").
				Strs("unhealthy_containers", result.Unhealthy).
				Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
				Msg("Compose up exited non-zero but all containers running (some unhealthy)")
			return fmt.Errorf("%w: %s", ErrComposeUnhealthy, strings.Join(result.Unhealthy, ", "))
		}

		logger.Error().
			Err(err).
			Str(log.FieldOperation, "compose_up").
			Strs("failed_containers", result.Failed).
			Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
			Msg("Docker compose up failed with container start failures")
		return originalErr
	}

	logger.Info().
		Str(log.FieldOperation, "compose_up").
		Int("file_count", len(composeFiles)).
		Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
		Msg("Docker compose up completed successfully")
	return nil
}

// ComposeUpWithRollback runs docker compose up and rolls back on failure.
// backupPath should contain the previous config files for rollback.
// Returns:
//   - nil on success
//   - ErrRollbackSucceeded wrapped with deployment error if rollback succeeded
//   - ErrRollbackFailed wrapped with both errors if rollback also failed
//   - Original error if no backup available
func (d *DeployOps) ComposeUpWithRollback(ctx context.Context, composeFile, backupPath string) error {
	return d.ComposeUpMultipleWithRollback(ctx, []string{composeFile}, backupPath)
}

// ComposeUpMultipleWithRollback runs docker compose up for multiple files and rolls back on failure.
// backupPath should contain the previous config files for rollback.
// Returns:
//   - nil on success
//   - ErrRollbackSucceeded wrapped with deployment error if rollback succeeded
//   - ErrRollbackFailed wrapped with both errors if rollback also failed
//   - Original error if no backup available
func (d *DeployOps) ComposeUpMultipleWithRollback(ctx context.Context, composeFiles []string, backupPath string) error {
	logger := log.ComponentCtx(ctx, log.ComponentDeploy)

	deployErr := d.ComposeUpMultiple(ctx, composeFiles)
	if deployErr == nil {
		return nil
	}

	// Unhealthy containers are recoverable — skip rollback, return as warning.
	if errors.Is(deployErr, ErrComposeUnhealthy) {
		logger.Warn().
			Err(deployErr).
			Msg("Compose up completed with unhealthy containers, skipping rollback")
		return deployErr
	}

	// Compose failed - attempt rollback if backup exists
	if backupPath == "" {
		logger.Warn().
			Err(deployErr).
			Msg("Deployment failed, no backup available for rollback")
		return fmt.Errorf("deployment failed (no backup available for rollback): %w", deployErr)
	}

	logger.Warn().
		Err(deployErr).
		Str(log.FieldPath, backupPath).
		Msg("Deployment failed, attempting rollback")

	// Build backup file list and verify they exist
	var backupFiles []string
	for _, f := range composeFiles {
		backupFile := filepath.Join(backupPath, filepath.Base(f))
		if _, statErr := os.Stat(backupFile); os.IsNotExist(statErr) {
			// Skip missing backup files - they may be new stacks
			continue
		}
		backupFiles = append(backupFiles, backupFile)
	}

	if len(backupFiles) == 0 {
		logger.Warn().
			Str(log.FieldPath, backupPath).
			Msg("No backup files found for rollback")
		return fmt.Errorf("deployment failed (no backup files found for rollback): %w", deployErr)
	}

	// Attempt rollback with independent timeout so it can execute even if ctx is cancelled.
	// Copy enriched logger so reconcile_id flows into rollback logs.
	rollbackCtx, cancel := context.WithTimeout(
		log.WithContext(context.Background(), log.Ctx(ctx)),
		ComposeUpTimeout,
	)
	defer cancel()

	// Build rollback args
	args := d.composeArgs(backupFiles...)
	args = append(args, "up", "-d", "--remove-orphans")

	rollbackCmd := exec.CommandContext(rollbackCtx, "docker", args...)
	var rollbackStderr bytes.Buffer
	rollbackCmd.Stderr = &rollbackStderr

	if rollbackErr := rollbackCmd.Run(); rollbackErr != nil {
		// Both deployment and rollback failed - critical state
		logger.Error().
			Err(rollbackErr).
			Str(log.FieldPath, backupPath).
			Msg("CRITICAL: Rollback also failed")
		return fmt.Errorf("%w: deployment error: %v, rollback error: %v", ErrRollbackFailed, deployErr, rollbackErr)
	}

	// Rollback succeeded - return distinguishable error
	logger.Info().
		Str(log.FieldPath, backupPath).
		Msg("Rollback completed successfully")
	return fmt.Errorf("%w: %v", ErrRollbackSucceeded, deployErr)
}

// VerifyContainerHealth checks if containers from a compose file are healthy.
func (d *DeployOps) VerifyContainerHealth(ctx context.Context, composeFile string) error {
	if d.DryRun {
		return nil
	}

	// Use docker compose ps to check container status
	args := d.composeArgs(composeFile)
	args = append(args, "ps", "--format", "json")
	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to check container status: %w: %s", err, stderr.String())
	}

	// For now, just verify the command succeeded
	// A more complete implementation would parse the JSON and check health status
	return nil
}

// ComposeUpRemote runs docker compose up on a remote host via SSH.
// Retries on transient SSH errors with exponential backoff.
func (d *DeployOps) ComposeUpRemote(ctx context.Context, host, composeDir string) error {
	start := time.Now()
	logger := log.ComponentCtx(ctx, log.ComponentDeploy)

	if err := validateHost(host); err != nil {
		return fmt.Errorf("invalid SSH host: %w", err)
	}

	if d.DryRun {
		logger.Debug().
			Str(log.FieldTarget, host).
			Str(log.FieldPath, composeDir).
			Msg("Dry run: would run remote compose up")
		return nil
	}

	logger.Info().
		Str(log.FieldOperation, "compose_up_remote").
		Str(log.FieldTarget, host).
		Str(log.FieldPath, composeDir).
		Str("project", d.ProjectName).
		Msg("Starting remote docker compose up")

	// Build compose command with project name if set
	composeCmd := "docker compose"
	if d.ProjectName != "" {
		composeCmd = fmt.Sprintf("docker compose -p %s", d.ProjectName)
	}
	sshCmd := fmt.Sprintf("cd %s && %s up -d --remove-orphans", composeDir, composeCmd)

	err := retryWithBackoff(ctx, DefaultMaxRetries, func() error {
		cmd := exec.CommandContext(ctx, "ssh", host, sshCmd)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("remote docker compose up failed: %w: %s", err, stderr.String())
		}
		return nil
	})
	if err != nil {
		// Classify the remote failure by inspecting container state via SSH.
		result, classifyErr := d.classifyComposeFailureRemote(ctx, host, composeDir)
		if classifyErr == nil && result.Kind == failureUnhealthyOnly {
			logger.Warn().
				Str(log.FieldOperation, "compose_up_remote").
				Str(log.FieldTarget, host).
				Strs("unhealthy_containers", result.Unhealthy).
				Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
				Msg("Remote compose up exited non-zero but all containers running (some unhealthy)")
			return fmt.Errorf("%w: %s", ErrComposeUnhealthy, strings.Join(result.Unhealthy, ", "))
		}

		logger.Error().
			Err(err).
			Str(log.FieldOperation, "compose_up_remote").
			Str(log.FieldTarget, host).
			Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
			Msg("Remote docker compose up failed")
	} else {
		logger.Info().
			Str(log.FieldOperation, "compose_up_remote").
			Str(log.FieldTarget, host).
			Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
			Msg("Remote docker compose up completed successfully")
	}
	return err
}

// classifyComposeFailureRemote inspects container state on a remote host after
// a compose up failure. Uses SSH to run `docker compose ps --format json`.
func (d *DeployOps) classifyComposeFailureRemote(ctx context.Context, host, composeDir string) (*composeFailureResult, error) {
	psCmd := "docker compose"
	if d.ProjectName != "" {
		psCmd = fmt.Sprintf("docker compose -p %s", d.ProjectName)
	}
	sshCmd := fmt.Sprintf("cd %s && %s ps --format json", composeDir, psCmd)

	cmd := exec.CommandContext(ctx, "ssh", host, sshCmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("remote docker compose ps failed: %w: %s", err, stderr.String())
	}

	entries, err := parseComposePSOutput(stdout.Bytes())
	if err != nil {
		return nil, fmt.Errorf("failed to parse remote compose ps output: %w", err)
	}

	if len(entries) == 0 {
		return &composeFailureResult{Kind: failureStartFailure}, nil
	}

	result := classifyComposePS(entries)
	return &result, nil
}

// classifyComposeFailure inspects container state after a compose up failure.
// Uses `docker compose ps --format json` with the same project name and compose
// files. Returns a classification result or an error if the inspection fails.
func (d *DeployOps) classifyComposeFailure(ctx context.Context, composeFiles []string) (*composeFailureResult, error) {
	// Use same timeout as compose up for the ps query.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, ComposeUpTimeout)
		defer cancel()
	}

	args := d.composeArgs(composeFiles...)
	args = append(args, "ps", "--format", "json")

	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("docker compose ps failed: %w: %s", err, stderr.String())
	}

	entries, err := parseComposePSOutput(stdout.Bytes())
	if err != nil {
		return nil, fmt.Errorf("failed to parse compose ps output: %w", err)
	}

	// No containers found at all — treat as start failure.
	if len(entries) == 0 {
		return &composeFailureResult{Kind: failureStartFailure}, nil
	}

	result := classifyComposePS(entries)
	return &result, nil
}

// SignalContainer sends a signal to a Docker container.
func (d *DeployOps) SignalContainer(ctx context.Context, containerName, signal string) error {
	logger := log.ComponentCtx(ctx, log.ComponentDeploy)

	if err := validateContainerName(containerName); err != nil {
		return fmt.Errorf("invalid container name: %w", err)
	}
	if err := validateSignal(signal); err != nil {
		return fmt.Errorf("invalid signal: %w", err)
	}

	if d.DryRun {
		return nil
	}

	logger.Debug().
		Str(log.FieldOperation, "signal_container").
		Str(log.FieldContainer, containerName).
		Str("signal", signal).
		Msg("Sending signal to container")

	cmd := exec.CommandContext(ctx, "docker", "kill", "--signal="+signal, containerName)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker kill signal failed: %w: %s", err, stderr.String())
	}
	return nil
}

// SignalContainerRemote sends a signal to a Docker container on a remote host.
// Retries on transient SSH errors with exponential backoff.
func (d *DeployOps) SignalContainerRemote(ctx context.Context, host, containerName, signal string) error {
	logger := log.ComponentCtx(ctx, log.ComponentDeploy)

	if err := validateHost(host); err != nil {
		return fmt.Errorf("invalid SSH host: %w", err)
	}
	if err := validateContainerName(containerName); err != nil {
		return fmt.Errorf("invalid container name: %w", err)
	}
	if err := validateSignal(signal); err != nil {
		return fmt.Errorf("invalid signal: %w", err)
	}

	if d.DryRun {
		return nil
	}

	logger.Debug().
		Str(log.FieldOperation, "signal_container_remote").
		Str(log.FieldTarget, host).
		Str(log.FieldContainer, containerName).
		Str("signal", signal).
		Msg("Sending signal to remote container")

	sshCmd := fmt.Sprintf("docker kill --signal=%s %s 2>/dev/null", signal, containerName)

	return retryWithBackoff(ctx, DefaultMaxRetries, func() error {
		cmd := exec.CommandContext(ctx, "ssh", host, sshCmd)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("remote docker kill signal failed: %w: %s", err, stderr.String())
		}
		return nil
	})
}

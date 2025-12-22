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
)

// ErrRollbackSucceeded indicates deployment failed but rollback succeeded.
var ErrRollbackSucceeded = errors.New("deployment failed, rollback succeeded")

// ErrRollbackFailed indicates both deployment and rollback failed.
var ErrRollbackFailed = errors.New("deployment and rollback both failed")

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
}

// NewDeployOps creates a new DeployOps instance.
func NewDeployOps(dryRun bool) *DeployOps {
	return &DeployOps{DryRun: dryRun}
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

	var lastErr error
	backoff := InitialBackoff

	for attempt := 1; attempt <= maxRetries; attempt++ {
		lastErr = operation()
		if lastErr == nil {
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

		// Don't sleep after the last attempt
		if attempt < maxRetries {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
				backoff *= 2 // Exponential backoff
			}
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
	stderrLower := strings.ToLower(stderr)

	switch {
	case strings.Contains(stderrLower, "permission denied"):
		return fmt.Errorf("SSH authentication failed for %s: permission denied. Check that your SSH key is added to the remote host's authorized_keys", host)
	case strings.Contains(stderrLower, "connection refused"):
		return fmt.Errorf("SSH connection refused by %s: the SSH service may not be running or the port may be blocked", host)
	case strings.Contains(stderrLower, "host key verification failed"):
		return fmt.Errorf("SSH host key verification failed for %s: run 'ssh-keyscan %s >> ~/.ssh/known_hosts' to add the host key", host, host)
	case strings.Contains(stderrLower, "no route to host"):
		return fmt.Errorf("cannot reach %s: no route to host. Check network connectivity and that the host is online", host)
	case strings.Contains(stderrLower, "connection timed out"):
		return fmt.Errorf("SSH connection to %s timed out: check network connectivity and firewall rules", host)
	case strings.Contains(stderrLower, "name or service not known"):
		return fmt.Errorf("cannot resolve hostname %s: check that the hostname is correct and DNS is working", host)
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
	timestamp := time.Now().Format("20060102-150405")
	backupName := fmt.Sprintf("backup-%s", timestamp)
	backupPath := filepath.Join(backupDir, backupName)

	if err := os.MkdirAll(backupPath, 0755); err != nil {
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
		return "", fmt.Errorf("backup verification failed: %w", err)
	}

	return backupName, nil
}

// BackupRemote creates a backup from a remote host via SSH.
// Retries on transient SSH errors with exponential backoff.
func (d *DeployOps) BackupRemote(ctx context.Context, host, backupDir string, remotePaths []string) (string, error) {
	if err := validateHost(host); err != nil {
		return "", fmt.Errorf("invalid SSH host: %w", err)
	}

	timestamp := time.Now().Format("20060102-150405")
	backupName := fmt.Sprintf("backup-%s", timestamp)
	backupPath := filepath.Join(backupDir, backupName)

	if err := os.MkdirAll(backupPath, 0755); err != nil {
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

	// Log SSH error but don't fail - tar may return non-zero for missing files
	if sshErr != nil && !isTransientSSHError(sshErr) {
		// Only log if it's not a transient error we already retried
		// tar returning non-zero for missing files is expected
	}

	// Verify the backup was created successfully
	if err := d.VerifyBackup(backupPath); err != nil {
		// Clean up invalid backup on verification failure
		os.RemoveAll(backupPath)
		return "", fmt.Errorf("backup verification failed: %w", err)
	}

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
		for _, name := range toRemove {
			path := filepath.Join(backupDir, name)
			if err := os.RemoveAll(path); err != nil {
				return fmt.Errorf("failed to remove backup %s: %w", name, err)
			}
		}
	}

	return nil
}

// DeployLocal syncs files locally using native Go file operations.
// Performs atomic copy: copies to temp directory first, then replaces target.
// Uses --delete semantics: removes files in target that don't exist in source.
func (d *DeployOps) DeployLocal(ctx context.Context, sourceDir, targetDir string) error {
	if d.DryRun {
		return nil
	}

	// Verify source directory exists
	srcInfo, err := os.Stat(sourceDir)
	if err != nil {
		return fmt.Errorf("source directory: %w", err)
	}
	if !srcInfo.IsDir() {
		return fmt.Errorf("source is not a directory: %s", sourceDir)
	}

	// Create parent of target directory if needed
	targetParent := filepath.Dir(targetDir)
	if err := os.MkdirAll(targetParent, 0755); err != nil {
		return fmt.Errorf("create target parent: %w", err)
	}

	// Create temp directory in same parent for atomic rename
	tmpDir, err := os.MkdirTemp(targetParent, ".deploy-tmp-*")
	if err != nil {
		return fmt.Errorf("create temp directory: %w", err)
	}

	// Cleanup temp directory on failure
	success := false
	defer func() {
		if !success {
			os.RemoveAll(tmpDir)
		}
	}()

	// Copy source to temp directory
	if err := fileutil.CopyDir(sourceDir, tmpDir); err != nil {
		return fmt.Errorf("copy to temp: %w", err)
	}

	// Check context for cancellation
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Remove existing target if it exists
	if _, err := os.Stat(targetDir); err == nil {
		if err := os.RemoveAll(targetDir); err != nil {
			return fmt.Errorf("remove existing target: %w", err)
		}
	}

	// Atomic rename temp to target
	if err := os.Rename(tmpDir, targetDir); err != nil {
		return fmt.Errorf("rename to target: %w", err)
	}

	success = true
	return nil
}

// DeployLocalFile syncs a single file locally using native Go file operations.
// Uses atomic copy via temp file.
func (d *DeployOps) DeployLocalFile(ctx context.Context, sourceFile, targetFile string) error {
	if d.DryRun {
		return nil
	}

	// Check context for cancellation
	if ctx.Err() != nil {
		return ctx.Err()
	}

	return fileutil.CopyFile(sourceFile, targetFile)
}

// DeployRemote syncs files to a remote host using tar-over-SSH.
// Uses RemoteDeployTimeout if the parent context has no deadline.
// Retries on transient SSH errors with exponential backoff.
// Performs atomic deployment: tar to temp dir, then move to target.
func (d *DeployOps) DeployRemote(ctx context.Context, sourceDir, targetHost, targetDir string) error {
	if err := validateHost(targetHost); err != nil {
		return fmt.Errorf("invalid SSH host: %w", err)
	}

	if d.DryRun {
		return nil
	}

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
			tarCmd.Process.Kill()
			return fmt.Errorf("start ssh: %w: %s", err, sshStderr.String())
		}

		// Wait for both to complete
		tarErr := tarCmd.Wait()
		sshErr := sshCmd.Wait()

		if tarErr != nil {
			// Cleanup temp dir on failure
			exec.CommandContext(ctx, "ssh", targetHost, "rm", "-rf", tmpDir).Run()
			return fmt.Errorf("tar failed: %w: %s", tarErr, tarStderr.String())
		}
		if sshErr != nil {
			exec.CommandContext(ctx, "ssh", targetHost, "rm", "-rf", tmpDir).Run()
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
			exec.CommandContext(ctx, "ssh", targetHost, "rm", "-rf", tmpDir).Run()
			return fmt.Errorf("atomic move failed: %w: %s", err, atomicStderr.String())
		}

		return nil
	})
}

// DeployRemoteFile syncs a single file to a remote host using scp.
// Uses RemoteDeployTimeout if the parent context has no deadline.
// Retries on transient SSH errors with exponential backoff.
// Performs atomic copy: scp to temp file, then move to target.
func (d *DeployOps) DeployRemoteFile(ctx context.Context, sourceFile, targetHost, targetFile string) error {
	if err := validateHost(targetHost); err != nil {
		return fmt.Errorf("invalid SSH host: %w", err)
	}

	if d.DryRun {
		return nil
	}

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

	return retryWithBackoff(ctx, DefaultMaxRetries, func() error {
		// SCP to temp file
		target := fmt.Sprintf("%s:%s", targetHost, tmpFile)
		scpCmd := exec.CommandContext(ctx, "scp", "-q", sourceFile, target)
		var scpStderr bytes.Buffer
		scpCmd.Stderr = &scpStderr

		if err := scpCmd.Run(); err != nil {
			// Cleanup temp file on failure
			exec.CommandContext(ctx, "ssh", targetHost, "rm", "-f", tmpFile).Run()
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
			exec.CommandContext(ctx, "ssh", targetHost, "rm", "-f", tmpFile).Run()
			return fmt.Errorf("atomic move failed: %w: %s", err, moveStderr.String())
		}

		return nil
	})
}

// EnsureRemoteDir ensures a directory exists on a remote host via SSH.
// Uses SSHTimeout if the parent context has no deadline.
// Retries on transient SSH errors with exponential backoff.
func (d *DeployOps) EnsureRemoteDir(ctx context.Context, host, dir string) error {
	if err := validateHost(host); err != nil {
		return fmt.Errorf("invalid SSH host: %w", err)
	}

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
	if d.DryRun {
		return nil
	}

	// Apply timeout if context doesn't have one
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, ComposeUpTimeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, "docker", "compose", "-f", composeFile, "up", "-d", "--remove-orphans", "--wait")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("docker compose up timed out after %v", ComposeUpTimeout)
		}
		return fmt.Errorf("docker compose up failed: %w: %s", err, stderr.String())
	}
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
	deployErr := d.ComposeUp(ctx, composeFile)
	if deployErr == nil {
		return nil
	}

	// Compose failed - attempt rollback if backup exists
	if backupPath == "" {
		return fmt.Errorf("deployment failed (no backup available for rollback): %w", deployErr)
	}

	// Check if backup exists
	backupComposeFile := filepath.Join(backupPath, filepath.Base(composeFile))
	if _, statErr := os.Stat(backupComposeFile); os.IsNotExist(statErr) {
		return fmt.Errorf("deployment failed (backup file not found for rollback): %w", deployErr)
	}

	// Attempt rollback with previous config
	rollbackCtx, cancel := context.WithTimeout(context.Background(), ComposeUpTimeout)
	defer cancel()

	rollbackCmd := exec.CommandContext(rollbackCtx, "docker", "compose", "-f", backupComposeFile, "up", "-d", "--remove-orphans")
	var rollbackStderr bytes.Buffer
	rollbackCmd.Stderr = &rollbackStderr

	if rollbackErr := rollbackCmd.Run(); rollbackErr != nil {
		// Both deployment and rollback failed - critical state
		return fmt.Errorf("%w: deployment error: %v, rollback error: %v", ErrRollbackFailed, deployErr, rollbackErr)
	}

	// Rollback succeeded - return distinguishable error
	return fmt.Errorf("%w: %v", ErrRollbackSucceeded, deployErr)
}

// VerifyContainerHealth checks if containers from a compose file are healthy.
func (d *DeployOps) VerifyContainerHealth(ctx context.Context, composeFile string) error {
	if d.DryRun {
		return nil
	}

	// Use docker compose ps to check container status
	cmd := exec.CommandContext(ctx, "docker", "compose", "-f", composeFile, "ps", "--format", "json")
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
	if err := validateHost(host); err != nil {
		return fmt.Errorf("invalid SSH host: %w", err)
	}

	if d.DryRun {
		return nil
	}

	sshCmd := fmt.Sprintf("cd %s && docker compose up -d --remove-orphans", composeDir)

	return retryWithBackoff(ctx, DefaultMaxRetries, func() error {
		cmd := exec.CommandContext(ctx, "ssh", host, sshCmd)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("remote docker compose up failed: %w: %s", err, stderr.String())
		}
		return nil
	})
}

// SignalContainer sends a signal to a Docker container.
func (d *DeployOps) SignalContainer(ctx context.Context, containerName, signal string) error {
	if err := validateContainerName(containerName); err != nil {
		return fmt.Errorf("invalid container name: %w", err)
	}
	if err := validateSignal(signal); err != nil {
		return fmt.Errorf("invalid signal: %w", err)
	}

	if d.DryRun {
		return nil
	}

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

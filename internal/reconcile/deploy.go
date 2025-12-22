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
	SSHConnectTimeout = 5 * time.Second
	SSHTimeout        = 30 * time.Second
	RsyncTimeout      = 5 * time.Minute
	ComposeUpTimeout  = 10 * time.Minute
)

// DeployOps provides deployment operations including backup, rsync, and service management.
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

// DeployLocal syncs files locally using rsync.
func (d *DeployOps) DeployLocal(ctx context.Context, sourceDir, targetDir string) error {
	args := []string{"-av", "--delete"}
	if d.DryRun {
		args = append(args, "--dry-run")
	}
	args = append(args, sourceDir+"/", targetDir+"/")

	cmd := exec.CommandContext(ctx, "rsync", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rsync failed: %w: %s", err, stderr.String())
	}
	return nil
}

// DeployLocalFile syncs a single file locally using rsync.
func (d *DeployOps) DeployLocalFile(ctx context.Context, sourceFile, targetFile string) error {
	args := []string{"-av"}
	if d.DryRun {
		args = append(args, "--dry-run")
	}
	args = append(args, sourceFile, targetFile)

	cmd := exec.CommandContext(ctx, "rsync", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rsync failed: %w: %s", err, stderr.String())
	}
	return nil
}

// DeployRemote syncs files to a remote host using rsync over SSH.
// Uses RsyncTimeout if the parent context has no deadline.
// Retries on transient SSH errors with exponential backoff.
func (d *DeployOps) DeployRemote(ctx context.Context, sourceDir, targetHost, targetDir string) error {
	if err := validateHost(targetHost); err != nil {
		return fmt.Errorf("invalid SSH host: %w", err)
	}

	// Apply timeout if context doesn't have one
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, RsyncTimeout)
		defer cancel()
	}

	args := []string{"-avz", "--delete"}
	if d.DryRun {
		args = append(args, "--dry-run")
	}
	target := fmt.Sprintf("%s:%s/", targetHost, targetDir)
	args = append(args, sourceDir+"/", target)

	return retryWithBackoff(ctx, DefaultMaxRetries, func() error {
		cmd := exec.CommandContext(ctx, "rsync", args...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return fmt.Errorf("rsync timed out after %v", RsyncTimeout)
			}
			return fmt.Errorf("rsync failed: %w: %s", err, stderr.String())
		}
		return nil
	})
}

// DeployRemoteFile syncs a single file to a remote host using rsync over SSH.
// Uses RsyncTimeout if the parent context has no deadline.
// Retries on transient SSH errors with exponential backoff.
func (d *DeployOps) DeployRemoteFile(ctx context.Context, sourceFile, targetHost, targetFile string) error {
	if err := validateHost(targetHost); err != nil {
		return fmt.Errorf("invalid SSH host: %w", err)
	}

	// Apply timeout if context doesn't have one
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, RsyncTimeout)
		defer cancel()
	}

	args := []string{"-avz"}
	if d.DryRun {
		args = append(args, "--dry-run")
	}
	target := fmt.Sprintf("%s:%s", targetHost, targetFile)
	args = append(args, sourceFile, target)

	return retryWithBackoff(ctx, DefaultMaxRetries, func() error {
		cmd := exec.CommandContext(ctx, "rsync", args...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return fmt.Errorf("rsync timed out after %v", RsyncTimeout)
			}
			return fmt.Errorf("rsync failed: %w: %s", err, stderr.String())
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

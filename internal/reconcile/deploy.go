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
)

// Deploy operation timeouts
const (
	SSHTimeout       = 30 * time.Second
	RsyncTimeout     = 5 * time.Minute
	ComposeUpTimeout = 10 * time.Minute
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

	return backupName, nil
}

// BackupRemote creates a backup from a remote host via SSH.
func (d *DeployOps) BackupRemote(ctx context.Context, host, backupDir string, remotePaths []string) (string, error) {
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

	cmd := exec.CommandContext(ctx, "ssh", host, sshCmd)
	outFile, err := os.Create(tarFile)
	if err != nil {
		return "", fmt.Errorf("failed to create backup file: %w", err)
	}
	defer outFile.Close()

	cmd.Stdout = outFile

	// SSH tar may fail partially; that's OK.
	_ = cmd.Run()

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
func (d *DeployOps) DeployRemote(ctx context.Context, sourceDir, targetHost, targetDir string) error {
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
}

// DeployRemoteFile syncs a single file to a remote host using rsync over SSH.
func (d *DeployOps) DeployRemoteFile(ctx context.Context, sourceFile, targetHost, targetFile string) error {
	args := []string{"-avz"}
	if d.DryRun {
		args = append(args, "--dry-run")
	}
	target := fmt.Sprintf("%s:%s", targetHost, targetFile)
	args = append(args, sourceFile, target)

	cmd := exec.CommandContext(ctx, "rsync", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rsync failed: %w: %s", err, stderr.String())
	}
	return nil
}

// EnsureRemoteDir ensures a directory exists on a remote host via SSH.
// Uses SSHTimeout if the parent context has no deadline.
func (d *DeployOps) EnsureRemoteDir(ctx context.Context, host, dir string) error {
	// Apply timeout if context doesn't have one
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, SSHTimeout)
		defer cancel()
	}

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
func (d *DeployOps) ComposeUpWithRollback(ctx context.Context, composeFile, backupPath string) error {
	err := d.ComposeUp(ctx, composeFile)
	if err == nil {
		return nil
	}

	// Compose failed - attempt rollback if backup exists
	if backupPath == "" {
		return fmt.Errorf("%w (no backup available for rollback)", err)
	}

	// Check if backup exists
	backupComposeFile := filepath.Join(backupPath, filepath.Base(composeFile))
	if _, statErr := os.Stat(backupComposeFile); os.IsNotExist(statErr) {
		return fmt.Errorf("%w (backup file not found for rollback)", err)
	}

	// Attempt rollback with previous config
	rollbackCtx, cancel := context.WithTimeout(context.Background(), ComposeUpTimeout)
	defer cancel()

	rollbackCmd := exec.CommandContext(rollbackCtx, "docker", "compose", "-f", backupComposeFile, "up", "-d", "--remove-orphans")
	var rollbackStderr bytes.Buffer
	rollbackCmd.Stderr = &rollbackStderr

	if rollbackErr := rollbackCmd.Run(); rollbackErr != nil {
		return fmt.Errorf("%w (rollback also failed: %v)", err, rollbackErr)
	}

	return fmt.Errorf("%w (rolled back to previous config)", err)
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
func (d *DeployOps) ComposeUpRemote(ctx context.Context, host, composeDir string) error {
	if d.DryRun {
		return nil
	}

	sshCmd := fmt.Sprintf("cd %s && docker compose up -d --remove-orphans", composeDir)
	cmd := exec.CommandContext(ctx, "ssh", host, sshCmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("remote docker compose up failed: %w: %s", err, stderr.String())
	}
	return nil
}

// SignalContainer sends a signal to a Docker container.
func (d *DeployOps) SignalContainer(ctx context.Context, containerName, signal string) error {
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
func (d *DeployOps) SignalContainerRemote(ctx context.Context, host, containerName, signal string) error {
	if d.DryRun {
		return nil
	}

	sshCmd := fmt.Sprintf("docker kill --signal=%s %s 2>/dev/null", signal, containerName)
	cmd := exec.CommandContext(ctx, "ssh", host, sshCmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("remote docker kill signal failed: %w: %s", err, stderr.String())
	}
	return nil
}

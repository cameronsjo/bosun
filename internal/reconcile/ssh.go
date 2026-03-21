package reconcile

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cameronsjo/bosun/internal/log"
)

// SSH retry configuration
const (
	DefaultMaxRetries = 3
	InitialBackoff    = 1 * time.Second
)

// Deploy operation timeouts
const (
	SSHConnectTimeout   = 5 * time.Second
	SSHTimeout          = 30 * time.Second
	RemoteDeployTimeout = 5 * time.Minute
)

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

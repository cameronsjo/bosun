package reconcile

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/cameronsjo/bosun/internal/log"
	"github.com/cameronsjo/bosun/internal/telemetry"
	"github.com/kballard/go-shellquote"
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

// bosunOldSuffix marks the retained previous target tree during a swap
// (<target>.bosun-old.<unix-nanos>). Crash recovery globs on it.
const bosunOldSuffix = ".bosun-old."

// buildRemoteSwapCommand returns the POSIX shell that promotes tmpDir into
// targetDir while retaining the old tree until the new one is in place (#343):
// move-aside → move-in → cleanup, with a rollback branch that restores the
// old tree when the move-in fails. The rollback clears a partial targetDir
// first — on Unraid's /mnt/user shfs a cross-device mv can fail after
// creating a partial destination, and a naive `mv old target` would nest the
// old tree INTO that partial dir instead of restoring it.
//
// This is "safe, not atomic": shfs renames are not kernel-atomic even for
// same-parent paths (EXDEV persists), so the guarantee is that no sequence
// deletes the live target before the replacement is durably in place — an
// interrupted swap leaves the old OR the new tree, never neither.
// withSync appends a `sync` after the move-in (FUSE settle discipline, #402).
func buildRemoteSwapCommand(targetDir, tmpDir, oldDir string, withSync bool) string {
	target := shellquote.Join(targetDir)
	tmp := shellquote.Join(tmpDir)
	old := shellquote.Join(oldDir)

	syncStep := ""
	if withSync {
		syncStep = "sync; "
	}

	return "set -e; " +
		"if [ -e " + target + " ]; then mv " + target + " " + old + "; fi; " +
		"if mv " + tmp + " " + target + "; then " +
		syncStep +
		"rm -rf " + old + "; " +
		"else " +
		"status=$?; " +
		"if [ -e " + target + " ]; then rm -rf " + target + "; fi; " +
		"if [ -e " + old + " ]; then mv " + old + " " + target + "; fi; " +
		"exit $status; " +
		"fi"
}

// buildRemoteRecoverCommand returns the POSIX shell that heals an interrupted
// swap at the start of the next deploy (#343 crash recovery): when targetDir
// is missing but retained `.bosun-old.<ts>` siblings exist, the newest one is
// promoted back to targetDir (timestamps are fixed-width unix nanos, so
// lexical sort orders them), then remaining orphans are removed. A failed
// promotion aborts (set -e) before the orphan cleanup can delete the only
// surviving copy of the target.
func buildRemoteRecoverCommand(targetDir string) string {
	target := shellquote.Join(targetDir)
	// The glob star must stay outside the quoting so the remote shell expands it.
	oldGlob := shellquote.Join(targetDir+bosunOldSuffix) + "*"

	// The 2>/dev/null on ls silences the expected no-retained-copies case: an
	// unmatched glob reaches ls as a literal and errors, but the pipeline's
	// exit is tail's (0), so only the noise needs suppressing — an empty
	// $newest already encodes "nothing to promote".
	// The trailing 2>/dev/null || true lets the orphan cleanup tolerate the
	// same unmatched-glob literal under set -e: no orphans is the healthy
	// steady state, not a deploy failure.
	return "set -e; " +
		"if [ ! -e " + target + " ]; then " +
		"newest=$(ls -d " + oldGlob + " 2>/dev/null | sort | tail -n 1); " +
		"if [ -n \"$newest\" ]; then mv \"$newest\" " + target + "; fi; " +
		"fi; " +
		"rm -rf " + oldGlob + " 2>/dev/null || true"
}

// DeployRemote syncs files to a remote host using tar-over-SSH.
// Uses RemoteDeployTimeout if the parent context has no deadline.
// Retries on transient SSH errors with exponential backoff.
// Deployment is safe-by-ordering: tar to a temp dir, then a retain-old
// rename-swap that never deletes the live target before the replacement
// is in place (#343), with crash recovery for interrupted swaps.
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

	ctx, deploySpan := telemetry.Tracer("reconcile").Start(ctx, "reconcile.deploy_remote",
		trace.WithAttributes(
			telemetry.StringAttr("target_host", targetHost),
			telemetry.StringAttr("target_dir", targetDir),
			telemetry.BoolAttr("under_fuse", IsUnderFUSEDeployPath(targetDir)),
		),
	)
	defer deploySpan.End()

	// Ensure target directory parent exists on remote
	targetParent := filepath.Dir(targetDir)
	if err := d.EnsureRemoteDir(ctx, targetHost, targetParent); err != nil {
		err = fmt.Errorf("ensure remote parent dir: %w", err)
		telemetry.SpanError(deploySpan, err)
		return err
	}

	// Create temp directory on remote for atomic deployment
	// Use unique name based on target to avoid collisions
	tmpDirName := fmt.Sprintf(".deploy-tmp-%d", time.Now().UnixNano())
	tmpDir := filepath.Join(targetParent, tmpDirName)

	deployErr := retryWithBackoff(ctx, DefaultMaxRetries, func() error {
		// Heal any interrupted swap from a prior run before staging this one:
		// promote the newest retained tree when the target is missing, then
		// clean orphaned .bosun-old.<ts> siblings (#343 crash recovery).
		// Running it per attempt also self-heals between retries.
		logger.Debug().
			Str(log.FieldOperation, "recover_swap").
			Str(log.FieldTarget, targetHost).
			Str(log.FieldPath, targetDir).
			Msg("Preparing to recover any interrupted swap from prior deploy")
		recoverCmd := exec.CommandContext(ctx, "ssh", targetHost, buildRemoteRecoverCommand(targetDir))
		var recoverStderr bytes.Buffer
		recoverCmd.Stderr = &recoverStderr
		if err := recoverCmd.Run(); err != nil {
			return fmt.Errorf("recover interrupted swap, expected target %s present or a promotable retained copy: %w: %s", targetDir, err, recoverStderr.String())
		}

		// Create temp directory on remote
		logger.Debug().
			Str(log.FieldOperation, "prepare_staging").
			Str(log.FieldTarget, targetHost).
			Str(log.FieldPath, tmpDir).
			Msg("Preparing to create staging directory on remote")
		mkdirCmd := exec.CommandContext(ctx, "ssh", targetHost, "mkdir", "-p", tmpDir)
		var mkdirStderr bytes.Buffer
		mkdirCmd.Stderr = &mkdirStderr
		if err := mkdirCmd.Run(); err != nil {
			return fmt.Errorf("create remote temp dir: %w: %s", err, mkdirStderr.String())
		}

		// Tar source directory and pipe to SSH for extraction on remote
		// tar -C sourceDir -cf - . | ssh host "tar -C tmpDir -xf -"
		logger.Debug().
			Str(log.FieldOperation, "tar_extract").
			Str(log.FieldTarget, targetHost).
			Str("source", sourceDir).
			Str("dest", tmpDir).
			Msg("Preparing to tar and extract staging directory to remote")
		tarCmd := exec.CommandContext(ctx, "tar", "-C", sourceDir, "-cf", "-", ".")
		sshCmd := exec.CommandContext(ctx, "ssh", targetHost, fmt.Sprintf("tar -C %s -xf -", shellquote.Join(tmpDir)))

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

		// Retain-old rename-swap (#343): the live target is moved aside, not
		// deleted, until the replacement is in place; a failed move-in rolls
		// the old tree back (clearing any partial destination first).
		underFUSE := IsUnderFUSEDeployPath(targetDir)
		oldDir := fmt.Sprintf("%s%s%d", targetDir, bosunOldSuffix, time.Now().UnixNano())
		logger.Debug().
			Str(log.FieldOperation, "swap_deploy").
			Str(log.FieldTarget, targetHost).
			Str(log.FieldPath, targetDir).
			Bool("under_fuse", underFUSE).
			Msg("Preparing to swap staged directory into live target (retain-old-until-new pattern)")
		swapCmd := exec.CommandContext(ctx, "ssh", targetHost, buildRemoteSwapCommand(targetDir, tmpDir, oldDir, underFUSE))
		var swapStderr bytes.Buffer
		swapCmd.Stderr = &swapStderr

		if err := swapCmd.Run(); err != nil {
			// Try to cleanup temp dir; the swap's rollback branch already
			// restored the old target (or the next attempt's recovery will).
			_ = exec.CommandContext(ctx, "ssh", targetHost, "rm", "-rf", tmpDir).Run()
			return fmt.Errorf("swap staged tree into %s failed, old tree retained or restored: %w: %s", targetDir, err, swapStderr.String())
		}
		logger.Info().
			Str(log.FieldOperation, "swap_deploy").
			Str(log.FieldTarget, targetHost).
			Msg("Successfully promoted staged tree to live target (old tree cleaned up)")

		// FUSE settle discipline (#402): shfs writes need time to propagate
		// before consumers (compose up, hooks, doctor) read the files back.
		if underFUSE {
			logger.Debug().
				Str(log.FieldPath, targetDir).
				Dur("settle_delay", defaultFUSESettleDelay).
				Msg("Deploy target is on a FUSE mount, waiting for writes to settle")
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(defaultFUSESettleDelay):
			}
		}

		logger.Debug().
			Str(log.FieldOperation, "deploy_remote").
			Str(log.FieldTarget, targetHost).
			Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
			Msg("Remote deployment completed")
		return nil
	})
	if deployErr != nil {
		telemetry.SpanError(deploySpan, deployErr)
	} else {
		telemetry.SpanOK(deploySpan)
	}
	return deployErr
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

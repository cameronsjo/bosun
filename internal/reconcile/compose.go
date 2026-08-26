package reconcile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/cameronsjo/bosun/internal/log"
	"github.com/cameronsjo/bosun/internal/ui"
	"github.com/kballard/go-shellquote"
)

// DefaultComposeUpTimeout is the maximum time allowed for docker compose up.
const DefaultComposeUpTimeout = 10 * time.Minute

// ErrRollbackSucceeded indicates deployment failed but rollback succeeded.
var ErrRollbackSucceeded = errors.New("deployment failed, rollback succeeded")

// ErrRollbackFailed indicates both deployment and rollback failed.
var ErrRollbackFailed = errors.New("deployment and rollback both failed")

// ErrComposeUnhealthy indicates compose exited non-zero but all containers are
// running (some unhealthy). This is recoverable and should not trigger rollback.
var ErrComposeUnhealthy = errors.New("compose up completed with unhealthy containers")

// ComposeUp runs docker compose up for the specified compose file.
// Uses the configured compose up timeout if the parent context has no deadline.
// Returns an error if compose up fails (caller should handle rollback).
func (d *DeployOps) ComposeUp(ctx context.Context, composeFile string) error {
	return d.ComposeUpMultiple(ctx, []string{composeFile})
}

// ComposeUpMultiple runs docker compose up for multiple compose files.
// Uses the configured compose up timeout if the parent context has no deadline.
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
	timeout := d.composeUpTimeout()
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// Build args: docker compose -p project -f file1.yml -f file2.yml up -d [--remove-orphans]
	// Note: --wait is intentionally omitted. It exits non-zero when ANY container is
	// unhealthy, even pre-existing ones, which would block all deployments. Post-deploy
	// verification (verifyPostDeploy) handles health inspection separately.
	args := d.composeUpArgs(composeFiles)

	cmd := dockerComposeCommandContext(ctx, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			if !errors.Is(ctxErr, context.DeadlineExceeded) {
				return fmt.Errorf("docker compose up canceled: %w", ctxErr)
			}
			logger.Error().
				Str(log.FieldOperation, "compose_up").
				Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
				Msg("Docker compose up timed out")
			return fmt.Errorf("docker compose up timed out after %v: %w", timeout, ctxErr)
		}

		stderrStr := stderr.String()
		originalErr := fmt.Errorf("docker compose up failed: %w: %s", err, stderrStr)

		// Detect container name conflicts from project name mismatch.
		logger.Debug().
			Str(log.FieldOperation, "compose_up").
			Int("file_count", len(composeFiles)).
			Msg("Scanning stderr for container name conflicts")
		if conflicts := detectNameConflicts(stderrStr); len(conflicts) > 0 {
			logger.Error().
				Str(log.FieldOperation, "compose_up").
				Strs("conflicting_containers", conflicts).
				Str("project", d.ProjectName).
				Msg("Container name conflict detected — existing containers were created under a different project name")
			ui.Warning("Container name conflict: existing containers were created under a different project name")
			for _, name := range conflicts {
				logger.Warn().
					Str(log.FieldContainer, name).
					Msgf("Remediation: docker rm -f %s", name)
				ui.Warning("  Conflicting container %q — remediation: docker rm -f %s", name, name)
			}
			logger.Info().
				Str(log.FieldOperation, "compose_up").
				Int("conflict_count", len(conflicts)).
				Msg("Container name conflicts detected, remediation commands logged")
		}

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

// errRollbackNotAttempted marks a RollbackFromBackup failure that occurred
// before any rollback command ran (no backup path, no archive, or no matching
// backup files) — distinct from the rollback command itself failing.
var errRollbackNotAttempted = errors.New("rollback not attempted")

// ComposeUpIsolated runs compose up per-file with isolated failure handling.
// Phase 1: each file gets its own compose up (no --remove-orphans). On failure,
// the single file is rolled back from backup if available.
// Phase 2: a single orphan-reconciliation pass with all files and --remove-orphans.
func (d *DeployOps) ComposeUpIsolated(ctx context.Context, composeFiles []string, backupPath string) (*ComposeUpSummary, error) {
	logger := log.ComponentCtx(ctx, log.ComponentDeploy)

	if d.DryRun {
		logger.Debug().
			Int("file_count", len(composeFiles)).
			Msg("Dry run: would run isolated compose up per file")
		results := make([]ComposeFileResult, len(composeFiles))
		for i, f := range composeFiles {
			results[i] = ComposeFileResult{File: f, Success: true}
		}
		summary := classifyComposeResults(results)
		return &summary, nil
	}

	upFn := d.composeUpFn
	if upFn == nil {
		// Clone DeployOps with RemoveOrphans disabled for Phase 1.
		// Phase 1 runs each file individually — --remove-orphans would kill
		// containers from other files not passed to the command.
		noOrphanOps := *d
		noOrphanOps.RemoveOrphans = false
		upFn = noOrphanOps.ComposeUpMultiple
	}

	// Lazily extract the backup archive on first rollback need. Backup() writes
	// configs.tar.gz (absolute paths inside), not loose files under backupPath,
	// so rollback must consult the extracted tree (#332/#335). Extraction is
	// skipped entirely on the happy path (no failures).
	var backupRoot string
	var backupCleanup func()
	var backupExtracted bool // memoize the attempt: one bad archive must not
	var backupAvailable bool // re-run extraction (and re-warn) for every failed file
	defer func() {
		if backupCleanup != nil {
			backupCleanup()
		}
	}()
	ensureBackupExtracted := func() (string, bool) {
		if backupPath == "" {
			return "", false
		}
		if backupExtracted {
			return backupRoot, backupAvailable
		}
		backupExtracted = true
		// Independent timeout so extraction runs even if ctx was cancelled.
		exCtx, exCancel := context.WithTimeout(
			log.WithContext(context.Background(), log.Ctx(ctx)),
			d.composeUpTimeout(),
		)
		defer exCancel()
		tarFile := filepath.Join(backupPath, "configs.tar.gz")
		root, cleanup, err := d.extractBackup(exCtx, tarFile)
		if err != nil {
			logger.Warn().
				Err(err).
				Str(log.FieldPath, backupPath).
				Msg("No backup archive available for rollback")
			return "", false
		}
		backupRoot = root
		backupCleanup = cleanup
		backupAvailable = true
		return backupRoot, true
	}

	// Phase 1: per-file compose up without --remove-orphans.
	results := make([]ComposeFileResult, 0, len(composeFiles))
	for _, f := range composeFiles {
		logger.Info().
			Str(log.FieldPath, f).
			Msg("Starting isolated compose up for file")

		err := upFn(ctx, []string{f})
		if err == nil {
			results = append(results, ComposeFileResult{File: f, Success: true})
			continue
		}

		// Unhealthy-only is a warning, not a failure — treat as success.
		if errors.Is(err, ErrComposeUnhealthy) {
			logger.Warn().
				Err(err).
				Str(log.FieldPath, f).
				Msg("Compose file has unhealthy containers, continuing")
			results = append(results, ComposeFileResult{File: f, Success: true, Err: err})
			continue
		}

		// Real failure — attempt per-file rollback.
		logger.Warn().
			Err(err).
			Str(log.FieldPath, f).
			Msg("Compose up failed for file")

		rolledBack := false
		if root, ok := ensureBackupExtracted(); ok {
			if backupFile, exists := resolveBackupFile(root, f); exists {
				logger.Info().
					Str(log.FieldPath, backupFile).
					Msg("Rolling back single file from backup")

				rollbackCtx, cancel := context.WithTimeout(
					log.WithContext(context.Background(), log.Ctx(ctx)),
					d.composeUpTimeout(),
				)
				rollbackArgs := d.buildUpArgs([]string{backupFile}, false)
				rollbackCmd := dockerComposeCommandContext(rollbackCtx, rollbackArgs...)
				var rollbackStderr bytes.Buffer
				rollbackCmd.Stderr = &rollbackStderr

				if rollbackErr := rollbackCmd.Run(); rollbackErr != nil {
					logger.Error().
						Err(rollbackErr).
						Str(log.FieldPath, backupFile).
						Msg("Per-file rollback failed")
				} else {
					logger.Info().
						Str(log.FieldPath, backupFile).
						Msg("Per-file rollback succeeded")
					rolledBack = true
				}
				cancel()
			}
		}

		results = append(results, ComposeFileResult{File: f, Success: false, RolledBack: rolledBack, Err: err})
	}

	summary := classifyComposeResults(results)

	// Phase 2: orphan reconciliation pass with every phase-1 success and verified
	// rollback. Rolled-back files use their extracted backup copy so the pass
	// reconciles Docker to the previous service set (#332/#335). Eligibility,
	// rather than a new-file success, is the gate: an all-rolled-back attempt still
	// needs orphan cleanup against its restored service set.
	orphanFiles := buildOrphanPassFiles(results, backupRoot)
	if d.RemoveOrphans && len(orphanFiles) > 0 {
		logger.Info().
			Int("file_count", len(orphanFiles)).
			Msg("Running orphan reconciliation pass")

		orphanArgs := d.buildUpArgs(orphanFiles, true)
		orphanCtx, orphanCancel := context.WithTimeout(ctx, d.composeUpTimeout())
		defer orphanCancel()
		orphanCmd := dockerComposeCommandContext(orphanCtx, orphanArgs...)
		var orphanStderr bytes.Buffer
		orphanCmd.Stderr = &orphanStderr

		if orphanErr := orphanCmd.Run(); orphanErr != nil {
			logger.Warn().
				Err(orphanErr).
				Str("stderr", orphanStderr.String()).
				Msg("Orphan reconciliation pass failed (non-fatal)")
		} else {
			logger.Info().Msg("Orphan reconciliation pass completed")
		}
	}

	// Determine overall error.
	if len(composeFiles) > 0 && summary.Failed == len(composeFiles) {
		phaseErrs := make([]error, 0, len(results))
		for _, result := range results {
			if result.Err != nil {
				phaseErrs = append(phaseErrs, result.Err)
			}
		}
		if phaseErr := errors.Join(phaseErrs...); phaseErr != nil {
			return &summary, fmt.Errorf("all %d compose files failed to deploy: %w", summary.Failed, phaseErr)
		}
		return &summary, fmt.Errorf("all %d compose files failed to deploy", summary.Failed)
	}

	return &summary, nil
}

// VerifyContainerHealth checks if containers from a compose file are healthy.
func (d *DeployOps) VerifyContainerHealth(ctx context.Context, composeFile string) error {
	if d.DryRun {
		return nil
	}

	// Use docker compose ps to check container status
	args := d.composeArgs(composeFile)
	args = append(args, "ps", "--all", "--format", "json")
	cmd := dockerComposeCommandContext(ctx, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to check container status: %w: %s", err, stderr.String())
	}

	entries, err := parseComposePSOutput(stdout.Bytes())
	if err != nil {
		return fmt.Errorf("failed to parse container status: %w", err)
	}

	result := classifyComposePS(entries)
	if len(result.Failed) > 0 {
		return fmt.Errorf("containers not running: %s", strings.Join(result.Failed, ", "))
	}
	if len(result.Unhealthy) > 0 {
		return fmt.Errorf("%w: %s", ErrComposeUnhealthy, strings.Join(result.Unhealthy, ", "))
	}
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

	sshCmd := d.remoteComposeUpCmd(composeDir)

	err := retryWithBackoff(ctx, DefaultMaxRetries, func() error {
		cmd := sshExecCommand(ctx, host, sshCmd)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			return &sshCommandError{
				operation: "remote docker compose up failed",
				cause:     err,
				stderr:    stderr.String(),
			}
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
// Path and project name are shell-quoted to prevent injection from commit-authored config values.
func (d *DeployOps) classifyComposeFailureRemote(ctx context.Context, host, composeDir string) (*composeFailureResult, error) {
	args := []string{"docker", "compose"}
	if d.ProjectName != "" {
		args = append(args, "-p", d.ProjectName)
	}
	args = append(args, "ps", "--all", "--format", "json")
	sshCmd := fmt.Sprintf("cd %s && %s", shellquote.Join(composeDir), shellquote.Join(args...))

	cmd := sshExecCommand(ctx, host, sshCmd)
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
		ctx, cancel = context.WithTimeout(ctx, d.composeUpTimeout())
		defer cancel()
	}

	args := d.composeArgs(composeFiles...)
	args = append(args, "ps", "--all", "--format", "json")

	cmd := dockerComposeCommandContext(ctx, args...)
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
	if d.signalContainerFn != nil {
		return d.signalContainerFn(ctx, containerName, signal)
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

	killArgs := []string{"docker", "kill", "--signal=" + signal, containerName}
	sshCmd := shellquote.Join(killArgs...)

	return retryWithBackoff(ctx, DefaultMaxRetries, func() error {
		cmd := sshExecCommand(ctx, host, sshCmd)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("remote docker kill signal failed: %w: %s", err, stderr.String())
		}
		return nil
	})
}

// nameConflictPattern matches Docker's container name conflict error message.
// Example: `Conflict. The container name "/agregarr" is already in use`
var nameConflictPattern = regexp.MustCompile(`container name "/?([^"]+)" is already in use`)

// detectNameConflicts extracts container names from Docker's name conflict errors.
// Returns nil if no conflicts are found.
func detectNameConflicts(stderr string) []string {
	matches := nameConflictPattern.FindAllStringSubmatch(stderr, -1)
	if len(matches) == 0 {
		return nil
	}
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) > 1 {
			names = append(names, m[1])
		}
	}
	return names
}

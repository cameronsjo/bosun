package reconcile

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/cameronsjo/bosun/internal/fileutil"
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
	SSHConnectTimeout    = 5 * time.Second
	SSHTimeout           = 30 * time.Second
	RemoteDeployTimeout  = 5 * time.Minute
	RemoteCleanupTimeout = 10 * time.Second
)

// ErrTransferIntegrity marks a remote tar-over-SSH transfer whose staged tree
// failed SHA-256 verification against the locally-built manifest — a truncated,
// partial, or misdirected transfer that must never be promoted to the live
// target (#334). It is RETRYABLE: retryWithBackoff stages into a fresh remote
// tmpDir on the next attempt, so a re-transfer is never overlaid on the dirty
// leftover of a failed one.
var ErrTransferIntegrity = errors.New("remote transfer integrity check failed")

// ErrUnsafeChecksumPath marks a staged filename that cannot be represented in a
// `sha256sum -c` manifest: busybox sha256sum lacks GNU's backslash escaping, so
// a newline or backslash in a name would corrupt the newline-delimited,
// space-separated manifest parse. Rejected at the local staging walk rather than
// trusted to format escaping (#334). The input is repo-controlled, so this
// guards against a pathological rendered filename, not a hostile actor.
var ErrUnsafeChecksumPath = errors.New("unsafe filename for checksum manifest")

// buildTransferChecksums walks root (the LOCAL staging tmpDir) and returns a
// SHA-256 manifest in `sha256sum -c` format — one "<hex>  <relpath>" line per
// regular file, sorted for determinism, "/"-separated paths relative to root so
// they resolve against the remote staged dir where the tar extraction lands
// them. Symlinks and irregular entries are skipped (the tar path does not
// meaningfully verify them). Returns "" when root holds no regular files, which
// the caller reads as "nothing to verify". A newline or backslash in any
// relative path fails with ErrUnsafeChecksumPath.
func buildTransferChecksums(root string) (string, error) {
	var entries []string
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if strings.ContainsAny(rel, "\n\\") {
			return fmt.Errorf("%w: %q", ErrUnsafeChecksumPath, rel)
		}
		sum, err := fileutil.FileHash(path)
		if err != nil {
			return err
		}
		// GNU coreutils `sha256sum -c` format: "<hex>  <path>" (two spaces =
		// text mode). macOS `shasum -a 256 -c` reads the identical layout.
		entries = append(entries, hex.EncodeToString(sum[:])+"  "+rel)
		return nil
	})
	if walkErr != nil {
		return "", walkErr
	}
	if len(entries) == 0 {
		return "", nil
	}
	sort.Strings(entries)
	return strings.Join(entries, "\n") + "\n", nil
}

// remoteHasSha256sum probes whether `sha256sum` is on the remote PATH. A host
// that lacks it (a minimal busybox without the applet) degrades to the pre-#334
// behavior: the transfer is not verified. Never hard-fail a host that deployed
// fine yesterday for lacking coreutils.
//
// The host is validated before any ssh exec: this probe is the earliest ssh
// contact on the deploy path, and TargetHost is re-read from the watched repo
// after every pull, so an unvalidated host string like `-oProxyCommand=<cmd>`
// would reach `ssh` as an option and execute on the daemon. An invalid host
// returns false (no verification) — the caller's subsequent validated ssh call
// then fails loudly rather than this probe silently launching a poisoned exec.
func (d *DeployOps) remoteHasSha256sum(ctx context.Context, host string) bool {
	if err := validateHost(host); err != nil {
		return false
	}
	return sshExecCommand(ctx, host, "command -v sha256sum").Run() == nil
}

// verifyRemoteTransfer pipes manifest to the remote and runs `sha256sum -c -`
// with cwd at stagedDir, so the manifest's relative paths resolve against the
// just-extracted staging tree. A non-zero exit — a missing file or a content
// mismatch — is an integrity failure (ErrTransferIntegrity). manifest must be
// non-empty; the caller skips verification for an empty manifest.
func (d *DeployOps) verifyRemoteTransfer(ctx context.Context, host, stagedDir, manifest string) error {
	remoteCmd := fmt.Sprintf("cd %s && sha256sum -c -", shellquote.Join(stagedDir))
	cmd := sshExecCommand(ctx, host, remoteCmd)
	cmd.Stdin = strings.NewReader(manifest)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %v: %s", ErrTransferIntegrity, err, joinNonEmpty(stdout.String(), stderr.String()))
	}
	return nil
}

// joinNonEmpty trims each part and joins the non-empty ones with a single space,
// so a blank stdout or stderr leaves no stray separator in the message.
func joinNonEmpty(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, " ")
}

// hostKeyOptions returns the ssh/scp `-o` flags that enforce the configured
// host-key policy on the exec'd deploy path, so it honors the same
// BOSUN_SSH_KNOWN_HOSTS / BOSUN_SSH_INSECURE_HOST_KEY config as the go-git
// clone/pull path (git.go's getHostKeyCallback), including the on-disk
// resolution: it consults the same buildKnownHostsPaths candidates (the env
// var, then /config/known_hosts) and pins strict verification against the
// first that exists. This closes the parity gap where the documented homelab
// convention — known_hosts at /config/known_hosts with the env var unset —
// got strict verification on git ops but only accept-new on deploys.
//
// Precedence mirrors the callback: BOSUN_SSH_INSECURE_HOST_KEY=true is checked
// first and wins over any known_hosts file.
//
// A candidate is only skipped when it is genuinely absent (fs.ErrNotExist).
// Any other stat error (permission denied, ENOTDIR, I/O) FAILS CLOSED: the
// helper pins strict against that candidate rather than downgrading to
// accept-new, so a configured known_hosts we cannot prove absent never
// silently becomes TOFU — ssh then surfaces the real read error.
//
// The one remaining INTENTIONAL divergence is the terminal case: when no
// known_hosts file exists and insecure is not set, git.go falls back to
// InsecureIgnoreHostKey (no verification), but the deploy channel carries a
// secret-bearing tar stream to a root account, so it uses openssh's TOFU
// (accept-new) instead — the first connection pins the key and later
// mismatches fail. Verification is never silently disabled here; only an
// explicit BOSUN_SSH_INSECURE_HOST_KEY=true opts out.
func hostKeyOptions() []string {
	if strings.EqualFold(os.Getenv("BOSUN_SSH_INSECURE_HOST_KEY"), "true") {
		return []string{
			"-o", "StrictHostKeyChecking=no",
			"-o", "UserKnownHostsFile=/dev/null",
		}
	}
	for _, path := range knownHostsCandidates(os.Getenv("BOSUN_SSH_KNOWN_HOSTS")) {
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			// Genuinely absent — try the next candidate.
			continue
		}
		// The file exists, OR stat failed for a reason other than "not found".
		// Fail closed in both cases: pin strict against this candidate rather
		// than downgrading an explicitly configured policy to TOFU.
		return []string{
			"-o", "StrictHostKeyChecking=yes",
			"-o", "UserKnownHostsFile=" + path,
		}
	}
	return []string{"-o", "StrictHostKeyChecking=accept-new"}
}

// knownHostsCandidates resolves the ordered known_hosts candidate paths for the
// deploy-path host-key policy, defaulting to git.go's buildKnownHostsPaths so
// deploy and git ops share one resolution (the env var, then /config/known_hosts).
// It is a package var so tests can inject a controlled candidate list.
var knownHostsCandidates = buildKnownHostsPaths

// execWithHostKeyOptions builds an exec.Cmd for name (ssh or scp) with the
// host-key policy flags (hostKeyOptions) prepended before the caller's args, so
// every exec'd ssh/scp call gets the same policy and it never drifts per-site.
func execWithHostKeyOptions(ctx context.Context, name string, args ...string) *exec.Cmd {
	full := append(hostKeyOptions(), args...)
	return exec.CommandContext(ctx, name, full...)
}

// sshExecCommand builds an `ssh` command with the host-key policy flags
// prepended before the caller's args (host + remote command).
func sshExecCommand(ctx context.Context, args ...string) *exec.Cmd {
	return execWithHostKeyOptions(ctx, "ssh", args...)
}

// newSSHTransferCommand is the tar-over-SSH command seam. Tests replace it to
// exercise an SSH spawn failure after the local tar process has started.
var newSSHTransferCommand = sshExecCommand

// scpExecCommand builds an `scp` command with the host-key policy flags
// prepended before the caller's args. Counterpart to sshExecCommand for the
// single-file copy path.
func scpExecCommand(ctx context.Context, args ...string) *exec.Cmd {
	return execWithHostKeyOptions(ctx, "scp", args...)
}

// remoteCleanupContext preserves correlation values without inheriting the
// failed deploy's cancellation or deadline, then adds a short cleanup bound.
func remoteCleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), RemoteCleanupTimeout)
}

// cleanupRemotePath removes a disposable remote staging path. Cleanup is
// best-effort so it cannot replace the primary deploy error.
func cleanupRemotePath(ctx context.Context, targetHost, path, reason string, recursive bool) {
	cleanupCtx, cancel := remoteCleanupContext(ctx)
	defer cancel()

	flag := "-f"
	if recursive {
		flag = "-rf"
	}
	if err := sshExecCommand(cleanupCtx, targetHost, shellquote.Join("rm", flag, path)).Run(); err != nil {
		logger := log.ComponentCtx(ctx, log.ComponentDeploy)
		logger.Warn().
			Err(err).
			Str(log.FieldTarget, targetHost).
			Str(log.FieldPath, path).
			Str("reason", reason).
			Msg("Failed to clean up remote staging path, leaving orphan behind")
	}
}

// killAndReapProcess terminates a started child and collects its process state.
// Kill alone leaves a zombie and retains the command's pipe descriptors until
// Wait is called.
func killAndReapProcess(cmd *exec.Cmd) {
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
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

		// Only retry on transient errors, plus a transfer-integrity failure
		// whose recovery is a fresh staging dir on the next attempt (#334):
		// DeployRemote stages into a new remote tmpDir each pass, so re-verifying
		// never reads a dirty leftover.
		if !isTransientSSHError(lastErr) && !errors.Is(lastErr, ErrTransferIntegrity) {
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

	cmd := sshExecCommand(ctx,
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
//
// When verifyChecksums is true, the staged tree is SHA-256 verified against a
// locally-built manifest AFTER the tar lands and BEFORE the swap to live: a
// truncated or misdirected transfer fails with ErrTransferIntegrity and retries
// into a fresh remote tmpDir (#334). Callers set it from a once-per-deploy
// `sha256sum` availability probe (remoteHasSha256sum); false degrades to the
// pre-#334 behavior of promoting whatever landed.
func (d *DeployOps) DeployRemote(ctx context.Context, sourceDir, targetHost, targetDir string, verifyChecksums bool) error {
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

	// Build the SHA-256 transfer manifest ONCE from the local source: it is
	// deterministic across attempts and a filename-guard failure (newline or
	// backslash) must abort the whole deploy, not burn retries. An empty
	// manifest (source has no regular files) skips verification entirely.
	manifest := ""
	if verifyChecksums {
		m, mErr := buildTransferChecksums(sourceDir)
		if mErr != nil {
			mErr = fmt.Errorf("build transfer checksums: %w", mErr)
			telemetry.SpanError(deploySpan, mErr)
			return mErr
		}
		manifest = m
	}

	deployErr := retryWithBackoff(ctx, DefaultMaxRetries, func() error {
		// A FRESH staging dir per attempt (#342): reusing one temp dir across
		// retries would extract the new archive over a partial leftover when
		// the prior attempt's cleanup failed — files deleted from the source
		// could survive and be promoted to the live target.
		tmpDir := filepath.Join(targetParent, fmt.Sprintf(".deploy-tmp-%d", time.Now().UnixNano()))
		cleanupTmp := func(reason string) {
			cleanupRemotePath(ctx, targetHost, tmpDir, reason, true)
		}
		// Heal any interrupted swap from a prior run before staging this one:
		// promote the newest retained tree when the target is missing, then
		// clean orphaned .bosun-old.<ts> siblings (#343 crash recovery).
		// Running it per attempt also self-heals between retries.
		logger.Debug().
			Str(log.FieldOperation, "recover_swap").
			Str(log.FieldTarget, targetHost).
			Str(log.FieldPath, targetDir).
			Msg("Preparing to recover any interrupted swap from prior deploy")
		recoverCmd := sshExecCommand(ctx, targetHost, buildRemoteRecoverCommand(targetDir))
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
		mkdirCmd := sshExecCommand(ctx, targetHost, shellquote.Join("mkdir", "-p", tmpDir))
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
		sshCmd := newSSHTransferCommand(ctx, targetHost, fmt.Sprintf("tar -C %s -xf -", shellquote.Join(tmpDir)))

		// Connect tar stdout to ssh stdin. The remote tmpDir already exists
		// (mkdir above), so every failure from here on must clean it up or it
		// orphans an empty staging dir on the remote.
		pipe, err := tarCmd.StdoutPipe()
		if err != nil {
			cleanupTmp("pipe_failed")
			return fmt.Errorf("create pipe: %w", err)
		}
		sshCmd.Stdin = pipe

		var tarStderr, sshStderr bytes.Buffer
		tarCmd.Stderr = &tarStderr
		sshCmd.Stderr = &sshStderr

		// Start both commands
		if err := tarCmd.Start(); err != nil {
			cleanupTmp("tar_start_failed")
			return fmt.Errorf("start tar: %w", err)
		}
		if err := sshCmd.Start(); err != nil {
			killAndReapProcess(tarCmd)
			cleanupTmp("ssh_start_failed")
			return fmt.Errorf("start ssh: %w: %s", err, sshStderr.String())
		}

		// Wait for both to complete
		tarErr := tarCmd.Wait()
		sshErr := sshCmd.Wait()

		if tarErr != nil {
			cleanupTmp("tar_failed")
			return fmt.Errorf("tar failed: %w: %s", tarErr, tarStderr.String())
		}
		if sshErr != nil {
			cleanupTmp("ssh_extract_failed")
			if ctx.Err() == context.DeadlineExceeded {
				return fmt.Errorf("ssh timed out after %v", RemoteDeployTimeout)
			}
			return fmt.Errorf("ssh extract failed: %w: %s", sshErr, sshStderr.String())
		}

		// Integrity gate (#334): verify the staged tree matches the local source
		// byte-for-byte BEFORE promoting it. A truncated or misdirected transfer
		// fails here with ErrTransferIntegrity — retryable, and the next attempt
		// stages into a fresh tmpDir, so a re-verify never trusts a dirty
		// leftover. Skipped when the manifest is empty (no regular files) or the
		// remote lacks sha256sum (verifyChecksums=false).
		if manifest != "" {
			if verifyErr := d.verifyRemoteTransfer(ctx, targetHost, tmpDir, manifest); verifyErr != nil {
				logger.Warn().
					Err(verifyErr).
					Str(log.FieldTarget, targetHost).
					Str(log.FieldPath, tmpDir).
					Msg("Staged tree failed integrity verification, discarding before swap")
				cleanupTmp("integrity_failed")
				return verifyErr
			}
			logger.Debug().
				Str(log.FieldTarget, targetHost).
				Str(log.FieldPath, tmpDir).
				Msg("Staged tree passed SHA-256 integrity verification")
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
		swapCmd := sshExecCommand(ctx, targetHost, buildRemoteSwapCommand(targetDir, tmpDir, oldDir, underFUSE))
		var swapStderr bytes.Buffer
		swapCmd.Stderr = &swapStderr

		if err := swapCmd.Run(); err != nil {
			// The swap's rollback branch already restored the old target (or
			// the next attempt's recovery will); the staging dir is disposable.
			cleanupTmp("swap_failed")
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
	cleanupTmp := func(reason string) {
		cleanupRemotePath(ctx, targetHost, tmpFile, reason, false)
	}

	err := retryWithBackoff(ctx, DefaultMaxRetries, func() error {
		// SCP to temp file
		target := fmt.Sprintf("%s:%s", targetHost, tmpFile)
		scpCmd := scpExecCommand(ctx, "-q", sourceFile, target)
		var scpStderr bytes.Buffer
		scpCmd.Stderr = &scpStderr

		if err := scpCmd.Run(); err != nil {
			cleanupTmp("scp_failed")
			if ctx.Err() == context.DeadlineExceeded {
				return fmt.Errorf("scp timed out after %v", RemoteDeployTimeout)
			}
			return fmt.Errorf("scp failed: %w: %s", err, scpStderr.String())
		}

		// Atomic move temp file to target
		moveCmd := sshExecCommand(ctx, targetHost, shellquote.Join("mv", tmpFile, targetFile))
		var moveStderr bytes.Buffer
		moveCmd.Stderr = &moveStderr

		if err := moveCmd.Run(); err != nil {
			cleanupTmp("move_failed")
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
	if d.DryRun {
		logger.Debug().
			Str(log.FieldTarget, host).
			Str(log.FieldPath, dir).
			Msg("Dry run: would ensure remote directory exists")
		return nil
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
		cmd := sshExecCommand(ctx, host, shellquote.Join("mkdir", "-p", dir))
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

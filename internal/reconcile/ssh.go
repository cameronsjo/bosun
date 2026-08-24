package reconcile

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

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
	SSHConnectTimeout    = 5 * time.Second
	SSHTimeout           = 30 * time.Second
	RemoteDeployTimeout  = 5 * time.Minute
	RemoteCleanupTimeout = 10 * time.Second
	commandDiagnosticMax = 4096
	commandOutputByteMax = commandDiagnosticMax * 4
	remoteStageIDBytes   = 16
)

// ErrTransferIntegrity marks a remote tar-over-SSH transfer whose staged tree
// failed SHA-256 verification against the locally-built manifest — a truncated,
// partial, or misdirected transfer that must never be promoted to the live
// target (#334). It is RETRYABLE: retryWithBackoff stages into a fresh remote
// tmpDir on the next attempt, so a re-transfer is never overlaid on the dirty
// leftover of a failed one.
var ErrTransferIntegrity = errors.New("remote transfer integrity check failed")

// ErrUnsupportedTransferEntry marks an entry that tar can archive but Bosun
// cannot safely verify after extraction. Deploy staging trees are expected to
// contain regular files, directories, and symlinks (including hard-linked
// regular files); devices, sockets, and FIFOs fail closed.
var ErrUnsupportedTransferEntry = errors.New("unsupported entry in remote transfer")

type transferEntry struct {
	Path       string
	Kind       byte
	SHA256     string
	LinkTarget string
	HardlinkTo string
}

// buildTransferManifest snapshots every deployable entry below root. Paths are
// kept as strings rather than serialized into a line-oriented checksum format,
// so newlines, backslashes, and other control characters remain unambiguous.
// Hard-link relationships are recorded in addition to each file's content hash.
func buildTransferManifest(ctx context.Context, root string) ([]transferEntry, error) {
	var entries []transferEntry
	type regularEntry struct {
		path string
		info fs.FileInfo
	}
	var regulars []regularEntry
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		entry := transferEntry{Path: rel}
		switch {
		case d.Type()&os.ModeSymlink != 0:
			entry.Kind = 'l'
			entry.LinkTarget, err = os.Readlink(path)
		case d.IsDir():
			entry.Kind = 'd'
		case d.Type().IsRegular():
			entry.Kind = 'f'
			info, infoErr := d.Info()
			err = infoErr
			if err == nil {
				for _, regular := range regulars {
					if os.SameFile(regular.info, info) {
						entry.HardlinkTo = regular.path
						break
					}
				}
				regulars = append(regulars, regularEntry{path: rel, info: info})
			}
			if err == nil {
				var sum [32]byte
				sum, err = hashFileWithContext(ctx, path)
				entry.SHA256 = hex.EncodeToString(sum[:])
			}
		default:
			return fmt.Errorf("%w: %q (%s)", ErrUnsupportedTransferEntry, rel, d.Type())
		}
		if err != nil {
			return err
		}
		entries = append(entries, entry)
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

func hashFileWithContext(ctx context.Context, filePath string) ([sha256.Size]byte, error) {
	var sum [sha256.Size]byte
	if err := ctx.Err(); err != nil {
		return sum, err
	}
	f, err := os.Open(filePath)
	if err != nil {
		return sum, err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	buf := make([]byte, 128*1024)
	for {
		if err := ctx.Err(); err != nil {
			return sum, err
		}
		n, readErr := f.Read(buf)
		if n > 0 {
			_, _ = h.Write(buf[:n])
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return sum, readErr
		}
	}
	if err := ctx.Err(); err != nil {
		return sum, err
	}
	copy(sum[:], h.Sum(nil))
	return sum, nil
}

func manifestsEqual(want, got []transferEntry) bool {
	if len(want) != len(got) {
		return false
	}
	for i := range want {
		if want[i].Path != got[i].Path || want[i].Kind != got[i].Kind ||
			want[i].SHA256 != got[i].SHA256 || want[i].LinkTarget != got[i].LinkTarget ||
			want[i].HardlinkTo != got[i].HardlinkTo {
			return false
		}
	}
	return true
}

// buildRemoteVerifyScript produces a shell program on stdin rather than a
// line-oriented data manifest. shellquote.Join makes each path one shell word,
// including names with newlines, backslashes, quotes, or other controls. The
// total-entry check rejects unexpected files; per-entry checks cover type,
// contents, symlink destinations, and hard-link topology.
func buildRemoteVerifyScript(stagedDir string, manifest []transferEntry) string {
	var script strings.Builder
	script.WriteString("set -eu\n")
	root := shellquote.Join(stagedDir)
	fmt.Fprintf(&script, "root=%s\n", root)
	script.WriteString("command -v sha256sum >/dev/null 2>&1 || { echo 'sha256sum is required for transfer verification' >&2; exit 1; }\n")
	// Count the root itself as well as its descendants. Avoiding `find -path`
	// keeps glob metacharacters in a configured staging path from being treated
	// as a pattern and falsely rejecting an otherwise complete tree.
	fmt.Fprintf(&script, "count=$(find \"$root\" -exec printf x \\; | wc -c | tr -d '[:space:]')\n[ \"$count\" = %s ]\n", strconv.Itoa(len(manifest)+1))
	hardlinkCounts := make(map[string]int)
	for _, entry := range manifest {
		if entry.Kind != 'f' {
			continue
		}
		canonical := entry.HardlinkTo
		if canonical == "" {
			canonical = entry.Path
		}
		hardlinkCounts[canonical]++
	}
	for _, entry := range manifest {
		entryPath := path.Join(stagedDir, entry.Path)
		quotedPath := shellquote.Join(entryPath)
		switch entry.Kind {
		case 'd':
			fmt.Fprintf(&script, "[ -d %s ] && [ ! -L %s ]\n", quotedPath, quotedPath)
		case 'l':
			linkSum := sha256.Sum256([]byte(entry.LinkTarget + "\n"))
			fmt.Fprintf(&script, "[ -L %s ]\nactual=$(readlink %s | sha256sum); actual=${actual%%%% *}; [ \"$actual\" = %s ]\n", quotedPath, quotedPath, shellquote.Join(hex.EncodeToString(linkSum[:])))
		case 'f':
			fmt.Fprintf(&script, "[ -f %s ] && [ ! -L %s ]\nactual=$(sha256sum < %s); actual=${actual%%%% *}; [ \"$actual\" = %s ]\n", quotedPath, quotedPath, quotedPath, shellquote.Join(entry.SHA256))
			if entry.HardlinkTo != "" {
				other := shellquote.Join(path.Join(stagedDir, entry.HardlinkTo))
				fmt.Fprintf(&script, "[ %s -ef %s ]\n", quotedPath, other)
			} else {
				// Exact link counts cover the negative relationships without an
				// O(files^2) verifier: independently stored equal-content files may
				// not be silently coalesced, and expected groups may not be merged.
				expectedLinks := hardlinkCounts[entry.Path]
				fmt.Fprintf(&script, "links=$(find %s -prune -links %s -exec printf x \\;); [ \"$links\" = x ]\n", quotedPath, strconv.Itoa(expectedLinks))
			}
		}
	}
	return script.String()
}

func (d *DeployOps) verifyRemoteTransfer(ctx context.Context, host, stagedDir string, manifest []transferEntry) error {
	cmd := sshExecCommand(ctx, host, shellquote.Join("sh", "-s"))
	cmd.Stdin = strings.NewReader(buildRemoteVerifyScript(stagedDir, manifest))
	var stdout, stderr boundedCommandBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %v: %s", ErrTransferIntegrity, err, joinNonEmpty(stdout.String(), stderr.String()))
	}
	return nil
}

// newLocalArchiveCommand is the local tar creation seam. Tests replace it with
// an exit-zero producer of an incomplete but valid archive.
var newLocalArchiveCommand = func(ctx context.Context, sourceDir, archivePath string) *exec.Cmd {
	return exec.CommandContext(ctx, "tar", "-C", sourceDir, "-cf", archivePath, ".")
}

func createVerifiedTransferArchive(ctx context.Context, sourceDir string) (archivePath, archiveSHA string, manifest []transferEntry, cleanup func(), err error) {
	want, err := buildTransferManifest(ctx, sourceDir)
	if err != nil {
		return "", "", nil, func() {}, fmt.Errorf("snapshot transfer source: %w", err)
	}
	f, err := os.CreateTemp("", "bosun-deploy-*.tar")
	if err != nil {
		return "", "", nil, func() {}, fmt.Errorf("create transfer archive: %w", err)
	}
	archivePath = f.Name()
	cleanup = func() {
		// Best-effort temp cleanup must not replace the deploy result.
		_ = os.Remove(archivePath)
	}
	if err = f.Close(); err != nil {
		cleanup()
		return "", "", nil, func() {}, fmt.Errorf("close transfer archive: %w", err)
	}
	cmd := newLocalArchiveCommand(ctx, sourceDir, archivePath)
	var stderr boundedCommandBuffer
	cmd.Stderr = &stderr
	if err = cmd.Run(); err != nil {
		cleanup()
		return "", "", nil, func() {}, fmt.Errorf("create transfer archive: %w: %s", err, joinNonEmpty(stderr.String()))
	}

	scratch, err := os.MkdirTemp("", "bosun-deploy-verify-*")
	if err != nil {
		cleanup()
		return "", "", nil, func() {}, fmt.Errorf("create archive verification directory: %w", err)
	}
	defer func() {
		// Best-effort scratch cleanup must not replace a verification failure.
		_ = os.RemoveAll(scratch)
	}()
	extract := exec.CommandContext(ctx, "tar", "-C", scratch, "-xf", archivePath)
	var extractStderr boundedCommandBuffer
	extract.Stderr = &extractStderr
	if err = extract.Run(); err != nil {
		cleanup()
		return "", "", nil, func() {}, fmt.Errorf("verify transfer archive extraction: %w: %s", err, joinNonEmpty(extractStderr.String()))
	}
	got, err := buildTransferManifest(ctx, scratch)
	if err != nil || !manifestsEqual(want, got) {
		cleanup()
		if err != nil {
			return "", "", nil, func() {}, fmt.Errorf("verify transfer archive contents: %w", err)
		}
		return "", "", nil, func() {}, fmt.Errorf("%w: local archive does not match source snapshot", ErrTransferIntegrity)
	}
	sum, err := hashFileWithContext(ctx, archivePath)
	if err != nil {
		cleanup()
		return "", "", nil, func() {}, fmt.Errorf("hash transfer archive: %w", err)
	}
	return archivePath, hex.EncodeToString(sum[:]), want, cleanup, nil
}

// joinNonEmpty trims each part and joins the non-empty ones with a single space,
// so a blank stdout or stderr leaves no stray separator in the message.
func joinNonEmpty(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			p = strings.Map(func(r rune) rune {
				if unicode.IsControl(r) {
					return '?'
				}
				return r
			}, p)
			kept = append(kept, p)
		}
	}
	joined := []rune(strings.Join(kept, " "))
	if len(joined) <= commandDiagnosticMax {
		return string(joined)
	}
	return string(joined[:commandDiagnosticMax-1]) + "…"
}

// boundedCommandBuffer keeps a remote or local tool from making Bosun retain
// unbounded attacker-controlled diagnostics. Write reports the full input as
// consumed so os/exec does not interpret truncation as a command I/O failure.
type boundedCommandBuffer struct {
	data      []byte
	truncated bool
}

func (b *boundedCommandBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := commandOutputByteMax - len(b.data)
	if remaining > 0 {
		if remaining > len(p) {
			remaining = len(p)
		}
		b.data = append(b.data, p[:remaining]...)
	}
	if remaining < len(p) {
		b.truncated = true
	}
	return n, nil
}

func (b *boundedCommandBuffer) String() string {
	if b.truncated {
		return string(b.data) + "…"
	}
	return string(b.data)
}

// newRemoteStageID makes each remote staging namespace unpredictable. It is a
// seam so a collision can be exercised without guessing production randomness.
var newRemoteStageID = func() (string, error) {
	var raw [remoteStageIDBytes]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func buildRemoteStageCommand(tmpRoot, stagedDir string) string {
	root := shellquote.Join(tmpRoot)
	staged := shellquote.Join(stagedDir)
	// The first mkdir is deliberately exclusive (no -p). A collision must not
	// reuse or delete an existing tree: tar extraction is allowed only inside a
	// private namespace this attempt proved it created.
	return "set -eu; umask 077; " +
		"mkdir " + root + " || exit $?; " +
		"mkdir " + staged + " || { status=$?; rm -rf " + root + "; exit $status; }"
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

// sshCommandError keeps a remote command's stderr available for diagnostics
// without letting application output decide whether the SSH transport failed.
// OpenSSH reserves exit status 255 for client/transport failures; all other
// exit statuses came from the remote command and must fail without retry.
type sshCommandError struct {
	operation string
	cause     error
	stderr    string
}

func (e *sshCommandError) Error() string {
	return fmt.Sprintf("%s: %v: %s", e.operation, e.cause, e.stderr)
}

func (e *sshCommandError) Unwrap() error {
	return e.cause
}

func (e *sshCommandError) isTransient() bool {
	var exitErr *exec.ExitError
	if errors.As(e.cause, &exitErr) {
		return exitErr.ExitCode() == 255 && containsTransientSSHPattern(e.stderr)
	}
	return containsTransientSSHPattern(e.cause.Error())
}

// isTransientSSHError checks if an error is transient and worth retrying.
// Transient errors include connection refused, timeout, and network unreachable.
func isTransientSSHError(err error) bool {
	if err == nil {
		return false
	}
	var commandErr *sshCommandError
	if errors.As(err, &commandErr) {
		return commandErr.isTransient()
	}
	return containsTransientSSHPattern(err.Error())
}

func containsTransientSSHPattern(message string) bool {
	errStr := strings.ToLower(message)
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
// The immutable local archive is verified against a pre-archive source
// snapshot, SHA-256 checked after transport, and its extracted tree is verified
// again BEFORE the swap to live. Any incomplete stage fails closed (#252).
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
	targetParent := path.Dir(targetDir)
	if err := d.EnsureRemoteDir(ctx, targetHost, targetParent); err != nil {
		err = fmt.Errorf("ensure remote parent dir: %w", err)
		telemetry.SpanError(deploySpan, err)
		return err
	}

	// Materialize the archive once. This closes the count/checksum TOCTOU seam:
	// the source snapshot is compared to a local round-trip before the immutable
	// archive is retried or sent to the remote.
	archivePath, archiveSHA, manifest, cleanupArchive, archiveErr := createVerifiedTransferArchive(ctx, sourceDir)
	if archiveErr != nil {
		telemetry.SpanError(deploySpan, archiveErr)
		return archiveErr
	}
	defer cleanupArchive()

	deployErr := retryWithBackoff(ctx, DefaultMaxRetries, func() error {
		// A FRESH, exclusively-created staging root per attempt (#342): reusing
		// a pre-existing tree lets unverified modes, ownership, ACLs, or xattrs
		// survive extraction even when the content manifest passes.
		stageID, stageIDErr := newRemoteStageID()
		if stageIDErr != nil {
			return fmt.Errorf("generate remote staging identifier: %w", stageIDErr)
		}
		tmpRoot := path.Join(targetParent, ".deploy-tmp-"+stageID)
		tmpDir := path.Join(tmpRoot, "tree")
		// Transport metadata is isolated from the extracted tree, so every
		// possible source filename remains legitimate and countable.
		remoteArchive := path.Join(tmpRoot, "archive.tar")
		cleanupTmp := func(reason string) {
			cleanupRemotePath(ctx, targetHost, tmpRoot, reason, true)
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
		var recoverStderr boundedCommandBuffer
		recoverCmd.Stderr = &recoverStderr
		if err := recoverCmd.Run(); err != nil {
			return fmt.Errorf("recover interrupted swap, expected target %s present or a promotable retained copy: %w: %s", targetDir, err, joinNonEmpty(recoverStderr.String()))
		}

		// Create temp directory on remote
		logger.Debug().
			Str(log.FieldOperation, "prepare_staging").
			Str(log.FieldTarget, targetHost).
			Str(log.FieldPath, tmpDir).
			Msg("Preparing to create staging directory on remote")
		mkdirCmd := sshExecCommand(ctx, targetHost, buildRemoteStageCommand(tmpRoot, tmpDir))
		var mkdirStderr boundedCommandBuffer
		mkdirCmd.Stderr = &mkdirStderr
		if err := mkdirCmd.Run(); err != nil {
			return fmt.Errorf("create remote temp dir: %w: %s", err, joinNonEmpty(mkdirStderr.String()))
		}

		// Stream the already-verified archive. The remote stores it inside the
		// disposable private staging root, verifies its transport hash, extracts, and
		// removes it. A missing sha256sum is an integrity failure, never an
		// implicit opt-out.
		logger.Debug().
			Str(log.FieldOperation, "tar_extract").
			Str(log.FieldTarget, targetHost).
			Str("source", sourceDir).
			Str("dest", tmpDir).
			Msg("Preparing to tar and extract staging directory to remote")
		remoteCmd := fmt.Sprintf(
			"set -eu; umask 077; command -v sha256sum >/dev/null 2>&1 || { echo 'sha256sum is required for transfer verification' >&2; exit 1; }; cat > %s; actual=$(sha256sum < %s); actual=${actual%%%% *}; [ \"$actual\" = %s ]; tar -C %s -xf %s; rm -f %s",
			shellquote.Join(remoteArchive), shellquote.Join(remoteArchive), shellquote.Join(archiveSHA),
			shellquote.Join(tmpDir), shellquote.Join(remoteArchive), shellquote.Join(remoteArchive),
		)
		sshCmd := newSSHTransferCommand(ctx, targetHost, remoteCmd)
		archive, err := os.Open(archivePath)
		if err != nil {
			cleanupTmp("archive_open_failed")
			return fmt.Errorf("open transfer archive: %w", err)
		}
		sshCmd.Stdin = archive

		var sshStderr boundedCommandBuffer
		sshCmd.Stderr = &sshStderr

		if err := sshCmd.Start(); err != nil {
			// Closing an unread local temp file is best-effort; the SSH start
			// failure remains the actionable deploy error.
			_ = archive.Close()
			cleanupTmp("ssh_start_failed")
			return fmt.Errorf("start ssh: %w: %s", err, joinNonEmpty(sshStderr.String()))
		}

		sshErr := sshCmd.Wait()
		closeErr := archive.Close()

		if sshErr != nil {
			cleanupTmp("ssh_extract_failed")
			if ctx.Err() == context.DeadlineExceeded {
				return fmt.Errorf("ssh timed out after %v", RemoteDeployTimeout)
			}
			return fmt.Errorf("%w: ssh extract failed: %v: %s", ErrTransferIntegrity, sshErr, joinNonEmpty(sshStderr.String()))
		}
		if closeErr != nil {
			cleanupTmp("archive_close_failed")
			return fmt.Errorf("close transfer archive: %w", closeErr)
		}

		// Post-extraction gate verifies empty trees and non-file entries too.
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
			Msg("Staged tree passed SHA-256 and structural integrity verification")

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
		var swapStderr boundedCommandBuffer
		swapCmd.Stderr = &swapStderr

		if err := swapCmd.Run(); err != nil {
			// The swap's rollback branch already restored the old target (or
			// the next attempt's recovery will); the staging dir is disposable.
			cleanupTmp("swap_failed")
			return fmt.Errorf("swap staged tree into %s failed, old tree retained or restored: %w: %s", targetDir, err, joinNonEmpty(swapStderr.String()))
		}
		// The tree moved out of tmpRoot; remove the now-empty private transport
		// namespace without allowing cleanup trouble to replace a successful swap.
		cleanupRemotePath(ctx, targetHost, tmpRoot, "promoted", true)
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

package reconcile

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// exitError returns a real *exec.ExitError carrying the given status.
func exitError(t *testing.T, code int) error {
	t.Helper()
	err := exec.Command("/bin/sh", "-c", "exit "+strconv.Itoa(code)).Run()
	var exitErr *exec.ExitError
	require.True(t, errors.As(err, &exitErr), "expected an ExitError for status %d", code)
	require.Equal(t, code, exitErr.ExitCode())
	return err
}

// signalError returns a real *exec.ExitError for a process killed by a signal
// (ExitCode reports -1).
func signalError(t *testing.T) error {
	t.Helper()
	err := exec.Command("/bin/sh", "-c", "kill -9 $$").Run()
	var exitErr *exec.ExitError
	require.True(t, errors.As(err, &exitErr), "expected an ExitError for a signal-killed shell")
	require.Equal(t, -1, exitErr.ExitCode())
	return err
}

func retryable(err error) bool {
	return isTransientSSHError(err) || errors.Is(err, ErrTransferIntegrity)
}

func TestClassifyRemoteExit(t *testing.T) {
	tests := []struct {
		name            string
		err             func(t *testing.T) error
		stderr          string
		wantUnavailable bool
		wantIntegrity   bool
		wantRetryable   bool
	}{
		{"78 sha256sum missing", func(t *testing.T) error { return exitError(t, 78) }, "sha256sum is required for transfer verification", true, false, false},
		{"79 checksum mismatch", func(t *testing.T) error { return exitError(t, 79) }, "", false, true, true},
		{"255 connection reset", func(t *testing.T) error { return exitError(t, 255) }, "Connection reset by peer", false, false, true},
		{"255 broken pipe", func(t *testing.T) error { return exitError(t, 255) }, "client_loop: send disconnect: Broken pipe", false, false, true},
		{"255 closed by remote host", func(t *testing.T) error { return exitError(t, 255) }, "Connection to host closed by remote host.", false, false, true},
		{"255 server not responding", func(t *testing.T) error { return exitError(t, 255) }, "Timeout, server host not responding.", false, false, true},
		{"255 host key", func(t *testing.T) error { return exitError(t, 255) }, "Host key verification failed.", false, false, false},
		{"2 disk full", func(t *testing.T) error { return exitError(t, 2) }, "tar: write error: No space left on device", false, false, false},
		{"1 permission denied", func(t *testing.T) error { return exitError(t, 1) }, "Permission denied", false, false, false},
		// A remote command's own status is never transient, even when its
		// stderr happens to contain a transport-looking phrase.
		{"1 with transient-looking stderr", func(t *testing.T) error { return exitError(t, 1) }, "connection reset", false, false, false},
		{"signal killed", signalError, "", false, true, true},
		{"non-exit error", func(t *testing.T) error { return errors.New("wait: pipe closed unexpectedly") }, "", false, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyRemoteExit("ssh extract", "user@testhost", tt.err(t), tt.stderr)
			require.Error(t, got)
			assert.Equal(t, tt.wantUnavailable, errors.Is(got, ErrRemoteIntegrityUnavailable), "ErrRemoteIntegrityUnavailable")
			assert.Equal(t, tt.wantIntegrity, errors.Is(got, ErrTransferIntegrity), "ErrTransferIntegrity")
			assert.Equal(t, tt.wantRetryable, retryable(got), "retryable")
			if tt.stderr != "" {
				assert.Contains(t, got.Error(), tt.stderr, "stderr must stay in the diagnostic")
			}
		})
	}
}

// installTransferSeam replaces the archive-transfer leg with a local shell that
// drains stdin, records one attempt, writes stderrMsg, and exits with code.
// Every other leg still runs through setupSSHShim. Returns the counter path.
func installTransferSeam(t *testing.T, code int, stderrMsg string) string {
	t.Helper()
	counter := filepath.Join(t.TempDir(), "attempts")
	original := newSSHTransferCommand
	t.Cleanup(func() { newSSHTransferCommand = original })
	newSSHTransferCommand = func(ctx context.Context, _ ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, "/bin/sh", "-c", `cat >/dev/null; echo x >> "$SEAM_COUNTER"; printf '%s\n' "$SEAM_MSG" >&2; exit "$SEAM_CODE"`)
		cmd.Env = append(os.Environ(),
			"SEAM_COUNTER="+counter,
			"SEAM_MSG="+stderrMsg,
			"SEAM_CODE="+strconv.Itoa(code),
		)
		return cmd
	}
	return counter
}

func countAttempts(t *testing.T, counter string) int {
	t.Helper()
	b, err := os.ReadFile(counter)
	require.NoError(t, err)
	return strings.Count(string(b), "x")
}

func TestDeployRemote_ExtractFailureClassification(t *testing.T) {
	tests := []struct {
		name          string
		code          int
		stderr        string
		wantAttempts  int
		wantGuidance  bool
		wantIntegrity bool
	}{
		{"remote disk full is permanent", 2, "tar: write error: No space left on device", 1, false, false},
		{"remote permission denied is permanent", 1, "tar: compose: Cannot open: Permission denied", 1, false, false},
		{"host key failure fails fast with guidance", 255, "Host key verification failed.", 1, true, false},
		{"transport broken pipe is retried", 255, "client_loop: send disconnect: Broken pipe", DefaultMaxRetries, false, false},
		{"checksum mismatch is retried", remoteTransferChecksumMismatchExit, "", DefaultMaxRetries, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupSSHShim(t)
			counter := installTransferSeam(t, tt.code, tt.stderr)
			base := t.TempDir()
			source := filepath.Join(base, "source")
			parent := filepath.Join(base, "deploy")
			target := filepath.Join(parent, "compose")
			writeMarker(t, source, "new", "new")
			writeMarker(t, target, "live", "old")

			err := (&DeployOps{}).DeployRemote(context.Background(), source, "user@testhost", target)

			require.Error(t, err)
			assert.Equal(t, tt.wantAttempts, countAttempts(t, counter), "transfer attempts")
			assert.Equal(t, tt.wantGuidance, strings.Contains(err.Error(), hostKeyGuidanceMarker), "host-key guidance")
			assert.Equal(t, tt.wantIntegrity, errors.Is(err, ErrTransferIntegrity), "ErrTransferIntegrity")
			if tt.stderr != "" {
				assert.Contains(t, err.Error(), tt.stderr)
			}
			assert.Equal(t, "old", readMarker(t, target, "live"), "a failed transfer must not touch the live target")
			entries, readErr := os.ReadDir(parent)
			require.NoError(t, readErr)
			require.Len(t, entries, 1, "each failed attempt must clean its staging root")
			assert.Equal(t, "compose", entries[0].Name())
		})
	}
}

func TestDeployRemote_ChecksumMismatchExitsDistinctly(t *testing.T) {
	setupSSHShim(t)
	// A sha256sum that always reports a hash the archive cannot have, so the
	// real remote command reaches its mismatch branch. It counts attempts.
	counter := filepath.Join(t.TempDir(), "attempts")
	dir := t.TempDir()
	shim := "#!/bin/sh\ncat >/dev/null\necho x >> " + counter + "\necho '0000000000000000000000000000000000000000000000000000000000000000  -'\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sha256sum"), []byte(shim), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	base := t.TempDir()
	source := filepath.Join(base, "source")
	target := filepath.Join(base, "deploy", "compose")
	writeMarker(t, source, "new", "new")
	writeMarker(t, target, "live", "old")

	err := (&DeployOps{}).DeployRemote(context.Background(), source, "user@testhost", target)

	require.ErrorIs(t, err, ErrTransferIntegrity)
	assert.Contains(t, err.Error(), "checksum mismatch")
	assert.Equal(t, DefaultMaxRetries, countAttempts(t, counter), "a damaged transfer is retried with fresh staging")
	assert.Equal(t, "old", readMarker(t, target, "live"))
	assert.NoFileExists(t, filepath.Join(target, "new"))
}

func TestDeployRemote_TrailingSlashTarget(t *testing.T) {
	setupSSHShim(t)
	base := t.TempDir()
	source := filepath.Join(base, "source")
	parent := filepath.Join(base, "deploy")
	target := filepath.Join(parent, "compose")
	writeMarker(t, source, "marker", "v1")
	writeMarker(t, target, "marker", "v0")

	require.NoError(t, (&DeployOps{}).DeployRemote(context.Background(), source, "user@testhost", target+"/"))

	assert.Equal(t, "v1", readMarker(t, target, "marker"))
	inside, err := os.ReadDir(target)
	require.NoError(t, err)
	for _, e := range inside {
		assert.NotContains(t, e.Name(), ".deploy-tmp-", "staging must never be created inside the target")
		assert.NotContains(t, e.Name(), bosunOldSuffix, "the retained copy must never be created inside the target")
	}
	siblings, err := os.ReadDir(parent)
	require.NoError(t, err)
	require.Len(t, siblings, 1)
	assert.Equal(t, "compose", siblings[0].Name())
}

func TestDeployRemote_RejectsTargetWithoutParent(t *testing.T) {
	for _, dryRun := range []bool{true, false} {
		for _, target := range []string{"/", "//", ".", "", ".."} {
			t.Run(strconv.Quote(target)+" dryRun="+strconv.FormatBool(dryRun), func(t *testing.T) {
				callLog := filepath.Join(t.TempDir(), "ssh-calls")
				dir := t.TempDir()
				shim := "#!/bin/sh\necho \"$*\" >> " + callLog + "\nexit 0\n"
				require.NoError(t, os.WriteFile(filepath.Join(dir, "ssh"), []byte(shim), 0o755))
				t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

				err := (&DeployOps{DryRun: dryRun}).DeployRemote(context.Background(), t.TempDir(), "user@testhost", target)

				require.Error(t, err)
				assert.Contains(t, err.Error(), "has no parent directory")
				assert.NoFileExists(t, callLog, "no ssh leg may run for a target with no parent")
			})
		}
	}
}

func TestNormalizeRemoteSwapTarget(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"/srv/compose", "/srv/compose", false},
		{"/srv/compose/", "/srv/compose", false},
		{"/srv//compose/./", "/srv/compose", false},
		{"compose", "compose", false},
		{"a/b/", "a/b", false},
		{"/", "", true},
		{"//", "", true},
		{".", "", true},
		{"", "", true},
		{"..", "", true},
	}
	for _, tt := range tests {
		t.Run(strconv.Quote(tt.in), func(t *testing.T) {
			got, err := normalizeRemoteSwapTarget(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "has no parent directory")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

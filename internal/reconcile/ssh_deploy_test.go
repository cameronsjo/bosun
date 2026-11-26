package reconcile

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKillAndReapProcess(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "30")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})

	killAndReapProcess(cmd)

	require.NotNil(t, cmd.ProcessState, "Wait must collect the killed child")
	assert.False(t, cmd.ProcessState.Success())
}

func TestRemoteCleanupContext_DetachesCancellationAndRemainsBounded(t *testing.T) {
	type contextKey struct{}
	parent, cancelParent := context.WithCancel(context.WithValue(context.Background(), contextKey{}, "trace-value"))
	cancelParent()

	cleanupCtx, cancelCleanup := remoteCleanupContext(parent)
	defer cancelCleanup()

	require.NoError(t, cleanupCtx.Err())
	assert.Equal(t, "trace-value", cleanupCtx.Value(contextKey{}))
	deadline, ok := cleanupCtx.Deadline()
	require.True(t, ok)
	remaining := time.Until(deadline)
	assert.LessOrEqual(t, remaining, RemoteCleanupTimeout)
	assert.Greater(t, remaining, RemoteCleanupTimeout-time.Second)
}

func TestCleanupRemotePath_FailureRemainsBestEffort(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ssh"), []byte("#!/bin/sh\nexit 1\n"), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.NotPanics(t, func() {
		cleanupRemotePath(ctx, "user@testhost", "/tmp/bosun-staging", "test_failure", true)
	})
}

// setupSSHShim installs a fake `ssh` ahead of PATH that executes the remote
// command locally (`ssh [-o opt]... <host> <cmd...>` → `/bin/sh -c "<cmd...>"`),
// so DeployRemote's full pipeline — crash recovery, staging mkdir, tar-over-ssh
// extraction, and the retain-old swap — runs end-to-end against real local
// directories with no network. stdin flows through, so archive streaming works.
// The shim skips the leading `-o KEY=VAL` host-key policy flags (and any other
// dash-prefixed option) before dropping the host, mirroring real ssh argv
// parsing; every path involved comes from t.TempDir().
func setupSSHShim(t *testing.T) {
	t.Helper()
	setupSha256sumShim(t)
	dir := t.TempDir()
	shim := "#!/bin/sh\n" +
		"while [ $# -gt 0 ]; do\n" +
		"  case \"$1\" in\n" +
		"    -o) shift 2 ;;\n" +
		"    -*) shift ;;\n" +
		"    *) break ;;\n" +
		"  esac\n" +
		"done\n" +
		"shift\n" +
		"exec /bin/sh -c \"$*\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ssh"), []byte(shim), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestDeployRemote_EndToEnd(t *testing.T) {
	t.Run("first deploy creates the target from the staged tree", func(t *testing.T) {
		setupSSHShim(t)
		base := t.TempDir()
		source := filepath.Join(base, "source")
		target := filepath.Join(base, "deploy", "compose")
		writeMarker(t, source, "marker", "v1")

		d := &DeployOps{}
		require.NoError(t, d.DeployRemote(context.Background(), source, "user@testhost", target))

		assert.Equal(t, "v1", readMarker(t, target, "marker"))
	})

	t.Run("redeploy replaces the target and leaves no residue", func(t *testing.T) {
		setupSSHShim(t)
		base := t.TempDir()
		source := filepath.Join(base, "source")
		parent := filepath.Join(base, "deploy")
		target := filepath.Join(parent, "compose")
		writeMarker(t, source, "marker", "v2")
		writeMarker(t, target, "marker", "v1")

		d := &DeployOps{}
		require.NoError(t, d.DeployRemote(context.Background(), source, "user@testhost", target))

		assert.Equal(t, "v2", readMarker(t, target, "marker"))
		entries, err := os.ReadDir(parent)
		require.NoError(t, err)
		require.Len(t, entries, 1, "no retained copies or staging temp dirs may survive a successful deploy")
		assert.Equal(t, "compose", entries[0].Name())
	})

	t.Run("interrupted prior swap self-heals then deploys", func(t *testing.T) {
		setupSSHShim(t)
		base := t.TempDir()
		source := filepath.Join(base, "source")
		parent := filepath.Join(base, "deploy")
		target := filepath.Join(parent, "compose")
		writeMarker(t, source, "marker", "new")
		// Simulate a crash mid-swap: target gone, retained copy stranded.
		writeMarker(t, target+bosunOldSuffix+"1111111111111111111", "marker", "stranded")

		d := &DeployOps{}
		require.NoError(t, d.DeployRemote(context.Background(), source, "user@testhost", target))

		assert.Equal(t, "new", readMarker(t, target, "marker"))
		entries, err := os.ReadDir(parent)
		require.NoError(t, err)
		require.Len(t, entries, 1, "the stranded retained copy must be promoted then replaced, not leaked")
	})

	t.Run("missing source fails before staging and preserves the target", func(t *testing.T) {
		setupSSHShim(t)
		base := t.TempDir()
		parent := filepath.Join(base, "deploy")
		target := filepath.Join(parent, "compose")
		writeMarker(t, target, "marker", "live")
		missingSource := filepath.Join(base, "does-not-exist")

		d := &DeployOps{}
		err := d.DeployRemote(context.Background(), missingSource, "user@testhost", target)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "snapshot transfer source")
		assert.Equal(t, "live", readMarker(t, target, "marker"), "a failed staging transfer must not touch the live target")
		entries, readErr := os.ReadDir(parent)
		require.NoError(t, readErr)
		require.Len(t, entries, 1, "staging temp dir must be cleaned up on failure")
	})

	t.Run("dry run performs no remote work", func(t *testing.T) {
		// No shim on purpose: any ssh invocation would fail loudly.
		base := t.TempDir()
		target := filepath.Join(base, "deploy", "compose")

		d := &DeployOps{DryRun: true}
		require.NoError(t, d.DeployRemote(context.Background(), filepath.Join(base, "source"), "user@testhost", target))
		assert.NoDirExists(t, target)
	})

	t.Run("invalid host is rejected before any work", func(t *testing.T) {
		d := &DeployOps{}
		err := d.DeployRemote(context.Background(), t.TempDir(), "-oProxyCommand=evil", "/tmp/x")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid SSH host")
	})
}

func TestDeployRemote_SSHStartFailureCleansStaging(t *testing.T) {
	setupSSHShim(t)
	original := newSSHTransferCommand
	t.Cleanup(func() { newSSHTransferCommand = original })
	missingSSH := filepath.Join(t.TempDir(), "missing-ssh")
	newSSHTransferCommand = func(ctx context.Context, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, missingSSH)
	}

	base := t.TempDir()
	source := filepath.Join(base, "source")
	targetParent := filepath.Join(base, "deploy")
	writeMarker(t, source, "marker", "v1")

	err := (&DeployOps{}).DeployRemote(context.Background(), source, "user@testhost", filepath.Join(targetParent, "compose"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "start ssh")
	entries, readErr := os.ReadDir(targetParent)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "SSH spawn failure must clean the staging directory")
}

func TestDeployRemote_CancelledContextCleansStagingTempDir(t *testing.T) {
	dir := t.TempDir()
	extractStarted := filepath.Join(dir, "extract-started")
	t.Setenv("SSH_EXTRACT_STARTED", extractStarted)
	shim := "#!/bin/sh\n" +
		"while [ $# -gt 0 ]; do\n" +
		"  case \"$1\" in\n" +
		"    -o) shift 2 ;;\n" +
		"    -*) shift ;;\n" +
		"    *) break ;;\n" +
		"  esac\n" +
		"done\n" +
		"shift\n" +
		"case \"$*\" in\n" +
		"  *'cat > '*'.deploy-tmp-'*'/archive.tar'*) : > \"$SSH_EXTRACT_STARTED\"; exec /bin/sleep 30 ;;\n" +
		"esac\n" +
		"exec /bin/sh -c \"$*\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ssh"), []byte(shim), 0o755))
	setupSha256sumShim(t)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	base := t.TempDir()
	source := filepath.Join(base, "source")
	parent := filepath.Join(base, "deploy")
	target := filepath.Join(parent, "compose")
	writeMarker(t, source, "marker", "new")
	writeMarker(t, target, "marker", "live")

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errCh := make(chan error, 1)
	go func() {
		errCh <- (&DeployOps{}).DeployRemote(ctx, source, "user@testhost", target)
	}()

	require.Eventually(t, func() bool {
		_, err := os.Stat(extractStarted)
		return err == nil
	}, 5*time.Second, 10*time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("DeployRemote did not return after cancellation")
	}

	assert.Equal(t, "live", readMarker(t, target, "marker"))
	entries, err := os.ReadDir(parent)
	require.NoError(t, err)
	require.Len(t, entries, 1, "cancelled deploy must clean its remote staging directory")
	assert.Equal(t, "compose", entries[0].Name())
}

func TestEnsureRemoteDir_EndToEnd(t *testing.T) {
	setupSSHShim(t)
	dir := filepath.Join(t.TempDir(), "nested", "path")

	d := &DeployOps{}
	require.NoError(t, d.EnsureRemoteDir(context.Background(), "user@testhost", dir))
	assert.DirExists(t, dir)
}

func TestDeployRemoteFile_EndToEnd(t *testing.T) {
	setupSSHShim(t)
	callLog := setupSCPTripwire(t)
	base := t.TempDir()
	source := filepath.Join(base, "config.yml")
	require.NoError(t, os.WriteFile(source, []byte("v1"), 0o644))
	require.NoError(t, os.Chmod(source, 0o640))
	remoteDir := filepath.Join(base, "remote")
	target := filepath.Join(remoteDir, "config.yml")

	d := &DeployOps{}
	require.NoError(t, d.DeployRemoteFile(context.Background(), source, "user@testhost", target))

	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "v1", string(got))
	info, err := os.Stat(target)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o640), info.Mode().Perm(), "the deployed file keeps the source mode")
	entries, err := os.ReadDir(remoteDir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "a successful deploy leaves no staging directory behind")
	assertSCPNeverInvoked(t, callLog)
}

func TestCheckSSHConnectivity_EndToEnd(t *testing.T) {
	setupSSHShim(t)

	d := &DeployOps{}
	require.NoError(t, d.CheckSSHConnectivity(context.Background(), "user@testhost"))
}

// assertNoTempRemnant asserts dir holds no single-file staging entry: neither
// a private `.deploy-tmp-` directory nor a legacy `.tmp.` file. It is proof
// that the on-failure `ssh rm -rf <stage>` cleanup ran; deleting that call
// leaves the staging directory behind and fails this assertion.
func assertNoTempRemnant(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return
	}
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".deploy-tmp-", "staging directory must be cleaned up, found: %s", e.Name())
		assert.NotContains(t, e.Name(), ".tmp.", "staging temp file must be cleaned up, found: %s", e.Name())
	}
}

// TestDeployRemoteFile_StreamFailureCleansStaging drives the stream-failure
// branch: the remote command writes a partial file into the private staging
// directory and fails, so the on-failure cleanup must remove the directory.
func TestDeployRemoteFile_StreamFailureCleansStaging(t *testing.T) {
	setupSSHShim(t)
	setupSCPTripwire(t)
	seam := installStreamSeam(t, `echo x >> "$SEAM_COUNTER"; partial=$(printf '%s' "$STREAM_CMD" | sed -e 's/cat > /printf partial > /' -e 's/; actual=.*/; exit 1/'); exec /bin/sh -c "$partial"`)
	source, remoteDir, target := fileDeployFixture(t, "v1")

	err := (&DeployOps{}).DeployRemoteFile(context.Background(), source, "user@testhost", target)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "ssh file stream")
	assert.Equal(t, 1, countAttempts(t, seam.counter), "a remote command failure is not retried")
	assert.Equal(t, "old", readMarker(t, remoteDir, "config.yml"))
	assertNoTempRemnant(t, remoteDir)
}

// TestDeployRemoteFile_MoveFailureCleansStaging drives a failure after the
// verified file is staged: a failing `mv` on the remote PATH leaves the staged
// file in place, so cleanup must remove the staging directory.
func TestDeployRemoteFile_MoveFailureCleansStaging(t *testing.T) {
	setupSSHShim(t)
	setupSCPTripwire(t)
	fakeBin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(fakeBin, "mv"), []byte("#!/bin/sh\necho 'mv: cannot move' >&2\nexit 1\n"), 0o755))
	seam := installStreamSeam(t, `echo x >> "$SEAM_COUNTER"; PATH="$FAKE_BIN:$PATH" exec /bin/sh -c "$STREAM_CMD"`, "FAKE_BIN="+fakeBin)
	source, remoteDir, target := fileDeployFixture(t, "v1")

	err := (&DeployOps{}).DeployRemoteFile(context.Background(), source, "user@testhost", target)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "ssh file stream")
	assert.Contains(t, err.Error(), "mv: cannot move")
	assert.Equal(t, 1, countAttempts(t, seam.counter))
	assert.Equal(t, "old", readMarker(t, remoteDir, "config.yml"))
	assertNoTempRemnant(t, remoteDir)
}

func TestDeployRemoteFile_CancelledContextCleansStaging(t *testing.T) {
	setupSSHShim(t)
	setupSCPTripwire(t)
	started := filepath.Join(t.TempDir(), "stream-started")
	installStreamSeam(t, `: > "$STREAM_STARTED"; exec /bin/sleep 30`, "STREAM_STARTED="+started)
	source, remoteDir, target := fileDeployFixture(t, "v1")

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errCh := make(chan error, 1)
	go func() {
		errCh <- (&DeployOps{}).DeployRemoteFile(ctx, source, "user@testhost", target)
	}()

	require.Eventually(t, func() bool {
		_, err := os.Stat(started)
		return err == nil
	}, 5*time.Second, 10*time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("DeployRemoteFile did not return after cancellation")
	}
	assert.Equal(t, "old", readMarker(t, remoteDir, "config.yml"))
	assertNoTempRemnant(t, remoteDir)
}

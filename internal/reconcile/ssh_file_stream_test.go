package reconcile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests pin DeployRemoteFile's SSH stream (#795, item 2): the single-file
// path no longer uses scp, so no remote path reaches a shell unquoted, a
// truncated transfer never replaces the target, and the private staging
// directory is created exclusively per attempt.

// setupSCPTripwire installs an `scp` that records each call and fails, so a
// test can prove the single-file path never invokes scp. Returns the log path.
func setupSCPTripwire(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	callLog := filepath.Join(dir, "scp-calls")
	t.Setenv("SCP_TRIPWIRE_LOG", callLog)
	shim := "#!/bin/sh\necho \"$*\" >> \"$SCP_TRIPWIRE_LOG\"\necho 'scp: tripwire' >&2\nexit 1\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "scp"), []byte(shim), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return callLog
}

func assertSCPNeverInvoked(t *testing.T, callLog string) {
	t.Helper()
	assert.NoFileExists(t, callLog, "the single-file deploy path must not invoke scp")
}

// streamSeam replaces the single-file stream leg. The script runs under
// /bin/sh with STREAM_CMD set to the remote command bosun built, SEAM_COUNTER
// set to a per-test counter file, and the source file as stdin.
type streamSeam struct {
	counter string
	mu      sync.Mutex
	cmds    []string
}

func installStreamSeam(t *testing.T, script string, env ...string) *streamSeam {
	t.Helper()
	seam := &streamSeam{counter: filepath.Join(t.TempDir(), "attempts")}
	original := newSSHTransferCommand
	t.Cleanup(func() { newSSHTransferCommand = original })
	newSSHTransferCommand = func(ctx context.Context, args ...string) *exec.Cmd {
		remote := ""
		if len(args) > 0 {
			remote = args[len(args)-1]
		}
		seam.mu.Lock()
		seam.cmds = append(seam.cmds, remote)
		seam.mu.Unlock()
		cmd := exec.CommandContext(ctx, "/bin/sh", "-c", script)
		cmd.Env = append(append(os.Environ(), "STREAM_CMD="+remote, "SEAM_COUNTER="+seam.counter), env...)
		return cmd
	}
	return seam
}

func (s *streamSeam) commands() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.cmds...)
}

// setStageIDs makes newRemoteStageID return ids in order. Returns a pointer
// to the number of calls made so far.
func setStageIDs(t *testing.T, ids ...string) *int {
	t.Helper()
	original := newRemoteStageID
	t.Cleanup(func() { newRemoteStageID = original })
	calls := 0
	newRemoteStageID = func() (string, error) {
		id := ids[len(ids)-1]
		if calls < len(ids) {
			id = ids[calls]
		}
		calls++
		return id, nil
	}
	return &calls
}

// fileDeployFixture writes a source file and an existing target with old
// content in a fresh "remote" directory.
func fileDeployFixture(t *testing.T, content string) (source, remoteDir, target string) {
	t.Helper()
	base := t.TempDir()
	source = filepath.Join(base, "config.yml")
	require.NoError(t, os.WriteFile(source, []byte(content), 0o644))
	remoteDir = filepath.Join(base, "remote")
	writeMarker(t, remoteDir, "config.yml", "old")
	return source, remoteDir, filepath.Join(remoteDir, "config.yml")
}

const passthroughStream = `echo x >> "$SEAM_COUNTER"; exec /bin/sh -c "$STREAM_CMD"`

// C-T1: the deploy never calls scp. The legacy-protocol scp shim evaluates
// the remote path in a shell, as OpenSSH's legacy SCP mode does, so on the
// scp-based code the command substitution in the file name runs.
func TestDeployRemoteFile_DoesNotInvokeSCP(t *testing.T) {
	setupSSHShim(t)
	dir := t.TempDir()
	callLog := filepath.Join(dir, "scp-calls")
	t.Setenv("SCP_TRIPWIRE_LOG", callLog)
	legacySCP := "#!/bin/sh\n" +
		"echo \"$*\" >> \"$SCP_TRIPWIRE_LOG\"\n" +
		"while [ $# -gt 0 ]; do\n" +
		"  case \"$1\" in\n" +
		"    -o) shift 2 ;;\n" +
		"    -*) shift ;;\n" +
		"    *) break ;;\n" +
		"  esac\n" +
		"done\n" +
		"exec /bin/sh -c \"cp \\\"$1\\\" ${2#*:}\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "scp"), []byte(legacySCP), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	canary := filepath.Join(t.TempDir(), "canary")
	t.Setenv("BOSUN_CANARY", canary)

	base := t.TempDir()
	source := filepath.Join(base, "config.yml")
	require.NoError(t, os.WriteFile(source, []byte("v1"), 0o644))
	target := filepath.Join(base, "remote", `x-$(touch "$BOSUN_CANARY").yml`)

	require.NoError(t, (&DeployOps{}).DeployRemoteFile(context.Background(), source, "user@testhost", target))

	assertSCPNeverInvoked(t, callLog)
	assert.NoFileExists(t, canary, "no part of the remote path may be evaluated by a shell")
	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "v1", string(got), "the file lands at the literal path")
}

// C-T2: every attempt stages into a fresh, newly named private directory.
func TestDeployRemoteFile_FreshStagingPerAttempt(t *testing.T) {
	setupSSHShim(t)
	callLog := setupSCPTripwire(t)
	calls := setStageIDs(t, "a", "b")
	seam := installStreamSeam(t, `if [ ! -f "$SEAM_COUNTER" ]; then echo x >> "$SEAM_COUNTER"; echo 'Connection reset by peer' >&2; exit 255; fi; `+passthroughStream)
	source, remoteDir, target := fileDeployFixture(t, "v1")

	require.NoError(t, (&DeployOps{}).DeployRemoteFile(context.Background(), source, "user@testhost", target))

	assert.Equal(t, 2, *calls, "one staging identifier per attempt")
	cmds := seam.commands()
	require.Len(t, cmds, 2)
	assert.Contains(t, cmds[0], ".deploy-tmp-a/")
	assert.Contains(t, cmds[1], ".deploy-tmp-b/")
	assert.Equal(t, "v1", readMarker(t, remoteDir, "config.yml"))
	assertNoTempRemnant(t, remoteDir)
	assertSCPNeverInvoked(t, callLog)
}

// C-T3: a stream that reaches the remote incomplete fails its SHA-256 check
// and never replaces the target. On the scp-based code a truncating copy that
// exits 0 is moved over the live file.
func TestDeployRemoteFile_TruncatedStreamLeavesTargetUntouched(t *testing.T) {
	setupSSHShim(t)
	dir := t.TempDir()
	truncatingSCP := "#!/bin/sh\n" +
		"while [ $# -gt 0 ]; do\n" +
		"  case \"$1\" in\n" +
		"    -o) shift 2 ;;\n" +
		"    -*) shift ;;\n" +
		"    *) break ;;\n" +
		"  esac\n" +
		"done\n" +
		"head -c 3 \"$1\" > \"${2#*:}\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "scp"), []byte(truncatingSCP), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	seam := installStreamSeam(t, `echo x >> "$SEAM_COUNTER"; head -c 3 | /bin/sh -c "$STREAM_CMD"`)
	source, remoteDir, target := fileDeployFixture(t, "version-two")

	err := (&DeployOps{}).DeployRemoteFile(context.Background(), source, "user@testhost", target)

	require.ErrorIs(t, err, ErrTransferIntegrity)
	assert.Equal(t, DefaultMaxRetries, countAttempts(t, seam.counter), "a damaged transfer is retried with fresh staging")
	assert.Equal(t, "old", readMarker(t, remoteDir, "config.yml"), "a truncated stream must not replace the target")
	assertNoTempRemnant(t, remoteDir)
}

// C-T4: the deployed mode is the source mode masked by the remote umask.
func TestDeployRemoteFile_PreservesMode(t *testing.T) {
	tests := []struct {
		name string
		src  os.FileMode
		want os.FileMode
	}{
		{"secret 0600", 0o600, 0o600},
		{"executable 0755", 0o755, 0o755},
		{"group-writable 0664 under umask 022", 0o664, 0o644},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupSSHShim(t)
			callLog := setupSCPTripwire(t)
			installStreamSeam(t, `echo x >> "$SEAM_COUNTER"; umask 022; exec /bin/sh -c "$STREAM_CMD"`)
			source, _, target := fileDeployFixture(t, "v1")
			require.NoError(t, os.Chmod(source, tt.src))

			require.NoError(t, (&DeployOps{}).DeployRemoteFile(context.Background(), source, "user@testhost", target))

			info, err := os.Stat(target)
			require.NoError(t, err)
			assert.Equal(t, tt.want, info.Mode().Perm())
			assertSCPNeverInvoked(t, callLog)
		})
	}
}

// C-T5: an entry already at the staging name is never reused, written
// through, or removed, and the attempt fails permanently.
func TestDeployRemoteFile_PreexistingStagingEntrySurvives(t *testing.T) {
	tests := []struct {
		name  string
		plant func(t *testing.T, at string)
		check func(t *testing.T, at string)
	}{
		{
			name:  "fifo",
			plant: func(t *testing.T, at string) { require.NoError(t, syscall.Mkfifo(at, 0o600)) },
			check: func(t *testing.T, at string) {
				info, err := os.Lstat(at)
				require.NoError(t, err)
				assert.NotZero(t, info.Mode()&os.ModeNamedPipe, "the planted FIFO must survive")
			},
		},
		{
			name:  "directory",
			plant: func(t *testing.T, at string) { writeMarker(t, at, "keep", "planted") },
			check: func(t *testing.T, at string) {
				assert.Equal(t, "planted", readMarker(t, at, "keep"), "the planted directory must survive intact")
				_, err := os.Stat(filepath.Join(at, "file"))
				assert.ErrorIs(t, err, os.ErrNotExist, "nothing may be written into a planted directory")
			},
		},
		{
			name: "symlink",
			plant: func(t *testing.T, at string) {
				elsewhere := filepath.Join(filepath.Dir(filepath.Dir(at)), "elsewhere")
				writeMarker(t, elsewhere, "keep", "planted")
				require.NoError(t, os.Symlink(elsewhere, at))
			},
			check: func(t *testing.T, at string) {
				info, err := os.Lstat(at)
				require.NoError(t, err)
				assert.NotZero(t, info.Mode()&os.ModeSymlink, "the planted symlink must survive")
				assert.Equal(t, "planted", readMarker(t, at, "keep"))
				_, err = os.Stat(filepath.Join(at, "file"))
				assert.ErrorIs(t, err, os.ErrNotExist, "nothing may be written through a planted symlink")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupSSHShim(t)
			callLog := setupSCPTripwire(t)
			calls := setStageIDs(t, "fixed")
			seam := installStreamSeam(t, passthroughStream)
			source, remoteDir, target := fileDeployFixture(t, "v1")
			stageDir := filepath.Join(remoteDir, ".deploy-tmp-fixed")
			tt.plant(t, stageDir)

			err := (&DeployOps{}).DeployRemoteFile(context.Background(), source, "user@testhost", target)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "create remote staging dir")
			assert.Equal(t, 1, *calls, "an occupied staging name is a permanent failure")
			assert.NoFileExists(t, seam.counter, "nothing may be streamed after the exclusive mkdir fails")
			tt.check(t, stageDir)
			assert.Equal(t, "old", readMarker(t, remoteDir, "config.yml"))
			assertSCPNeverInvoked(t, callLog)
		})
	}
}

// C-T6: a remote host without sha256sum fails closed on the first attempt.
func TestDeployRemoteFile_SHA256Unavailable(t *testing.T) {
	callLog := setupSCPTripwire(t)
	counter := filepath.Join(t.TempDir(), "attempts")
	dir := t.TempDir()
	shim := "#!/bin/sh\nwhile [ $# -gt 0 ]; do case \"$1\" in -o) shift 2 ;; -*) shift ;; *) break ;; esac; done\nshift\ncase \"$*\" in *'command -v sha256sum'*) echo x >> " + counter + "; exit 78 ;; esac\nexec /bin/sh -c \"$*\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ssh"), []byte(shim), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	source, remoteDir, target := fileDeployFixture(t, "v1")

	err := (&DeployOps{}).DeployRemoteFile(context.Background(), source, "user@testhost", target)

	require.ErrorIs(t, err, ErrRemoteIntegrityUnavailable)
	assert.NotErrorIs(t, err, ErrTransferIntegrity)
	assert.Equal(t, 1, countAttempts(t, counter), "a missing remote capability is not retryable")
	assert.Equal(t, "old", readMarker(t, remoteDir, "config.yml"))
	assertNoTempRemnant(t, remoteDir)
	assertSCPNeverInvoked(t, callLog)
}

// C-T7: fault injection for each error return in the stream path.
func TestDeployRemoteFile_FaultInjection(t *testing.T) {
	t.Run("missing source fails before any ssh leg", func(t *testing.T) {
		callLog := filepath.Join(t.TempDir(), "ssh-calls")
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "ssh"), []byte("#!/bin/sh\necho \"$*\" >> "+callLog+"\nexit 0\n"), 0o755))
		t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
		setupSCPTripwire(t)

		err := (&DeployOps{}).DeployRemoteFile(context.Background(), filepath.Join(t.TempDir(), "missing"), "user@testhost", "/srv/config.yml")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "open source file")
		assert.NoFileExists(t, callLog, "a missing source must fail before any remote work")
	})

	t.Run("unreadable source fails while hashing", func(t *testing.T) {
		setupSSHShim(t)
		setupSCPTripwire(t)
		_, remoteDir, target := fileDeployFixture(t, "v1")

		// Opening a directory succeeds; reading it fails, which drives the
		// hash error branch.
		err := (&DeployOps{}).DeployRemoteFile(context.Background(), t.TempDir(), "user@testhost", target)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "hash source file")
		assert.Equal(t, "old", readMarker(t, remoteDir, "config.yml"))
	})

	t.Run("staging identifier failure", func(t *testing.T) {
		setupSSHShim(t)
		setupSCPTripwire(t)
		original := newRemoteStageID
		t.Cleanup(func() { newRemoteStageID = original })
		calls := 0
		newRemoteStageID = func() (string, error) {
			calls++
			return "", errors.New("entropy unavailable")
		}
		source, remoteDir, target := fileDeployFixture(t, "v1")

		err := (&DeployOps{}).DeployRemoteFile(context.Background(), source, "user@testhost", target)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "generate remote staging identifier")
		assert.Equal(t, 1, calls)
		assert.Equal(t, "old", readMarker(t, remoteDir, "config.yml"))
	})

	t.Run("stream start failure cleans staging", func(t *testing.T) {
		setupSSHShim(t)
		setupSCPTripwire(t)
		original := newSSHTransferCommand
		t.Cleanup(func() { newSSHTransferCommand = original })
		missingSSH := filepath.Join(t.TempDir(), "missing-ssh")
		newSSHTransferCommand = func(ctx context.Context, _ ...string) *exec.Cmd {
			return exec.CommandContext(ctx, missingSSH)
		}
		source, remoteDir, target := fileDeployFixture(t, "v1")

		err := (&DeployOps{}).DeployRemoteFile(context.Background(), source, "user@testhost", target)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "start ssh file stream")
		assert.Equal(t, "old", readMarker(t, remoteDir, "config.yml"))
		assertNoTempRemnant(t, remoteDir)
	})
}

// C-T8: both ssh legs of a single-file deploy carry the host-key policy.
func TestDeployRemoteFile_CarriesHostKeyOptions(t *testing.T) {
	kh := writeTempKnownHosts(t)
	t.Setenv("BOSUN_SSH_INSECURE_HOST_KEY", "")
	t.Setenv("BOSUN_SSH_KNOWN_HOSTS", kh)
	callLog := setupSCPTripwire(t)
	capture := installSSHCaptureShim(t, "exit 0")
	base := t.TempDir()
	source := filepath.Join(base, "config.yml")
	require.NoError(t, os.WriteFile(source, []byte("v1"), 0o644))

	require.NoError(t, (&DeployOps{}).DeployRemoteFile(context.Background(), source, "user@testhost", "/srv/app/config.yml"))

	b, err := os.ReadFile(capture)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	var sawStage, sawStream bool
	for _, line := range lines {
		assert.Contains(t, line, "-o StrictHostKeyChecking=yes", "every ssh leg carries strict checking: %s", line)
		assert.Contains(t, line, "-o UserKnownHostsFile="+kh, "every ssh leg pins the known_hosts file: %s", line)
		if strings.Contains(line, "mkdir -- ") && strings.Contains(line, ".deploy-tmp-") {
			sawStage = true
		}
		if strings.Contains(line, "cat > ") {
			sawStream = true
		}
	}
	assert.True(t, sawStage, "the staging mkdir leg must run over ssh")
	assert.True(t, sawStream, "the file stream leg must run over ssh")
	assertSCPNeverInvoked(t, callLog)
}

func TestTolerateRemoteModeFailure(t *testing.T) {
	tests := []struct {
		target string
		want   bool
	}{
		{"/boot/config/plugins/compose.manager/projects/core/docker-compose.yml", true},
		{"/boot/config//plugins/x.yml", true},
		{"/boot/configx/a.yml", false},
		{"/boot/config/../etc/a.yml", false},
		{"/mnt/user/boot/config/a.yml", false},
		{"/mnt/user/appdata/traefik/traefik.yml", false},
	}
	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			assert.Equal(t, tt.want, tolerateRemoteModeFailure(tt.target))
		})
	}
}

// runStreamCommand runs a built single-file stream command in a local shell
// with content on stdin and fakeBin ahead of PATH. It returns stderr and the
// exit error.
func runStreamCommand(t *testing.T, remoteCmd, content, fakeBin string) (string, error) {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", remoteCmd)
	cmd.Stdin = strings.NewReader(content)
	cmd.Env = append(os.Environ(), "PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stderr.String(), err
}

// The Compose Manager mirror lives on FAT, which cannot store permission
// bits. Only there is a failed chmod tolerated and reported; anywhere else it
// fails the attempt before the move.
func TestBuildRemoteFileStreamCommand_ModeFailureTolerance(t *testing.T) {
	for _, tolerate := range []bool{true, false} {
		t.Run("tolerate="+strconv.FormatBool(tolerate), func(t *testing.T) {
			setupSha256sumShim(t)
			fakeBin := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(fakeBin, "chmod"), []byte("#!/bin/sh\necho 'chmod: Operation not permitted' >&2\nexit 1\n"), 0o755))
			remoteDir := t.TempDir()
			writeMarker(t, remoteDir, "docker-compose.yml", "old")
			stageDir := filepath.Join(remoteDir, ".deploy-tmp-x")
			require.NoError(t, os.Mkdir(stageDir, 0o700))
			target := filepath.Join(remoteDir, "docker-compose.yml")
			content := "services: {}\n"
			sum := sha256.Sum256([]byte(content))

			stderr, err := runStreamCommand(t, buildRemoteFileStreamCommand(stageDir, filepath.Join(stageDir, "file"), target, hex.EncodeToString(sum[:]), 0o644, tolerate), content, fakeBin)

			if tolerate {
				require.NoError(t, err, stderr)
				assert.Contains(t, stderr, remoteModeUnsupportedMarker)
				assert.Equal(t, content, readMarker(t, remoteDir, "docker-compose.yml"))
				assert.NoDirExists(t, stageDir, "a successful stream removes its staging directory")
				return
			}
			require.Error(t, err)
			assert.NotContains(t, stderr, remoteModeUnsupportedMarker)
			assert.Equal(t, "old", readMarker(t, remoteDir, "docker-compose.yml"), "a failed chmod must stop the move")
		})
	}
}

// The command checks the received bytes against the expected hash: a
// mismatch exits 79 and leaves the target alone.
func TestBuildRemoteFileStreamCommand_ChecksumMismatchExits79(t *testing.T) {
	setupSha256sumShim(t)
	remoteDir := t.TempDir()
	writeMarker(t, remoteDir, "config.yml", "old")
	stageDir := filepath.Join(remoteDir, ".deploy-tmp-x")
	require.NoError(t, os.Mkdir(stageDir, 0o700))
	sum := sha256.Sum256([]byte("expected"))

	_, err := runStreamCommand(t, buildRemoteFileStreamCommand(stageDir, filepath.Join(stageDir, "file"), filepath.Join(remoteDir, "config.yml"), hex.EncodeToString(sum[:]), 0o644, false), "different", t.TempDir())

	var exitErr *exec.ExitError
	require.True(t, errors.As(err, &exitErr), "expected an exit error, got %v", err)
	assert.Equal(t, remoteTransferChecksumMismatchExit, exitErr.ExitCode())
	assert.Equal(t, "old", readMarker(t, remoteDir, "config.yml"))
}

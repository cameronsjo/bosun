package reconcile

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// listArchiveMembers returns the member names inside a tar.gz archive.
func listArchiveMembers(t *testing.T, tarFile string) []string {
	t.Helper()
	cmd := exec.Command("tar", "-tzf", tarFile)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Run(), "listing archive members (stderr: %s)", stderr.String())
	var members []string
	for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		if line != "" {
			members = append(members, line)
		}
	}
	return members
}

// TestBackup_ExcludesBackupDestination verifies that when the configured backup
// destination is nested within a backed-up path, the created archive does NOT
// contain the backup destination directory or any prior backup it holds. This
// is the #319 recursion fix: tar must never archive its own growing output.
func TestBackup_ExcludesBackupDestination(t *testing.T) {
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar not installed")
	}

	tmpDir := evalSymlinks(t, t.TempDir())
	appdata := filepath.Join(tmpDir, "appdata")
	serviceDir := filepath.Join(appdata, "service")
	backupDir := filepath.Join(appdata, "backups")
	priorBackup := filepath.Join(backupDir, "backup-old")

	require.NoError(t, os.MkdirAll(serviceDir, 0755))
	require.NoError(t, os.MkdirAll(priorBackup, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(serviceDir, "conf.yml"), []byte("real config"), 0644))
	// A prior backup nested under the backup destination — must not be re-archived.
	require.NoError(t, os.WriteFile(filepath.Join(priorBackup, "configs.tar.gz"), []byte("OLD BACKUP BYTES"), 0644))

	d := NewDeployOps(false, "")
	// Back up the whole appdata tree; backupDir lives INSIDE it (the recursion trap).
	backupName, err := d.Backup(context.Background(), backupDir, []string{appdata})
	require.NoError(t, err)

	tarFile := filepath.Join(backupDir, backupName, "configs.tar.gz")
	members := listArchiveMembers(t, tarFile)

	for _, m := range members {
		assert.NotContains(t, m, "/backups/",
			"archive must not contain the backup destination subtree (member: %s)", m)
	}

	// Sanity: the real config IS present, so we excluded backups without nuking content.
	var foundService bool
	for _, m := range members {
		if strings.Contains(m, "service/conf.yml") {
			foundService = true
			break
		}
	}
	assert.True(t, foundService, "archive should still contain the real service config")
}

// TestBackup_ScopesToDeployedFiles verifies the end-to-end fix for bosun-5qx:
// the backup footprint enumerated from the staging source (backupFilesFromTargets)
// archives only bosun-managed config, so large runtime data co-located in the
// same appdata directory — the cause of the 5m timeout — never enters the archive.
func TestBackup_ScopesToDeployedFiles(t *testing.T) {
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar not installed")
	}

	// Staging footprint: only the rendered config bosun deploys.
	staging := evalSymlinks(t, t.TempDir())
	require.NoError(t, os.MkdirAll(filepath.Join(staging, "appdata/svc"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(staging, "appdata/svc/config.yml"), []byte("managed"), 0644))

	// Destination appdata: the managed config PLUS unrelated runtime data that is
	// NOT part of the staging footprint (media/db/cache stand-in).
	appdata := evalSymlinks(t, t.TempDir())
	require.NoError(t, os.MkdirAll(filepath.Join(appdata, "svc/data"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(appdata, "svc/config.yml"), []byte("managed"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(appdata, "svc/data/runtime.bin"), make([]byte, 1<<20), 0644))

	targets := []DeployTarget{{RelPath: "appdata/svc", TargetPath: "svc", IsDir: true}}
	paths, err := backupFilesFromTargets(staging, targets, appdata)
	require.NoError(t, err)
	require.Equal(t, []string{filepath.Join(appdata, "svc/config.yml")}, paths,
		"enumeration must scope to the staging footprint, excluding runtime data")

	backupDir := evalSymlinks(t, t.TempDir())
	d := NewDeployOps(false, "")
	backupName, err := d.Backup(context.Background(), backupDir, paths)
	require.NoError(t, err)

	members := listArchiveMembers(t, filepath.Join(backupDir, backupName, "configs.tar.gz"))
	var foundConfig bool
	for _, m := range members {
		assert.NotContains(t, m, "runtime.bin",
			"runtime data must not enter the archive (member: %s)", m)
		if strings.Contains(m, "svc/config.yml") {
			foundConfig = true
		}
	}
	assert.True(t, foundConfig, "archive should contain the managed config file")
}

// TestVerifyBackup_HonorsCancelledContext verifies that backup verification runs
// under the caller's context, so a cancelled/deadline-exceeded context aborts the
// `tar -tzf` listing rather than blocking indefinitely on a growing archive (#319).
func TestVerifyBackup_HonorsCancelledContext(t *testing.T) {
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar not installed")
	}

	tmpDir := evalSymlinks(t, t.TempDir())
	backupPath := filepath.Join(tmpDir, "backup")
	require.NoError(t, os.MkdirAll(backupPath, 0755))

	// A valid, non-empty archive so the failure can only come from cancellation.
	srcFile := filepath.Join(tmpDir, "test.txt")
	require.NoError(t, os.WriteFile(srcFile, []byte("test content"), 0644))
	tarFile := filepath.Join(backupPath, "configs.tar.gz")
	require.NoError(t, exec.Command("tar", "-czf", tarFile, "-C", tmpDir, "test.txt").Run())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before verification runs

	d := NewDeployOps(false, "")
	err := d.VerifyBackup(ctx, backupPath)
	require.Error(t, err, "verification under a cancelled context must fail")
	assert.True(t, errors.Is(err, context.Canceled),
		"error should wrap context.Canceled, got: %v", err)
}

// TestCreateBackup_HonorsBackupTimeout verifies that createBackup wraps the
// pipeline context with a bounded BackupTimeout, so a backup that would
// otherwise hang (the #319 wedge) is aborted and surfaced as a failure rather
// than blocking the reconcile indefinitely.
func TestCreateBackup_HonorsBackupTimeout(t *testing.T) {
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar not installed")
	}

	tmpDir := evalSymlinks(t, t.TempDir())
	appdataDir := filepath.Join(tmpDir, "appdata")
	stagingDir := filepath.Join(tmpDir, "staging")
	// Backup destination nested under appdata — the realistic #319 layout.
	backupDir := filepath.Join(appdataDir, "bosun", "backups")

	// Staging footprint defines what gets backed up: a real file in the staging
	// source so backupFilesFromTargets enumerates it (the "bosun" service)...
	require.NoError(t, os.MkdirAll(filepath.Join(stagingDir, "appdata", "bosun"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(stagingDir, "appdata", "bosun", "conf.yml"), []byte("config"), 0644))
	// ...and the matching destination on disk so Backup keeps it and invokes tar
	// (non-empty work, so the expired deadline surfaces instead of an early return).
	require.NoError(t, os.MkdirAll(filepath.Join(appdataDir, "bosun"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(appdataDir, "bosun", "conf.yml"), []byte("config"), 0644))

	cfg := &Config{
		StagingDir:       stagingDir,
		InfraSubDir:      ".",
		BackupDir:        backupDir,
		LocalAppdataPath: appdataDir,
		BackupsToKeep:    3,
		// Already-expired budget: the wrapped context fires immediately, so a
		// correctly-bounded createBackup must fail fast instead of running tar.
		BackupTimeout: time.Nanosecond,
	}
	r := NewReconciler(cfg)

	done := make(chan error, 1)
	go func() { done <- r.createBackup(context.Background(), nil, true) }()

	select {
	case err := <-done:
		// Without the timeout wrap, createBackup(Background) would tar the real
		// content and succeed; the bounded deadline must turn that into a failure.
		require.Error(t, err, "expired BackupTimeout must surface as a backup failure")
		assert.True(t, errors.Is(err, context.DeadlineExceeded),
			"timeout failure must wrap context.DeadlineExceeded, got: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("createBackup did not return within bound — BackupTimeout not honored")
	}
}

// TestCreateBackup_RemotePathPropagatesFailure drives the remote branch of
// createBackup: with local=false it selects RemoteAppdataPath and calls
// BackupRemote, whose validateHost guard rejects the unresolved host and fails
// fast (no SSH attempt). The non-timeout failure must propagate to the caller.
func TestCreateBackup_RemotePathPropagatesFailure(t *testing.T) {
	tmpDir := evalSymlinks(t, t.TempDir())
	stagingDir := filepath.Join(tmpDir, "staging")
	backupDir := filepath.Join(tmpDir, "backups")

	// A staging footprint so backupFilesFromTargets enumerates a remote path.
	require.NoError(t, os.MkdirAll(filepath.Join(stagingDir, "appdata", "svc"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(stagingDir, "appdata", "svc", "conf.yml"), []byte("config"), 0644))

	cfg := &Config{
		StagingDir:        stagingDir,
		InfraSubDir:       ".",
		BackupDir:         backupDir,
		RemoteAppdataPath: "/mnt/appdata",
		BackupsToKeep:     3,
		// No TargetHost and nil secrets -> getTargetHost yields an unusable host
		// that validateHost rejects, so BackupRemote fails before any ssh call.
	}
	r := NewReconciler(cfg)

	err := r.createBackup(context.Background(), nil, false)
	require.Error(t, err, "remote backup with an invalid host must propagate a failure")
	assert.False(t, errors.Is(err, context.DeadlineExceeded),
		"failure must be the host/ssh error, not a timeout, got: %v", err)
}

// TestCreateBackup_DiscoveryFailureFallsBackToFullAppdata verifies that when
// deploy-target discovery fails (here, a missing staging subtree), the backup
// falls back to the full appdata path rather than producing a no-op archive —
// preserving rollback protection exactly when the footprint is unknown.
func TestCreateBackup_DiscoveryFailureFallsBackToFullAppdata(t *testing.T) {
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar not installed")
	}

	tmpDir := evalSymlinks(t, t.TempDir())
	appdataDir := filepath.Join(tmpDir, "appdata")
	backupDir := filepath.Join(tmpDir, "backups")
	require.NoError(t, os.MkdirAll(appdataDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(appdataDir, "live.conf"), []byte("existing"), 0644))

	cfg := &Config{
		// InfraSubDir points at a path that does not exist under StagingDir, so
		// discoverDeployTargets fails and the full-appdata fallback engages.
		StagingDir:       tmpDir,
		InfraSubDir:      "missing-staging",
		BackupDir:        backupDir,
		LocalAppdataPath: appdataDir,
		BackupsToKeep:    3,
	}
	r := NewReconciler(cfg)

	require.NoError(t, r.createBackup(context.Background(), nil, true))

	members := listArchiveMembers(t, filepath.Join(r.lastBackupPath, "configs.tar.gz"))
	var found bool
	for _, m := range members {
		if strings.Contains(m, "live.conf") {
			found = true
		}
	}
	assert.True(t, found, "discovery-failure fallback must back up the full appdata path, not a no-op")
}

// TestExtractBackupArchive round-trips through the real Backup(): create an
// archive, extract it, and confirm the backed-up file resolves under the
// extracted root accounting for tar's leading-'/' stripping (#332/#335).
func TestExtractBackupArchive(t *testing.T) {
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar not installed")
	}

	tmpDir := evalSymlinks(t, t.TempDir())
	appdata := filepath.Join(tmpDir, "appdata", "compose")
	require.NoError(t, os.MkdirAll(appdata, 0755))
	composeFile := filepath.Join(appdata, "stack.yml")
	require.NoError(t, os.WriteFile(composeFile, []byte("services: {}"), 0644))

	backupDir := filepath.Join(tmpDir, "backups")
	d := NewDeployOps(false, "")
	backupName, err := d.Backup(context.Background(), backupDir, []string{appdata})
	require.NoError(t, err)
	backupPath := filepath.Join(backupDir, backupName)

	root, cleanup, err := extractBackupArchive(context.Background(), backupPath)
	require.NoError(t, err)
	defer cleanup()
	require.NotEmpty(t, root)

	// The deployed compose file's backed-up copy must resolve from the extracted
	// tree using its original absolute path.
	resolved, ok := resolveBackupFile(root, composeFile)
	require.True(t, ok, "backed-up compose file should resolve under extracted root")
	content, err := os.ReadFile(resolved)
	require.NoError(t, err)
	assert.Equal(t, "services: {}", string(content))

	// cleanup must remove the temp tree.
	cleanup()
	_, statErr := os.Stat(root)
	assert.True(t, os.IsNotExist(statErr), "cleanup should remove the extracted temp dir")
}

func TestExtractBackupArchive_MissingArchive(t *testing.T) {
	backupPath := t.TempDir() // No configs.tar.gz inside.
	root, cleanup, err := extractBackupArchive(context.Background(), backupPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "backup archive not found")
	assert.Empty(t, root)
	assert.NotNil(t, cleanup, "cleanup must be safe to call even on error")
	cleanup() // must not panic
}

// makeValidArchive writes a valid, non-empty tar.gz at dst containing a single
// member (memberName -> content), using the real tar binary so the fixture
// matches what Backup()/BackupRemote() produce in the field.
func makeValidArchive(t *testing.T, dst, memberName string, content []byte) {
	t.Helper()
	srcDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, memberName), content, 0644))
	cmd := exec.Command("tar", "-czf", dst, "-C", srcDir, memberName)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Run(), "creating payload archive (stderr: %s)", stderr.String())
}

// writeFakeSSH installs a fake `ssh` executable on PATH for the duration of the
// test so BackupRemote's exec.Command("ssh", ...) invokes it instead of the real
// client. Behavior is driven by env vars the caller sets:
//
//	FAKE_SSH_PAYLOAD    path to a valid tar.gz emitted on a successful attempt
//	FAKE_SSH_COUNTER    path to a file the script uses to count attempts
//	FAKE_SSH_SUCCEED_ON 1-based attempt to succeed on; earlier attempts emit a
//	                    partial stream + a transient error and exit non-zero
//	FAKE_SSH_FATAL      "1" => emit the full payload then exit non-zero with a
//	                    NON-transient error (a valid-looking but flagged backup)
func writeFakeSSH(t *testing.T) {
	t.Helper()
	binDir := t.TempDir()
	script := `#!/bin/sh
counter="$FAKE_SSH_COUNTER"
n=$(cat "$counter" 2>/dev/null || echo 0)
n=$((n + 1))
printf '%s' "$n" > "$counter"

if [ "$FAKE_SSH_FATAL" = "1" ]; then
  cat "$FAKE_SSH_PAYLOAD"
  echo "tar: some files changed as we read them" >&2
  exit 1
fi

if [ -n "$FAKE_SSH_SUCCEED_ON" ] && [ "$n" -lt "$FAKE_SSH_SUCCEED_ON" ]; then
  printf 'PARTIAL_ATTEMPT_%s' "$n"
  echo "ssh: connect to host port 22: connection refused" >&2
  exit 255
fi

cat "$FAKE_SSH_PAYLOAD"
exit 0
`
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "ssh"), []byte(script), 0755))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// assertNoBackupDirs asserts backupDir holds no backup-* archive directory, i.e.
// a failed backup left nothing behind for a rollback to trust.
func assertNoBackupDirs(t *testing.T, backupDir string) {
	t.Helper()
	entries, err := os.ReadDir(backupDir)
	if os.IsNotExist(err) {
		return
	}
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, strings.HasPrefix(e.Name(), "backup-"),
			"a failed backup must leave no archive behind (found: %s)", e.Name())
	}
}

// TestBackupRemote_SSHErrorIsFatal verifies the #240 data-integrity fix: when the
// remote ssh/tar command reports an error, BackupRemote MUST fail and remove the
// partial archive — even when the streamed bytes happen to be a complete, listable
// archive. The prior behavior logged the error, let VerifyBackup pass the valid
// stream, and returned success — trusting a backup the remote flagged as bad.
func TestBackupRemote_SSHErrorIsFatal(t *testing.T) {
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar not installed")
	}
	writeFakeSSH(t)

	tmp := evalSymlinks(t, t.TempDir())
	payload := filepath.Join(tmp, "payload.tar.gz")
	makeValidArchive(t, payload, "conf.yml", []byte("a complete, valid archive body"))

	t.Setenv("FAKE_SSH_PAYLOAD", payload)
	t.Setenv("FAKE_SSH_COUNTER", filepath.Join(tmp, "counter"))
	t.Setenv("FAKE_SSH_FATAL", "1")

	backupDir := filepath.Join(tmp, "backups")
	d := NewDeployOps(false, "")
	backupName, err := d.BackupRemote(context.Background(), "localhost", backupDir, []string{"/mnt/appdata/svc"})

	require.Error(t, err, "a non-nil ssh/tar error must fail the backup even when the stream lists cleanly")
	assert.Empty(t, backupName, "no backup name may be returned on failure")
	assertNoBackupDirs(t, backupDir)
}

// TestBackupRemote_RetryDoesNotConcatenateStreams verifies the #240 stream-reset
// fix: a transient failure that streamed a partial archive, followed by a
// successful retry, MUST yield a file holding exactly the successful attempt's
// single stream — not the failed attempt's bytes concatenated ahead of it.
func TestBackupRemote_RetryDoesNotConcatenateStreams(t *testing.T) {
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar not installed")
	}
	writeFakeSSH(t)

	tmp := evalSymlinks(t, t.TempDir())
	payload := filepath.Join(tmp, "payload.tar.gz")
	makeValidArchive(t, payload, "conf.yml", []byte("retry payload config"))
	payloadBytes, err := os.ReadFile(payload)
	require.NoError(t, err)

	t.Setenv("FAKE_SSH_PAYLOAD", payload)
	t.Setenv("FAKE_SSH_COUNTER", filepath.Join(tmp, "counter"))
	t.Setenv("FAKE_SSH_SUCCEED_ON", "2") // attempt 1 fails transiently, attempt 2 succeeds

	backupDir := filepath.Join(tmp, "backups")
	d := NewDeployOps(false, "")
	backupName, err := d.BackupRemote(context.Background(), "localhost", backupDir, []string{"/mnt/appdata/svc"})
	require.NoError(t, err, "a transient failure then success must yield a valid backup")
	require.NotEmpty(t, backupName)

	got, err := os.ReadFile(filepath.Join(backupDir, backupName, "configs.tar.gz"))
	require.NoError(t, err)
	assert.NotContains(t, string(got), "PARTIAL_ATTEMPT_",
		"a retried attempt must not keep the failed attempt's partial stream")
	assert.Equal(t, payloadBytes, got,
		"the archive must contain exactly the successful attempt's single stream")
}

// TestVerifyBackup_RejectsTruncatedArchive verifies the #240 integrity fix: a
// truncated archive (headers intact, trailing data lost) MUST be rejected. The
// prior listing-only check (`tar -tzf`) could accept such an archive; the full
// gzip+tar read-through catches the truncated stream.
func TestVerifyBackup_RejectsTruncatedArchive(t *testing.T) {
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar not installed")
	}

	tmp := evalSymlinks(t, t.TempDir())
	backupPath := filepath.Join(tmp, "backup")
	require.NoError(t, os.MkdirAll(backupPath, 0755))
	tarFile := filepath.Join(backupPath, "configs.tar.gz")

	// Incompressible content so the compressed archive is substantial and a
	// mid-file truncation reliably lands inside real deflate data, not padding.
	body := make([]byte, 16<<10)
	_, err := cryptorand.Read(body)
	require.NoError(t, err)
	makeValidArchive(t, tarFile, "data.bin", body)

	d := NewDeployOps(false, "")
	require.NoError(t, d.VerifyBackup(context.Background(), backupPath),
		"a complete archive must pass verification")

	// Truncate to half its size: the gzip stream is now incomplete.
	info, err := os.Stat(tarFile)
	require.NoError(t, err)
	require.NoError(t, os.Truncate(tarFile, info.Size()/2))

	require.Error(t, d.VerifyBackup(context.Background(), backupPath),
		"a truncated archive must be rejected by verification")
}

// TestVerifyArchiveIntegrity covers the #240 read-through directly: a complete
// archive reads to EOF cleanly, while a truncated one fails when the incomplete
// gzip/tar stream cannot be fully decompressed — the corruption a header-only
// listing can miss.
func TestVerifyArchiveIntegrity(t *testing.T) {
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar not installed")
	}

	tmp := evalSymlinks(t, t.TempDir())
	tarFile := filepath.Join(tmp, "configs.tar.gz")
	body := make([]byte, 16<<10)
	_, err := cryptorand.Read(body)
	require.NoError(t, err)
	makeValidArchive(t, tarFile, "data.bin", body)

	require.NoError(t, verifyArchiveIntegrity(context.Background(), tarFile),
		"a complete archive must read through to EOF without error")

	info, err := os.Stat(tarFile)
	require.NoError(t, err)
	require.NoError(t, os.Truncate(tarFile, info.Size()/2))

	require.Error(t, verifyArchiveIntegrity(context.Background(), tarFile),
		"a truncated stream must fail the full read-through")
}

func TestResolveBackupFile(t *testing.T) {
	root := t.TempDir()
	// Simulate an extracted tree: a member at <root>/mnt/appdata/compose/stack.yml.
	member := filepath.Join(root, "mnt", "appdata", "compose", "stack.yml")
	require.NoError(t, os.MkdirAll(filepath.Dir(member), 0755))
	require.NoError(t, os.WriteFile(member, []byte("x"), 0644))

	t.Run("resolves absolute original path with leading slash stripped", func(t *testing.T) {
		resolved, ok := resolveBackupFile(root, "/mnt/appdata/compose/stack.yml")
		require.True(t, ok)
		assert.Equal(t, member, resolved)
	})

	t.Run("reports missing file", func(t *testing.T) {
		resolved, ok := resolveBackupFile(root, "/mnt/appdata/compose/absent.yml")
		assert.False(t, ok)
		assert.Empty(t, resolved)
	})
}

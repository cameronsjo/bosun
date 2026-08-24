package reconcile

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"errors"
	"io"
	"io/fs"
	"net"
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

// TestCreateBackup_RemoteEmptyFootprintCreatesNoAnchor covers the caller-level
// fresh-host path from #459. A successfully discovered but empty staging tree
// produces no remote paths, so createBackup must validate the configured host
// but perform no SSH work or record an archive directory as a rollback anchor.
func TestCreateBackup_RemoteEmptyFootprintCreatesNoAnchor(t *testing.T) {
	tmpDir := evalSymlinks(t, t.TempDir())
	stagingDir := filepath.Join(tmpDir, "staging")
	backupDir := filepath.Join(tmpDir, "backups")
	require.NoError(t, os.MkdirAll(stagingDir, 0755))

	cfg := &Config{
		StagingDir:        stagingDir,
		InfraSubDir:       ".",
		BackupDir:         backupDir,
		RemoteAppdataPath: "/mnt/appdata",
		BackupsToKeep:     3,
		TargetHost:        "localhost",
	}
	r := NewReconciler(cfg)
	// A no-archive result records this run's state explicitly; it must not rely
	// on Run having cleared a prior cycle's rollback anchor first.
	r.lastBackupPath = filepath.Join(backupDir, "stale-anchor")
	r.lastBackupIsFresh = true

	require.NoError(t, r.createBackup(context.Background(), nil, false))
	assert.Empty(t, r.lastBackupPath, "an empty remote footprint must not create a rollback anchor")
	assert.False(t, r.lastBackupIsFresh, "no archive was created, so no fresh anchor exists")
	assert.NoDirExists(t, backupDir, "an empty remote footprint must not leave a backup artifact")
}

// TestCreateBackup_RemoteEnumerationFailureIsNotEmptyFootprint distinguishes a
// successful empty staging tree from a walk that failed before discovering any
// paths. The latter is an unknown backup footprint and must fail before the
// remote empty-footprint shortcut can clear the rollback requirement.
func TestCreateBackup_RemoteEnumerationFailureIsNotEmptyFootprint(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("chmod 0000 does not deny root; skipping permission-based fault injection")
	}

	tmpDir := evalSymlinks(t, t.TempDir())
	stagingDir := filepath.Join(tmpDir, "staging")
	locked := filepath.Join(stagingDir, "appdata", "service")
	backupDir := filepath.Join(tmpDir, "backups")
	require.NoError(t, os.MkdirAll(locked, 0755))
	require.NoError(t, os.Chmod(locked, 0000))
	t.Cleanup(func() { _ = os.Chmod(locked, 0755) })

	r := NewReconciler(&Config{
		StagingDir:        stagingDir,
		InfraSubDir:       ".",
		BackupDir:         backupDir,
		RemoteAppdataPath: "/mnt/appdata",
		BackupsToKeep:     3,
		TargetHost:        "localhost",
	})

	err := r.createBackup(context.Background(), nil, false)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrBackupFootprintIncomplete)
	assert.ErrorIs(t, err, fs.ErrPermission)
	assert.Empty(t, r.lastBackupPath)
	assert.False(t, r.lastBackupIsFresh)
	assert.NoDirExists(t, backupDir, "enumeration failure must abort before SSH or artifact creation")
}

// TestCreateBackup_RemotePartialEnumerationFailureFailsClosed proves that a
// non-empty partial list is still an unknown footprint. Recording a backup of
// the readable prefix as a fresh rollback anchor would let the deploy proceed
// without a recoverable copy of the unreadable managed subtree.
func TestCreateBackup_RemotePartialEnumerationFailureFailsClosed(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("chmod 0000 does not deny root; skipping permission-based fault injection")
	}

	tmpDir := evalSymlinks(t, t.TempDir())
	stagingDir := filepath.Join(tmpDir, "staging")
	serviceDir := filepath.Join(stagingDir, "appdata", "service")
	locked := filepath.Join(serviceDir, "zzz-locked")
	backupDir := filepath.Join(tmpDir, "backups")
	require.NoError(t, os.MkdirAll(locked, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(serviceDir, "aaa-readable.yml"), []byte("managed"), 0644))
	require.NoError(t, os.Chmod(locked, 0000))
	t.Cleanup(func() { _ = os.Chmod(locked, 0755) })

	r := NewReconciler(&Config{
		StagingDir:        stagingDir,
		InfraSubDir:       ".",
		BackupDir:         backupDir,
		RemoteAppdataPath: "/mnt/appdata",
		BackupsToKeep:     3,
		TargetHost:        "localhost",
	})

	err := r.createBackup(context.Background(), nil, false)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrBackupFootprintIncomplete)
	assert.ErrorIs(t, err, fs.ErrPermission)
	assert.Empty(t, r.lastBackupPath)
	assert.False(t, r.lastBackupIsFresh)
	assert.NoDirExists(t, backupDir, "partial enumeration must abort before SSH or artifact creation")
}

// TestReconcilerRun_IncompleteBackupFootprintAbortsBeforeDeploy covers the
// pipeline caller, not only createBackup. Even with a prior verified backup
// available, an unknown current footprint cannot safely use that stale anchor:
// it may omit paths this deploy is about to mutate. Both zero-result and
// partially enumerated failures must return before deploy changes appdata.
func TestReconcilerRun_IncompleteBackupFootprintAbortsBeforeDeploy(t *testing.T) {
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar not installed")
	}

	tests := []struct {
		name  string
		paths []string
	}{
		{name: "zero paths before failure"},
		{name: "partial paths before failure", paths: []string{"/mnt/appdata/service/config.yml"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := evalSymlinks(t, t.TempDir())
			repoDir := filepath.Join(tmpDir, "repo")
			stagingDir := filepath.Join(tmpDir, "staging")
			appdataDir := filepath.Join(tmpDir, "appdata")
			backupDir := filepath.Join(tmpDir, "backups")
			stateFile := filepath.Join(tmpDir, "state.json")

			cfg := &Config{
				LockFile:         filepath.Join(tmpDir, "reconcile.lock"),
				StateFile:        stateFile,
				RepoDir:          repoDir,
				StagingDir:       stagingDir,
				LocalAppdataPath: appdataDir,
				BackupDir:        backupDir,
				BackupsToKeep:    3,
				InfraSubDir:      "unraid",
				ContentHashSync:  true,
				OnFailure:        true,
			}
			seedStubComposeService(t, cfg)
			sourceConfig := filepath.Join(repoDir, "unraid", "appdata", "service", "config.yml")
			destinationConfig := filepath.Join(appdataDir, "service", "config.yml")
			require.NoError(t, os.MkdirAll(filepath.Dir(sourceConfig), 0755))
			require.NoError(t, os.MkdirAll(filepath.Dir(destinationConfig), 0755))
			require.NoError(t, os.WriteFile(sourceConfig, []byte("new config"), 0644))
			require.NoError(t, os.WriteFile(destinationConfig, []byte("old config"), 0644))

			// Make the stale-anchor fallback available so this test fails if Run
			// accidentally routes the sentinel through applyBackupFailurePolicy.
			_ = mkBackupDir(t, backupDir, "backup-20200101-000001", true)

			alerter := &mockAlertSender{}
			r := NewReconciler(cfg,
				WithGitOperations(&mockGitOps{
					syncChanged: true,
					syncBefore:  "aaa111",
					syncAfter:   "bbb222",
				}),
				WithAlerter(alerter),
			)
			r.backupFilesFromTargetsFn = func(string, []DeployTarget, string) ([]string, error) {
				return append([]string(nil), tt.paths...), fs.ErrPermission
			}

			err := r.Run(context.Background())

			require.Error(t, err)
			assert.ErrorIs(t, err, ErrBackupFootprintIncomplete)
			assert.ErrorIs(t, err, fs.ErrPermission)
			content, readErr := os.ReadFile(destinationConfig)
			require.NoError(t, readErr)
			assert.Equal(t, "old config", string(content), "deploy must not mutate appdata after incomplete enumeration")
			assert.Empty(t, LoadState(stateFile).LastDeployedCommit, "failed admission must not record the commit as deployed")
			assert.Equal(t, 1, alerter.deployFailureCalls)
		})
	}
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

// TestSafeExtractBackup_RoundTrip round-trips through the real Backup(): create an
// archive, extract it, and confirm the backed-up file resolves under the
// extracted root accounting for tar's leading-'/' stripping (#332/#335).
func TestSafeExtractBackup_RoundTrip(t *testing.T) {
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

	root, cleanup, err := safeExtractBackup(context.Background(), filepath.Join(backupPath, "configs.tar.gz"))
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

func TestSafeExtractBackup_MissingArchive(t *testing.T) {
	backupPath := t.TempDir() // No configs.tar.gz inside.
	root, cleanup, err := safeExtractBackup(context.Background(), filepath.Join(backupPath, "configs.tar.gz"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot open archive")
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

// TestBackupRemote_EmptyFootprintSkipsAllWork verifies the leaf operation's
// ordering, not just its final return values. Nothing-to-back-up is a clean
// no-op after host validation even when later-stage values are adversarial: no
// destination creation, context-bound SSH, or archive verification may run when
// the remote footprint is empty.
func TestBackupRemote_EmptyFootprintSkipsAllWork(t *testing.T) {
	t.Run("invalid host still fails at the common remote boundary", func(t *testing.T) {
		backupDir := filepath.Join(t.TempDir(), "backups")
		d := NewDeployOps(false, "")

		backupName, err := d.BackupRemote(context.Background(), "-oProxyCommand=sh", backupDir, nil)

		require.ErrorContains(t, err, "invalid SSH host")
		assert.Empty(t, backupName)
		assert.NoDirExists(t, backupDir)
	})

	t.Run("uncreatable destination is irrelevant without an archive", func(t *testing.T) {
		tmpDir := t.TempDir()
		parentFile := filepath.Join(tmpDir, "not-a-directory")
		require.NoError(t, os.WriteFile(parentFile, []byte("sentinel"), 0644))
		d := NewDeployOps(false, "")

		backupName, err := d.BackupRemote(
			context.Background(),
			"localhost",
			filepath.Join(parentFile, "backups"),
			[]string{},
		)

		require.NoError(t, err)
		assert.Empty(t, backupName)
	})

	t.Run("cancelled context is irrelevant without subprocess work", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		backupDir := filepath.Join(t.TempDir(), "backups")
		d := NewDeployOps(false, "")

		backupName, err := d.BackupRemote(ctx, "localhost", backupDir, nil)

		require.NoError(t, err)
		assert.Empty(t, backupName)
		assert.NoDirExists(t, backupDir)
	})
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

	require.NoError(t, verifyArchiveIntegrity(context.Background(), tarFile, MaxVerifyDecompressedBytes),
		"a complete archive must read through to EOF without error")

	info, err := os.Stat(tarFile)
	require.NoError(t, err)
	require.NoError(t, os.Truncate(tarFile, info.Size()/2))

	require.Error(t, verifyArchiveIntegrity(context.Background(), tarFile, MaxVerifyDecompressedBytes),
		"a truncated stream must fail the full read-through")
}

// TestVerifyArchiveIntegrity_RejectsOversizedArchive covers the #240 DoS bound:
// an archive whose decompressed size exceeds the byte budget is rejected promptly
// with ErrBackupTooLarge, rather than decompressed in full (a decompression bomb).
// The member is a block of zeros — tiny on disk, large decompressed — so the test
// exercises the cap without writing a huge fixture.
func TestVerifyArchiveIntegrity_RejectsOversizedArchive(t *testing.T) {
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar not installed")
	}

	tmp := evalSymlinks(t, t.TempDir())
	tarFile := filepath.Join(tmp, "configs.tar.gz")
	// 64 KiB of zeros compresses to a few hundred bytes but decompresses well past
	// the 1 KiB budget below.
	makeValidArchive(t, tarFile, "big.bin", make([]byte, 64<<10))

	err := verifyArchiveIntegrity(context.Background(), tarFile, 1<<10) // 1 KiB budget
	require.Error(t, err, "an archive exceeding the decompressed-byte budget must be rejected")
	assert.ErrorIs(t, err, ErrBackupTooLarge, "the overflow must surface as ErrBackupTooLarge")
}

// TestVerifyArchiveIntegrity_HonorsCancelledContext verifies the read-through
// aborts on a cancelled context (the outer entry-boundary guard) rather than
// running to completion (#240).
func TestVerifyArchiveIntegrity_HonorsCancelledContext(t *testing.T) {
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar not installed")
	}

	tmp := evalSymlinks(t, t.TempDir())
	tarFile := filepath.Join(tmp, "configs.tar.gz")
	makeValidArchive(t, tarFile, "data.bin", []byte("some content"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := verifyArchiveIntegrity(ctx, tarFile, MaxVerifyDecompressedBytes)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

// cancelOnFirstRead is a test reader that cancels its context on the first Read,
// then keeps offering zero bytes. It lets a test prove copyCtx stops mid-stream at
// the next ctx check instead of draining the reader.
type cancelOnFirstRead struct {
	cancel    context.CancelFunc
	remaining int64
	fired     bool
}

func (c *cancelOnFirstRead) Read(p []byte) (int, error) {
	if !c.fired {
		c.fired = true
		c.cancel()
	}
	if c.remaining <= 0 {
		return 0, io.EOF
	}
	n := int64(len(p))
	if n > c.remaining {
		n = c.remaining
	}
	c.remaining -= n
	return int(n), nil
}

// TestCopyCtx_InterruptsMidStream verifies the #240 mid-member guard: copyCtx
// stops at the next chunk boundary once ctx is cancelled, rather than draining a
// large member — this is the decompression-DoS interruption the review flagged as
// untested. The reader offers 100 MiB but cancels on its first Read, so copyCtx
// must return context.Canceled having copied far less than the whole reader.
func TestCopyCtx_InterruptsMidStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &cancelOnFirstRead{cancel: cancel, remaining: 100 << 20} // 100 MiB available

	n, err := copyCtx(ctx, io.Discard, r)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Less(t, n, int64(100<<20), "copy must stop early on cancellation, not drain the reader")
}

// mkBackupDir creates a backup-<name> directory under backupDir containing a
// configs.tar.gz; when valid is false the archive is truncated so VerifyBackup
// rejects it. Returns the backup directory path.
func mkBackupDir(t *testing.T, backupDir, name string, valid bool) string {
	t.Helper()
	dir := filepath.Join(backupDir, name)
	require.NoError(t, os.MkdirAll(dir, 0755))
	tarFile := filepath.Join(dir, "configs.tar.gz")
	makeValidArchive(t, tarFile, "conf.yml", []byte("config "+name))
	if !valid {
		info, err := os.Stat(tarFile)
		require.NoError(t, err)
		require.NoError(t, os.Truncate(tarFile, info.Size()/2))
	}
	return dir
}

// TestLatestVerifiedBackup_SkipsCorruptReturnsNewestValid verifies the #240
// rollback-anchor scan: the newest backup that passes VerifyBackup wins, and a
// corrupt newer backup is skipped rather than selected.
func TestLatestVerifiedBackup_SkipsCorruptReturnsNewestValid(t *testing.T) {
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar not installed")
	}
	tmp := evalSymlinks(t, t.TempDir())
	backupDir := filepath.Join(tmp, "backups")

	_ = mkBackupDir(t, backupDir, "backup-20200101-000001", true)           // older, valid
	validNewer := mkBackupDir(t, backupDir, "backup-20200101-000002", true) // newer, valid
	_ = mkBackupDir(t, backupDir, "backup-20200101-000003", false)          // newest, corrupt

	d := NewDeployOps(false, "")
	got, err := d.LatestVerifiedBackup(context.Background(), backupDir)
	require.NoError(t, err)
	assert.Equal(t, validNewer, got, "must skip the corrupt newest and return the newest verified backup")
}

// TestApplyBackupFailurePolicy_FallsBackToPriorVerifiedBackup covers the #240
// invariant: when a fresh backup fails but a prior verified backup exists, the
// deploy proceeds with that prior backup as the rollback anchor.
func TestApplyBackupFailurePolicy_FallsBackToPriorVerifiedBackup(t *testing.T) {
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar not installed")
	}
	tmp := evalSymlinks(t, t.TempDir())
	backupDir := filepath.Join(tmp, "backups")
	priorPath := mkBackupDir(t, backupDir, "backup-20200101-000001", true)

	cfg := DefaultConfig()
	cfg.BackupDir = backupDir
	r := NewReconciler(cfg)

	err := r.applyBackupFailurePolicy(context.Background(), errors.New("remote backup failed: exit status 1"))
	require.NoError(t, err, "a verified prior backup must let the deploy proceed")
	assert.Equal(t, priorPath, r.lastBackupPath, "rollback anchor must point at the prior verified backup")
}

// TestApplyBackupFailurePolicy_AbortsWhenNoAnchor covers the #240 fail-safe: when
// a fresh backup fails and NO verified prior backup exists, the deploy is aborted
// (error returned) and no rollback anchor is set — never mutate without an anchor.
func TestApplyBackupFailurePolicy_AbortsWhenNoAnchor(t *testing.T) {
	tmp := evalSymlinks(t, t.TempDir())
	backupDir := filepath.Join(tmp, "backups") // does not exist / empty

	cfg := DefaultConfig()
	cfg.BackupDir = backupDir
	r := NewReconciler(cfg)

	err := r.applyBackupFailurePolicy(context.Background(), errors.New("remote backup failed"))
	require.Error(t, err, "no verified prior backup must abort the deploy")
	assert.Empty(t, r.lastBackupPath, "no anchor may be set when the deploy is aborted")
	assert.ErrorContains(t, err, "no verified prior backup")
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

// TestBackup_PreservesSymlinks verifies the native archive writer records a
// symlink as a symlink (with its target), not as a copy of the pointed-to file
// and not as a walk failure — appdata configs routinely symlink into siblings.
func TestBackup_PreservesSymlinks(t *testing.T) {
	tmpDir := evalSymlinks(t, t.TempDir())
	appdata := filepath.Join(tmpDir, "appdata")
	require.NoError(t, os.MkdirAll(appdata, 0755))
	target := filepath.Join(appdata, "real.yaml")
	require.NoError(t, os.WriteFile(target, []byte("key: value\n"), 0644))
	link := filepath.Join(appdata, "alias.yaml")
	require.NoError(t, os.Symlink("real.yaml", link))

	backupDir := filepath.Join(tmpDir, "backups")
	d := NewDeployOps(false, "")
	backupName, err := d.Backup(context.Background(), backupDir, []string{appdata})
	require.NoError(t, err)

	root, cleanup, err := safeExtractBackup(
		context.Background(),
		filepath.Join(backupDir, backupName, "configs.tar.gz"),
	)
	require.NoError(t, err)
	defer cleanup()

	extracted, ok := resolveBackupFile(root, link)
	require.True(t, ok, "symlink member missing from archive")
	fi, err := os.Lstat(extracted)
	require.NoError(t, err)
	assert.NotZero(t, fi.Mode()&os.ModeSymlink, "symlink must extract as a symlink, not a regular file")
	got, err := os.Readlink(extracted)
	require.NoError(t, err)
	assert.Equal(t, "real.yaml", got)
}

// TestBackup_SkipsIrregularFiles verifies a live unix socket inside a backed-up
// path is skipped rather than failing the backup. Appdata is live container
// state — postgres/docker sockets are routinely present — and since #240 a
// failed backup aborts the whole deploy, so an archiving error here would turn
// every socket into a deploy outage.
func TestBackup_SkipsIrregularFiles(t *testing.T) {
	tmpDir := evalSymlinks(t, t.TempDir())
	appdata := filepath.Join(tmpDir, "appdata")
	require.NoError(t, os.MkdirAll(appdata, 0755))
	cfg := filepath.Join(appdata, "config.yaml")
	require.NoError(t, os.WriteFile(cfg, []byte("key: value\n"), 0644))

	// Bind via a short relative path from inside appdata: sockaddr_un caps
	// socket paths (~104 bytes on macOS) and t.TempDir() paths exceed it.
	origWd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(appdata))
	ln, err := net.Listen("unix", "live.sock")
	require.NoError(t, os.Chdir(origWd))
	require.NoError(t, err, "creating unix socket fixture")
	defer func() { _ = ln.Close() }()

	backupDir := filepath.Join(tmpDir, "backups")
	d := NewDeployOps(false, "")
	backupName, err := d.Backup(context.Background(), backupDir, []string{appdata})
	require.NoError(t, err, "a unix socket in appdata must not fail the backup")

	members := listArchiveMembers(t, filepath.Join(backupDir, backupName, "configs.tar.gz"))
	joined := strings.Join(members, "\n")
	assert.Contains(t, joined, "config.yaml")
	assert.NotContains(t, joined, "live.sock", "sockets must be skipped, not archived")
}

// TestBackup_UnreadableFileIsFatal locks in the #395 semantics change: an
// archive that cannot be fully produced fails loudly at creation (with the
// partial backup directory removed, #352) instead of being discovered as
// missing/short at verification — or worse, silently succeeding.
func TestBackup_UnreadableFileIsFatal(t *testing.T) {
	tmpDir := evalSymlinks(t, t.TempDir())
	appdata := filepath.Join(tmpDir, "appdata")
	require.NoError(t, os.MkdirAll(appdata, 0755))
	secret := filepath.Join(appdata, "unreadable.yaml")
	require.NoError(t, os.WriteFile(secret, []byte("key: value\n"), 0644))

	backupDir := filepath.Join(tmpDir, "backups")
	d := NewDeployOps(false, "")
	d.backupFS = &backupArchiveFS{open: func(name string) (*os.File, error) {
		if name == secret {
			return nil, &os.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
		}
		return os.Open(name)
	}}
	_, err := d.Backup(context.Background(), backupDir, []string{appdata})
	require.Error(t, err, "an unreadable file must fail the backup loudly")
	assert.ErrorIs(t, err, fs.ErrPermission)
	assert.Contains(t, err.Error(), "unreadable.yaml")

	entries, readErr := os.ReadDir(backupDir)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "failed backup must not leave a partial backup directory behind (#352)")
}

// TestZeroReader verifies the padding source yields zeros indefinitely — the
// churn-tolerance paths (vanished/replaced/shrunk files) depend on it to keep
// a partially-written entry structurally valid.
func TestZeroReader(t *testing.T) {
	buf := make([]byte, 64)
	for i := range buf {
		buf[i] = 0xFF
	}
	n, err := zeroReader{}.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, 64, n)
	for i, b := range buf {
		require.Zerof(t, b, "byte %d not zeroed", i)
	}
	got, err := io.ReadAll(io.LimitReader(zeroReader{}, 1024))
	require.NoError(t, err)
	assert.Len(t, got, 1024)
}

// TestBackup_UnreadableDirIsFatal covers the walk-error branch: a directory
// that cannot be descended into (as opposed to a file that cannot be opened)
// must fail the backup loudly with path context, and leave no partial backup.
func TestBackup_UnreadableDirIsFatal(t *testing.T) {
	tmpDir := evalSymlinks(t, t.TempDir())
	appdata := filepath.Join(tmpDir, "appdata")
	locked := filepath.Join(appdata, "locked")
	require.NoError(t, os.MkdirAll(locked, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(locked, "hidden.yaml"), []byte("key: value\n"), 0644))

	backupDir := filepath.Join(tmpDir, "backups")
	d := NewDeployOps(false, "")
	d.backupFS = &backupArchiveFS{walk: func(root string, walkFn filepath.WalkFunc) error {
		info, err := os.Lstat(root)
		if err != nil {
			return err
		}
		if err := walkFn(root, info, nil); err != nil {
			return err
		}
		return walkFn(locked, nil, &os.PathError{Op: "readdir", Path: locked, Err: fs.ErrPermission})
	}}
	_, err := d.Backup(context.Background(), backupDir, []string{appdata})
	require.Error(t, err, "an unreadable directory must fail the backup loudly")
	assert.ErrorIs(t, err, fs.ErrPermission)
	assert.Contains(t, err.Error(), "locked")

	entries, readErr := os.ReadDir(backupDir)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "failed backup must not leave a partial backup directory behind")
}

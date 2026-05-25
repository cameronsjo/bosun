package reconcile

import (
	"bytes"
	"context"
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

	// Staging target so discoverDeployTargets finds the "bosun" service...
	require.NoError(t, os.MkdirAll(filepath.Join(stagingDir, "appdata", "bosun"), 0755))
	// ...and a real source path so Backup actually invokes tar (non-empty work).
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

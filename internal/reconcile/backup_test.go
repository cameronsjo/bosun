package reconcile

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// listArchiveMembers returns the member names inside a tar.gz archive.
func listArchiveMembers(t *testing.T, tarFile string) []string {
	t.Helper()
	cmd := exec.Command("tar", "-tzf", tarFile)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	require.NoError(t, cmd.Run(), "listing archive members")
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

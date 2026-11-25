package reconcile

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	logpkg "github.com/cameronsjo/bosun/internal/log"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// namedTargetConfigs derives two named-target configs from one base whose
// BackupDir is root, the way the daemon does (#791).
func namedTargetConfigs(t *testing.T, root string) (cfgA, cfgB *Config) {
	t.Helper()
	base := DefaultConfig()
	base.BackupDir = root
	base.BackupsToKeep = 1
	return base.ConfigForTarget(Target{Name: "a"}), base.ConfigForTarget(Target{Name: "b"})
}

// TestCreateBackup_NamedTargetsUseOwnDirAndExcludeRoot drives the real
// createBackup for two named targets that share one appdata tree whose backup
// root lives inside it (the full-appdata fallback is the worst case).
//
// On the original code both targets inherit the base BackupDir, so each
// lastBackupPath is a direct child of the root and the parent-directory
// assertions fail. With per-target directories but no root exclusion, B's
// backups under <root>/b land in A's archive and the member assertion fails.
func TestCreateBackup_NamedTargetsUseOwnDirAndExcludeRoot(t *testing.T) {
	tmp := evalSymlinks(t, t.TempDir())
	appdata := filepath.Join(tmp, "appdata")
	root := filepath.Join(appdata, "backups")
	require.NoError(t, os.MkdirAll(appdata, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(appdata, "live.conf"), []byte("existing"), 0o644))
	// A backup an earlier release left directly under the shared root: it must
	// stay out of both archives and only draw a warning.
	_ = mkBackupDir(t, root, "backup-20200101-000009", true)

	base := &Config{
		StagingDir: tmp,
		// A staging subtree that does not exist makes target discovery fail, so
		// createBackup falls back to backing up the full appdata path.
		InfraSubDir:      "missing-staging",
		BackupDir:        root,
		LocalAppdataPath: appdata,
		BackupsToKeep:    3,
		StateFile:        filepath.Join(tmp, "state", DefaultStateFile),
		LockFile:         filepath.Join(tmp, "reconcile.lock"),
	}

	var logs bytes.Buffer
	logger := zerolog.New(&logs).Level(zerolog.WarnLevel)
	ctx := logpkg.WithContext(context.Background(), &logger)

	rB := NewReconciler(base.ConfigForTarget(Target{Name: "b"}))
	require.NoError(t, rB.createBackup(ctx, nil, true))
	rA := NewReconciler(base.ConfigForTarget(Target{Name: "a"}))
	require.NoError(t, rA.createBackup(ctx, nil, true))

	assert.Equal(t, filepath.Join(root, "a"), filepath.Dir(rA.lastBackupPath), "target a must back up under its own directory")
	assert.Equal(t, filepath.Join(root, "b"), filepath.Dir(rB.lastBackupPath), "target b must back up under its own directory")

	members := listArchiveMembers(t, filepath.Join(rA.lastBackupPath, "configs.tar.gz"))
	var foundLive bool
	for _, m := range members {
		assert.NotContains(t, m, "appdata/backups", "target a's archive must not contain the shared backup root or a sibling's backups (member: %s)", m)
		if strings.Contains(m, "live.conf") {
			foundLive = true
		}
	}
	assert.True(t, foundLive, "the real config must still be archived")

	assert.Equal(t, 2, strings.Count(logs.String(), "Legacy shared backups"),
		"each named target's run warns once about the backup left under the shared root")
}

// TestApplyBackupFailurePolicy_NamedTargetUsesOwnAnchor seeds a newer backup for
// a sibling and a still newer legacy backup directly under the root, then fails
// target a's backup.
//
// On the original code every target shares the root, so all three backups sit in
// one directory and the newest (the sibling's or legacy one) is returned as a's
// rollback anchor: the assertion fails because of that contamination, not
// because nothing was found.
func TestApplyBackupFailurePolicy_NamedTargetUsesOwnAnchor(t *testing.T) {
	root := filepath.Join(evalSymlinks(t, t.TempDir()), "backups")
	cfgA, cfgB := namedTargetConfigs(t, root)

	own := mkBackupDir(t, cfgA.BackupDir, "backup-20200101-000001", true)
	_ = mkBackupDir(t, cfgB.BackupDir, "backup-20200101-000002", true)
	_ = mkBackupDir(t, root, "backup-20200101-000003", true)

	r := NewReconciler(cfgA)
	require.NoError(t, r.applyBackupFailurePolicy(context.Background(), errors.New("injected backup failure")))

	assert.Equal(t, own, r.lastBackupPath, "rollback anchor must come from the target's own backups")
	assert.False(t, r.lastBackupIsFresh, "a prior-run anchor is stale")
}

// TestCleanupBackupsAfterSuccess_NamedTargetLeavesSiblingsAlone prunes target a
// to one backup while target b holds two valid backups and one truncated
// (in-progress) archive.
//
// On the original code all four share one directory: keeping one backup removes
// a's own backup and b's older one, and the truncated archive reads as corrupt
// and is deleted too, so the first assertion fails because a's retention
// deleted b's backups.
func TestCleanupBackupsAfterSuccess_NamedTargetLeavesSiblingsAlone(t *testing.T) {
	root := filepath.Join(evalSymlinks(t, t.TempDir()), "backups")
	cfgA, cfgB := namedTargetConfigs(t, root)

	ownDir := mkBackupDir(t, cfgA.BackupDir, "backup-20200101-000001", true)
	siblingDirs := []string{
		mkBackupDir(t, cfgB.BackupDir, "backup-20200101-000002", true),
		mkBackupDir(t, cfgB.BackupDir, "backup-20200101-000003", true),
		mkBackupDir(t, cfgB.BackupDir, "backup-20200101-000004", false),
	}

	r := NewReconciler(cfgA)
	r.lastBackupIsFresh = true
	r.cleanupBackupsAfterSuccess(context.Background())

	assert.DirExists(t, ownDir, "target a keeps its own newest backup")
	for _, dir := range siblingDirs {
		assert.DirExists(t, dir, "target a's retention must not touch target b's backups")
	}
}

// TestBackupExcluding_ExcludesSharedRoot backs up a tree that contains a
// sibling's backup under the shared root, writing into a's own directory.
// Without root exclusion only <root>/a is skipped and the sibling's archive is
// swallowed; the original code has no way to name the root (compile failure),
// and TestCreateBackup_NamedTargetsUseOwnDirAndExcludeRoot pins the behavior
// through the reconciler.
func TestBackupExcluding_ExcludesSharedRoot(t *testing.T) {
	tmp := evalSymlinks(t, t.TempDir())
	appdata := filepath.Join(tmp, "appdata")
	root := filepath.Join(appdata, "backups")
	service := filepath.Join(appdata, "service")
	require.NoError(t, os.MkdirAll(service, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(service, "conf.yml"), []byte("real config"), 0o644))
	_ = mkBackupDir(t, filepath.Join(root, "b"), "backup-old", true)

	d := NewDeployOps(false, "")
	name, err := d.backupExcluding(context.Background(), root, filepath.Join(root, "a"), []string{appdata})
	require.NoError(t, err)

	members := listArchiveMembers(t, filepath.Join(root, "a", name, "configs.tar.gz"))
	var foundService bool
	for _, m := range members {
		assert.NotContains(t, m, "/backups/", "archive must not contain the shared backup root (member: %s)", m)
		if strings.Contains(m, "service/conf.yml") {
			foundService = true
		}
	}
	assert.True(t, foundService, "archive should still contain the real service config")
}

// TestBackupRemoteExcluding_ExcludeArgument asserts on the tar command the ssh
// shim receives. The original BackupRemote emits only --exclude <backupDir>, so
// a named target's remote archive would not exclude the shared root.
func TestBackupRemoteExcluding_ExcludeArgument(t *testing.T) {
	tests := []struct {
		name        string
		relative    bool
		wantExclude bool
	}{
		{name: "absolute shared root is excluded", wantExclude: true},
		{name: "relative root skips self-exclusion", relative: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kh := writeTempKnownHosts(t)
			t.Setenv("BOSUN_SSH_INSECURE_HOST_KEY", "")
			t.Setenv("BOSUN_SSH_KNOWN_HOSTS", kh)
			// Emit a real, non-empty gzip'd tar so verification passes.
			body := "tmp=$(mktemp -d)\n: > \"$tmp/f\"\ntar -czf - -C \"$tmp\" .\n"
			capture := installSSHCaptureShim(t, body)

			root := evalSymlinks(t, t.TempDir())
			excludeDir := root
			if tt.relative {
				excludeDir = "relative/backups"
			}
			d := &DeployOps{}
			name, err := d.backupRemoteExcluding(context.Background(), "user@testhost", excludeDir, filepath.Join(root, "a"), []string{"/srv/appdata"})
			require.NoError(t, err)
			assert.NotEmpty(t, name)

			b, err := os.ReadFile(capture)
			require.NoError(t, err)
			got := string(b)
			if tt.wantExclude {
				assert.Contains(t, got, "--exclude "+root+" ", "the shared root must be the excluded path")
				assert.NotContains(t, got, "--exclude "+filepath.Join(root, "a"), "excluding only the target directory leaves siblings in the archive")
				return
			}
			assert.NotContains(t, got, "--exclude")
		})
	}
}

// TestBackupExcluding_ResolveFailureIsFatalAndLeavesNoDir injects the failure
// filepath.Abs returns when the working directory is unreadable. The resolve
// must run before MkdirAll so no partial directory is left behind. This pins a
// failure branch no earlier test reached; it cannot compile on the original
// code, which has no seam.
func TestBackupExcluding_ResolveFailureIsFatalAndLeavesNoDir(t *testing.T) {
	original := backupAbs
	backupAbs = func(string) (string, error) { return "", errors.New("injected getwd failure") }
	t.Cleanup(func() { backupAbs = original })

	tmp := evalSymlinks(t, t.TempDir())
	backupDir := filepath.Join(tmp, "backups")

	d := NewDeployOps(false, "")
	name, err := d.backupExcluding(context.Background(), backupDir, backupDir, []string{tmp})

	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to resolve backup destination")
	assert.Empty(t, name)
	assert.NoDirExists(t, backupDir, "resolution must fail before any directory is created")
}

func TestLegacySharedBackupCount(t *testing.T) {
	writeArchive := func(t *testing.T, dir string) {
		t.Helper()
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "configs.tar.gz"), []byte("x"), 0o644))
	}

	tests := []struct {
		name  string
		setup func(t *testing.T, root string)
		want  int
	}{
		{name: "missing root", setup: func(*testing.T, string) {}, want: 0},
		{name: "empty root", setup: func(t *testing.T, root string) {
			require.NoError(t, os.MkdirAll(root, 0o755))
		}, want: 0},
		{name: "two legacy backups", setup: func(t *testing.T, root string) {
			writeArchive(t, filepath.Join(root, "backup-20200101-000001"))
			writeArchive(t, filepath.Join(root, "backup-20200101-000002"))
		}, want: 2},
		{name: "target directory named like a backup is not legacy", setup: func(t *testing.T, root string) {
			writeArchive(t, filepath.Join(root, "backup-pi", "backup-20200101-000001"))
		}, want: 0},
		{name: "legacy directory without an archive", setup: func(t *testing.T, root string) {
			require.NoError(t, os.MkdirAll(filepath.Join(root, "backup-20200101-000001"), 0o755))
		}, want: 0},
		{name: "plain file named like a backup", setup: func(t *testing.T, root string) {
			require.NoError(t, os.MkdirAll(root, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(root, "backup-x"), []byte("x"), 0o644))
		}, want: 0},
		{name: "unrelated directory holding an archive", setup: func(t *testing.T, root string) {
			writeArchive(t, filepath.Join(root, "a"))
		}, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "backups")
			tt.setup(t, root)
			assert.Equal(t, tt.want, legacySharedBackupCount(root))
		})
	}
}

func TestWarnLegacySharedBackups(t *testing.T) {
	var logs bytes.Buffer
	logger := zerolog.New(&logs).Level(zerolog.WarnLevel)
	ctx := logpkg.WithContext(context.Background(), &logger)

	root := filepath.Join(t.TempDir(), "backups")
	warnLegacySharedBackups(ctx, root)
	assert.Empty(t, logs.String(), "no legacy backups, no warning")

	require.NoError(t, os.MkdirAll(filepath.Join(root, "backup-20200101-000001"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "backup-20200101-000001", "configs.tar.gz"), []byte("x"), 0o644))
	warnLegacySharedBackups(ctx, root)
	assert.Contains(t, logs.String(), "Legacy shared backups")
	assert.Contains(t, logs.String(), `"legacy_backups":1`)
}

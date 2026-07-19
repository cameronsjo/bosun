package reconcile

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/docker"
)

// writeLive writes content to <dir>/<rel> (creating parents) and returns the path.
func writeLive(t *testing.T, dir, rel, content string) string {
	t.Helper()
	p := filepath.Join(dir, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	return p
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	require.NoError(t, err)
	return string(b)
}

func TestRollbackFromBackupSet_RestoresBytes(t *testing.T) {
	base := t.TempDir()
	live := filepath.Join(base, "appdata")
	a := writeLive(t, live, "authelia/configuration.yml", "OLD-authelia")
	b := writeLive(t, live, "compose/core.yml", "OLD-compose")

	// Archive the OLD content, then simulate a bad deploy overwriting both files.
	backupDir := filepath.Join(base, "backup")
	writeTestBackupArchive(t, backupDir, a, b)
	require.NoError(t, os.WriteFile(a, []byte("NEW-bad-authelia"), 0o644))
	require.NoError(t, os.WriteFile(b, []byte("NEW-bad-compose"), 0o644))

	d := &DeployOps{ProjectName: "test"}
	// No ComposeFiles: exercise the file-restore path without invoking docker.
	err := d.RollbackFromBackupSet(context.Background(), RollbackSet{Files: []string{a, b}}, backupDir)
	require.NoError(t, err)

	assert.Equal(t, "OLD-authelia", read(t, a), "config restored byte-for-byte from backup")
	assert.Equal(t, "OLD-compose", read(t, b))
}

func TestRollbackFromBackupSet_MissingParentRecreated(t *testing.T) {
	base := t.TempDir()
	live := filepath.Join(base, "appdata")
	f := writeLive(t, live, "svc/config.yml", "OLD")
	backupDir := filepath.Join(base, "backup")
	writeTestBackupArchive(t, backupDir, f)

	// The failed deploy removed the whole subtree; restore must recreate the parent.
	require.NoError(t, os.RemoveAll(filepath.Join(live, "svc")))

	d := &DeployOps{ProjectName: "test"}
	err := d.RollbackFromBackupSet(context.Background(), RollbackSet{Files: []string{f}}, backupDir)
	require.NoError(t, err)
	assert.Equal(t, "OLD", read(t, f), "restore recreates the missing parent directory")
}

func TestRollbackFromBackupSet_JoinedErrorContinues(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("chmod-based permission failure is bypassed by root")
	}
	base := t.TempDir()
	live := filepath.Join(base, "appdata")
	good := writeLive(t, live, "good/config.yml", "OLD-good")
	// A second managed file under a directory we'll lock to 0000 so its restore
	// (CopyFile's temp-write into the parent) fails with permission denied while
	// `good` still restores.
	bad := writeLive(t, live, "locked/config.yml", "OLD-locked")

	backupDir := filepath.Join(base, "backup")
	writeTestBackupArchive(t, backupDir, good, bad)
	require.NoError(t, os.WriteFile(good, []byte("NEW-bad"), 0o644))

	lockedDir := filepath.Join(live, "locked")
	require.NoError(t, os.Chmod(lockedDir, 0o000))
	defer func() { _ = os.Chmod(lockedDir, 0o755) }() // restore for t.TempDir cleanup

	d := &DeployOps{ProjectName: "test"}
	err := d.RollbackFromBackupSet(context.Background(), RollbackSet{Files: []string{good, bad}}, backupDir)
	require.Error(t, err, "a per-file failure surfaces")
	assert.Contains(t, err.Error(), bad, "the joined error names the failed file")
	assert.Equal(t, "OLD-good", read(t, good), "restore CONTINUES past the failed file")
}

func TestRollbackFromBackupSet_DeleteMissing(t *testing.T) {
	setup := func(t *testing.T) (string, string, string) {
		base := t.TempDir()
		live := filepath.Join(base, "appdata")
		kept := writeLive(t, live, "svc/config.yml", "OLD")
		// A file the failed deploy ADDED — present live, absent from the backup.
		added := writeLive(t, live, "svc/added-by-deploy.yml", "junk")
		backupDir := filepath.Join(base, "backup")
		writeTestBackupArchive(t, backupDir, kept) // only `kept` is in the backup
		return backupDir, added, kept
	}

	t.Run("DeleteMissing removes backup-absent files", func(t *testing.T) {
		backupDir, added, kept := setup(t)
		d := &DeployOps{ProjectName: "test"}
		err := d.RollbackFromBackupSet(context.Background(),
			RollbackSet{Files: []string{kept, added}, DeleteMissing: true}, backupDir)
		require.NoError(t, err)
		assert.NoFileExists(t, added, "a file the deploy added is removed on a fresh anchor")
		assert.FileExists(t, kept)
	})

	t.Run("stale anchor retention: DeleteMissing false keeps them", func(t *testing.T) {
		backupDir, added, kept := setup(t)
		d := &DeployOps{ProjectName: "test"}
		err := d.RollbackFromBackupSet(context.Background(),
			RollbackSet{Files: []string{kept, added}, DeleteMissing: false}, backupDir)
		require.NoError(t, err)
		assert.FileExists(t, added, "against a stale anchor, backup-absent files are RETAINED, not deleted")
	})
}

func TestRollbackFromBackupSet_NoBackup(t *testing.T) {
	d := &DeployOps{ProjectName: "test"}

	t.Run("empty backup path", func(t *testing.T) {
		err := d.RollbackFromBackupSet(context.Background(), RollbackSet{Files: []string{"/x"}}, "")
		require.ErrorIs(t, err, errRollbackNotAttempted)
	})

	t.Run("missing archive", func(t *testing.T) {
		err := d.RollbackFromBackupSet(context.Background(), RollbackSet{Files: []string{"/x"}}, t.TempDir())
		require.ErrorIs(t, err, errRollbackNotAttempted)
	})
}

func TestBackupIsFresh(t *testing.T) {
	base := t.TempDir()
	backupDir := filepath.Join(base, "backup")
	require.NoError(t, os.MkdirAll(backupDir, 0o755))
	archive := filepath.Join(backupDir, "configs.tar.gz")
	require.NoError(t, os.WriteFile(archive, []byte("x"), 0o644))

	now := time.Now()

	t.Run("archive newer than run start is fresh", func(t *testing.T) {
		// mtime is ~now; a run that started a minute ago sees it as fresh.
		assert.True(t, backupIsFresh(backupDir, now.Add(-time.Minute)))
	})

	t.Run("archive older than run start is stale", func(t *testing.T) {
		old := now.Add(-2 * time.Hour)
		require.NoError(t, os.Chtimes(archive, old, old))
		assert.False(t, backupIsFresh(backupDir, now.Add(-time.Minute)),
			"a fallback anchor from before this run is stale")
	})

	t.Run("missing archive is not fresh", func(t *testing.T) {
		assert.False(t, backupIsFresh(filepath.Join(base, "nope"), now))
	})
}

func TestBuildRollbackSet(t *testing.T) {
	tmp := t.TempDir()
	appdata := filepath.Join(tmp, "appdata")
	backupDir := filepath.Join(tmp, "backup")
	require.NoError(t, os.MkdirAll(backupDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(backupDir, "configs.tar.gz"), []byte("x"), 0o644))

	r := NewReconciler(&Config{LocalAppdataPath: appdata})
	r.lastBackupPath = backupDir
	r.lastComposeFiles = []string{filepath.Join(appdata, "compose", "core.yml")}
	r.runStartTime = time.Now().Add(-time.Minute) // archive (just written) is fresh

	dr := &DeployResult{ManagedFiles: []string{"authelia/configuration.yml", "compose/core.yml"}}
	set := r.buildRollbackSet(dr)

	assert.Equal(t, []string{
		filepath.Join(appdata, "authelia", "configuration.yml"),
		filepath.Join(appdata, "compose", "core.yml"),
	}, set.Files, "ManagedFiles map to live absolute paths under appdata")
	assert.Equal(t, r.lastComposeFiles, set.ComposeFiles)
	assert.True(t, set.DeleteMissing, "delete-missing enabled against a fresh anchor")

	t.Run("stale anchor disables delete-missing", func(t *testing.T) {
		old := time.Now().Add(-time.Hour)
		require.NoError(t, os.Chtimes(filepath.Join(backupDir, "configs.tar.gz"), old, old))
		r.runStartTime = time.Now()
		assert.False(t, r.buildRollbackSet(dr).DeleteMissing)
	})

	t.Run("nil deployResult yields no files", func(t *testing.T) {
		assert.Empty(t, r.buildRollbackSet(nil).Files)
	})
}

// TestRun_HealthGateRollback_RevertsFullTree is the integration case: a deploy
// overwrites both a compose file and a NON-compose appdata config, the critical
// health gate fails, and the full-tree rollback (#445) reverts BOTH to their
// backed-up content — proving the restore is no longer compose-only.
func TestRun_HealthGateRollback_RevertsFullTree(t *testing.T) {
	setupDockerShim(t, 0) // RollbackFromBackupSet + SignalContainer shell out to `docker`

	tmp := t.TempDir()
	repoDir := filepath.Join(tmp, "repo")
	appdata := filepath.Join(tmp, "appdata")
	stateFile := filepath.Join(tmp, "state.json")

	// Repo (staging source) carries the NEW commit's content.
	require.NoError(t, os.MkdirAll(filepath.Join(repoDir, "downstream"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(repoDir, "compose"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "downstream", "config.yml"), []byte("NEW-config"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "compose", "stub.yml"),
		[]byte("services:\n  stub:\n    image: alpine:new\n"), 0o644))

	// Live appdata holds the PREVIOUS content — this is what the pre-deploy backup
	// captures and what a rollback must restore.
	require.NoError(t, os.MkdirAll(filepath.Join(appdata, "downstream"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(appdata, "compose"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(appdata, "downstream", "config.yml"), []byte("OLD-config"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(appdata, "compose", "stub.yml"),
		[]byte("services:\n  stub:\n    image: alpine:old\n"), 0o644))

	require.NoError(t, SaveState(stateFile, &DeployState{SchemaVersion: 2, LastDeployedCommit: "prevcommit"}))

	mockAPI := newReconcileMockDockerAPI()
	mockAPI.containerInspectFunc = func(_ context.Context, name string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
		return makeInspectResponse(name, "running", &container.Health{Status: "unhealthy"}), nil
	}
	dockerClient := docker.NewClientWithAPI(mockAPI)

	deploy := &DeployOps{
		DryRun:          false,
		ProjectName:     "test",
		ContentHashSync: true,
		composeUpFn:     func(_ context.Context, _ []string) error { return nil },
	}

	cfg := &Config{
		DryRun:             false,
		LockFile:           filepath.Join(tmp, "reconcile.lock"),
		StateFile:          stateFile,
		RepoDir:            repoDir,
		StagingDir:         filepath.Join(tmp, "staging"),
		BackupDir:          filepath.Join(tmp, "backups"),
		BackupsToKeep:      3, // keep the pre-deploy backup so the rollback can read it
		LocalAppdataPath:   appdata,
		InfraSubDir:        ".",
		CriticalContainers: NewConfigField([]string{"chronic-critical"}),
		HealthGateTimeout:  50 * time.Millisecond,
	}
	r := NewReconciler(cfg,
		WithGitOperations(&mockGitOps{syncChanged: true, syncBefore: "prevcommit", syncAfter: "newcommit"}),
		WithDeployOps(deploy), WithDockerClient(dockerClient))

	err := r.Run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "health gate failed")

	// Full-tree revert: BOTH the appdata config and the compose file are back to
	// their backed-up (OLD) content — the config revert is the #445 widening.
	assert.Equal(t, "OLD-config", read(t, filepath.Join(appdata, "downstream", "config.yml")),
		"non-compose appdata config reverted to its backed-up content (#445)")
	assert.Equal(t, "services:\n  stub:\n    image: alpine:old\n",
		read(t, filepath.Join(appdata, "compose", "stub.yml")), "compose file reverted too")

	saved := LoadState(stateFile)
	assert.True(t, saved.NeedsRedeploy)
	assert.Equal(t, "prevcommit", saved.LastDeployedCommit, "commit must not advance past a failed gate")
}

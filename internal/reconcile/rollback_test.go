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
	err := d.RollbackFromBackupSet(context.Background(), RollbackSet{Files: []string{a, b}, Root: live}, backupDir)
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
	err := d.RollbackFromBackupSet(context.Background(), RollbackSet{Files: []string{f}, Root: live}, backupDir)
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
	err := d.RollbackFromBackupSet(context.Background(), RollbackSet{Files: []string{good, bad}, Root: live}, backupDir)
	require.Error(t, err, "a per-file failure surfaces")
	assert.Contains(t, err.Error(), bad, "the joined error names the failed file")
	assert.Equal(t, "OLD-good", read(t, good), "restore CONTINUES past the failed file")
}

func TestRollbackFromBackupSet_DeleteMissing(t *testing.T) {
	setup := func(t *testing.T) (backupDir, live, added, kept string) {
		base := t.TempDir()
		live = filepath.Join(base, "appdata")
		kept = writeLive(t, live, "svc/config.yml", "OLD")
		// A file the failed deploy ADDED — present live, absent from the backup.
		added = writeLive(t, live, "svc/added-by-deploy.yml", "junk")
		backupDir = filepath.Join(base, "backup")
		writeTestBackupArchive(t, backupDir, kept) // only `kept` is in the backup
		return backupDir, live, added, kept
	}

	t.Run("DeleteMissing removes backup-absent files", func(t *testing.T) {
		backupDir, live, added, kept := setup(t)
		d := &DeployOps{ProjectName: "test"}
		err := d.RollbackFromBackupSet(context.Background(),
			RollbackSet{Files: []string{kept, added}, Root: live, DeleteMissing: true}, backupDir)
		require.NoError(t, err)
		assert.NoFileExists(t, added, "a file the deploy added is removed on a fresh anchor")
		assert.FileExists(t, kept)
	})

	t.Run("stale anchor retention: DeleteMissing false keeps them", func(t *testing.T) {
		backupDir, live, added, kept := setup(t)
		d := &DeployOps{ProjectName: "test"}
		err := d.RollbackFromBackupSet(context.Background(),
			RollbackSet{Files: []string{kept, added}, Root: live, DeleteMissing: false}, backupDir)
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

func TestRollbackFromBackupSet_DryRun(t *testing.T) {
	base := t.TempDir()
	live := filepath.Join(base, "appdata")
	f := writeLive(t, live, "svc/config.yml", "OLD")
	backupDir := filepath.Join(base, "backup")
	writeTestBackupArchive(t, backupDir, f)
	require.NoError(t, os.WriteFile(f, []byte("NEW"), 0o644))

	d := &DeployOps{ProjectName: "test", DryRun: true}
	err := d.RollbackFromBackupSet(context.Background(), RollbackSet{Files: []string{f}, Root: live}, backupDir)
	require.NoError(t, err)
	assert.Equal(t, "NEW", read(t, f), "dry run restores nothing")
}

// TestRollbackFromBackupSet_DeleteError injects an os.Remove failure that is NOT
// IsNotExist: a backup-absent target that is a NON-EMPTY directory (ENOTEMPTY).
// The failure joins into the error rather than counting as a deletion.
func TestRollbackFromBackupSet_DeleteError(t *testing.T) {
	base := t.TempDir()
	live := filepath.Join(base, "appdata")
	kept := writeLive(t, live, "svc/config.yml", "OLD")
	// A non-empty directory absent from the backup — os.Remove can't remove it.
	addedDir := filepath.Join(live, "added-dir")
	writeLive(t, live, "added-dir/inner.yml", "junk")

	backupDir := filepath.Join(base, "backup")
	writeTestBackupArchive(t, backupDir, kept)

	d := &DeployOps{ProjectName: "test"}
	err := d.RollbackFromBackupSet(context.Background(),
		RollbackSet{Files: []string{kept, addedDir}, Root: live, DeleteMissing: true}, backupDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "delete added file", "a non-IsNotExist Remove failure joins the error")
	assert.DirExists(t, addedDir, "the undeletable directory is left in place")
}

// TestRollbackFromBackupSet_MkdirParentError injects an os.MkdirAll failure: the
// restored file's parent path component is now a regular FILE (ENOTDIR), so the
// parent cannot be recreated. The failure joins rather than restoring.
func TestRollbackFromBackupSet_MkdirParentError(t *testing.T) {
	base := t.TempDir()
	live := filepath.Join(base, "appdata")
	f := writeLive(t, live, "sub/config.yml", "OLD")
	backupDir := filepath.Join(base, "backup")
	writeTestBackupArchive(t, backupDir, f) // archive holds sub/config.yml

	// Failed deploy left `sub` as a regular file, so MkdirAll(sub) hits ENOTDIR.
	require.NoError(t, os.RemoveAll(filepath.Join(live, "sub")))
	require.NoError(t, os.WriteFile(filepath.Join(live, "sub"), []byte("now-a-file"), 0o644))

	d := &DeployOps{ProjectName: "test"}
	err := d.RollbackFromBackupSet(context.Background(), RollbackSet{Files: []string{f}, Root: live}, backupDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create parent", "MkdirAll ENOTDIR joins the error")
}

// TestWithinAppdata pins the containment guard directly: a target inside the
// root passes; the filepath.Join prefix-drop escape (an absolute ManagedFiles
// entry collapsing to a path outside appdata) and any climbing path are rejected.
func TestWithinAppdata(t *testing.T) {
	root := filepath.Join(t.TempDir(), "appdata")

	assert.True(t, withinAppdata(root, filepath.Join(root, "svc", "config.yml")),
		"a file under the appdata root is allowed")
	assert.False(t, withinAppdata(root, "/etc/passwd"),
		"the prefix-drop escape (filepath.Join collapsing an absolute entry) is rejected")
	assert.False(t, withinAppdata(root, filepath.Join(root, "..", "outside.yml")),
		"a climbing path that escapes the root is rejected")
	assert.False(t, withinAppdata("relative/root", "/absolute/target"),
		"an unrelatable pair (relative root vs absolute target) fails closed via the Rel error")
}

// TestRollbackFromBackupSet_ContainmentGuard proves the guard fires on the DELETE
// path: an escaping target absent from the backup is NOT os.Remove'd even with
// DeleteMissing set — the guard rejects it first and surfaces a joined error,
// while an in-root file still restores.
func TestRollbackFromBackupSet_ContainmentGuard(t *testing.T) {
	base := t.TempDir()
	live := filepath.Join(base, "appdata")
	kept := writeLive(t, live, "svc/config.yml", "OLD")
	// An out-of-root file the guard must refuse to delete (simulates a poisoned
	// ManagedFiles entry that escaped appdata via the prefix-drop footgun).
	outside := writeLive(t, base, "outside/secret.yml", "DO-NOT-DELETE")

	backupDir := filepath.Join(base, "backup")
	writeTestBackupArchive(t, backupDir, kept) // only `kept` in the backup; `outside` is "missing"

	d := &DeployOps{ProjectName: "test"}
	err := d.RollbackFromBackupSet(context.Background(),
		RollbackSet{Files: []string{kept, outside}, Root: live, DeleteMissing: true}, backupDir)
	require.Error(t, err, "the containment refusal surfaces as an error")
	assert.Contains(t, err.Error(), "outside appdata root")
	assert.FileExists(t, outside, "the escaping file is NOT deleted despite DeleteMissing")
	assert.Equal(t, "OLD", read(t, kept), "an in-root file still restores past the refusal")
}

func TestBuildRollbackSet(t *testing.T) {
	tmp := t.TempDir()
	appdata := filepath.Join(tmp, "appdata")
	backupDir := filepath.Join(tmp, "backup")

	r := NewReconciler(&Config{LocalAppdataPath: appdata})
	r.lastBackupPath = backupDir
	r.lastBackupIsFresh = true // this run's own pre-deploy backup
	r.lastComposeFiles = []string{filepath.Join(appdata, "compose", "core.yml")}

	dr := &DeployResult{ManagedFiles: []string{"authelia/configuration.yml", "compose/core.yml"}}
	set := r.buildRollbackSet(dr)

	assert.Equal(t, []string{
		filepath.Join(appdata, "authelia", "configuration.yml"),
		filepath.Join(appdata, "compose", "core.yml"),
	}, set.Files, "ManagedFiles map to live absolute paths under appdata")
	assert.Equal(t, r.lastComposeFiles, set.ComposeFiles)
	assert.Equal(t, appdata, set.Root, "Root is the appdata containment boundary")
	assert.True(t, set.DeleteMissing, "delete-missing enabled against a fresh anchor")

	t.Run("stale fallback anchor disables delete-missing", func(t *testing.T) {
		// The bool — not an archive mtime — drives the gate: a substituted stale
		// anchor (lastBackupIsFresh=false) must suppress deletion, un-spoofable by
		// touching the archive forward.
		r.lastBackupIsFresh = false
		assert.False(t, r.buildRollbackSet(dr).DeleteMissing,
			"a stale fallback anchor suppresses delete-missing")
	})

	t.Run("no backup path disables delete-missing", func(t *testing.T) {
		r.lastBackupIsFresh = true
		r.lastBackupPath = ""
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
	assertPrivateStagingTree(t, cfg.StagingDir)

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

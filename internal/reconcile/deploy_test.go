package reconcile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDeployOps(t *testing.T) {
	t.Run("with dry run", func(t *testing.T) {
		deploy := NewDeployOps(true, "")
		assert.True(t, deploy.DryRun)
		assert.True(t, deploy.RemoveOrphans, "RemoveOrphans should default to true")
	})

	t.Run("without dry run", func(t *testing.T) {
		deploy := NewDeployOps(false, "")
		assert.False(t, deploy.DryRun)
		assert.True(t, deploy.RemoveOrphans, "RemoveOrphans should default to true")
	})
}

func TestDeployOps_ComposeUpTimeout(t *testing.T) {
	t.Run("returns default when not configured", func(t *testing.T) {
		d := &DeployOps{}
		assert.Equal(t, DefaultComposeUpTimeout, d.composeUpTimeout())
	})

	t.Run("returns configured value", func(t *testing.T) {
		d := &DeployOps{ComposeUpTimeout: 30 * time.Minute}
		assert.Equal(t, 30*time.Minute, d.composeUpTimeout())
	})

	t.Run("NewDeployOps leaves timeout at zero (uses default)", func(t *testing.T) {
		d := NewDeployOps(false, "test")
		assert.Zero(t, d.ComposeUpTimeout)
		assert.Equal(t, DefaultComposeUpTimeout, d.composeUpTimeout())
	})
}

func TestDeployOps_ComposeUpArgs_RemoveOrphans(t *testing.T) {
	tests := []struct {
		name           string
		removeOrphans  bool
		projectName    string
		wantContains   string
		wantMissing    string
		useConstructor bool
	}{
		{
			name:          "remove orphans enabled includes flag",
			removeOrphans: true,
			projectName:   "bosun",
			wantContains:  "--remove-orphans",
		},
		{
			name:          "remove orphans disabled omits flag",
			removeOrphans: false,
			projectName:   "bosun",
			wantMissing:   "--remove-orphans",
		},
		{
			name:           "default NewDeployOps includes remove-orphans",
			projectName:    "bosun",
			wantContains:   "--remove-orphans",
			useConstructor: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d *DeployOps
			if tt.useConstructor {
				d = NewDeployOps(false, tt.projectName)
			} else {
				d = &DeployOps{
					ProjectName:   tt.projectName,
					RemoveOrphans: tt.removeOrphans,
				}
			}
			args := d.composeUpArgs([]string{"docker-compose.yml"})

			if tt.wantContains != "" {
				assert.Contains(t, args, tt.wantContains)
			}
			if tt.wantMissing != "" {
				assert.NotContains(t, args, tt.wantMissing)
			}
		})
	}
}

func TestDeployOps_ComposeUpRemoteCmd_RemoveOrphans(t *testing.T) {
	tests := []struct {
		name          string
		removeOrphans bool
		projectName   string
		wantContains  string
		wantMissing   string
	}{
		{
			name:          "remote compose up includes remove-orphans",
			removeOrphans: true,
			projectName:   "bosun",
			wantContains:  "--remove-orphans",
		},
		{
			name:          "remote compose up omits remove-orphans",
			removeOrphans: false,
			projectName:   "bosun",
			wantMissing:   "--remove-orphans",
		},
		{
			name:          "default NewDeployOps remote includes remove-orphans",
			removeOrphans: true,
			projectName:   "bosun",
			wantContains:  "--remove-orphans",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d *DeployOps
			if tt.name == "default NewDeployOps remote includes remove-orphans" {
				d = NewDeployOps(false, tt.projectName)
			} else {
				d = &DeployOps{
					ProjectName:   tt.projectName,
					RemoveOrphans: tt.removeOrphans,
				}
			}
			sshCmd := d.remoteComposeUpCmd("/opt/compose")

			if tt.wantContains != "" {
				assert.Contains(t, sshCmd, tt.wantContains)
			}
			if tt.wantMissing != "" {
				assert.NotContains(t, sshCmd, tt.wantMissing)
			}
		})
	}
}

func TestDeployOps_ZeroValueDeployOps_RemoveOrphansDisabled(t *testing.T) {
	// Zero-value DeployOps{} has RemoveOrphans=false; NewDeployOps defaults to true.
	zero := &DeployOps{}
	assert.False(t, zero.RemoveOrphans, "zero-value DeployOps should have RemoveOrphans=false")
	assert.NotContains(t, zero.composeUpArgs([]string{"f.yml"}), "--remove-orphans")

	constructed := NewDeployOps(false, "test")
	assert.True(t, constructed.RemoveOrphans, "NewDeployOps should default RemoveOrphans to true")
	assert.Contains(t, constructed.composeUpArgs([]string{"f.yml"}), "--remove-orphans")
}

func TestDeployOps_Backup(t *testing.T) {
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar not installed")
	}

	t.Run("backup existing paths", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		// Create source files
		srcDir := filepath.Join(tmpDir, "source")
		require.NoError(t, os.MkdirAll(srcDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "file1.txt"), []byte("content1"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "file2.txt"), []byte("content2"), 0644))

		backupDir := filepath.Join(tmpDir, "backups")

		deploy := NewDeployOps(false, "")
		backupName, err := deploy.Backup(ctx, backupDir, []string{srcDir})

		require.NoError(t, err)
		assert.NotEmpty(t, backupName)
		assert.Contains(t, backupName, "backup-")

		// Verify backup directory was created
		assert.DirExists(t, filepath.Join(backupDir, backupName))

		// Verify tar file was created
		tarFile := filepath.Join(backupDir, backupName, "configs.tar.gz")
		assert.FileExists(t, tarFile)
	})

	t.Run("backup non-existent paths returns no anchor", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		backupDir := filepath.Join(tmpDir, "backups")

		deploy := NewDeployOps(false, "")
		backupName, err := deploy.Backup(ctx, backupDir, []string{"/non/existent/path"})

		// No live path existed, so there is no valid rollback anchor. Backup must
		// NOT report a content-free success (#360): empty name, no error, and no
		// empty backup dir left behind to masquerade as an anchor.
		require.NoError(t, err)
		assert.Empty(t, backupName, "no archive was written, so no anchor name is returned")
		entries, err := os.ReadDir(backupDir)
		if err == nil {
			for _, e := range entries {
				assert.NotContains(t, e.Name(), "backup-", "the empty backup dir is removed, not left as litter")
			}
		}
	})

	t.Run("backup empty paths list returns no anchor", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		backupDir := filepath.Join(tmpDir, "backups")

		deploy := NewDeployOps(false, "")
		backupName, err := deploy.Backup(ctx, backupDir, []string{})

		require.NoError(t, err)
		assert.Empty(t, backupName, "an empty paths list creates no anchor (#360)")
		entries, err := os.ReadDir(backupDir)
		if err == nil {
			for _, e := range entries {
				assert.NotContains(t, e.Name(), "backup-", "no empty backup dir is left behind")
			}
		}
	})
}

// makeBackupDir creates a backup-<name> dir under parent. With withArchive it
// writes a REAL configs.tar.gz (via writeTestBackupArchive, which skips the test
// when tar is unavailable) so CleanupBackups' VerifyBackup accepts it as valid;
// without, the dir is corrupt/partial (no archive) and must not count toward
// retention.
func makeBackupDir(t *testing.T, parent, name string, withArchive bool) string {
	t.Helper()
	dir := filepath.Join(parent, name)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	if withArchive {
		src := filepath.Join(t.TempDir(), "seed.yml")
		require.NoError(t, os.WriteFile(src, []byte("seed"), 0o644))
		writeTestBackupArchive(t, dir, src)
	}
	return dir
}

func TestDeployOps_CleanupBackups(t *testing.T) {
	t.Run("cleanup old backups", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create valid backup directories (each with a real archive).
		for i := 1; i <= 10; i++ {
			timestamp := time.Now().Add(time.Duration(-i) * time.Hour).Format("20060102-150405")
			makeBackupDir(t, tmpDir, "backup-"+timestamp, true)
		}

		deploy := NewDeployOps(false, "")
		err := deploy.CleanupBackups(context.Background(), tmpDir, 5)

		require.NoError(t, err)

		// Count remaining backups
		entries, err := os.ReadDir(tmpDir)
		require.NoError(t, err)

		count := 0
		for _, e := range entries {
			if e.IsDir() && len(e.Name()) > 7 && e.Name()[:7] == "backup-" {
				count++
			}
		}
		assert.Equal(t, 5, count)
	})

	t.Run("cleanup with fewer backups than keep", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create only 3 valid backup directories
		for i := 1; i <= 3; i++ {
			timestamp := time.Now().Add(time.Duration(-i) * time.Hour).Format("20060102-150405")
			makeBackupDir(t, tmpDir, "backup-"+timestamp, true)
		}

		deploy := NewDeployOps(false, "")
		err := deploy.CleanupBackups(context.Background(), tmpDir, 5)

		require.NoError(t, err)

		// All backups should remain
		entries, err := os.ReadDir(tmpDir)
		require.NoError(t, err)
		assert.Len(t, entries, 3)
	})

	t.Run("cleanup non-existent directory", func(t *testing.T) {
		deploy := NewDeployOps(false, "")
		err := deploy.CleanupBackups(context.Background(), "/non/existent/dir", 5)

		// Should not error
		require.NoError(t, err)
	})

	t.Run("cleanup ignores non-backup directories", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create valid backup directories
		for i := 1; i <= 3; i++ {
			timestamp := time.Now().Add(time.Duration(-i) * time.Hour).Format("20060102-150405")
			makeBackupDir(t, tmpDir, "backup-"+timestamp, true)
		}

		// Create non-backup directory
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "other-dir"), 0755))

		deploy := NewDeployOps(false, "")
		err := deploy.CleanupBackups(context.Background(), tmpDir, 2)

		require.NoError(t, err)

		// Non-backup directory should still exist
		assert.DirExists(t, filepath.Join(tmpDir, "other-dir"))
	})

	t.Run("corrupt dir does not evict a good backup", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Three VALID backups (oldest → newest) plus a NEWER corrupt dir whose
		// archive is NON-EMPTY but not a real tar.gz — exactly the case a mere
		// existence check would wrongly count as good; VerifyBackup rejects it.
		good1 := makeBackupDir(t, tmpDir, "backup-20240101-000001", true)
		good2 := makeBackupDir(t, tmpDir, "backup-20240101-000002", true)
		good3 := makeBackupDir(t, tmpDir, "backup-20240101-000003", true)
		corrupt := filepath.Join(tmpDir, "backup-20240101-000004")
		require.NoError(t, os.MkdirAll(corrupt, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(corrupt, "configs.tar.gz"), []byte("not-a-real-tar"), 0o644))

		deploy := NewDeployOps(false, "")
		// keep=3: the old counting treated all 4 dirs as backups and evicted the
		// OLDEST (good1) to keep 3, retaining the corrupt one. VerifyBackup-gated
		// retention counts only the three good ones — all survive, corrupt goes.
		require.NoError(t, deploy.CleanupBackups(context.Background(), tmpDir, 3))

		assert.DirExists(t, good1, "the oldest GOOD backup is not evicted by a corrupt dir (#353)")
		assert.DirExists(t, good2)
		assert.DirExists(t, good3)
		assert.NoDirExists(t, corrupt, "the corrupt (non-empty but invalid) dir is removed, not retained")
	})
}

func TestDeployOps_DeployLocal(t *testing.T) {
	t.Run("sync directories", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		sourceDir := filepath.Join(tmpDir, "source")
		targetDir := filepath.Join(tmpDir, "target")

		require.NoError(t, os.MkdirAll(sourceDir, 0755))

		// Create source files
		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "file1.txt"), []byte("content1"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "file2.txt"), []byte("content2"), 0644))

		deploy := NewDeployOps(false, "")
		err := deploy.DeployLocal(ctx, sourceDir, targetDir, nil, nil)

		require.NoError(t, err)

		// Verify files were synced
		assert.FileExists(t, filepath.Join(targetDir, "file1.txt"))
		assert.FileExists(t, filepath.Join(targetDir, "file2.txt"))

		// Verify content
		content1, err := os.ReadFile(filepath.Join(targetDir, "file1.txt"))
		require.NoError(t, err)
		assert.Equal(t, "content1", string(content1))

		content2, err := os.ReadFile(filepath.Join(targetDir, "file2.txt"))
		require.NoError(t, err)
		assert.Equal(t, "content2", string(content2))
	})

	t.Run("sync nested directories", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		sourceDir := filepath.Join(tmpDir, "source")
		targetDir := filepath.Join(tmpDir, "target")

		// Create nested directory structure
		require.NoError(t, os.MkdirAll(filepath.Join(sourceDir, "subdir", "nested"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "root.txt"), []byte("root"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "subdir", "sub.txt"), []byte("sub"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "subdir", "nested", "deep.txt"), []byte("deep"), 0644))

		deploy := NewDeployOps(false, "")
		err := deploy.DeployLocal(ctx, sourceDir, targetDir, nil, nil)

		require.NoError(t, err)

		// Verify all files were synced
		assert.FileExists(t, filepath.Join(targetDir, "root.txt"))
		assert.FileExists(t, filepath.Join(targetDir, "subdir", "sub.txt"))
		assert.FileExists(t, filepath.Join(targetDir, "subdir", "nested", "deep.txt"))
	})

	t.Run("replaces existing target", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		sourceDir := filepath.Join(tmpDir, "source")
		targetDir := filepath.Join(tmpDir, "target")

		// Create source with one file
		require.NoError(t, os.MkdirAll(sourceDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "new.txt"), []byte("new"), 0644))

		// Create target with different file
		require.NoError(t, os.MkdirAll(targetDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(targetDir, "old.txt"), []byte("old"), 0644))

		deploy := NewDeployOps(false, "")
		err := deploy.DeployLocal(ctx, sourceDir, targetDir, nil, nil)

		require.NoError(t, err)

		// New file should exist
		assert.FileExists(t, filepath.Join(targetDir, "new.txt"))
		// Old file should be gone (delete semantics)
		assert.NoFileExists(t, filepath.Join(targetDir, "old.txt"))
	})

	t.Run("dry run does not sync", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		sourceDir := filepath.Join(tmpDir, "source")
		targetDir := filepath.Join(tmpDir, "target")

		require.NoError(t, os.MkdirAll(sourceDir, 0755))
		require.NoError(t, os.MkdirAll(targetDir, 0755))

		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "file.txt"), []byte("content"), 0644))

		deploy := NewDeployOps(true, "")
		err := deploy.DeployLocal(ctx, sourceDir, targetDir, nil, nil)

		require.NoError(t, err)

		// File should NOT exist in target (dry run)
		assert.NoFileExists(t, filepath.Join(targetDir, "file.txt"))
	})

	t.Run("error on non-existent source", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		sourceDir := filepath.Join(tmpDir, "nonexistent")
		targetDir := filepath.Join(tmpDir, "target")

		deploy := NewDeployOps(false, "")
		err := deploy.DeployLocal(ctx, sourceDir, targetDir, nil, nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "source directory")
	})

	t.Run("error when source is a file", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		sourceFile := filepath.Join(tmpDir, "source.txt")
		targetDir := filepath.Join(tmpDir, "target")

		require.NoError(t, os.WriteFile(sourceFile, []byte("content"), 0644))

		deploy := NewDeployOps(false, "")
		err := deploy.DeployLocal(ctx, sourceFile, targetDir, nil, nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "not a directory")
	})
}

func TestDeployOps_DeployLocalFile(t *testing.T) {
	t.Run("sync single file", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		sourceFile := filepath.Join(tmpDir, "source.txt")
		targetFile := filepath.Join(tmpDir, "target.txt")

		require.NoError(t, os.WriteFile(sourceFile, []byte("content"), 0644))

		deploy := NewDeployOps(false, "")
		err := deploy.DeployLocalFile(ctx, sourceFile, targetFile, nil)

		require.NoError(t, err)
		assert.FileExists(t, targetFile)

		content, err := os.ReadFile(targetFile)
		require.NoError(t, err)
		assert.Equal(t, "content", string(content))
	})

	t.Run("creates parent directories", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		sourceFile := filepath.Join(tmpDir, "source.txt")
		targetFile := filepath.Join(tmpDir, "nested", "dir", "target.txt")

		require.NoError(t, os.WriteFile(sourceFile, []byte("content"), 0644))

		deploy := NewDeployOps(false, "")
		err := deploy.DeployLocalFile(ctx, sourceFile, targetFile, nil)

		require.NoError(t, err)
		assert.FileExists(t, targetFile)
	})

	t.Run("dry run does not copy", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		sourceFile := filepath.Join(tmpDir, "source.txt")
		targetFile := filepath.Join(tmpDir, "target.txt")

		require.NoError(t, os.WriteFile(sourceFile, []byte("content"), 0644))

		deploy := NewDeployOps(true, "")
		err := deploy.DeployLocalFile(ctx, sourceFile, targetFile, nil)

		require.NoError(t, err)
		assert.NoFileExists(t, targetFile)
	})

	t.Run("error on non-existent source", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		sourceFile := filepath.Join(tmpDir, "nonexistent.txt")
		targetFile := filepath.Join(tmpDir, "target.txt")

		deploy := NewDeployOps(false, "")
		err := deploy.DeployLocalFile(ctx, sourceFile, targetFile, nil)

		require.Error(t, err)
	})
}

func TestDeployOps_DeployLocal_ContentHash(t *testing.T) {
	t.Run("skips unchanged files and reports written", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		sourceDir := filepath.Join(tmpDir, "source")
		targetDir := filepath.Join(tmpDir, "target")

		require.NoError(t, os.MkdirAll(sourceDir, 0755))
		require.NoError(t, os.MkdirAll(targetDir, 0755))

		// Source has two files; target already has one with matching content.
		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "unchanged.txt"), []byte("same"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "changed.txt"), []byte("new"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(targetDir, "unchanged.txt"), []byte("same"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(targetDir, "changed.txt"), []byte("old"), 0644))

		deploy := &DeployOps{ContentHashSync: true}
		result := &DeployResult{}
		err := deploy.DeployLocal(ctx, sourceDir, targetDir, result, nil)

		require.NoError(t, err)
		assert.Equal(t, []string{"changed.txt"}, result.WrittenFiles)
	})

	t.Run("removes stale files from target", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		sourceDir := filepath.Join(tmpDir, "source")
		targetDir := filepath.Join(tmpDir, "target")

		require.NoError(t, os.MkdirAll(sourceDir, 0755))
		require.NoError(t, os.MkdirAll(targetDir, 0755))

		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "keep.txt"), []byte("keep"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(targetDir, "keep.txt"), []byte("keep"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(targetDir, "stale.txt"), []byte("remove me"), 0644))

		// Both files were in bosun's previous deploy manifest. stale.txt is gone
		// from source, so the managed-set prune removes it; keep.txt survives.
		prevManaged := map[string]bool{"keep.txt": true, "stale.txt": true}
		deploy := &DeployOps{ContentHashSync: true}
		err := deploy.DeployLocal(ctx, sourceDir, targetDir, &DeployResult{}, prevManaged)

		require.NoError(t, err)
		assert.FileExists(t, filepath.Join(targetDir, "keep.txt"))
		assert.NoFileExists(t, filepath.Join(targetDir, "stale.txt"))
	})

	t.Run("empty result when all identical", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		sourceDir := filepath.Join(tmpDir, "source")
		targetDir := filepath.Join(tmpDir, "target")

		require.NoError(t, os.MkdirAll(sourceDir, 0755))
		require.NoError(t, os.MkdirAll(targetDir, 0755))

		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "a.txt"), []byte("same"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(targetDir, "a.txt"), []byte("same"), 0644))

		deploy := &DeployOps{ContentHashSync: true}
		result := &DeployResult{}
		err := deploy.DeployLocal(ctx, sourceDir, targetDir, result, nil)

		require.NoError(t, err)
		assert.Empty(t, result.WrittenFiles)
	})
}

func TestDeployOps_DeployLocalFile_ContentHash(t *testing.T) {
	t.Run("skips unchanged file", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		src := filepath.Join(tmpDir, "src.txt")
		dst := filepath.Join(tmpDir, "dst.txt")
		require.NoError(t, os.WriteFile(src, []byte("identical"), 0644))
		require.NoError(t, os.WriteFile(dst, []byte("identical"), 0644))

		deploy := &DeployOps{ContentHashSync: true}
		result := &DeployResult{}
		err := deploy.DeployLocalFile(ctx, src, dst, result)

		require.NoError(t, err)
		assert.Empty(t, result.WrittenFiles)
	})

	t.Run("writes changed file and records it", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		src := filepath.Join(tmpDir, "src.txt")
		dst := filepath.Join(tmpDir, "dst.txt")
		require.NoError(t, os.WriteFile(src, []byte("new"), 0644))
		require.NoError(t, os.WriteFile(dst, []byte("old"), 0644))

		deploy := &DeployOps{ContentHashSync: true}
		result := &DeployResult{}
		err := deploy.DeployLocalFile(ctx, src, dst, result)

		require.NoError(t, err)
		// WrittenFiles now stores basename (relative), not absolute path.
		// The caller (deployLocal) prefixes with the target's RelPath.
		assert.Contains(t, result.WrittenFiles, filepath.Base(dst))

		got, _ := os.ReadFile(dst)
		assert.Equal(t, "new", string(got))
	})
}

func TestDeployOps_ComposeUp(t *testing.T) {
	t.Run("dry run skips execution", func(t *testing.T) {
		ctx := context.Background()

		deploy := NewDeployOps(true, "")
		err := deploy.ComposeUp(ctx, "/any/compose.yml")

		// Dry run should not error
		require.NoError(t, err)
	})

	t.Run("invalid compose file", func(t *testing.T) {
		if _, err := exec.LookPath("docker"); err != nil {
			t.Skip("docker not installed")
		}

		ctx := context.Background()

		deploy := NewDeployOps(false, "")
		err := deploy.ComposeUp(ctx, "/non/existent/compose.yml")

		assert.Error(t, err)
	})
}

func TestDeployOps_SignalContainer(t *testing.T) {
	t.Run("dry run skips execution", func(t *testing.T) {
		ctx := context.Background()

		deploy := NewDeployOps(true, "")
		err := deploy.SignalContainer(ctx, "container-name", "SIGHUP")

		require.NoError(t, err)
	})

	t.Run("signal non-existent container", func(t *testing.T) {
		if _, err := exec.LookPath("docker"); err != nil {
			t.Skip("docker not installed")
		}

		ctx := context.Background()

		deploy := NewDeployOps(false, "")
		err := deploy.SignalContainer(ctx, "non-existent-container-12345", "SIGHUP")

		assert.Error(t, err)
	})
}

func TestDeployOps_EnsureRemoteDir(t *testing.T) {
	// Skip remote tests as they require SSH setup
	t.Skip("requires SSH setup")
}

func TestDeployOps_DeployRemote(t *testing.T) {
	// Skip remote tests as they require SSH setup
	t.Skip("requires SSH setup")
}

func TestDeployOps_BackupRemote(t *testing.T) {
	// Skip remote tests as they require SSH setup
	t.Skip("requires SSH setup")
}

func TestDeployOps_ComposeUpRemote(t *testing.T) {
	t.Run("dry run skips execution", func(t *testing.T) {
		ctx := context.Background()

		deploy := NewDeployOps(true, "")
		err := deploy.ComposeUpRemote(ctx, "host", "/any/path")

		require.NoError(t, err)
	})
}

func TestDeployOps_SignalContainerRemote(t *testing.T) {
	t.Run("dry run skips execution", func(t *testing.T) {
		ctx := context.Background()

		deploy := NewDeployOps(true, "")
		err := deploy.SignalContainerRemote(ctx, "host", "container", "SIGHUP")

		require.NoError(t, err)
	})
}

func TestIsTransientSSHError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"connection refused", fmt.Errorf("ssh: connect to host: Connection refused"), true},
		{"connection reset", fmt.Errorf("connection reset by peer"), true},
		{"connection timed out", fmt.Errorf("ssh: connection timed out"), true},
		{"network unreachable", fmt.Errorf("network is unreachable"), true},
		{"no route to host", fmt.Errorf("no route to host"), true},
		{"host is down", fmt.Errorf("host is down"), true},
		{"i/o timeout", fmt.Errorf("dial tcp: i/o timeout"), true},
		{"temporary failure", fmt.Errorf("temporary failure in name resolution"), true},
		{"operation timed out", fmt.Errorf("operation timed out"), true},
		{"permission denied", fmt.Errorf("permission denied (publickey)"), false},
		{"authentication failure", fmt.Errorf("authentication failed"), false},
		{"file not found", fmt.Errorf("file not found"), false},
		{"generic error", fmt.Errorf("some other error"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isTransientSSHError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRetryWithBackoff(t *testing.T) {
	t.Run("succeeds on first attempt", func(t *testing.T) {
		ctx := context.Background()
		attempts := 0

		err := retryWithBackoff(ctx, 3, func() error {
			attempts++
			return nil
		})

		require.NoError(t, err)
		assert.Equal(t, 1, attempts)
	})

	t.Run("retries on transient error and succeeds", func(t *testing.T) {
		ctx := context.Background()
		attempts := 0

		err := retryWithBackoff(ctx, 3, func() error {
			attempts++
			if attempts < 2 {
				return fmt.Errorf("connection refused")
			}
			return nil
		})

		require.NoError(t, err)
		assert.Equal(t, 2, attempts)
	})

	t.Run("fails immediately on non-transient error", func(t *testing.T) {
		ctx := context.Background()
		attempts := 0

		err := retryWithBackoff(ctx, 3, func() error {
			attempts++
			return fmt.Errorf("permission denied")
		})

		require.Error(t, err)
		assert.Equal(t, 1, attempts)
		assert.Contains(t, err.Error(), "permission denied")
	})

	t.Run("exhausts retries on persistent transient error", func(t *testing.T) {
		ctx := context.Background()
		attempts := 0

		err := retryWithBackoff(ctx, 3, func() error {
			attempts++
			return fmt.Errorf("connection refused")
		})

		require.Error(t, err)
		assert.Equal(t, 3, attempts)
		assert.Contains(t, err.Error(), "operation failed after 3 attempts")
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		attempts := 0

		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		err := retryWithBackoff(ctx, 5, func() error {
			attempts++
			return fmt.Errorf("connection refused")
		})

		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("uses default max retries when zero", func(t *testing.T) {
		ctx := context.Background()
		attempts := 0

		err := retryWithBackoff(ctx, 0, func() error {
			attempts++
			return fmt.Errorf("connection refused")
		})

		require.Error(t, err)
		assert.Equal(t, DefaultMaxRetries, attempts)
	})

	t.Run("already cancelled context returns immediately", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		attempts := 0

		err := retryWithBackoff(ctx, 3, func() error {
			attempts++
			return fmt.Errorf("connection refused")
		})

		require.Error(t, err)
		// The operation runs once, then ctx.Err() is checked and returns.
		assert.Equal(t, 1, attempts)
	})
}

func TestDeployResult_AddWritten(t *testing.T) {
	result := &DeployResult{}
	result.AddWritten("file1.txt", "file2.txt")
	assert.Equal(t, []string{"file1.txt", "file2.txt"}, result.WrittenFiles)

	result.AddWritten("file3.txt")
	assert.Len(t, result.WrittenFiles, 3)
}

func TestDeployOps_ComposeArgs(t *testing.T) {
	t.Run("without project name", func(t *testing.T) {
		d := &DeployOps{}
		args := d.composeArgs("file1.yml", "file2.yml")
		assert.Equal(t, []string{"compose", "-f", "file1.yml", "-f", "file2.yml"}, args)
	})

	t.Run("with project name", func(t *testing.T) {
		d := &DeployOps{ProjectName: "myproject"}
		args := d.composeArgs("file1.yml")
		assert.Equal(t, []string{"compose", "-p", "myproject", "-f", "file1.yml"}, args)
	})

	t.Run("no files", func(t *testing.T) {
		d := &DeployOps{ProjectName: "proj"}
		args := d.composeArgs()
		assert.Equal(t, []string{"compose", "-p", "proj"}, args)
	})
}

func TestDeployOps_VerifyBackup(t *testing.T) {
	t.Run("archive not found", func(t *testing.T) {
		tmpDir := t.TempDir()
		d := NewDeployOps(false, "")
		err := d.VerifyBackup(context.Background(), filepath.Join(tmpDir, "nonexistent"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "backup archive not found")
	})

	t.Run("empty archive file", func(t *testing.T) {
		tmpDir := t.TempDir()
		backupPath := filepath.Join(tmpDir, "backup")
		require.NoError(t, os.MkdirAll(backupPath, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(backupPath, "configs.tar.gz"), []byte{}, 0644))

		d := NewDeployOps(false, "")
		err := d.VerifyBackup(context.Background(), backupPath)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "backup archive is empty")
	})

	t.Run("corrupted archive file", func(t *testing.T) {
		if _, err := exec.LookPath("tar"); err != nil {
			t.Skip("tar not installed")
		}
		tmpDir := t.TempDir()
		backupPath := filepath.Join(tmpDir, "backup")
		require.NoError(t, os.MkdirAll(backupPath, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(backupPath, "configs.tar.gz"), []byte("not a tar file"), 0644))

		d := NewDeployOps(false, "")
		err := d.VerifyBackup(context.Background(), backupPath)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "corrupted")
	})

	t.Run("valid archive", func(t *testing.T) {
		if _, err := exec.LookPath("tar"); err != nil {
			t.Skip("tar not installed")
		}
		tmpDir := t.TempDir()
		backupPath := filepath.Join(tmpDir, "backup")
		require.NoError(t, os.MkdirAll(backupPath, 0755))

		// Create a real tar.gz file with some content.
		srcFile := filepath.Join(tmpDir, "test.txt")
		require.NoError(t, os.WriteFile(srcFile, []byte("test content"), 0644))

		tarFile := filepath.Join(backupPath, "configs.tar.gz")
		cmd := exec.Command("tar", "-czf", tarFile, "-C", tmpDir, "test.txt")
		require.NoError(t, cmd.Run())

		d := NewDeployOps(false, "")
		err := d.VerifyBackup(context.Background(), backupPath)
		assert.NoError(t, err)
	})
}

func TestDeployOps_ComposeUpMultiple_EmptyFiles(t *testing.T) {
	d := NewDeployOps(false, "")
	err := d.ComposeUpMultiple(context.Background(), []string{})
	assert.NoError(t, err)
}

func TestDeployOps_DeployLocal_ContextCancelled(t *testing.T) {
	tmpDir := evalSymlinks(t, t.TempDir())
	sourceDir := filepath.Join(tmpDir, "source")
	targetDir := filepath.Join(tmpDir, "target")
	require.NoError(t, os.MkdirAll(sourceDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "file.txt"), []byte("content"), 0644))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	d := &DeployOps{ContentHashSync: true}
	err := d.DeployLocal(ctx, sourceDir, targetDir, nil, nil)
	// Context should propagate through the sync.
	if err != nil {
		assert.ErrorIs(t, err, context.Canceled)
	}
}

func TestDeployOps_DeployLocalFile_ContextCancelled(t *testing.T) {
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "src.txt")
	dst := filepath.Join(tmpDir, "dst.txt")
	require.NoError(t, os.WriteFile(src, []byte("content"), 0644))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	d := NewDeployOps(false, "")
	err := d.DeployLocalFile(ctx, src, dst, nil)
	// Context should cause early return.
	if err != nil {
		assert.ErrorIs(t, err, context.Canceled)
	}
}

func TestDeployOps_SignalContainer_InvalidName(t *testing.T) {
	d := NewDeployOps(false, "")
	err := d.SignalContainer(context.Background(), "", "SIGHUP")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid container name")
}

func TestDeployOps_SignalContainer_InvalidSignal(t *testing.T) {
	d := NewDeployOps(false, "")
	err := d.SignalContainer(context.Background(), "mycontainer", "INVALID")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid signal")
}

func TestDeployOps_SignalContainerRemote_InvalidHost(t *testing.T) {
	d := NewDeployOps(false, "")
	err := d.SignalContainerRemote(context.Background(), "", "container", "SIGHUP")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid SSH host")
}

func TestDeployOps_SignalContainerRemote_InvalidContainerName(t *testing.T) {
	d := NewDeployOps(false, "")
	err := d.SignalContainerRemote(context.Background(), "host", "", "SIGHUP")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid container name")
}

func TestDeployOps_SignalContainerRemote_InvalidSignal(t *testing.T) {
	d := NewDeployOps(false, "")
	err := d.SignalContainerRemote(context.Background(), "host", "container", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid signal")
}

func TestDeployOps_DeployRemote_InvalidHost(t *testing.T) {
	d := NewDeployOps(false, "")
	err := d.DeployRemote(context.Background(), "/src", "", "/dst", false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid SSH host")
}

func TestDeployOps_DeployRemoteFile_InvalidHost(t *testing.T) {
	d := NewDeployOps(false, "")
	err := d.DeployRemoteFile(context.Background(), "/src/file", "", "/dst/file")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid SSH host")
}

func TestDeployOps_EnsureRemoteDir_InvalidHost(t *testing.T) {
	d := NewDeployOps(false, "")
	err := d.EnsureRemoteDir(context.Background(), "", "/some/dir")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid SSH host")
}

func TestDeployOps_BackupRemote_InvalidHost(t *testing.T) {
	d := NewDeployOps(false, "")
	_, err := d.BackupRemote(context.Background(), "", "/backups", []string{"/some/path"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid SSH host")
}

func TestDeployOps_ComposeUpRemote_InvalidHost(t *testing.T) {
	d := NewDeployOps(false, "")
	err := d.ComposeUpRemote(context.Background(), "", "/compose")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid SSH host")
}

func TestDeployOps_CheckSSHConnectivity_InvalidHost(t *testing.T) {
	d := NewDeployOps(false, "")
	err := d.CheckSSHConnectivity(context.Background(), "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid SSH host")
}

func TestDeployOps_DryRun_Remote(t *testing.T) {
	d := NewDeployOps(true, "")
	ctx := context.Background()

	t.Run("deploy remote dry run", func(t *testing.T) {
		err := d.DeployRemote(ctx, "/src", "host", "/dst", false)
		assert.NoError(t, err)
	})

	t.Run("deploy remote file dry run", func(t *testing.T) {
		err := d.DeployRemoteFile(ctx, "/src/file", "host", "/dst/file")
		assert.NoError(t, err)
	})

	t.Run("compose up remote dry run", func(t *testing.T) {
		err := d.ComposeUpRemote(ctx, "host", "/compose")
		assert.NoError(t, err)
	})

	t.Run("verify container health dry run", func(t *testing.T) {
		err := d.VerifyContainerHealth(ctx, "/compose.yml")
		assert.NoError(t, err)
	})
}

func TestRemoveStaleFiles(t *testing.T) {
	ctx := context.Background()

	t.Run("removes managed file gone from source", func(t *testing.T) {
		srcDir := evalSymlinks(t, t.TempDir())
		tgtDir := evalSymlinks(t, t.TempDir())

		// Source has file-a; target has file-a + file-b. Both were managed last
		// deploy; file-b is now gone from source, so it should be pruned.
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "file-a.txt"), []byte("a"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(tgtDir, "file-a.txt"), []byte("a"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(tgtDir, "file-b.txt"), []byte("b"), 0644))

		prevManaged := map[string]bool{"file-a.txt": true, "file-b.txt": true}
		result := &DeployResult{}
		require.NoError(t, removeStaleFiles(ctx, srcDir, tgtDir, result, prevManaged))

		assert.FileExists(t, filepath.Join(tgtDir, "file-a.txt"))
		assert.NoFileExists(t, filepath.Join(tgtDir, "file-b.txt"))
		// #234: the deletion must be recorded so post-sync hooks can match it.
		assert.Equal(t, []string{"file-b.txt"}, result.DeletedFiles)
	})

	t.Run("preserves unmanaged runtime file", func(t *testing.T) {
		srcDir := evalSymlinks(t, t.TempDir())
		tgtDir := evalSymlinks(t, t.TempDir())

		// Repo deploys config-only; target also holds a runtime DB bosun never
		// wrote. The DB is NOT in the manifest, so it must survive.
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "configuration.yml"), []byte("cfg"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(tgtDir, "configuration.yml"), []byte("cfg"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(tgtDir, "db.sqlite3"), []byte("data"), 0644))

		// Manifest contains only the config file; db.sqlite3 is unmanaged.
		prevManaged := map[string]bool{"configuration.yml": true}
		result := &DeployResult{}
		require.NoError(t, removeStaleFiles(ctx, srcDir, tgtDir, result, prevManaged))

		assert.FileExists(t, filepath.Join(tgtDir, "configuration.yml"))
		assert.FileExists(t, filepath.Join(tgtDir, "db.sqlite3"), "unmanaged runtime data must not be pruned")
		assert.Empty(t, result.DeletedFiles)
	})

	t.Run("prunes managed file in subdir", func(t *testing.T) {
		srcDir := evalSymlinks(t, t.TempDir())
		tgtDir := evalSymlinks(t, t.TempDir())

		// Managed nested file gone from source -> pruned; sibling unmanaged
		// runtime file in the same subdir survives.
		require.NoError(t, os.MkdirAll(filepath.Join(tgtDir, "sub"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(tgtDir, "sub", "old.yml"), []byte("old"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(tgtDir, "sub", "runtime.log"), []byte("log"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "keep.txt"), []byte("k"), 0644))

		prevManaged := map[string]bool{"sub/old.yml": true, "keep.txt": true}
		result := &DeployResult{}
		require.NoError(t, removeStaleFiles(ctx, srcDir, tgtDir, result, prevManaged))

		assert.NoFileExists(t, filepath.Join(tgtDir, "sub", "old.yml"))
		assert.FileExists(t, filepath.Join(tgtDir, "sub", "runtime.log"), "unmanaged file must survive")
		assert.Equal(t, []string{"sub/old.yml"}, result.DeletedFiles)
	})

	t.Run("empty manifest prunes nothing", func(t *testing.T) {
		srcDir := evalSymlinks(t, t.TempDir())
		tgtDir := evalSymlinks(t, t.TempDir())

		// First deploy after upgrade: no prior manifest. Target's existing files
		// (e.g. runtime data) must be left untouched.
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "file-a.txt"), []byte("a"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(tgtDir, "leftover.txt"), []byte("x"), 0644))

		require.NoError(t, removeStaleFiles(ctx, srcDir, tgtDir, nil, nil))

		assert.FileExists(t, filepath.Join(tgtDir, "leftover.txt"))
	})

	t.Run("empty source skips pruning (render-failure guard)", func(t *testing.T) {
		srcDir := evalSymlinks(t, t.TempDir())
		tgtDir := evalSymlinks(t, t.TempDir())

		// Source rendered empty but the manifest is non-empty -> suspected render
		// failure. Pruning must be skipped so the populated target is preserved.
		require.NoError(t, os.WriteFile(filepath.Join(tgtDir, "a.txt"), []byte("a"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(tgtDir, "b.txt"), []byte("b"), 0644))

		prevManaged := map[string]bool{"a.txt": true, "b.txt": true}
		require.NoError(t, removeStaleFiles(ctx, srcDir, tgtDir, nil, prevManaged))

		assert.FileExists(t, filepath.Join(tgtDir, "a.txt"))
		assert.FileExists(t, filepath.Join(tgtDir, "b.txt"))
	})

	t.Run("keeps managed files still in source", func(t *testing.T) {
		srcDir := evalSymlinks(t, t.TempDir())
		tgtDir := evalSymlinks(t, t.TempDir())

		require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "sub"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "sub", "keep.txt"), []byte("keep"), 0644))
		require.NoError(t, os.MkdirAll(filepath.Join(tgtDir, "sub"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(tgtDir, "sub", "keep.txt"), []byte("keep"), 0644))

		prevManaged := map[string]bool{"sub/keep.txt": true}
		require.NoError(t, removeStaleFiles(ctx, srcDir, tgtDir, nil, prevManaged))

		assert.FileExists(t, filepath.Join(tgtDir, "sub", "keep.txt"))
	})

	t.Run("non-existent target returns error", func(t *testing.T) {
		srcDir := evalSymlinks(t, t.TempDir())
		// Source must have a regular file to pass the empty-source guard so the
		// walk of the (missing) target is reached.
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "x.txt"), []byte("x"), 0644))
		err := removeStaleFiles(ctx, srcDir, "/nonexistent/target", nil, map[string]bool{"gone.txt": true})
		assert.Error(t, err)
	})

	t.Run("returns error when file removal fails", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("chmod-based delete permission behavior is not reliable on Windows")
		}
		if os.Getuid() == 0 {
			t.Skip("skipping permission test when running as root")
		}

		srcDir := evalSymlinks(t, t.TempDir())
		tgtDir := evalSymlinks(t, t.TempDir())

		// Create a managed stale file inside a read-only directory so os.Remove fails.
		lockedDir := filepath.Join(tgtDir, "locked")
		require.NoError(t, os.MkdirAll(lockedDir, 0755))
		staleFile := filepath.Join(lockedDir, "stale.txt")
		require.NoError(t, os.WriteFile(staleFile, []byte("stale"), 0644))

		// Source has a regular file (passes the empty-source guard) but not the stale file.
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "present.txt"), []byte("p"), 0644))

		// Make the directory read-only so the file inside cannot be removed.
		require.NoError(t, os.Chmod(lockedDir, 0555))
		t.Cleanup(func() {
			_ = os.Chmod(lockedDir, 0755)
		})

		prevManaged := map[string]bool{"locked/stale.txt": true, "present.txt": true}
		err := removeStaleFiles(ctx, srcDir, tgtDir, nil, prevManaged)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "stale file(s) could not be removed")
		assert.Contains(t, err.Error(), "stale.txt")

		// The file should still exist since removal failed.
		assert.FileExists(t, staleFile)
	})
}

func TestDeployOps_DeployLocal_ContentHashSync(t *testing.T) {
	srcDir := t.TempDir()
	tgtDir := filepath.Join(t.TempDir(), "target")

	// Create source files.
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "config.yml"), []byte("key: value"), 0644))

	d := NewDeployOps(false, "")
	d.ContentHashSync = true
	result := &DeployResult{}

	err := d.DeployLocal(context.Background(), srcDir, tgtDir, result, nil)
	require.NoError(t, err)

	// Target should have the file.
	assert.FileExists(t, filepath.Join(tgtDir, "config.yml"))
	// Written files should be tracked.
	assert.NotEmpty(t, result.WrittenFiles)
}

func TestDeployOps_DeployLocal_StandardMode(t *testing.T) {
	srcDir := t.TempDir()
	tgtDir := filepath.Join(t.TempDir(), "target")

	// Create source files.
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "app.yml"), []byte("name: app"), 0644))

	d := NewDeployOps(false, "")
	d.ContentHashSync = false
	result := &DeployResult{}

	err := d.DeployLocal(context.Background(), srcDir, tgtDir, result, nil)
	require.NoError(t, err)

	// Target should have the file.
	assert.FileExists(t, filepath.Join(tgtDir, "app.yml"))
}

func TestDeployOps_DeployLocal_SourceNotDir(t *testing.T) {
	// Source is a file, not a directory.
	tmpDir := t.TempDir()
	srcFile := filepath.Join(tmpDir, "not-a-dir")
	require.NoError(t, os.WriteFile(srcFile, []byte("file"), 0644))

	d := NewDeployOps(false, "")
	err := d.DeployLocal(context.Background(), srcFile, filepath.Join(tmpDir, "tgt"), nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

func TestDeployOps_DeployLocal_SourceMissing(t *testing.T) {
	d := NewDeployOps(false, "")
	err := d.DeployLocal(context.Background(), "/nonexistent/source/dir", "/tmp/tgt", nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "source directory")
}

func TestDeployOps_DeployLocal_ReplacesExistingTarget(t *testing.T) {
	srcDir := t.TempDir()
	tgtParent := t.TempDir()
	tgtDir := filepath.Join(tgtParent, "target")

	// Create source with new file.
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "new.yml"), []byte("new"), 0644))

	// Create target with old file (standard mode replaces entire dir).
	require.NoError(t, os.MkdirAll(tgtDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tgtDir, "old.yml"), []byte("old"), 0644))

	d := NewDeployOps(false, "")
	d.ContentHashSync = false

	err := d.DeployLocal(context.Background(), srcDir, tgtDir, nil, nil)
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(tgtDir, "new.yml"))
	assert.NoFileExists(t, filepath.Join(tgtDir, "old.yml"))
}

func TestDeployOps_DeployLocalFile_ContentHashSync(t *testing.T) {
	tmpDir := t.TempDir()
	srcFile := filepath.Join(tmpDir, "source.yml")
	tgtFile := filepath.Join(tmpDir, "target.yml")

	require.NoError(t, os.WriteFile(srcFile, []byte("content: original"), 0644))

	d := NewDeployOps(false, "")
	d.ContentHashSync = true
	result := &DeployResult{}

	err := d.DeployLocalFile(context.Background(), srcFile, tgtFile, result)
	require.NoError(t, err)

	// File should be written.
	data, err := os.ReadFile(tgtFile)
	require.NoError(t, err)
	assert.Equal(t, "content: original", string(data))
	assert.Contains(t, result.WrittenFiles, filepath.Base(tgtFile))
}

func TestDeployOps_DeployLocalFile_NoChangeSkips(t *testing.T) {
	tmpDir := t.TempDir()
	srcFile := filepath.Join(tmpDir, "source.yml")
	tgtFile := filepath.Join(tmpDir, "target.yml")

	content := []byte("content: same")
	require.NoError(t, os.WriteFile(srcFile, content, 0644))
	require.NoError(t, os.WriteFile(tgtFile, content, 0644))

	d := NewDeployOps(false, "")
	d.ContentHashSync = true
	result := &DeployResult{}

	err := d.DeployLocalFile(context.Background(), srcFile, tgtFile, result)
	require.NoError(t, err)

	// No change means no written files.
	assert.Empty(t, result.WrittenFiles)
}

func TestDeployOps_DeployLocalFile_StandardMode(t *testing.T) {
	tmpDir := t.TempDir()
	srcFile := filepath.Join(tmpDir, "source.yml")
	tgtFile := filepath.Join(tmpDir, "out", "target.yml")

	require.NoError(t, os.WriteFile(srcFile, []byte("data"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "out"), 0755))

	d := NewDeployOps(false, "")
	d.ContentHashSync = false

	err := d.DeployLocalFile(context.Background(), srcFile, tgtFile, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(tgtFile)
	require.NoError(t, err)
	assert.Equal(t, "data", string(data))
}

// TestDeployOps_DeployLocalFile_StandardMode_SymlinkSwallowed covers the B2 fix:
// in standard (non-content-hash) mode a symlink source must be treated as a
// benign no-op — CopyFile returns ErrSymlinkSkipped, which DeployLocalFile
// swallows rather than aborting the deploy. Content-hash mode already did this
// via CopyFileIfChanged; this asserts parity for the standard path.
func TestDeployOps_DeployLocalFile_StandardMode_SymlinkSwallowed(t *testing.T) {
	tmpDir := t.TempDir()
	realFile := filepath.Join(tmpDir, "real.yml")
	srcLink := filepath.Join(tmpDir, "link.yml")
	tgtFile := filepath.Join(tmpDir, "out", "target.yml")

	require.NoError(t, os.WriteFile(realFile, []byte("data"), 0644))
	require.NoError(t, os.Symlink(realFile, srcLink))

	d := NewDeployOps(false, "")
	d.ContentHashSync = false

	err := d.DeployLocalFile(context.Background(), srcLink, tgtFile, nil)
	require.NoError(t, err, "a symlink source must be a no-op, not a deploy failure")

	// The symlink was skipped, so no target file should have been created.
	_, statErr := os.Stat(tgtFile)
	assert.True(t, os.IsNotExist(statErr), "skipped symlink must not produce a destination file")
}

// writeTestBackupArchive creates backupDir/configs.tar.gz containing the given
// absolute file paths, mirroring how Backup() stores them (tar strips the
// leading '/', so member "/x/y" is stored as "x/y"). Used to exercise the
// rollback paths, which extract this archive rather than reading loose files.
func writeTestBackupArchive(t *testing.T, backupDir string, files ...string) {
	t.Helper()
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar not installed")
	}
	require.NoError(t, os.MkdirAll(backupDir, 0755))
	args := append([]string{"-czf", filepath.Join(backupDir, "configs.tar.gz")}, files...)
	out, err := exec.Command("tar", args...).CombinedOutput()
	require.NoError(t, err, "tar failed: %s", out)
}

func TestDeployOps_VerifyContainerHealth_DryRun(t *testing.T) {
	d := NewDeployOps(true, "")
	err := d.VerifyContainerHealth(context.Background(), "/any/compose.yml")
	assert.NoError(t, err)
}

func TestDeployOps_ComposeUpMultiple_DryRun(t *testing.T) {
	d := NewDeployOps(true, "test-project")
	err := d.ComposeUpMultiple(context.Background(), []string{"/some/compose.yml"})
	assert.NoError(t, err)
}

func TestDeployOps_SignalContainerRemote_DryRun(t *testing.T) {
	d := NewDeployOps(true, "")
	err := d.SignalContainerRemote(context.Background(), "host", "container", "SIGHUP")
	assert.NoError(t, err)
}

func TestDeployOps_DeployLocal_ContentHashSync_Stale(t *testing.T) {
	srcDir := t.TempDir()
	tgtDir := filepath.Join(t.TempDir(), "target")

	// Create source with one file.
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "keep.yml"), []byte("keep"), 0644))

	// Pre-create target with an extra stale file.
	require.NoError(t, os.MkdirAll(tgtDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tgtDir, "keep.yml"), []byte("old"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tgtDir, "stale.yml"), []byte("stale"), 0644))

	d := NewDeployOps(false, "")
	d.ContentHashSync = true
	result := &DeployResult{}

	// stale.yml was in the previous deploy manifest but is gone from source, so
	// the managed-set prune removes it. keep.yml is still in source and survives.
	prevManaged := map[string]bool{"keep.yml": true, "stale.yml": true}
	err := d.DeployLocal(context.Background(), srcDir, tgtDir, result, prevManaged)
	require.NoError(t, err)

	// Stale file should be removed.
	assert.NoFileExists(t, filepath.Join(tgtDir, "stale.yml"))
	assert.FileExists(t, filepath.Join(tgtDir, "keep.yml"))
}

func TestDeployOps_CleanupBackups_NonExistentDir(t *testing.T) {
	d := NewDeployOps(false, "")
	err := d.CleanupBackups(context.Background(), "/nonexistent/backup/dir", 5)
	assert.NoError(t, err) // Non-existent dir returns nil.
}

func TestDeployOps_CleanupBackups_RemovesOldest(t *testing.T) {
	tmpDir := t.TempDir()

	// Create 5 valid backup directories.
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("backup-2024-01-0%d", i+1)
		makeBackupDir(t, tmpDir, name, true)
	}

	d := NewDeployOps(false, "")
	err := d.CleanupBackups(context.Background(), tmpDir, 3)
	require.NoError(t, err)

	// Should have removed 2 oldest, kept 3 newest.
	entries, err := os.ReadDir(tmpDir)
	require.NoError(t, err)
	assert.Len(t, entries, 3)
}

// TestDeployOps_CleanupBackups_InvalidRemovalError covers the fault path where a
// corrupt dir cannot be removed: a read-only parent makes os.RemoveAll fail, and
// the error surfaces rather than being swallowed.
func TestDeployOps_CleanupBackups_InvalidRemovalError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses directory permission checks")
	}
	tmpDir := t.TempDir()
	makeBackupDir(t, tmpDir, "backup-20240101-000001", false) // invalid: no archive

	// Read-only parent: RemoveAll cannot unlink the corrupt dir.
	require.NoError(t, os.Chmod(tmpDir, 0o555))
	defer func() { _ = os.Chmod(tmpDir, 0o755) }() // restore for t.TempDir cleanup

	d := NewDeployOps(false, "")
	err := d.CleanupBackups(context.Background(), tmpDir, 3)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "corrupt backup", "the removal failure surfaces")
}

// TestDeployOps_CleanupBackups_ContextCancelled covers the cancellation guard: a
// cancelled context aborts the per-candidate verification loop rather than
// running VerifyBackup on every dir.
func TestDeployOps_CleanupBackups_ContextCancelled(t *testing.T) {
	tmpDir := t.TempDir()
	makeBackupDir(t, tmpDir, "backup-20240101-000001", true) // a candidate to iterate

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the loop reaches the ctx.Err() check

	d := NewDeployOps(false, "")
	err := d.CleanupBackups(ctx, tmpDir, 3)
	require.ErrorIs(t, err, context.Canceled)
}

func TestDeployOps_BackupMkdirError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping permission test when running as root")
	}

	// Use a read-only parent so MkdirAll fails.
	tmpDir := t.TempDir()
	readOnly := filepath.Join(tmpDir, "readonly")
	require.NoError(t, os.MkdirAll(readOnly, 0555))
	t.Cleanup(func() { _ = os.Chmod(readOnly, 0755) })

	d := NewDeployOps(false, "")
	_, err := d.Backup(context.Background(), filepath.Join(readOnly, "backups"), []string{"/tmp"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create backup directory")
}

func TestErrComposeUnhealthy_SentinelBehavior(t *testing.T) {
	t.Run("error wrapping preserves sentinel", func(t *testing.T) {
		err := fmt.Errorf("%w: obsidian, mealie", ErrComposeUnhealthy)
		assert.ErrorIs(t, err, ErrComposeUnhealthy)
		assert.Contains(t, err.Error(), "obsidian")
		assert.Contains(t, err.Error(), "mealie")
	})

	t.Run("sentinel is distinct from rollback errors", func(t *testing.T) {
		err := fmt.Errorf("%w: obsidian", ErrComposeUnhealthy)
		assert.False(t, errors.Is(err, ErrRollbackSucceeded))
		assert.False(t, errors.Is(err, ErrRollbackFailed))
	})
}

func TestDeployOps_DeployLocalStandardMode(t *testing.T) {
	ctx := context.Background()

	t.Run("standard mode deploys via nuke-and-replace", func(t *testing.T) {
		tmpDir := t.TempDir()
		sourceDir := filepath.Join(tmpDir, "source")
		targetDir := filepath.Join(tmpDir, "target")

		require.NoError(t, os.MkdirAll(sourceDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "file1.txt"), []byte("hello"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "file2.txt"), []byte("world"), 0644))

		// Pre-create target with old content to verify it gets replaced.
		require.NoError(t, os.MkdirAll(targetDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(targetDir, "old.txt"), []byte("stale"), 0644))

		d := &DeployOps{DryRun: false, ContentHashSync: false}
		result := &DeployResult{}
		err := d.DeployLocal(ctx, sourceDir, targetDir, result, nil)
		require.NoError(t, err)

		// Verify new files exist.
		content, err := os.ReadFile(filepath.Join(targetDir, "file1.txt"))
		require.NoError(t, err)
		assert.Equal(t, "hello", string(content))

		// Verify old file was removed (nuke-and-replace).
		_, err = os.Stat(filepath.Join(targetDir, "old.txt"))
		assert.True(t, os.IsNotExist(err), "old file should have been removed")
	})

	t.Run("standard mode context cancellation", func(t *testing.T) {
		tmpDir := t.TempDir()
		sourceDir := filepath.Join(tmpDir, "source")
		targetDir := filepath.Join(tmpDir, "target")

		require.NoError(t, os.MkdirAll(sourceDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "f.txt"), []byte("x"), 0644))

		cancelledCtx, cancel := context.WithCancel(ctx)
		cancel()

		d := &DeployOps{DryRun: false, ContentHashSync: false}
		err := d.DeployLocal(cancelledCtx, sourceDir, targetDir, nil, nil)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("content hash mode MkdirAll error", func(t *testing.T) {
		if os.Getuid() == 0 {
			t.Skip("skipping permission test when running as root")
		}

		tmpDir := t.TempDir()
		sourceDir := filepath.Join(tmpDir, "source")
		require.NoError(t, os.MkdirAll(sourceDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "f.txt"), []byte("x"), 0644))

		// Put a file where the target directory should be, so MkdirAll fails.
		targetPath := filepath.Join(tmpDir, "target")
		require.NoError(t, os.WriteFile(targetPath, []byte("block"), 0644))

		d := &DeployOps{DryRun: false, ContentHashSync: true}
		err := d.DeployLocal(ctx, sourceDir, targetPath, nil, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "create target directory")
	})
}

func TestDeployOps_ComposeUpIsolated(t *testing.T) {
	ctx := context.Background()

	t.Run("all succeed", func(t *testing.T) {
		d := &DeployOps{
			DryRun:        false,
			RemoveOrphans: true,
			composeUpFn: func(_ context.Context, files []string) error {
				return nil
			},
		}

		summary, err := d.ComposeUpIsolated(ctx, []string{"/a.yml", "/b.yml"}, "")
		require.NoError(t, err)
		assert.Equal(t, 2, summary.Succeeded)
		assert.Equal(t, 0, summary.Failed)
	})

	t.Run("one fails with backup triggers rollback", func(t *testing.T) {
		backupDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(backupDir, "bad.yml"), []byte("version: '3'"), 0644))

		callCount := 0
		d := &DeployOps{
			DryRun:        false,
			RemoveOrphans: false,
			composeUpFn: func(_ context.Context, files []string) error {
				callCount++
				if filepath.Base(files[0]) == "bad.yml" {
					return fmt.Errorf("bad image tag")
				}
				return nil
			},
		}

		summary, err := d.ComposeUpIsolated(ctx, []string{"/compose/good.yml", "/compose/bad.yml"}, backupDir)
		require.NoError(t, err) // partial failure is not a fatal error
		assert.Equal(t, 1, summary.Succeeded)
		assert.Equal(t, 1, summary.Failed)
		// Rollback doesn't go through composeUpFn — it calls docker directly.
		// So we can't verify RolledBack=true in unit tests without Docker.
		// But we verify the structure is correct.
		assert.Equal(t, 2, len(summary.Results))
	})

	t.Run("one fails without backup", func(t *testing.T) {
		d := &DeployOps{
			DryRun:        false,
			RemoveOrphans: false,
			composeUpFn: func(_ context.Context, files []string) error {
				if filepath.Base(files[0]) == "bad.yml" {
					return fmt.Errorf("image not found")
				}
				return nil
			},
		}

		summary, err := d.ComposeUpIsolated(ctx, []string{"/compose/good.yml", "/compose/bad.yml"}, "")
		require.NoError(t, err)
		assert.Equal(t, 1, summary.Succeeded)
		assert.Equal(t, 1, summary.Failed)
		assert.Equal(t, 0, summary.RolledBack)
	})

	t.Run("all fail returns error", func(t *testing.T) {
		d := &DeployOps{
			DryRun:        false,
			RemoveOrphans: false,
			composeUpFn: func(_ context.Context, _ []string) error {
				return fmt.Errorf("compose up failed")
			},
		}

		summary, err := d.ComposeUpIsolated(ctx, []string{"/a.yml", "/b.yml"}, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "all 2 compose files failed")
		assert.Equal(t, 0, summary.Succeeded)
		assert.Equal(t, 2, summary.Failed)
	})

	t.Run("unhealthy treated as success", func(t *testing.T) {
		d := &DeployOps{
			DryRun:        false,
			RemoveOrphans: false,
			composeUpFn: func(_ context.Context, _ []string) error {
				return fmt.Errorf("%w: mealie", ErrComposeUnhealthy)
			},
		}

		summary, err := d.ComposeUpIsolated(ctx, []string{"/a.yml"}, "")
		require.NoError(t, err)
		assert.Equal(t, 1, summary.Succeeded)
		assert.Equal(t, 0, summary.Failed)
		// The unhealthy error is preserved on the result.
		assert.ErrorIs(t, summary.Results[0].Err, ErrComposeUnhealthy)
	})

	t.Run("dry run succeeds immediately", func(t *testing.T) {
		d := &DeployOps{DryRun: true}
		summary, err := d.ComposeUpIsolated(ctx, []string{"/a.yml", "/b.yml"}, "/backup")
		require.NoError(t, err)
		assert.Equal(t, 2, summary.Succeeded)
		assert.Equal(t, 0, summary.Failed)
	})
}

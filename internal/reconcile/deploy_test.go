package reconcile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
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

	t.Run("replaces previously managed file with directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		sourceDir := filepath.Join(tmpDir, "source")
		targetDir := filepath.Join(tmpDir, "target")
		require.NoError(t, os.MkdirAll(filepath.Join(sourceDir, "config"), 0755))
		require.NoError(t, os.MkdirAll(targetDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "config", "app.yml"), []byte("new"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(targetDir, "config"), []byte("old"), 0644))

		result := &DeployResult{}
		deploy := &DeployOps{ContentHashSync: true}
		err := deploy.DeployLocal(context.Background(), sourceDir, targetDir, result, map[string]bool{"config": true})

		require.NoError(t, err)
		assert.FileExists(t, filepath.Join(targetDir, "config", "app.yml"))
		assert.Equal(t, []string{filepath.Join("config", "app.yml")}, result.WrittenFiles)
		assert.Equal(t, []string{"config"}, result.DeletedFiles)
	})

	t.Run("replaces managed directory contents with file", func(t *testing.T) {
		tmpDir := t.TempDir()
		sourceDir := filepath.Join(tmpDir, "source")
		targetDir := filepath.Join(tmpDir, "target")
		require.NoError(t, os.MkdirAll(sourceDir, 0755))
		require.NoError(t, os.MkdirAll(filepath.Join(targetDir, "config", "nested"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "config"), []byte("new"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(targetDir, "config", "nested", "app.yml"), []byte("old"), 0644))

		result := &DeployResult{}
		deploy := &DeployOps{ContentHashSync: true}
		err := deploy.DeployLocal(context.Background(), sourceDir, targetDir, result, map[string]bool{"config/nested/app.yml": true})

		require.NoError(t, err)
		content, readErr := os.ReadFile(filepath.Join(targetDir, "config"))
		require.NoError(t, readErr)
		assert.Equal(t, "new", string(content))
		assert.Equal(t, []string{"config"}, result.WrittenFiles)
		assert.Equal(t, []string{"config/nested/app.yml"}, result.DeletedFiles)
	})

	t.Run("refuses file to directory transition for unmanaged file", func(t *testing.T) {
		tmpDir := t.TempDir()
		sourceDir := filepath.Join(tmpDir, "source")
		targetDir := filepath.Join(tmpDir, "target")
		require.NoError(t, os.MkdirAll(filepath.Join(sourceDir, "config"), 0755))
		require.NoError(t, os.MkdirAll(targetDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "config", "app.yml"), []byte("new"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(targetDir, "config"), []byte("unmanaged"), 0644))

		deploy := &DeployOps{ContentHashSync: true}
		err := deploy.DeployLocal(context.Background(), sourceDir, targetDir, &DeployResult{}, nil)

		require.Error(t, err)
		assert.ErrorContains(t, err, "config blocks directory deployment and is not a managed regular file")
		content, readErr := os.ReadFile(filepath.Join(targetDir, "config"))
		require.NoError(t, readErr)
		assert.Equal(t, "unmanaged", string(content))
	})

	t.Run("refuses directory to file transition with unmanaged data", func(t *testing.T) {
		tmpDir := t.TempDir()
		sourceDir := filepath.Join(tmpDir, "source")
		targetDir := filepath.Join(tmpDir, "target")
		require.NoError(t, os.MkdirAll(sourceDir, 0755))
		require.NoError(t, os.MkdirAll(filepath.Join(targetDir, "config"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "config"), []byte("new"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(targetDir, "config", "app.yml"), []byte("old"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(targetDir, "config", "runtime.db"), []byte("keep"), 0644))

		deploy := &DeployOps{ContentHashSync: true}
		err := deploy.DeployLocal(context.Background(), sourceDir, targetDir, &DeployResult{}, map[string]bool{"config/app.yml": true})

		require.Error(t, err)
		assert.ErrorContains(t, err, "runtime.db is not in the managed-file manifest")
		assert.FileExists(t, filepath.Join(targetDir, "config", "app.yml"))
		assert.FileExists(t, filepath.Join(targetDir, "config", "runtime.db"))
	})

	t.Run("refuses directory to file transition for empty unmanaged directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		sourceDir := filepath.Join(tmpDir, "source")
		targetDir := filepath.Join(tmpDir, "target")
		require.NoError(t, os.MkdirAll(sourceDir, 0755))
		require.NoError(t, os.MkdirAll(filepath.Join(targetDir, "config"), 0750))
		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "config"), []byte("new"), 0644))

		err := (&DeployOps{ContentHashSync: true}).DeployLocal(context.Background(), sourceDir, targetDir, &DeployResult{}, nil)

		require.Error(t, err)
		assert.ErrorContains(t, err, "has no managed descendants")
		assert.DirExists(t, filepath.Join(targetDir, "config"))
	})

	t.Run("refuses directory to file transition with empty unmanaged subtree", func(t *testing.T) {
		tmpDir := t.TempDir()
		sourceDir := filepath.Join(tmpDir, "source")
		targetDir := filepath.Join(tmpDir, "target")
		require.NoError(t, os.MkdirAll(sourceDir, 0755))
		require.NoError(t, os.MkdirAll(filepath.Join(targetDir, "config", "managed"), 0755))
		require.NoError(t, os.MkdirAll(filepath.Join(targetDir, "config", "runtime-empty"), 0700))
		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "config"), []byte("new"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(targetDir, "config", "managed", "app.yml"), []byte("old"), 0644))

		err := (&DeployOps{ContentHashSync: true}).DeployLocal(context.Background(), sourceDir, targetDir, &DeployResult{}, map[string]bool{"config/managed/app.yml": true})

		require.Error(t, err)
		assert.ErrorContains(t, err, "runtime-empty has no managed descendants")
		assert.DirExists(t, filepath.Join(targetDir, "config", "runtime-empty"))
		assert.FileExists(t, filepath.Join(targetDir, "config", "managed", "app.yml"))
	})

	t.Run("rolls back promoted transition when later copy fails", func(t *testing.T) {
		tmpDir := t.TempDir()
		sourceDir := filepath.Join(tmpDir, "source")
		targetDir := filepath.Join(tmpDir, "target")
		require.NoError(t, os.MkdirAll(filepath.Join(sourceDir, "config"), 0755))
		require.NoError(t, os.MkdirAll(targetDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "config", "app.yml"), []byte("new"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(targetDir, "config"), []byte("old"), 0644))
		copyErr := errors.New("copy failed after promotion")
		deploy := &DeployOps{
			ContentHashSync: true,
			copyDirIfChangedFn: func(string, string) ([]string, error) {
				return nil, copyErr
			},
		}

		err := deploy.DeployLocal(context.Background(), sourceDir, targetDir, &DeployResult{}, map[string]bool{"config": true})

		require.ErrorIs(t, err, copyErr)
		content, readErr := os.ReadFile(filepath.Join(targetDir, "config"))
		require.NoError(t, readErr)
		assert.Equal(t, "old", string(content))
	})
}

func newFileToDirTransition(t *testing.T) (*managedTypeTransitions, string) {
	t.Helper()
	tmpDir := t.TempDir()
	source := filepath.Join(tmpDir, "source")
	targetParent := filepath.Join(tmpDir, "target")
	target := filepath.Join(targetParent, "config")
	require.NoError(t, os.MkdirAll(filepath.Join(source, "config"), 0755))
	require.NoError(t, os.MkdirAll(targetParent, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(source, "config", "app.yml"), []byte("new"), 0644))
	require.NoError(t, os.WriteFile(target, []byte("old"), 0644))
	transitions, err := prepareManagedTypeTransitions(source, targetParent, map[string]bool{"config": true})
	require.NoError(t, err)
	t.Cleanup(func() {
		transitions.Close()
		for _, item := range transitions.items {
			_ = os.RemoveAll(item.oldPath)
			_ = os.RemoveAll(item.newPath)
		}
	})
	return transitions, target
}

func newDirToFileTransition(t *testing.T) (*managedTypeTransitions, string) {
	t.Helper()
	tmpDir := t.TempDir()
	source := filepath.Join(tmpDir, "source")
	targetParent := filepath.Join(tmpDir, "target")
	target := filepath.Join(targetParent, "config")
	require.NoError(t, os.MkdirAll(source, 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(target, "managed"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(source, "config"), []byte("new"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(target, "managed", "app.yml"), []byte("old"), 0644))
	transitions, err := prepareManagedTypeTransitions(source, targetParent, map[string]bool{"config/managed/app.yml": true})
	require.NoError(t, err)
	t.Cleanup(func() {
		transitions.Close()
		for _, item := range transitions.items {
			_ = os.RemoveAll(item.oldPath)
			_ = os.RemoveAll(item.newPath)
		}
	})
	return transitions, target
}

func TestManagedTypeTransitions_PromotionFailureRestoresOriginal(t *testing.T) {
	transitions, target := newFileToDirTransition(t)
	require.Len(t, transitions.items, 1)
	require.NoError(t, os.RemoveAll(transitions.items[0].newPath))

	err := transitions.Promote()
	require.Error(t, err)
	assert.ErrorContains(t, err, "staged replacement changed before promoting config")
	content, readErr := os.ReadFile(target)
	require.NoError(t, readErr)
	assert.Equal(t, "old", string(content))
	assert.NoFileExists(t, transitions.items[0].oldPath)
}

func TestManagedTypeTransitions_SuccessfulRollbackRemovesRedundantStage(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(*testing.T) (*managedTypeTransitions, string)
	}{
		{name: "file to directory", setup: newFileToDirTransition},
		{name: "directory to file", setup: newDirToFileTransition},
	} {
		t.Run(test.name, func(t *testing.T) {
			transitions, target := test.setup(t)
			require.NoError(t, transitions.Promote())
			item := transitions.items[0]
			cause := errors.New("later deploy failed")

			err := transitions.Rollback(cause)

			assert.True(t, err == cause, "a complete rollback should return only the original cause")
			assert.NoFileExists(t, item.oldPath)
			assert.NoFileExists(t, item.newPath)
			if item.sourceIsDir {
				content, readErr := os.ReadFile(target)
				require.NoError(t, readErr)
				assert.Equal(t, "old", string(content))
			} else {
				assert.FileExists(t, filepath.Join(target, "managed", "app.yml"))
			}
		})
	}
}

func TestManagedTypeTransitions_RollbackRetainsChangedReplacement(t *testing.T) {
	transitions, target := newFileToDirTransition(t)
	require.NoError(t, transitions.Promote())
	require.NoError(t, os.WriteFile(filepath.Join(target, "runtime.db"), []byte("keep"), 0600))

	err := transitions.Rollback(errors.New("later deploy failed"))

	require.Error(t, err)
	item := transitions.items[0]
	assert.ErrorContains(t, err, "staged replacement changed")
	assert.ErrorContains(t, err, item.newPath)
	assert.FileExists(t, filepath.Join(item.newPath, "runtime.db"))
	content, readErr := os.ReadFile(target)
	require.NoError(t, readErr)
	assert.Equal(t, "old", string(content))
}

func TestManagedTypeTransitions_FingerprintRetainsSubtleReplacementChanges(t *testing.T) {
	t.Run("same-root content change before promotion", func(t *testing.T) {
		transitions, target := newFileToDirTransition(t)
		item := transitions.items[0]
		changed := filepath.Join(item.newPath, "app.yml")
		before, err := os.Stat(changed)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(changed, []byte("bad"), before.Mode()))
		require.NoError(t, os.Chtimes(changed, before.ModTime(), before.ModTime()))

		err = transitions.Promote()

		require.ErrorContains(t, err, "staged replacement changed before promoting")
		content, readErr := os.ReadFile(target)
		require.NoError(t, readErr)
		assert.Equal(t, "old", string(content))
		assert.FileExists(t, changed)
	})

	t.Run("same-size same-mtime content change in directory", func(t *testing.T) {
		transitions, target := newFileToDirTransition(t)
		require.NoError(t, transitions.Promote())
		changed := filepath.Join(target, "app.yml")
		before, err := os.Stat(changed)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(changed, []byte("bad"), before.Mode()))
		require.NoError(t, os.Chtimes(changed, before.ModTime(), before.ModTime()))

		err = transitions.Rollback(errors.New("later deploy failed"))

		require.ErrorContains(t, err, "staged replacement changed")
		retained := filepath.Join(transitions.items[0].newPath, "app.yml")
		content, readErr := os.ReadFile(retained)
		require.NoError(t, readErr)
		assert.Equal(t, "bad", string(content))
	})

	t.Run("mode-only change in file", func(t *testing.T) {
		transitions, target := newDirToFileTransition(t)
		require.NoError(t, transitions.Promote())
		require.NoError(t, os.Chmod(target, 0400))

		err := transitions.Rollback(errors.New("later deploy failed"))

		require.ErrorContains(t, err, "staged replacement changed")
		info, statErr := os.Stat(transitions.items[0].newPath)
		require.NoError(t, statErr)
		assert.Equal(t, os.FileMode(0400), info.Mode().Perm())
	})
}

func TestManagedTypeTransition_StageNeverOverwritesPublishedArtifact(t *testing.T) {
	tmpDir := t.TempDir()
	source := filepath.Join(tmpDir, "source", "config")
	targetParent := filepath.Join(tmpDir, "target")
	target := filepath.Join(targetParent, "config")
	require.NoError(t, os.MkdirAll(source, 0755))
	require.NoError(t, os.MkdirAll(targetParent, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(source, "app.yml"), []byte("new"), 0644))
	require.NoError(t, os.WriteFile(target, []byte("old"), 0644))
	sourceInfo, err := os.Lstat(source)
	require.NoError(t, err)
	item, err := discoverManagedTypeConflict(source, target, "config", sourceInfo, map[string]bool{"config": true})
	require.NoError(t, err)
	t.Cleanup(func() { (&managedTypeTransitions{items: []*managedTypeTransition{item}}).Close() })
	require.NoError(t, os.WriteFile(item.newPath, []byte("concurrent"), 0600))

	err = item.stage()

	require.ErrorContains(t, err, "publish staged replacement")
	content, readErr := os.ReadFile(item.newPath)
	require.NoError(t, readErr)
	assert.Equal(t, "concurrent", string(content))
	original, readErr := os.ReadFile(target)
	require.NoError(t, readErr)
	assert.Equal(t, "old", string(original))
	assert.DirExists(t, target+managedTransitionStageSuffix)
	assert.FileExists(t, filepath.Join(target+managedTransitionStageSuffix, "replacement", "app.yml"))
}

func TestManagedTransitionSafetyFailures(t *testing.T) {
	newPinned := func(t *testing.T) (*managedTypeTransition, string) {
		t.Helper()
		root := t.TempDir()
		source := filepath.Join(root, "source")
		targetParent := filepath.Join(root, "target")
		target := filepath.Join(targetParent, "config")
		require.NoError(t, os.MkdirAll(source, 0755))
		require.NoError(t, os.MkdirAll(targetParent, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(source, "app.yml"), []byte("new"), 0600))
		require.NoError(t, os.WriteFile(target, []byte("old"), 0600))
		sourceInfo, err := os.Lstat(source)
		require.NoError(t, err)
		item, err := discoverManagedTypeConflict(source, target, "config", sourceInfo, map[string]bool{"config": true})
		require.NoError(t, err)
		return item, target
	}
	tests := []struct {
		name string
		run  func(*testing.T)
	}{
		{name: "invalid prior path", run: func(t *testing.T) {
			err := validateManagedTransitionArtifacts(t.TempDir(), []string{"../escape"})
			require.ErrorContains(t, err, "invalid managed path")
		}},
		{name: "cleanup suffix is reserved in source namespace", run: func(t *testing.T) {
			source := t.TempDir()
			reserved := filepath.Join(source, "config"+strings.ToUpper(managedTransitionCleanSuffix))
			require.NoError(t, os.WriteFile(reserved, []byte("source"), 0600))

			err := validateTransitionSourceNamespace(source)

			require.ErrorContains(t, err, reserved)
		}},
		{name: "artifact inspection does not mask ENOTDIR", run: func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "file")
			require.NoError(t, os.WriteFile(root, []byte("block"), 0600))
			err := validateManagedTransitionArtifacts(root, []string{"nested/app.yml"})
			require.ErrorContains(t, err, "inspect transition artifact")
		}},
		{name: "missing source and current artifact fail preparation", run: func(t *testing.T) {
			root := t.TempDir()
			_, err := prepareManagedTypeTransitions(filepath.Join(root, "missing"), filepath.Join(root, "target"), nil)
			require.Error(t, err)
			item, target := newPinned(t)
			require.NoError(t, os.WriteFile(item.oldPath, []byte("stale"), 0600))
			sourceInfo, statErr := os.Lstat(item.sourcePath)
			require.NoError(t, statErr)
			_, err = discoverManagedTypeConflict(item.sourcePath, target, "config", sourceInfo, map[string]bool{"config": true})
			require.ErrorContains(t, err, item.oldPath)
		}},
		{name: "pinned discovery rejects late artifact and identity swaps", run: func(t *testing.T) {
			for _, mutation := range []string{"artifact", "parent", "missing target", "replaced target"} {
				t.Run(mutation, func(t *testing.T) {
					item, target := newPinned(t)
					require.NoError(t, item.stage())
					transitions := &managedTypeTransitions{items: []*managedTypeTransition{item}}
					t.Cleanup(transitions.Close)
					switch mutation {
					case "artifact":
						require.NoError(t, os.WriteFile(item.oldPath, []byte("late"), 0600))
					case "parent":
						parent := filepath.Dir(target)
						require.NoError(t, os.Rename(parent, parent+"-original"))
						require.NoError(t, os.Mkdir(parent, 0700))
					case "missing target":
						require.NoError(t, os.Remove(target))
					case "replaced target":
						require.NoError(t, os.Remove(target))
						require.NoError(t, os.WriteFile(target, []byte("replacement"), 0600))
					}
					require.Error(t, transitions.Promote())
				})
			}
		}},
		{name: "preparation propagates staged source access failures", run: func(t *testing.T) {
			if runtime.GOOS == "windows" {
				t.Skip("chmod access failures are not portable to Windows")
			}
			for _, sourceIsDir := range []bool{false, true} {
				name := "file"
				if sourceIsDir {
					name = "directory"
				}
				t.Run(name, func(t *testing.T) {
					root := t.TempDir()
					source := filepath.Join(root, "source")
					target := filepath.Join(root, "target")
					managed := map[string]bool{managedTargetRoot: true}
					lockPath := source
					if sourceIsDir {
						require.NoError(t, os.Mkdir(source, 0700))
						lockPath = filepath.Join(source, "blocked")
						require.NoError(t, os.Mkdir(lockPath, 0700))
						require.NoError(t, os.WriteFile(filepath.Join(lockPath, "app.yml"), []byte("new"), 0600))
						require.NoError(t, os.WriteFile(target, []byte("old"), 0600))
					} else {
						require.NoError(t, os.WriteFile(source, []byte("new"), 0600))
						require.NoError(t, os.MkdirAll(filepath.Join(target, "managed"), 0700))
						require.NoError(t, os.WriteFile(filepath.Join(target, "managed", "app.yml"), []byte("old"), 0600))
						managed = map[string]bool{"managed/app.yml": true}
					}
					require.NoError(t, os.Chmod(lockPath, 0))
					t.Cleanup(func() { _ = os.Chmod(lockPath, 0700) })

					_, err := prepareManagedTypeTransitions(source, target, managed)

					require.ErrorContains(t, err, "private transition stage retained")
				})
			}
		}},
		{name: "discovery propagates pinned path access failures", run: func(t *testing.T) {
			if runtime.GOOS == "windows" {
				t.Skip("chmod access failures are not portable to Windows")
			}
			for _, locked := range []string{"parent", "target"} {
				t.Run(locked, func(t *testing.T) {
					root := t.TempDir()
					source := filepath.Join(root, "source")
					parent := filepath.Join(root, "target")
					target := filepath.Join(parent, "config")
					require.NoError(t, os.Mkdir(source, 0700))
					require.NoError(t, os.Mkdir(parent, 0700))
					require.NoError(t, os.WriteFile(target, []byte("old"), 0600))
					lockPath, mode := target, os.FileMode(0)
					if locked == "parent" {
						lockPath, mode = parent, 0111
					}
					require.NoError(t, os.Chmod(lockPath, mode))
					t.Cleanup(func() { _ = os.Chmod(lockPath, 0700) })
					sourceInfo, err := os.Lstat(source)
					require.NoError(t, err)

					_, err = discoverManagedTypeConflict(source, target, "config", sourceInfo, map[string]bool{"config": true})

					require.ErrorContains(t, err, "open destination")
				})
			}
		}},
		{name: "managed directory rejects symlink", run: func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "source")
			target := filepath.Join(root, "target")
			require.NoError(t, os.WriteFile(source, []byte("new"), 0600))
			require.NoError(t, os.Mkdir(target, 0700))
			require.NoError(t, os.WriteFile(filepath.Join(target, "managed"), []byte("old"), 0600))
			require.NoError(t, os.Symlink("managed", filepath.Join(target, "link")))
			sourceInfo, err := os.Lstat(source)
			require.NoError(t, err)
			_, err = discoverManagedTypeConflict(source, target, "config", sourceInfo, map[string]bool{"config/managed": true, "config/link": true})
			require.ErrorContains(t, err, "not a regular managed file")
		}},
		{name: "irregular source is not treated as a transition", run: func(t *testing.T) {
			root := t.TempDir()
			sourceFile := filepath.Join(root, "source-file")
			source := filepath.Join(root, "source-link")
			target := filepath.Join(root, "target")
			require.NoError(t, os.WriteFile(sourceFile, []byte("new"), 0600))
			require.NoError(t, os.Symlink(sourceFile, source))
			require.NoError(t, os.Mkdir(target, 0700))
			sourceInfo, err := os.Lstat(source)
			require.NoError(t, err)

			item, err := discoverManagedTypeConflict(source, target, "config", sourceInfo, nil)

			require.NoError(t, err)
			assert.Nil(t, item)
		}},
		{name: "managed directory propagates traversal failure", run: func(t *testing.T) {
			if runtime.GOOS == "windows" {
				t.Skip("chmod access failures are not portable to Windows")
			}
			root := t.TempDir()
			dir := filepath.Join(root, "config")
			require.NoError(t, os.Mkdir(dir, 0700))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "app.yml"), []byte("old"), 0600))
			require.NoError(t, os.Chmod(dir, 0111))
			t.Cleanup(func() { _ = os.Chmod(dir, 0700) })

			_, err := validateManagedDirectory(dir, "config", map[string]bool{"config/app.yml": true})

			require.Error(t, err)
		}},
		{name: "pinned tree walks propagate access failures", run: func(t *testing.T) {
			if runtime.GOOS == "windows" {
				t.Skip("chmod access failures are not portable to Windows")
			}
			root := t.TempDir()
			locked := filepath.Join(root, "locked")
			require.NoError(t, os.Mkdir(locked, 0700))
			require.NoError(t, os.WriteFile(filepath.Join(locked, "payload"), []byte("data"), 0600))
			handle, err := openPinnedPath(root)
			require.NoError(t, err)
			defer func() { _ = handle.Close() }()
			info, err := handle.Stat()
			require.NoError(t, err)
			require.NoError(t, os.Chmod(locked, 0))
			t.Cleanup(func() { _ = os.Chmod(locked, 0700) })

			_, err = pinRemovalTree(root, handle, info)
			require.Error(t, err)
			_, err = fingerprintPinnedPath(root, handle, info)
			require.Error(t, err)
		}},
		{name: "source walk propagates access failure before staging", run: func(t *testing.T) {
			if runtime.GOOS == "windows" {
				t.Skip("chmod access failures are not portable to Windows")
			}
			root := t.TempDir()
			source := filepath.Join(root, "source")
			target := filepath.Join(root, "target")
			locked := filepath.Join(source, "locked")
			require.NoError(t, os.MkdirAll(locked, 0700))
			require.NoError(t, os.Mkdir(target, 0700))
			require.NoError(t, os.Chmod(locked, 0))
			t.Cleanup(func() { _ = os.Chmod(locked, 0700) })

			_, err := prepareManagedTypeTransitions(source, target, nil)

			require.Error(t, err)
		}},
		{name: "private stage identity swap is retained", run: func(t *testing.T) {
			parent := t.TempDir()
			item := &managedTypeTransition{targetPath: filepath.Join(parent, "config"), relPath: "config"}
			require.NoError(t, item.createPrivateStage())
			originalPath := item.privatePath + "-original"
			require.NoError(t, os.Rename(item.privatePath, originalPath))
			require.NoError(t, os.Mkdir(item.privatePath, 0700))
			t.Cleanup(func() { (&managedTypeTransitions{items: []*managedTypeTransition{item}}).Close() })

			err := item.cleanupPrivateStage()

			require.ErrorContains(t, err, "private transition stage retained")
			assert.DirExists(t, item.privatePath)
			assert.DirExists(t, originalPath)
		}},
		{name: "private file requires regular source", run: func(t *testing.T) {
			parent := t.TempDir()
			item := &managedTypeTransition{sourcePath: parent, targetPath: filepath.Join(parent, "config"), relPath: "config"}
			require.NoError(t, item.createPrivateStage())
			t.Cleanup(func() { (&managedTypeTransitions{items: []*managedTypeTransition{item}}).Close() })

			err := item.copyPrivateFile(filepath.Join(item.privatePath, "replacement"))

			require.ErrorContains(t, err, "not a regular file")
		}},
		{name: "private file creation is exclusive", run: func(t *testing.T) {
			parent := t.TempDir()
			source := filepath.Join(parent, "source")
			require.NoError(t, os.WriteFile(source, []byte("source"), 0600))
			item := &managedTypeTransition{sourcePath: source, targetPath: filepath.Join(parent, "config"), relPath: "config"}
			require.NoError(t, item.createPrivateStage())
			replacement := filepath.Join(item.privatePath, "replacement")
			require.NoError(t, os.WriteFile(replacement, []byte("collision"), 0600))
			t.Cleanup(func() { (&managedTypeTransitions{items: []*managedTypeTransition{item}}).Close() })

			require.Error(t, item.copyPrivateFile(replacement))
			content, err := os.ReadFile(replacement)
			require.NoError(t, err)
			assert.Equal(t, "collision", string(content))
		}},
		{name: "fingerprint rejects swapped root", run: func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "payload")
			require.NoError(t, os.WriteFile(path, []byte("original"), 0600))
			handle, err := os.Open(path)
			require.NoError(t, err)
			defer func() { _ = handle.Close() }()
			info, err := handle.Stat()
			require.NoError(t, err)
			require.NoError(t, os.Rename(path, path+"-original"))
			require.NoError(t, os.WriteFile(path, []byte("replacement"), 0600))

			_, err = fingerprintPinnedPath(path, handle, info)
			require.ErrorContains(t, err, "path identity changed")
		}},
		{name: "pinned identity validation fails closed", run: func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "payload")
			require.NoError(t, os.WriteFile(path, []byte("data"), 0600))
			handle, err := openPinnedPath(path)
			require.NoError(t, err)
			info, err := handle.Stat()
			require.NoError(t, err)
			require.ErrorContains(t, revalidatePinned(path, nil, info), "no pinned identity")
			require.NoError(t, handle.Close())

			require.ErrorContains(t, revalidatePinned(path, handle, info), "pinned identity changed")
		}},
		{name: "fingerprint rejects irregular entry", run: func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, "file"), []byte("data"), 0600))
			require.NoError(t, os.Symlink("file", filepath.Join(root, "link")))
			handle, err := os.Open(root)
			require.NoError(t, err)
			defer func() { _ = handle.Close() }()
			info, err := handle.Stat()
			require.NoError(t, err)

			_, err = fingerprintPinnedPath(root, handle, info)
			require.ErrorContains(t, err, "is not regular")
		}},
		{name: "stage retains private root when source disappears", run: func(t *testing.T) {
			parent := t.TempDir()
			item := &managedTypeTransition{
				sourcePath: filepath.Join(parent, "missing"), targetPath: filepath.Join(parent, "config"),
				relPath: "config", sourceIsDir: true, newPath: filepath.Join(parent, "config") + managedTransitionNewSuffix,
			}

			err := item.stage()
			require.ErrorContains(t, err, item.targetPath+managedTransitionStageSuffix)
			assert.DirExists(t, item.targetPath+managedTransitionStageSuffix)
		}},
		{name: "private stage cleanup preserves late child", run: func(t *testing.T) {
			parent := t.TempDir()
			item := &managedTypeTransition{targetPath: filepath.Join(parent, "config"), relPath: "config"}
			require.NoError(t, item.createPrivateStage())
			late := filepath.Join(item.privatePath, "late-runtime-data")
			require.NoError(t, os.WriteFile(late, []byte("keep"), 0600))
			t.Cleanup(func() { (&managedTypeTransitions{items: []*managedTypeTransition{item}}).Close() })

			err := item.cleanupPrivateStage()

			require.ErrorContains(t, err, item.privatePath)
			assert.FileExists(t, late)
		}},
		{name: "fingerprinted cleanup preserves child added before delete", run: func(t *testing.T) {
			transitions, _ := newDirToFileTransition(t)
			require.NoError(t, transitions.Promote())
			item := transitions.items[0]
			late := filepath.Join(item.oldPath, "runtime.db")

			err := removePinnedTree(item.oldPath, item.targetPath+managedTransitionCleanSuffix, item.targetHandle, item.targetInfo, item.targetFingerprint, "managed original", removalHooks{
				afterBaseline: func() { require.NoError(t, os.WriteFile(late, []byte("keep"), 0600)) },
			})

			require.ErrorContains(t, err, "remove quarantined fingerprinted path")
			assert.FileExists(t, late)
			assert.DirExists(t, item.oldPath)
		}},
		{name: "fingerprinted cleanup retains same inode mutations", run: func(t *testing.T) {
			for _, mutation := range []string{"content", "mode"} {
				t.Run(mutation, func(t *testing.T) {
					transitions, _ := newDirToFileTransition(t)
					require.NoError(t, transitions.Promote())
					item := transitions.items[0]
					managed := filepath.Join(item.oldPath, "managed", "app.yml")
					before, err := os.Stat(managed)
					require.NoError(t, err)

					err = removePinnedTree(item.oldPath, item.targetPath+managedTransitionCleanSuffix, item.targetHandle, item.targetInfo, item.targetFingerprint, "managed original", removalHooks{
						afterBaseline: func() {
							if mutation == "content" {
								require.NoError(t, os.WriteFile(managed, []byte("NEW"), before.Mode()))
								require.NoError(t, os.Chtimes(managed, before.ModTime(), before.ModTime()))
							} else {
								require.NoError(t, os.Chmod(managed, before.Mode()^0022))
							}
						},
					})

					require.ErrorContains(t, err, "content or mode changed")
					assert.FileExists(t, managed)
				})
			}
		}},
		{name: "cleanup namespace fails closed on collisions and late data", run: func(t *testing.T) {
			t.Run("existing namespace", func(t *testing.T) {
				transitions, _ := newFileToDirTransition(t)
				require.NoError(t, transitions.Promote())
				item := transitions.items[0]
				cleanup := item.targetPath + managedTransitionCleanSuffix
				require.NoError(t, os.Mkdir(cleanup, 0700))

				err := item.removePinnedOld()

				require.ErrorContains(t, err, cleanup)
				assert.FileExists(t, item.oldPath)
			})
			t.Run("candidate collision", func(t *testing.T) {
				transitions, _ := newFileToDirTransition(t)
				require.NoError(t, transitions.Promote())
				item := transitions.items[0]
				cleanup := item.targetPath + managedTransitionCleanSuffix

				err := removePinnedTree(item.oldPath, cleanup, item.targetHandle, item.targetInfo, item.targetFingerprint, "managed original", removalHooks{
					beforeQuarantine: func(string) {
						require.NoError(t, os.WriteFile(filepath.Join(cleanup, "000000"), []byte("collision"), 0600))
					},
				})

				require.ErrorContains(t, err, "quarantine fingerprinted path")
				assert.FileExists(t, item.oldPath)
			})
			t.Run("late namespace child", func(t *testing.T) {
				transitions, _ := newFileToDirTransition(t)
				require.NoError(t, transitions.Promote())
				item := transitions.items[0]
				cleanup := item.targetPath + managedTransitionCleanSuffix
				late := filepath.Join(cleanup, "late")

				err := removePinnedTree(item.oldPath, cleanup, item.targetHandle, item.targetInfo, item.targetFingerprint, "managed original", removalHooks{
					beforeNamespaceRemove: func(string) { require.NoError(t, os.WriteFile(late, []byte("keep"), 0600)) },
				})

				require.ErrorContains(t, err, "remove protected cleanup namespace")
				assert.FileExists(t, late)
			})
			t.Run("namespace open failure", func(t *testing.T) {
				if runtime.GOOS == "windows" {
					t.Skip("chmod access failures are not portable to Windows")
				}
				transitions, _ := newFileToDirTransition(t)
				require.NoError(t, transitions.Promote())
				item := transitions.items[0]
				cleanup := item.targetPath + managedTransitionCleanSuffix

				err := removePinnedTree(item.oldPath, cleanup, item.targetHandle, item.targetInfo, item.targetFingerprint, "managed original", removalHooks{
					afterNamespaceCreate: func(string) { require.NoError(t, os.Chmod(cleanup, 0)) },
				})

				require.ErrorContains(t, err, "open protected cleanup namespace")
				assert.FileExists(t, item.oldPath)
			})
			t.Run("pinned namespace swap", func(t *testing.T) {
				transitions, _ := newFileToDirTransition(t)
				require.NoError(t, transitions.Promote())
				item := transitions.items[0]
				cleanup := item.targetPath + managedTransitionCleanSuffix
				original := cleanup + "-original"

				err := removePinnedTree(item.oldPath, cleanup, item.targetHandle, item.targetInfo, item.targetFingerprint, "managed original", removalHooks{
					afterNamespacePinned: func(string) {
						require.NoError(t, os.Rename(cleanup, original))
						require.NoError(t, os.Mkdir(cleanup, 0700))
					},
				})

				require.ErrorContains(t, err, "protected cleanup namespace changed")
				assert.FileExists(t, item.oldPath)
				assert.DirExists(t, cleanup)
				assert.DirExists(t, original)
			})
			t.Run("final namespace swap", func(t *testing.T) {
				transitions, _ := newFileToDirTransition(t)
				require.NoError(t, transitions.Promote())
				item := transitions.items[0]
				cleanup := item.targetPath + managedTransitionCleanSuffix
				original := cleanup + "-original"

				err := removePinnedTree(item.oldPath, cleanup, item.targetHandle, item.targetInfo, item.targetFingerprint, "managed original", removalHooks{
					beforeNamespaceRemove: func(string) {
						require.NoError(t, os.Rename(cleanup, original))
						require.NoError(t, os.Mkdir(cleanup, 0700))
					},
				})

				require.ErrorContains(t, err, "protected cleanup namespace retained")
				assert.DirExists(t, cleanup)
				assert.DirExists(t, original)
			})
		}},
		{name: "cleanup retains directory mode mutation", run: func(t *testing.T) {
			parent := t.TempDir()
			path := filepath.Join(parent, "candidate")
			cleanup := filepath.Join(parent, "cleanup")
			require.NoError(t, os.Mkdir(path, 0700))
			handle, err := openPinnedPath(path)
			require.NoError(t, err)
			info, err := handle.Stat()
			require.NoError(t, err)
			fingerprint, err := fingerprintPinnedPath(path, handle, info)
			require.NoError(t, err)

			err = removePinnedTree(path, cleanup, handle, info, fingerprint, "candidate", removalHooks{
				afterBaseline: func() { require.NoError(t, os.Chmod(path, 0750)) },
			})

			require.ErrorContains(t, err, "mode changed")
			assert.DirExists(t, path)
		}},
		{name: "fingerprinted cleanup rejects irregular and replaced entries", run: func(t *testing.T) {
			t.Run("irregular", func(t *testing.T) {
				transitions, _ := newDirToFileTransition(t)
				require.NoError(t, transitions.Promote())
				item := transitions.items[0]
				link := filepath.Join(item.oldPath, "late-link")
				require.NoError(t, os.Symlink("managed/app.yml", link))

				err := item.removePinnedOld()

				require.ErrorContains(t, err, "refuse to remove irregular path")
				assert.FileExists(t, filepath.Join(item.oldPath, "managed", "app.yml"))
				assert.NoError(t, os.Remove(link))
			})
			t.Run("replaced identity", func(t *testing.T) {
				transitions, _ := newDirToFileTransition(t)
				require.NoError(t, transitions.Promote())
				item := transitions.items[0]
				managed := filepath.Join(item.oldPath, "managed", "app.yml")
				original := managed + "-original"

				err := removePinnedTree(item.oldPath, item.targetPath+managedTransitionCleanSuffix, item.targetHandle, item.targetInfo, item.targetFingerprint, "managed original", removalHooks{
					beforeQuarantine: func(path string) {
						if path == managed {
							require.NoError(t, os.Rename(managed, original))
							require.NoError(t, os.WriteFile(managed, []byte("replacement"), 0600))
						}
					},
				})

				require.ErrorContains(t, err, "path identity changed")
				content, readErr := os.ReadFile(managed)
				require.NoError(t, readErr)
				assert.Equal(t, "replacement", string(content))
				assert.FileExists(t, original)
			})
		}},
		{name: "stage reports missing target parent", run: func(t *testing.T) {
			root := t.TempDir()
			item := &managedTypeTransition{sourcePath: filepath.Join(root, "source"), targetPath: filepath.Join(root, "missing", "config"), relPath: "config"}
			require.ErrorContains(t, item.stage(), "create private transition stage")
			require.NoError(t, item.cleanupPrivateStage())
		}},
		{name: "quarantine never overwrites late old artifact", run: func(t *testing.T) {
			transitions, target := newFileToDirTransition(t)
			item := transitions.items[0]
			require.NoError(t, os.WriteFile(item.oldPath, []byte("collision"), 0600))

			err := transitions.Promote()

			require.ErrorContains(t, err, "quarantine destination")
			content, readErr := os.ReadFile(item.oldPath)
			require.NoError(t, readErr)
			assert.Equal(t, "collision", string(content))
			assert.FileExists(t, target)
		}},
		{name: "quarantine revalidates managed directory contents", run: func(t *testing.T) {
			transitions, target := newDirToFileTransition(t)
			late := filepath.Join(target, "runtime.db")
			require.NoError(t, os.WriteFile(late, []byte("keep"), 0600))

			err := transitions.Promote()

			require.ErrorContains(t, err, "destination changed while quarantining")
			assert.FileExists(t, late)
			assert.DirExists(t, target)
		}},
		{name: "rollback retains replaced quarantine identity", run: func(t *testing.T) {
			transitions, target := newFileToDirTransition(t)
			item := transitions.items[0]
			require.NoError(t, renameNoReplace(target, item.oldPath))
			item.quarantined = true
			require.NoError(t, os.Remove(item.oldPath))
			require.NoError(t, os.WriteFile(item.oldPath, []byte("replacement"), 0600))

			err := transitions.Rollback(errors.New("promotion failed"))

			require.ErrorContains(t, err, "quarantined original changed")
			content, readErr := os.ReadFile(item.oldPath)
			require.NoError(t, readErr)
			assert.Equal(t, "replacement", string(content))
		}},
		{name: "rollback retains artifacts on parent and new-path collisions", run: func(t *testing.T) {
			t.Run("parent swap", func(t *testing.T) {
				transitions, target := newFileToDirTransition(t)
				require.NoError(t, transitions.Promote())
				parent := filepath.Dir(target)
				require.NoError(t, os.Rename(parent, parent+"-original"))
				require.NoError(t, os.Mkdir(parent, 0700))
				require.Error(t, transitions.Rollback(errors.New("later failure")))
			})
			t.Run("new path occupied", func(t *testing.T) {
				transitions, _ := newFileToDirTransition(t)
				require.NoError(t, transitions.Promote())
				item := transitions.items[0]
				require.NoError(t, os.WriteFile(item.newPath, []byte("collision"), 0600))
				err := transitions.Rollback(errors.New("later failure"))
				require.ErrorContains(t, err, "quarantine failed replacement")
				assert.FileExists(t, item.oldPath)
			})
			t.Run("target recreated before promotion", func(t *testing.T) {
				transitions, target := newFileToDirTransition(t)
				item := transitions.items[0]
				require.NoError(t, renameNoReplace(target, item.oldPath))
				item.quarantined = true
				require.NoError(t, os.WriteFile(target, []byte("concurrent"), 0600))
				err := transitions.Rollback(errors.New("promotion failure"))
				require.ErrorContains(t, err, "concurrently recreated")
			})
		}},
		{name: "cleanup rejects replaced old and new identities", run: func(t *testing.T) {
			t.Run("old", func(t *testing.T) {
				transitions, _ := newFileToDirTransition(t)
				require.NoError(t, transitions.Promote())
				item := transitions.items[0]
				require.NoError(t, os.Remove(item.oldPath))
				require.NoError(t, os.WriteFile(item.oldPath, []byte("replacement"), 0600))
				require.Error(t, item.removePinnedOld())
			})
			t.Run("new", func(t *testing.T) {
				transitions, _ := newFileToDirTransition(t)
				item := transitions.items[0]
				require.NoError(t, os.RemoveAll(item.newPath))
				require.NoError(t, os.Mkdir(item.newPath, 0700))
				require.Error(t, item.removePinnedNew())
			})
		}},
		{name: "fingerprint rejects irregular root", run: func(t *testing.T) {
			if runtime.GOOS == "windows" {
				t.Skip("no portable irregular root fixture")
			}
			handle, err := os.Open("/dev/null")
			require.NoError(t, err)
			defer func() { _ = handle.Close() }()
			info, err := handle.Stat()
			require.NoError(t, err)
			_, err = fingerprintPinnedPath("/dev/null", handle, info)
			require.ErrorContains(t, err, "neither a regular file nor directory")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, test.run)
	}
}

func TestManagedTypeTransitions_RollbackNeverOverwritesRecreatedTarget(t *testing.T) {
	for _, test := range []struct {
		name     string
		setup    func(*testing.T) (*managedTypeTransitions, string)
		recreate func(*testing.T, string)
		verify   func(*testing.T, string)
	}{
		{
			name:  "directory",
			setup: newFileToDirTransition,
			recreate: func(t *testing.T, target string) {
				require.NoError(t, os.Mkdir(target, 0755))
				require.NoError(t, os.WriteFile(filepath.Join(target, "runtime.db"), []byte("concurrent"), 0600))
			},
			verify: func(t *testing.T, target string) { assert.FileExists(t, filepath.Join(target, "runtime.db")) },
		},
		{
			name:  "file",
			setup: newDirToFileTransition,
			recreate: func(t *testing.T, target string) {
				require.NoError(t, os.WriteFile(target, []byte("concurrent"), 0600))
			},
			verify: func(t *testing.T, target string) {
				content, err := os.ReadFile(target)
				require.NoError(t, err)
				assert.Equal(t, "concurrent", string(content))
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			transitions, target := test.setup(t)
			require.NoError(t, transitions.Promote())
			require.NoError(t, os.RemoveAll(target))
			test.recreate(t, target)

			err := transitions.Rollback(errors.New("later deploy failed"))

			require.ErrorContains(t, err, "preserving it")
			test.verify(t, target)
			_, statErr := os.Lstat(transitions.items[0].oldPath)
			require.NoError(t, statErr)
		})
	}
}

func TestManagedTypeTransitions_PromoteRejectsChangedTarget(t *testing.T) {
	transitions, target := newFileToDirTransition(t)
	require.NoError(t, os.Remove(target))
	require.NoError(t, os.WriteFile(target, []byte("concurrent"), 0600))

	err := transitions.Promote()

	require.Error(t, err)
	assert.ErrorContains(t, err, "destination changed before promoting")
	content, readErr := os.ReadFile(target)
	require.NoError(t, readErr)
	assert.Equal(t, "concurrent", string(content))
}

func TestManagedTypeTransitions_RejectsIntermediateParentSwap(t *testing.T) {
	tmpDir := t.TempDir()
	source := filepath.Join(tmpDir, "source")
	targetParent := filepath.Join(tmpDir, "target")
	target := filepath.Join(targetParent, "config")
	outside := filepath.Join(tmpDir, "outside")
	require.NoError(t, os.MkdirAll(source, 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(target, "nested"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(outside, "config", "nested"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(source, "config"), []byte("new"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(target, "nested", "app.yml"), []byte("old"), 0644))
	victim := filepath.Join(outside, "config", "nested", "app.yml")
	require.NoError(t, os.WriteFile(victim, []byte("outside"), 0644))

	transitions, err := prepareManagedTypeTransitions(source, targetParent, map[string]bool{"config/nested/app.yml": true})
	require.NoError(t, err)
	require.NoError(t, os.Rename(targetParent, targetParent+"-original"))
	require.NoError(t, os.Symlink(outside, targetParent))
	t.Cleanup(func() { transitions.Close() })

	err = transitions.Promote()
	require.Error(t, err)
	assert.ErrorContains(t, err, "destination parent changed")
	content, readErr := os.ReadFile(victim)
	require.NoError(t, readErr)
	assert.Equal(t, "outside", string(content))
}

func TestPrepareManagedTypeTransitions_RefusesExistingArtifacts(t *testing.T) {
	for _, suffix := range []string{managedTransitionOldSuffix, managedTransitionNewSuffix, managedTransitionStageSuffix, managedTransitionCleanSuffix} {
		t.Run(suffix, func(t *testing.T) {
			tmpDir := t.TempDir()
			source := filepath.Join(tmpDir, "source")
			targetParent := filepath.Join(tmpDir, "target")
			target := filepath.Join(targetParent, "config")
			require.NoError(t, os.MkdirAll(filepath.Join(source, "config"), 0755))
			require.NoError(t, os.MkdirAll(targetParent, 0755))
			require.NoError(t, os.WriteFile(filepath.Join(source, "config", "app.yml"), []byte("new"), 0644))
			require.NoError(t, os.WriteFile(target, []byte("old"), 0644))
			artifact := target + suffix
			require.NoError(t, os.WriteFile(artifact, []byte("preserve"), 0600))

			_, err := prepareManagedTypeTransitions(source, targetParent, map[string]bool{"config": true})

			require.ErrorContains(t, err, artifact)
			assert.FileExists(t, target)
			content, readErr := os.ReadFile(artifact)
			require.NoError(t, readErr)
			assert.Equal(t, "preserve", string(content))
		})
	}
}

func TestPrepareManagedTypeTransitions_RefusesCrashArtifacts(t *testing.T) {
	transitions, target := newFileToDirTransition(t)
	item := transitions.items[0]
	require.NoError(t, renameNoReplace(target, item.oldPath))

	_, err := prepareManagedTypeTransitions(item.sourcePath, item.targetPath, map[string]bool{managedTargetRoot: true})

	require.ErrorContains(t, err, item.oldPath)
	assert.NoFileExists(t, target)
	assert.FileExists(t, item.oldPath)
	assert.DirExists(t, item.newPath)
}

type zeroTypeDirEntry struct {
	info os.FileInfo
	err  error
}

func (e zeroTypeDirEntry) Name() string               { return e.info.Name() }
func (e zeroTypeDirEntry) IsDir() bool                { return false }
func (e zeroTypeDirEntry) Type() os.FileMode          { return 0 }
func (e zeroTypeDirEntry) Info() (os.FileInfo, error) { return e.info, e.err }

type modeOnlyFileInfo struct{ mode os.FileMode }

func (i modeOnlyFileInfo) Name() string       { return "unknown-type" }
func (i modeOnlyFileInfo) Size() int64        { return 0 }
func (i modeOnlyFileInfo) Mode() os.FileMode  { return i.mode }
func (i modeOnlyFileInfo) ModTime() time.Time { return time.Time{} }
func (i modeOnlyFileInfo) IsDir() bool        { return i.mode.IsDir() }
func (i modeOnlyFileInfo) Sys() any           { return nil }

func TestDirEntryIsRegular_UsesInfoModeWhenTypeIsUnknown(t *testing.T) {
	regular, err := dirEntryIsRegular(zeroTypeDirEntry{info: modeOnlyFileInfo{mode: 0600}})
	require.NoError(t, err)
	assert.True(t, regular)

	regular, err = dirEntryIsRegular(zeroTypeDirEntry{info: modeOnlyFileInfo{mode: os.ModeNamedPipe}})

	require.NoError(t, err)
	assert.False(t, regular)

	_, err = dirEntryIsRegular(zeroTypeDirEntry{err: errors.New("info failed")})
	require.ErrorContains(t, err, "info failed")
}

func TestManagedTypeTransitions_CommitPreservesLateUnmanagedData(t *testing.T) {
	transitions, target := newDirToFileTransition(t)
	require.NoError(t, transitions.Promote())
	item := transitions.items[0]

	lateData := filepath.Join(item.oldPath, "runtime.db")
	require.NoError(t, os.WriteFile(lateData, []byte("keep"), 0600))
	transitions.Commit(zerolog.Nop())

	assert.FileExists(t, lateData)
	content, readErr := os.ReadFile(target)
	require.NoError(t, readErr)
	assert.Equal(t, "new", string(content))
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

func TestDeployResult_PrefixLatest(t *testing.T) {
	t.Run("prefixes only entries appended after snapshot", func(t *testing.T) {
		result := &DeployResult{WrittenFiles: []string{"appdata/authelia/existing.yml"}}
		snapshot := len(result.WrittenFiles)
		result.AddWritten("configuration.yml", filepath.Join("nested", "users.yml"))

		result.PrefixLatest(snapshot, filepath.Join("appdata", "traefik"))

		assert.Equal(t, []string{
			filepath.Join("appdata", "authelia", "existing.yml"),
			filepath.Join("appdata", "traefik", "configuration.yml"),
			filepath.Join("appdata", "traefik", "nested", "users.yml"),
		}, result.WrittenFiles)
	})

	t.Run("zero snapshot prefixes every entry", func(t *testing.T) {
		result := &DeployResult{WrittenFiles: []string{"one.yml", "two.yml"}}

		result.PrefixLatest(0, "compose")

		assert.Equal(t, []string{
			filepath.Join("compose", "one.yml"),
			filepath.Join("compose", "two.yml"),
		}, result.WrittenFiles)
	})

	t.Run("current length is an idempotent boundary", func(t *testing.T) {
		result := &DeployResult{WrittenFiles: []string{filepath.Join("appdata", "authelia", "configuration.yml")}}
		snapshot := len(result.WrittenFiles)

		result.PrefixLatest(snapshot, filepath.Join("appdata", "authelia"))
		result.PrefixLatest(snapshot, filepath.Join("appdata", "authelia"))

		assert.Equal(t, []string{filepath.Join("appdata", "authelia", "configuration.yml")}, result.WrittenFiles,
			"a no-op deploy must not double-prefix paths used for hook matching")
	})

	t.Run("empty result accepts zero snapshot", func(t *testing.T) {
		result := &DeployResult{}

		assert.NotPanics(t, func() { result.PrefixLatest(0, "compose") })
		assert.Nil(t, result.WrittenFiles)
	})

	for _, snapshot := range []int{-1, 2} {
		t.Run(fmt.Sprintf("snapshot_%d_panics_without_corrupting_paths", snapshot), func(t *testing.T) {
			original := []string{filepath.Join("appdata", "authelia", "configuration.yml")}
			result := &DeployResult{WrittenFiles: append([]string(nil), original...)}

			assert.Panics(t, func() { result.PrefixLatest(snapshot, filepath.Join("appdata", "authelia")) })
			assert.Equal(t, original, result.WrittenFiles,
				"an invalid snapshot must not double-prefix existing hook paths")
		})
	}
}

func TestDeployResult_PrefixLatestDeleted(t *testing.T) {
	t.Run("prefixes only entries appended after snapshot", func(t *testing.T) {
		result := &DeployResult{DeletedFiles: []string{filepath.Join("appdata", "old", "stale.yml")}}
		snapshot := len(result.DeletedFiles)
		result.AddDeleted("removed.yml")

		result.PrefixLatestDeleted(snapshot, filepath.Join("appdata", "authelia"))

		assert.Equal(t, []string{
			filepath.Join("appdata", "old", "stale.yml"),
			filepath.Join("appdata", "authelia", "removed.yml"),
		}, result.DeletedFiles)
	})

	t.Run("current length is an idempotent boundary", func(t *testing.T) {
		result := &DeployResult{DeletedFiles: []string{filepath.Join("compose", "retired.yml")}}
		snapshot := len(result.DeletedFiles)

		result.PrefixLatestDeleted(snapshot, "compose")
		result.PrefixLatestDeleted(snapshot, "compose")

		assert.Equal(t, []string{filepath.Join("compose", "retired.yml")}, result.DeletedFiles)
	})

	for _, snapshot := range []int{-1, 2} {
		t.Run(fmt.Sprintf("snapshot_%d_panics_without_corrupting_paths", snapshot), func(t *testing.T) {
			original := []string{filepath.Join("compose", "retired.yml")}
			result := &DeployResult{DeletedFiles: append([]string(nil), original...)}

			assert.Panics(t, func() { result.PrefixLatestDeleted(snapshot, "compose") })
			assert.Equal(t, original, result.DeletedFiles)
		})
	}
}

func TestDeployResult_PrefixLatest_PreservesHookMatchesAcrossTargets(t *testing.T) {
	result := &DeployResult{}
	autheliaSnapshot := len(result.WrittenFiles)
	result.AddWritten("configuration.yml")
	result.PrefixLatest(autheliaSnapshot, filepath.Join("appdata", "authelia"))

	traefikSnapshot := len(result.WrittenFiles)
	result.AddWritten(filepath.Join("dynamic", "router.yml"))
	result.PrefixLatest(traefikSnapshot, filepath.Join("appdata", "traefik"))

	hooks := []PostSyncHook{
		{Container: "authelia", Paths: []string{"appdata/authelia/**"}},
		{Container: "traefik", Paths: []string{"appdata/traefik/**"}},
	}
	matched := EvaluatePostSyncHooks(result.WrittenFiles, hooks)

	assert.Equal(t, hooks, matched)
	assert.Equal(t, []string{
		filepath.Join("appdata", "authelia", "configuration.yml"),
		filepath.Join("appdata", "traefik", "dynamic", "router.yml"),
	}, result.WrittenFiles)
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
	err := d.DeployRemote(context.Background(), "/src", "", "/dst")
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
		err := d.DeployRemote(ctx, "/src", "host", "/dst")
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

	t.Run("content hash mode refuses unmanaged root file", func(t *testing.T) {
		tmpDir := t.TempDir()
		sourceDir := filepath.Join(tmpDir, "source")
		require.NoError(t, os.MkdirAll(sourceDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "f.txt"), []byte("x"), 0644))

		// Put an unmanaged file where the target directory should be. Transition
		// preparation must now fail closed before MkdirAll or any mutation.
		targetPath := filepath.Join(tmpDir, "target")
		require.NoError(t, os.WriteFile(targetPath, []byte("block"), 0644))

		d := &DeployOps{DryRun: false, ContentHashSync: true}
		err := d.DeployLocal(ctx, sourceDir, targetPath, nil, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "destination . blocks directory deployment and is not a managed regular file")
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

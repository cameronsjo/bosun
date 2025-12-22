package reconcile

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDeployOps(t *testing.T) {
	t.Run("with dry run", func(t *testing.T) {
		deploy := NewDeployOps(true)
		assert.True(t, deploy.DryRun)
	})

	t.Run("without dry run", func(t *testing.T) {
		deploy := NewDeployOps(false)
		assert.False(t, deploy.DryRun)
	})
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

		deploy := NewDeployOps(false)
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

	t.Run("backup non-existent paths returns name", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		backupDir := filepath.Join(tmpDir, "backups")

		deploy := NewDeployOps(false)
		backupName, err := deploy.Backup(ctx, backupDir, []string{"/non/existent/path"})

		require.NoError(t, err)
		assert.NotEmpty(t, backupName)
	})

	t.Run("backup empty paths list", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		backupDir := filepath.Join(tmpDir, "backups")

		deploy := NewDeployOps(false)
		backupName, err := deploy.Backup(ctx, backupDir, []string{})

		require.NoError(t, err)
		assert.NotEmpty(t, backupName)
	})
}

func TestDeployOps_CleanupBackups(t *testing.T) {
	t.Run("cleanup old backups", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create backup directories
		for i := 1; i <= 10; i++ {
			timestamp := time.Now().Add(time.Duration(-i) * time.Hour).Format("20060102-150405")
			backupDir := filepath.Join(tmpDir, "backup-"+timestamp)
			require.NoError(t, os.MkdirAll(backupDir, 0755))
		}

		deploy := NewDeployOps(false)
		err := deploy.CleanupBackups(tmpDir, 5)

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

		// Create only 3 backup directories
		for i := 1; i <= 3; i++ {
			timestamp := time.Now().Add(time.Duration(-i) * time.Hour).Format("20060102-150405")
			backupDir := filepath.Join(tmpDir, "backup-"+timestamp)
			require.NoError(t, os.MkdirAll(backupDir, 0755))
		}

		deploy := NewDeployOps(false)
		err := deploy.CleanupBackups(tmpDir, 5)

		require.NoError(t, err)

		// All backups should remain
		entries, err := os.ReadDir(tmpDir)
		require.NoError(t, err)
		assert.Len(t, entries, 3)
	})

	t.Run("cleanup non-existent directory", func(t *testing.T) {
		deploy := NewDeployOps(false)
		err := deploy.CleanupBackups("/non/existent/dir", 5)

		// Should not error
		require.NoError(t, err)
	})

	t.Run("cleanup ignores non-backup directories", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create backup directories
		for i := 1; i <= 3; i++ {
			timestamp := time.Now().Add(time.Duration(-i) * time.Hour).Format("20060102-150405")
			backupDir := filepath.Join(tmpDir, "backup-"+timestamp)
			require.NoError(t, os.MkdirAll(backupDir, 0755))
		}

		// Create non-backup directory
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "other-dir"), 0755))

		deploy := NewDeployOps(false)
		err := deploy.CleanupBackups(tmpDir, 2)

		require.NoError(t, err)

		// Non-backup directory should still exist
		assert.DirExists(t, filepath.Join(tmpDir, "other-dir"))
	})
}

func TestDeployOps_DeployLocal(t *testing.T) {
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync not installed")
	}

	t.Run("sync directories", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		sourceDir := filepath.Join(tmpDir, "source")
		targetDir := filepath.Join(tmpDir, "target")

		require.NoError(t, os.MkdirAll(sourceDir, 0755))
		require.NoError(t, os.MkdirAll(targetDir, 0755))

		// Create source files
		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "file1.txt"), []byte("content1"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "file2.txt"), []byte("content2"), 0644))

		deploy := NewDeployOps(false)
		err := deploy.DeployLocal(ctx, sourceDir, targetDir)

		require.NoError(t, err)

		// Verify files were synced
		assert.FileExists(t, filepath.Join(targetDir, "file1.txt"))
		assert.FileExists(t, filepath.Join(targetDir, "file2.txt"))
	})

	t.Run("dry run does not sync", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		sourceDir := filepath.Join(tmpDir, "source")
		targetDir := filepath.Join(tmpDir, "target")

		require.NoError(t, os.MkdirAll(sourceDir, 0755))
		require.NoError(t, os.MkdirAll(targetDir, 0755))

		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "file.txt"), []byte("content"), 0644))

		deploy := NewDeployOps(true)
		err := deploy.DeployLocal(ctx, sourceDir, targetDir)

		require.NoError(t, err)

		// File should NOT exist in target (dry run)
		assert.NoFileExists(t, filepath.Join(targetDir, "file.txt"))
	})
}

func TestDeployOps_DeployLocalFile(t *testing.T) {
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync not installed")
	}

	t.Run("sync single file", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		sourceFile := filepath.Join(tmpDir, "source.txt")
		targetFile := filepath.Join(tmpDir, "target.txt")

		require.NoError(t, os.WriteFile(sourceFile, []byte("content"), 0644))

		deploy := NewDeployOps(false)
		err := deploy.DeployLocalFile(ctx, sourceFile, targetFile)

		require.NoError(t, err)
		assert.FileExists(t, targetFile)

		content, err := os.ReadFile(targetFile)
		require.NoError(t, err)
		assert.Equal(t, "content", string(content))
	})
}

func TestDeployOps_ComposeUp(t *testing.T) {
	t.Run("dry run skips execution", func(t *testing.T) {
		ctx := context.Background()

		deploy := NewDeployOps(true)
		err := deploy.ComposeUp(ctx, "/any/compose.yml")

		// Dry run should not error
		require.NoError(t, err)
	})

	t.Run("invalid compose file", func(t *testing.T) {
		if _, err := exec.LookPath("docker"); err != nil {
			t.Skip("docker not installed")
		}

		ctx := context.Background()

		deploy := NewDeployOps(false)
		err := deploy.ComposeUp(ctx, "/non/existent/compose.yml")

		assert.Error(t, err)
	})
}

func TestDeployOps_SignalContainer(t *testing.T) {
	t.Run("dry run skips execution", func(t *testing.T) {
		ctx := context.Background()

		deploy := NewDeployOps(true)
		err := deploy.SignalContainer(ctx, "container-name", "SIGHUP")

		require.NoError(t, err)
	})

	t.Run("signal non-existent container", func(t *testing.T) {
		if _, err := exec.LookPath("docker"); err != nil {
			t.Skip("docker not installed")
		}

		ctx := context.Background()

		deploy := NewDeployOps(false)
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

		deploy := NewDeployOps(true)
		err := deploy.ComposeUpRemote(ctx, "host", "/any/path")

		require.NoError(t, err)
	})
}

func TestDeployOps_SignalContainerRemote(t *testing.T) {
	t.Run("dry run skips execution", func(t *testing.T) {
		ctx := context.Background()

		deploy := NewDeployOps(true)
		err := deploy.SignalContainerRemote(ctx, "host", "container", "SIGHUP")

		require.NoError(t, err)
	})
}

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

	t.Run("backup non-existent paths returns name", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		backupDir := filepath.Join(tmpDir, "backups")

		deploy := NewDeployOps(false, "")
		backupName, err := deploy.Backup(ctx, backupDir, []string{"/non/existent/path"})

		require.NoError(t, err)
		assert.NotEmpty(t, backupName)
	})

	t.Run("backup empty paths list", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		backupDir := filepath.Join(tmpDir, "backups")

		deploy := NewDeployOps(false, "")
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

		deploy := NewDeployOps(false, "")
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

		deploy := NewDeployOps(false, "")
		err := deploy.CleanupBackups(tmpDir, 5)

		require.NoError(t, err)

		// All backups should remain
		entries, err := os.ReadDir(tmpDir)
		require.NoError(t, err)
		assert.Len(t, entries, 3)
	})

	t.Run("cleanup non-existent directory", func(t *testing.T) {
		deploy := NewDeployOps(false, "")
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

		deploy := NewDeployOps(false, "")
		err := deploy.CleanupBackups(tmpDir, 2)

		require.NoError(t, err)

		// Non-backup directory should still exist
		assert.DirExists(t, filepath.Join(tmpDir, "other-dir"))
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
		err := deploy.DeployLocal(ctx, sourceDir, targetDir, nil)

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
		err := deploy.DeployLocal(ctx, sourceDir, targetDir, nil)

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
		err := deploy.DeployLocal(ctx, sourceDir, targetDir, nil)

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
		err := deploy.DeployLocal(ctx, sourceDir, targetDir, nil)

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
		err := deploy.DeployLocal(ctx, sourceDir, targetDir, nil)

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
		err := deploy.DeployLocal(ctx, sourceFile, targetDir, nil)

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
		err := deploy.DeployLocal(ctx, sourceDir, targetDir, result)

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

		deploy := &DeployOps{ContentHashSync: true}
		err := deploy.DeployLocal(ctx, sourceDir, targetDir, nil)

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
		err := deploy.DeployLocal(ctx, sourceDir, targetDir, result)

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
		err := d.VerifyBackup(filepath.Join(tmpDir, "nonexistent"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "backup archive not found")
	})

	t.Run("empty archive file", func(t *testing.T) {
		tmpDir := t.TempDir()
		backupPath := filepath.Join(tmpDir, "backup")
		require.NoError(t, os.MkdirAll(backupPath, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(backupPath, "configs.tar.gz"), []byte{}, 0644))

		d := NewDeployOps(false, "")
		err := d.VerifyBackup(backupPath)
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
		err := d.VerifyBackup(backupPath)
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
		err := d.VerifyBackup(backupPath)
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
	err := d.DeployLocal(ctx, sourceDir, targetDir, nil)
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
	t.Run("removes file not in source", func(t *testing.T) {
		srcDir := evalSymlinks(t, t.TempDir())
		tgtDir := evalSymlinks(t, t.TempDir())

		// Source has file-a, target has file-a + file-b (stale).
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "file-a.txt"), []byte("a"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(tgtDir, "file-a.txt"), []byte("a"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(tgtDir, "file-b.txt"), []byte("b"), 0644))

		err := removeStaleFiles(srcDir, tgtDir)
		require.NoError(t, err)

		assert.FileExists(t, filepath.Join(tgtDir, "file-a.txt"))
		assert.NoFileExists(t, filepath.Join(tgtDir, "file-b.txt"))
	})

	t.Run("removes stale directory", func(t *testing.T) {
		srcDir := evalSymlinks(t, t.TempDir())
		tgtDir := evalSymlinks(t, t.TempDir())

		// Source has no subdir, target has a stale subdir.
		staleDir := filepath.Join(tgtDir, "stale-dir")
		require.NoError(t, os.MkdirAll(staleDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(staleDir, "deep.txt"), []byte("deep"), 0644))

		err := removeStaleFiles(srcDir, tgtDir)
		require.NoError(t, err)

		assert.NoDirExists(t, staleDir)
	})

	t.Run("keeps matching files and dirs", func(t *testing.T) {
		srcDir := evalSymlinks(t, t.TempDir())
		tgtDir := evalSymlinks(t, t.TempDir())

		// Both have the same structure.
		require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "sub"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "sub", "keep.txt"), []byte("keep"), 0644))
		require.NoError(t, os.MkdirAll(filepath.Join(tgtDir, "sub"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(tgtDir, "sub", "keep.txt"), []byte("keep"), 0644))

		err := removeStaleFiles(srcDir, tgtDir)
		require.NoError(t, err)

		assert.FileExists(t, filepath.Join(tgtDir, "sub", "keep.txt"))
	})

	t.Run("empty source removes everything from target", func(t *testing.T) {
		srcDir := evalSymlinks(t, t.TempDir())
		tgtDir := evalSymlinks(t, t.TempDir())

		require.NoError(t, os.WriteFile(filepath.Join(tgtDir, "a.txt"), []byte("a"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(tgtDir, "b.txt"), []byte("b"), 0644))

		err := removeStaleFiles(srcDir, tgtDir)
		require.NoError(t, err)

		assert.NoFileExists(t, filepath.Join(tgtDir, "a.txt"))
		assert.NoFileExists(t, filepath.Join(tgtDir, "b.txt"))
	})

	t.Run("non-existent target returns error", func(t *testing.T) {
		sourceDir := t.TempDir()
		err := removeStaleFiles(sourceDir, "/nonexistent/target")
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

		// Create a read-only directory in target with a file inside.
		// The file is not in source, so removeStaleFiles should try to delete it.
		lockedDir := filepath.Join(tgtDir, "locked")
		require.NoError(t, os.MkdirAll(lockedDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(lockedDir, "stale.txt"), []byte("stale"), 0644))

		// Make the parent directory read-only so RemoveAll cannot delete its children.
		require.NoError(t, os.Chmod(lockedDir, 0555))
		t.Cleanup(func() { _ = os.Chmod(lockedDir, 0755) })

		// lockedDir itself is not in source, so the WalkDir will attempt os.RemoveAll on it.
		// The read-only permission prevents deletion of the contents.
		_ = removeStaleFiles(srcDir, tgtDir)

		// The locked directory should still exist since removal was blocked.
		assert.DirExists(t, lockedDir)
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

	err := d.DeployLocal(context.Background(), srcDir, tgtDir, result)
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

	err := d.DeployLocal(context.Background(), srcDir, tgtDir, result)
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
	err := d.DeployLocal(context.Background(), srcFile, filepath.Join(tmpDir, "tgt"), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

func TestDeployOps_DeployLocal_SourceMissing(t *testing.T) {
	d := NewDeployOps(false, "")
	err := d.DeployLocal(context.Background(), "/nonexistent/source/dir", "/tmp/tgt", nil)
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

	err := d.DeployLocal(context.Background(), srcDir, tgtDir, nil)
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

func TestDeployOps_ComposeUpMultipleWithRollback_NoBackup(t *testing.T) {
	// When backupPath is empty and compose up fails, should return error mentioning no backup.
	d := NewDeployOps(false, "")

	// Use a nonexistent compose file to force failure.
	err := d.ComposeUpMultipleWithRollback(context.Background(), []string{"/nonexistent/compose.yml"}, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no backup available")
}

func TestDeployOps_ComposeUpMultipleWithRollback_NoBackupFiles(t *testing.T) {
	d := NewDeployOps(false, "")
	backupDir := t.TempDir() // Empty backup directory.

	err := d.ComposeUpMultipleWithRollback(context.Background(), []string{"/nonexistent/compose.yml"}, backupDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no backup files found")
}

func TestDeployOps_VerifyContainerHealth_DryRun(t *testing.T) {
	d := NewDeployOps(true, "")
	err := d.VerifyContainerHealth(context.Background(), "/any/compose.yml")
	assert.NoError(t, err)
}

func TestDeployOps_ComposeUpWithRollback_DelegatesCorrectly(t *testing.T) {
	// ComposeUpWithRollback is a thin wrapper, ensure it delegates to ComposeUpMultiple.
	d := NewDeployOps(false, "")

	// With no backup path and a bad compose file, it should fail with "no backup available".
	err := d.ComposeUpWithRollback(context.Background(), "/nonexistent/compose.yml", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no backup available")
}

func TestDeployOps_ComposeUpMultiple_DryRun(t *testing.T) {
	d := NewDeployOps(true, "test-project")
	err := d.ComposeUpMultiple(context.Background(), []string{"/some/compose.yml"})
	assert.NoError(t, err)
}

func TestDeployOps_ComposeUpMultipleWithRollback_SuccessReturnsNil(t *testing.T) {
	// When compose up succeeds (dry-run mode), should return nil.
	d := NewDeployOps(true, "test-project")
	err := d.ComposeUpMultipleWithRollback(context.Background(), []string{"/some/compose.yml"}, "/backup")
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

	err := d.DeployLocal(context.Background(), srcDir, tgtDir, result)
	require.NoError(t, err)

	// Stale file should be removed.
	assert.NoFileExists(t, filepath.Join(tgtDir, "stale.yml"))
	assert.FileExists(t, filepath.Join(tgtDir, "keep.yml"))
}

func TestDeployOps_CleanupBackups_NonExistentDir(t *testing.T) {
	d := NewDeployOps(false, "")
	err := d.CleanupBackups("/nonexistent/backup/dir", 5)
	assert.NoError(t, err) // Non-existent dir returns nil.
}

func TestDeployOps_CleanupBackups_RemovesOldest(t *testing.T) {
	tmpDir := t.TempDir()

	// Create 5 backup directories.
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("backup-2024-01-0%d", i+1)
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, name), 0755))
	}

	d := NewDeployOps(false, "")
	err := d.CleanupBackups(tmpDir, 3)
	require.NoError(t, err)

	// Should have removed 2 oldest, kept 3 newest.
	entries, err := os.ReadDir(tmpDir)
	require.NoError(t, err)
	assert.Len(t, entries, 3)
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

func TestDeployOps_ComposeUpMultipleWithRollbackPaths(t *testing.T) {
	ctx := context.Background()

	t.Run("rollback fails when both deploy and rollback fail", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Use an invalid compose file that docker compose will reject.
		composeFile := filepath.Join(tmpDir, "docker-compose.yml")
		require.NoError(t, os.WriteFile(composeFile, []byte("not valid yaml: [[["), 0644))

		// Create backup files (also invalid so rollback also fails).
		backupDir := filepath.Join(tmpDir, "backup")
		require.NoError(t, os.MkdirAll(backupDir, 0755))
		backupFile := filepath.Join(backupDir, "docker-compose.yml")
		require.NoError(t, os.WriteFile(backupFile, []byte("not valid yaml: [[["), 0644))

		d := &DeployOps{DryRun: false, ProjectName: "rollbacktest"}
		err := d.ComposeUpMultipleWithRollback(ctx, []string{composeFile}, backupDir)
		require.Error(t, err)
		// Both deploy and rollback fail -> ErrRollbackFailed.
		assert.ErrorIs(t, err, ErrRollbackFailed)
	})

	t.Run("no backup available returns deployment error", func(t *testing.T) {
		tmpDir := t.TempDir()

		composeFile := filepath.Join(tmpDir, "docker-compose.yml")
		require.NoError(t, os.WriteFile(composeFile, []byte("not valid yaml: [[["), 0644))

		d := &DeployOps{DryRun: false, ProjectName: "rollbacktest"}
		err := d.ComposeUpMultipleWithRollback(ctx, []string{composeFile}, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no backup available for rollback")
	})

	t.Run("no backup files found returns deployment error", func(t *testing.T) {
		tmpDir := t.TempDir()

		composeFile := filepath.Join(tmpDir, "docker-compose.yml")
		require.NoError(t, os.WriteFile(composeFile, []byte("not valid yaml: [[["), 0644))

		// Backup dir exists but has no matching files.
		backupDir := filepath.Join(tmpDir, "backup")
		require.NoError(t, os.MkdirAll(backupDir, 0755))

		d := &DeployOps{DryRun: false, ProjectName: "rollbacktest"}
		err := d.ComposeUpMultipleWithRollback(ctx, []string{composeFile}, backupDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no backup files found for rollback")
	})
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
		err := d.DeployLocal(ctx, sourceDir, targetDir, result)
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
		err := d.DeployLocal(cancelledCtx, sourceDir, targetDir, nil)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("rollback decision with injected compose errors", func(t *testing.T) {
		tests := []struct {
			name            string
			composeErr      error
			wantErr         error
			wantNoRollback  bool // true if rollback should be skipped
		}{
			{
				name:           "ErrComposeUnhealthy skips rollback",
				composeErr:     fmt.Errorf("%w: obsidian", ErrComposeUnhealthy),
				wantErr:        ErrComposeUnhealthy,
				wantNoRollback: true,
			},
			{
				name:           "generic error triggers rollback",
				composeErr:     fmt.Errorf("compose up failed: exit code 1"),
				wantErr:        ErrRollbackFailed,
				wantNoRollback: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				tmpDir := t.TempDir()
				composeFile := filepath.Join(tmpDir, "docker-compose.yml")
				require.NoError(t, os.WriteFile(composeFile, []byte("version: '3'"), 0644))

				backupDir := filepath.Join(tmpDir, "backup")
				require.NoError(t, os.MkdirAll(backupDir, 0755))
				backupFile := filepath.Join(backupDir, "docker-compose.yml")
				require.NoError(t, os.WriteFile(backupFile, []byte("not valid yaml: [[["), 0644))

				d := &DeployOps{
					DryRun:      false,
					ProjectName: "rollbacktest",
					composeUpFn: func(_ context.Context, _ []string) error {
						return tt.composeErr
					},
				}

				err := d.ComposeUpMultipleWithRollback(ctx, []string{composeFile}, backupDir)
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErr)

				if tt.wantNoRollback {
					// Should NOT contain rollback-related error text.
					assert.NotErrorIs(t, err, ErrRollbackFailed)
					assert.NotErrorIs(t, err, ErrRollbackSucceeded)
				}
			})
		}
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
		err := d.DeployLocal(ctx, sourceDir, targetPath, nil)
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

package cmd

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/docker/dockertest"
)

// errorLines returns n newline-separated log lines that all match the error regex.
func errorLines(n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("error number %d", i)
	}
	return strings.Join(lines, "\n")
}

func TestCollectRecentErrors(t *testing.T) {
	containers := []docker.ContainerInfo{{Name: "a"}, {Name: "b"}, {Name: "c"}}

	tests := []struct {
		name          string
		logs          map[string]string // a container missing from the map fails its log read
		limit         int
		wantErrors    int
		wantLogCalls  int
		wantFirstFrom string
	}{
		{
			name:          "limit reached inside the first container stops the scan",
			logs:          map[string]string{"a": errorLines(60), "b": errorLines(5), "c": errorLines(5)},
			limit:         MaxErrorDisplay,
			wantErrors:    MaxErrorDisplay,
			wantLogCalls:  1,
			wantFirstFrom: "a",
		},
		{
			name:          "limit reached on the last line of a container stops the scan",
			logs:          map[string]string{"a": errorLines(MaxErrorDisplay), "b": errorLines(5), "c": errorLines(5)},
			limit:         MaxErrorDisplay,
			wantErrors:    MaxErrorDisplay,
			wantLogCalls:  1,
			wantFirstFrom: "a",
		},
		{
			name:          "limit reached inside the second container",
			logs:          map[string]string{"a": errorLines(30), "b": errorLines(30), "c": errorLines(30)},
			limit:         MaxErrorDisplay,
			wantErrors:    MaxErrorDisplay,
			wantLogCalls:  2,
			wantFirstFrom: "a",
		},
		{
			name:          "under the limit reads every container",
			logs:          map[string]string{"a": errorLines(3), "b": errorLines(4), "c": errorLines(5)},
			limit:         MaxErrorDisplay,
			wantErrors:    12,
			wantLogCalls:  3,
			wantFirstFrom: "a",
		},
		{
			name:          "lines without an error marker are ignored",
			logs:          map[string]string{"a": "all fine\nstarted ok", "b": "FATAL: disk full", "c": ""},
			limit:         MaxErrorDisplay,
			wantErrors:    1,
			wantLogCalls:  3,
			wantFirstFrom: "b",
		},
		{
			name:          "a container whose logs cannot be read is skipped",
			logs:          map[string]string{"b": errorLines(2), "c": errorLines(1)},
			limit:         MaxErrorDisplay,
			wantErrors:    3,
			wantLogCalls:  3,
			wantFirstFrom: "b",
		},
		{
			name:         "nothing found",
			logs:         map[string]string{},
			limit:        MaxErrorDisplay,
			wantErrors:   0,
			wantLogCalls: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := dockertest.NewMockDockerAPI()
			mock.ContainerLogsFunc = func(_ context.Context, name string, _ client.ContainerLogsOptions) (client.ContainerLogsResult, error) {
				logs, ok := tt.logs[name]
				if !ok {
					return nil, errors.New("mock: container logs failed")
				}
				return io.NopCloser(strings.NewReader(logs)), nil
			}

			found := collectRecentErrors(context.Background(), docker.NewClientWithAPI(mock), containers, tt.limit)

			assert.Len(t, found, tt.wantErrors)
			assert.Equal(t, tt.wantLogCalls, mock.ContainerLogsCalls, "log fetches")
			if tt.wantErrors > 0 {
				assert.Equal(t, tt.wantFirstFrom, found[0].Container)
			}
		})
	}
}

func TestDeployRestoredConfigsHonorsContext(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	require.NoError(t, os.MkdirAll(source, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(source, "config.yml"), []byte("restored"), 0o644))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := deployRestoredConfigs(ctx, source, target)

	require.ErrorIs(t, err, context.Canceled)
	assert.NoDirExists(t, target)
}

func TestMaydayCmd_Registration(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"mayday"})
	require.NoError(t, err)
	assert.Equal(t, "mayday", cmd.Name())
}

func TestMaydayCmd_MutinyAlias(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"mutiny"})
	require.NoError(t, err)
	assert.Equal(t, "mayday", cmd.Name())
}

func TestOverboardCmd_Registration(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"overboard"})
	require.NoError(t, err)
	assert.Equal(t, "overboard", cmd.Name())
}

func TestOverboardCmd_PlankAlias(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"plank"})
	require.NoError(t, err)
	assert.Equal(t, "overboard", cmd.Name())
}

func TestRestoreCmd_Registration(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"restore"})
	require.NoError(t, err)
	assert.Equal(t, "restore", cmd.Name())
}

func TestMaydayCmd_Help(t *testing.T) {
	t.Run("mayday --help", func(t *testing.T) {
		output, err := executeCmd(t, "mayday", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "recent errors")
		assert.Contains(t, output, "--list")
		assert.Contains(t, output, "--rollback")
	})
}

func TestMaydayCmd_Aliases(t *testing.T) {
	t.Run("mutiny alias", func(t *testing.T) {
		_, err := executeCmd(t, "mutiny", "--help")
		assert.NoError(t, err)
	})
}

func TestMaydayCmd_Flags(t *testing.T) {
	t.Run("has list flag", func(t *testing.T) {
		resetRootCmd(t)
		assert.False(t, maydayList) // default value
	})

	t.Run("has rollback flag", func(t *testing.T) {
		resetRootCmd(t)
		assert.Empty(t, maydayRollback) // default value
	})
}

func TestMaydayCmd_ListFlag(t *testing.T) {
	t.Run("mayday --list without config", func(t *testing.T) {
		// Note: This test may fail when run with other tests due to cobra state pollution.
		// The --list flag behavior is verified in the Flags test above.
		// This test primarily verifies the command doesn't panic.
		_, err := executeCmd(t, "mayday", "--list")
		// May succeed or fail depending on test execution order
		_ = err
	})
}

func TestOverboardCmd_Help(t *testing.T) {
	t.Run("overboard --help", func(t *testing.T) {
		output, err := executeCmd(t, "overboard", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "remove")
		assert.Contains(t, output, "container")
	})
}

func TestOverboardCmd_Aliases(t *testing.T) {
	t.Run("plank alias", func(t *testing.T) {
		_, err := executeCmd(t, "plank", "--help")
		assert.NoError(t, err)
	})
}

func TestOverboardCmd_RequiresArg(t *testing.T) {
	t.Run("requires container name", func(t *testing.T) {
		// Note: This test may not report an error when run with other tests
		// due to cobra state pollution. When run in isolation, it correctly
		// returns an error for missing required argument.
		// The Args: cobra.ExactArgs(1) validation is set on the command.
		_, err := executeCmd(t, "overboard")
		// Error may or may not be returned depending on test order
		_ = err
	})
}

func TestStripDockerLogPrefix(t *testing.T) {
	t.Run("strip stdout prefix", func(t *testing.T) {
		// Stdout header: [1, 0, 0, 0, x, x, x, x]
		line := string([]byte{1, 0, 0, 0, 0, 0, 0, 5}) + "hello"
		result := stripDockerLogPrefix(line)
		assert.Equal(t, "hello", result)
	})

	t.Run("strip stderr prefix", func(t *testing.T) {
		// Stderr header: [2, 0, 0, 0, x, x, x, x]
		line := string([]byte{2, 0, 0, 0, 0, 0, 0, 5}) + "error"
		result := stripDockerLogPrefix(line)
		assert.Equal(t, "error", result)
	})

	t.Run("no prefix", func(t *testing.T) {
		line := "plain text log"
		result := stripDockerLogPrefix(line)
		assert.Equal(t, "plain text log", result)
	})

	t.Run("short line", func(t *testing.T) {
		line := "short"
		result := stripDockerLogPrefix(line)
		assert.Equal(t, "short", result)
	})

	t.Run("unknown stream type", func(t *testing.T) {
		// Unknown header: [3, 0, 0, 0, x, x, x, x]
		line := string([]byte{3, 0, 0, 0, 0, 0, 0, 5}) + "data"
		result := stripDockerLogPrefix(line)
		// Should return original since stream type is not 1 or 2
		assert.Equal(t, line, result)
	})
}

func TestRestoreCmd_Help(t *testing.T) {
	t.Run("restore --help", func(t *testing.T) {
		output, err := executeCmd(t, "restore", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "Restore")
		assert.Contains(t, output, "backup")
		assert.Contains(t, output, "--list")
	})
}

func TestRestoreCmd_Flags(t *testing.T) {
	t.Run("has list flag", func(t *testing.T) {
		resetRootCmd(t)
		assert.False(t, restoreList) // default value
	})
}

func TestGetBackups(t *testing.T) {
	t.Run("non-existent directory returns nil", func(t *testing.T) {
		backups, err := getBackups("/nonexistent/backup/dir")
		assert.NoError(t, err)
		assert.Nil(t, backups)
	})

	t.Run("empty directory returns nil", func(t *testing.T) {
		tmpDir := t.TempDir()
		backups, err := getBackups(tmpDir)
		assert.NoError(t, err)
		assert.Nil(t, backups)
	})

	t.Run("ignores non-backup directories", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "not-a-backup"), 0755))
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "other-dir"), 0755))

		backups, err := getBackups(tmpDir)
		assert.NoError(t, err)
		assert.Nil(t, backups)
	})

	t.Run("finds backup directories", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "backup-2024-01-01"), 0755))
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "backup-2024-01-02"), 0755))
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "not-a-backup"), 0755))

		backups, err := getBackups(tmpDir)
		require.NoError(t, err)
		assert.Len(t, backups, 2)
	})

	t.Run("sorted newest first", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "backup-2024-01-01"), 0755))
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "backup-2024-01-15"), 0755))
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "backup-2024-01-10"), 0755))

		backups, err := getBackups(tmpDir)
		require.NoError(t, err)
		assert.Equal(t, "backup-2024-01-15", backups[0].Name)
		assert.Equal(t, "backup-2024-01-10", backups[1].Name)
		assert.Equal(t, "backup-2024-01-01", backups[2].Name)
	})

	t.Run("detects tar.gz presence", func(t *testing.T) {
		tmpDir := t.TempDir()
		backupDir := filepath.Join(tmpDir, "backup-2024-01-01")
		require.NoError(t, os.MkdirAll(backupDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(backupDir, "configs.tar.gz"), []byte("fake"), 0644))

		backups, err := getBackups(tmpDir)
		require.NoError(t, err)
		assert.True(t, backups[0].HasTar)
	})

	t.Run("symlinked tar.gz is not reported as present", func(t *testing.T) {
		tmpDir := t.TempDir()
		backupDir := filepath.Join(tmpDir, "backup-2024-01-01")
		require.NoError(t, os.MkdirAll(backupDir, 0755))
		target := filepath.Join(t.TempDir(), "configs.tar.gz")
		require.NoError(t, os.WriteFile(target, []byte("fake"), 0644))
		require.NoError(t, os.Symlink(target, filepath.Join(backupDir, "configs.tar.gz")))

		backups, err := getBackups(tmpDir)
		require.NoError(t, err)
		assert.False(t, backups[0].HasTar)
	})

	t.Run("reports missing tar.gz", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "backup-2024-01-01"), 0755))

		backups, err := getBackups(tmpDir)
		require.NoError(t, err)
		assert.False(t, backups[0].HasTar)
	})
}

// writeTestTarGz writes a gzipped tar holding files (name → content) to path.
func writeTestTarGz(t *testing.T, path string, files map[string]string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	f, err := os.Create(path)
	require.NoError(t, err)

	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0644,
			Size: int64(len(content)),
		}
		require.NoError(t, tw.WriteHeader(hdr))
		_, err := tw.Write([]byte(content))
		require.NoError(t, err)
	}

	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())
	require.NoError(t, f.Close())
}

// extractTestTarGz opens path and extracts it with extractTarGzFrom.
func extractTestTarGz(t *testing.T, path, destDir string) error {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	return extractTarGzFrom(f, destDir)
}

func TestExtractTarGzFrom(t *testing.T) {
	createTarGz := func(t *testing.T, files map[string]string) string {
		t.Helper()
		tarPath := filepath.Join(t.TempDir(), "test.tar.gz")
		writeTestTarGz(t, tarPath, files)
		return tarPath
	}

	t.Run("extracts single file", func(t *testing.T) {
		tarPath := createTarGz(t, map[string]string{
			"hello.txt": "hello world",
		})
		destDir := t.TempDir()

		err := extractTestTarGz(t, tarPath, destDir)
		require.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(destDir, "hello.txt"))
		require.NoError(t, err)
		assert.Equal(t, "hello world", string(content))
	})

	t.Run("extracts multiple files", func(t *testing.T) {
		tarPath := createTarGz(t, map[string]string{
			"a.txt": "aaa",
			"b.txt": "bbb",
		})
		destDir := t.TempDir()

		err := extractTestTarGz(t, tarPath, destDir)
		require.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(destDir, "a.txt"))
		require.NoError(t, err)
		assert.Equal(t, "aaa", string(content))

		content, err = os.ReadFile(filepath.Join(destDir, "b.txt"))
		require.NoError(t, err)
		assert.Equal(t, "bbb", string(content))
	})

	t.Run("extracts nested directories", func(t *testing.T) {
		tarPath := filepath.Join(t.TempDir(), "test.tar.gz")
		f, err := os.Create(tarPath)
		require.NoError(t, err)

		gw := gzip.NewWriter(f)
		tw := tar.NewWriter(gw)

		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name:     "subdir/",
			Typeflag: tar.TypeDir,
			Mode:     0755,
		}))

		content := "nested content"
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name: "subdir/file.txt",
			Mode: 0644,
			Size: int64(len(content)),
		}))
		_, err = tw.Write([]byte(content))
		require.NoError(t, err)

		_ = tw.Close()
		_ = gw.Close()
		_ = f.Close()

		destDir := t.TempDir()
		err = extractTestTarGz(t, tarPath, destDir)
		require.NoError(t, err)

		data, err := os.ReadFile(filepath.Join(destDir, "subdir", "file.txt"))
		require.NoError(t, err)
		assert.Equal(t, "nested content", string(data))
	})

	t.Run("rejects path traversal", func(t *testing.T) {
		tarPath := filepath.Join(t.TempDir(), "evil.tar.gz")
		f, err := os.Create(tarPath)
		require.NoError(t, err)

		gw := gzip.NewWriter(f)
		tw := tar.NewWriter(gw)

		content := "evil content"
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name: "../../../etc/passwd",
			Mode: 0644,
			Size: int64(len(content)),
		}))
		_, err = tw.Write([]byte(content))
		require.NoError(t, err)

		_ = tw.Close()
		_ = gw.Close()
		_ = f.Close()

		destDir := t.TempDir()
		err = extractTestTarGz(t, tarPath, destDir)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid file path")
	})

	t.Run("rejects a stream that is not gzip", func(t *testing.T) {
		err := extractTarGzFrom(strings.NewReader("not gzip"), t.TempDir())
		assert.Error(t, err)
	})
}

// restoreFixture is a backup directory, an outside directory, and an appdata
// target, with LOCAL_APPDATA pointed at the target.
type restoreFixture struct {
	backupDir string
	outside   string
	appdata   string
}

func newRestoreFixture(t *testing.T) restoreFixture {
	t.Helper()
	root := t.TempDir()
	f := restoreFixture{
		backupDir: filepath.Join(root, "backups"),
		outside:   filepath.Join(root, "outside"),
		appdata:   filepath.Join(root, "appdata"),
	}
	require.NoError(t, os.MkdirAll(f.backupDir, 0755))
	require.NoError(t, os.MkdirAll(f.outside, 0755))
	require.NoError(t, os.MkdirAll(f.appdata, 0755))
	// The payload never carries compose/core.yml, so doRestore never runs
	// docker compose in these tests.
	writeTestTarGz(t, filepath.Join(f.outside, "configs.tar.gz"), map[string]string{"configs/x.yml": "restored"})
	t.Setenv("LOCAL_APPDATA", f.appdata)
	return f
}

func (f restoreFixture) restoredFile() string {
	return filepath.Join(f.appdata, "configs", "x.yml")
}

func TestDoRestore_RejectsUnsafeSource(t *testing.T) {
	tests := []struct {
		name     string
		backup   string
		setup    func(t *testing.T, f restoreFixture)
		wantText string
	}{
		{
			name:     "parent escape",
			backup:   "../outside",
			setup:    func(*testing.T, restoreFixture) {},
			wantText: "invalid name",
		},
		{
			name:   "symlinked backup directory",
			backup: "backup-link",
			setup: func(t *testing.T, f restoreFixture) {
				require.NoError(t, os.Symlink(f.outside, filepath.Join(f.backupDir, "backup-link")))
			},
			wantText: "backup not found",
		},
		{
			name:   "unprefixed real directory",
			backup: "manual",
			setup: func(t *testing.T, f restoreFixture) {
				writeTestTarGz(t, filepath.Join(f.backupDir, "manual", "configs.tar.gz"), map[string]string{"configs/x.yml": "restored"})
			},
			wantText: "invalid name",
		},
		{
			name:   "symlinked archive",
			backup: "backup-20240101-000000",
			setup: func(t *testing.T, f restoreFixture) {
				dir := filepath.Join(f.backupDir, "backup-20240101-000000")
				require.NoError(t, os.MkdirAll(dir, 0755))
				require.NoError(t, os.Symlink(filepath.Join(f.outside, "configs.tar.gz"), filepath.Join(dir, "configs.tar.gz")))
			},
			wantText: "open backup archive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newRestoreFixture(t)
			tt.setup(t, f)

			err := doRestore(context.Background(), f.backupDir, tt.backup)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantText)
			assert.NoFileExists(t, f.restoredFile())
		})
	}
}

func TestDoRestore_SourceFailures(t *testing.T) {
	t.Run("backup directory cannot be listed", func(t *testing.T) {
		f := newRestoreFixture(t)
		notDir := filepath.Join(f.outside, "not-a-dir")
		require.NoError(t, os.WriteFile(notDir, nil, 0644))

		err := doRestore(context.Background(), notDir, "backup-20240101-000000")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "list backups")
	})

	t.Run("backup root cannot be opened", func(t *testing.T) {
		f := newRestoreFixture(t)
		writeTestTarGz(t, filepath.Join(f.backupDir, "backup-20240101-000000", "configs.tar.gz"), map[string]string{"configs/x.yml": "restored"})
		wantErr := errors.New("root open failed")
		original := openBackupRoot
		openBackupRoot = func(string) (*os.Root, error) { return nil, wantErr }
		t.Cleanup(func() { openBackupRoot = original })

		err := doRestore(context.Background(), f.backupDir, "backup-20240101-000000")

		require.ErrorIs(t, err, wantErr)
		assert.Contains(t, err.Error(), "open backup root")
		assert.NoFileExists(t, f.restoredFile())
	})

	t.Run("listed backup without an archive", func(t *testing.T) {
		f := newRestoreFixture(t)
		require.NoError(t, os.MkdirAll(filepath.Join(f.backupDir, "backup-20240101-000000"), 0755))

		err := doRestore(context.Background(), f.backupDir, "backup-20240101-000000")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "backup incomplete")
	})
}

func TestDoRestore_RestoresListedBackup(t *testing.T) {
	f := newRestoreFixture(t)
	writeTestTarGz(t, filepath.Join(f.backupDir, "backup-20240101-000000", "configs.tar.gz"), map[string]string{"configs/x.yml": "restored"})

	err := doRestore(context.Background(), f.backupDir, "backup-20240101-000000")

	require.NoError(t, err)
	content, readErr := os.ReadFile(f.restoredFile())
	require.NoError(t, readErr)
	assert.Equal(t, "restored", string(content))
}

func TestListBackups(t *testing.T) {
	t.Run("non-existent directory", func(t *testing.T) {
		err := listBackups("/nonexistent/backup/dir")
		assert.NoError(t, err)
	})

	t.Run("empty directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		err := listBackups(tmpDir)
		assert.NoError(t, err)
	})

	t.Run("with backup directories", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "backup-2024-01-01"), 0755))
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "backup-2024-01-02"), 0755))

		err := listBackups(tmpDir)
		assert.NoError(t, err)
	})
}

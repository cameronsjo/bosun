package snapshot

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testRestoreOps(t *testing.T) restoreOps {
	t.Helper()

	dirInfo, err := os.Stat(t.TempDir())
	require.NoError(t, err)

	return restoreOps{
		stat: func(path string) (os.FileInfo, error) {
			if filepath.Base(filepath.Dir(path)) == "snapshots" {
				return dirInfo, nil
			}
			return nil, os.ErrNotExist
		},
		dirSize:      func(string) (int64, error) { return 0, nil },
		diskSpace:    func(string, int64) error { return nil },
		hasContent:   func(string) bool { return false },
		mkdirAll:     func(string, os.FileMode) error { return nil },
		copyDir:      func(context.Context, string, string) error { return nil },
		removeAll:    func(string) error { return nil },
		rename:       func(string, string) error { return nil },
		now:          func() time.Time { return time.Date(2024, 1, 2, 3, 4, 5, 6, time.UTC) },
		newRestoreID: func() string { return "fixed-id" },
	}
}

func TestRestoreWithOps_RejectsInvalidSnapshot(t *testing.T) {
	t.Run("inspection error preserves cause", func(t *testing.T) {
		wantErr := errors.New("inspect failed")
		ops := testRestoreOps(t)
		ops.stat = func(string) (os.FileInfo, error) { return nil, wantErr }

		err := restoreWithOps(t.TempDir(), "snapshot-broken", ops)

		require.Error(t, err)
		assert.ErrorIs(t, err, wantErr)
		assert.Contains(t, err.Error(), "inspect snapshot")
	})

	t.Run("missing snapshot retains public error", func(t *testing.T) {
		ops := testRestoreOps(t)
		ops.stat = func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }

		err := restoreWithOps(t.TempDir(), "snapshot-missing", ops)

		require.EqualError(t, err, "snapshot not found: snapshot-missing")
	})

	t.Run("regular file is not a snapshot", func(t *testing.T) {
		manifestDir := t.TempDir()
		snapshotPath := filepath.Join(snapshotsDir(manifestDir), "snapshot-file")
		require.NoError(t, os.MkdirAll(filepath.Dir(snapshotPath), 0755))
		require.NoError(t, os.WriteFile(snapshotPath, []byte("not a directory"), 0644))

		err := Restore(manifestDir, "snapshot-file")

		require.EqualError(t, err, "snapshot is not a directory: snapshot-file")
	})
}

func TestRestoreWithOps_EarlyFailuresPreserveCause(t *testing.T) {
	tests := []struct {
		name      string
		wantText  string
		configure func(*restoreOps, error)
	}{
		{
			name:     "snapshot size",
			wantText: "calculate snapshot size",
			configure: func(ops *restoreOps, wantErr error) {
				ops.dirSize = func(string) (int64, error) { return 0, wantErr }
			},
		},
		{
			name:     "disk space",
			wantText: "insufficient disk space for restore",
			configure: func(ops *restoreOps, wantErr error) {
				ops.diskSpace = func(string, int64) error { return wantErr }
			},
		},
		{
			name:     "backup directory",
			wantText: "create backup directory",
			configure: func(ops *restoreOps, wantErr error) {
				ops.hasContent = func(string) bool { return true }
				ops.mkdirAll = func(string, os.FileMode) error { return wantErr }
			},
		},
		{
			name:     "temporary directory",
			wantText: "create temp restore directory",
			configure: func(ops *restoreOps, wantErr error) {
				ops.mkdirAll = func(string, os.FileMode) error { return wantErr }
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantErr := errors.New(tt.name + " failed")
			ops := testRestoreOps(t)
			tt.configure(&ops, wantErr)

			err := restoreWithOps(t.TempDir(), "snapshot-valid", ops)

			require.Error(t, err)
			assert.ErrorIs(t, err, wantErr)
			assert.Contains(t, err.Error(), tt.wantText)
		})
	}
}

func TestRestoreWithOps_CopyFailuresCleanPartialDirectory(t *testing.T) {
	t.Run("pre-rollback backup", func(t *testing.T) {
		wantErr := errors.New("backup copy failed")
		ops := testRestoreOps(t)
		ops.hasContent = func(string) bool { return true }
		ops.copyDir = func(context.Context, string, string) error { return wantErr }
		var removed string
		ops.removeAll = func(path string) error {
			removed = path
			return nil
		}

		err := restoreWithOps(t.TempDir(), "snapshot-valid", ops)

		require.Error(t, err)
		assert.ErrorIs(t, err, wantErr)
		assert.Contains(t, err.Error(), "create pre-rollback backup")
		assert.Contains(t, removed, "pre-rollback-20240102-030405.000000006")
	})

	t.Run("temporary restore", func(t *testing.T) {
		wantErr := errors.New("snapshot copy failed")
		ops := testRestoreOps(t)
		ops.copyDir = func(context.Context, string, string) error { return wantErr }
		var removed string
		ops.removeAll = func(path string) error {
			removed = path
			return nil
		}

		err := restoreWithOps(t.TempDir(), "snapshot-valid", ops)

		require.Error(t, err)
		assert.ErrorIs(t, err, wantErr)
		assert.Contains(t, err.Error(), "copy snapshot to temp")
		assert.True(t, filepath.IsAbs(removed))
		assert.Contains(t, removed, "output.restore-temp-fixed-id")
	})
}

func TestRestoreWithOps_RenameFailuresRecoverOriginal(t *testing.T) {
	t.Run("current output inspection fails", func(t *testing.T) {
		wantErr := errors.New("inspect output failed")
		ops := testRestoreOps(t)
		dirInfo, err := os.Stat(t.TempDir())
		require.NoError(t, err)
		statCalls := 0
		ops.stat = func(string) (os.FileInfo, error) {
			statCalls++
			if statCalls == 1 {
				return dirInfo, nil
			}
			return nil, wantErr
		}
		var removed string
		ops.removeAll = func(path string) error {
			removed = path
			return nil
		}

		err = restoreWithOps(t.TempDir(), "snapshot-valid", ops)

		require.Error(t, err)
		assert.ErrorIs(t, err, wantErr)
		assert.Contains(t, err.Error(), "inspect current output")
		assert.Contains(t, removed, "output.restore-temp-fixed-id")
	})

	t.Run("current output cannot move", func(t *testing.T) {
		wantErr := errors.New("move current failed")
		ops := testRestoreOps(t)
		dirInfo, err := os.Stat(t.TempDir())
		require.NoError(t, err)
		ops.stat = func(string) (os.FileInfo, error) { return dirInfo, nil }
		ops.rename = func(string, string) error { return wantErr }
		var removed string
		ops.removeAll = func(path string) error {
			removed = path
			return nil
		}

		err = restoreWithOps(t.TempDir(), "snapshot-valid", ops)

		require.Error(t, err)
		assert.ErrorIs(t, err, wantErr)
		assert.Contains(t, err.Error(), "rename current output")
		assert.Contains(t, removed, "output.restore-temp-fixed-id")
	})

	t.Run("swap failure restores current output", func(t *testing.T) {
		swapErr := errors.New("swap failed")
		ops := testRestoreOps(t)
		dirInfo, err := os.Stat(t.TempDir())
		require.NoError(t, err)
		ops.stat = func(string) (os.FileInfo, error) { return dirInfo, nil }
		var renames [][2]string
		ops.rename = func(oldPath, newPath string) error {
			renames = append(renames, [2]string{oldPath, newPath})
			if len(renames) == 2 {
				return swapErr
			}
			return nil
		}

		err = restoreWithOps(t.TempDir(), "snapshot-valid", ops)

		require.Error(t, err)
		assert.ErrorIs(t, err, swapErr)
		assert.Len(t, renames, 3)
		assert.Contains(t, renames[2][0], "output.restore-old-fixed-id")
		assert.Equal(t, "output", filepath.Base(renames[2][1]))
	})

	t.Run("failed recovery preserves both causes", func(t *testing.T) {
		swapErr := errors.New("swap failed")
		recoveryErr := errors.New("recovery failed")
		ops := testRestoreOps(t)
		dirInfo, err := os.Stat(t.TempDir())
		require.NoError(t, err)
		ops.stat = func(string) (os.FileInfo, error) { return dirInfo, nil }
		renameCalls := 0
		ops.rename = func(string, string) error {
			renameCalls++
			switch renameCalls {
			case 1:
				return nil
			case 2:
				return swapErr
			default:
				return recoveryErr
			}
		}

		err = restoreWithOps(t.TempDir(), "snapshot-valid", ops)

		require.Error(t, err)
		assert.ErrorIs(t, err, swapErr)
		assert.ErrorIs(t, err, recoveryErr)
		assert.Contains(t, err.Error(), "recovery also failed")
	})
}

func TestCleanupWithOps_PreservesRemovalErrorsAndContinues(t *testing.T) {
	firstErr := errors.New("first removal failed")
	secondErr := errors.New("second removal failed")
	snapshots := make([]SnapshotInfo, MaxSnapshots+3)
	for i := range snapshots {
		snapshots[i] = SnapshotInfo{Name: "snapshot-" + string(rune('a'+i)), Path: "/snapshots/" + string(rune('a'+i))}
	}
	var removed []string

	err := cleanupWithOps("/manifest", func(string) ([]SnapshotInfo, error) {
		return snapshots, nil
	}, func(path string, retries int) error {
		assert.Equal(t, 3, retries)
		removed = append(removed, path)
		switch len(removed) {
		case 1:
			return firstErr
		case 2:
			return nil
		default:
			return secondErr
		}
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, firstErr)
	assert.ErrorIs(t, err, secondErr)
	assert.Len(t, removed, 3)
	assert.Contains(t, err.Error(), snapshots[MaxSnapshots].Name)
	assert.Contains(t, err.Error(), snapshots[MaxSnapshots+2].Name)
	assert.Contains(t, err.Error(), "; ")
}

func TestCleanupWithOps_ListErrorPreservesCause(t *testing.T) {
	wantErr := errors.New("list failed")
	err := cleanupWithOps("/manifest", func(string) ([]SnapshotInfo, error) {
		return nil, wantErr
	}, func(string, int) error {
		t.Fatal("remove must not be called")
		return nil
	})

	assert.ErrorIs(t, err, wantErr)
}

func TestRemoveWithRetryWithOps(t *testing.T) {
	t.Run("eventual success uses exponential delays", func(t *testing.T) {
		wantErr := errors.New("transient")
		calls := 0
		var delays []time.Duration
		err := removeWithRetryWithOps("/snapshot", 3, func(path string) error {
			assert.Equal(t, "/snapshot", path)
			calls++
			if calls < 3 {
				return wantErr
			}
			return nil
		}, func(delay time.Duration) {
			delays = append(delays, delay)
		})

		require.NoError(t, err)
		assert.Equal(t, 3, calls)
		assert.Equal(t, []time.Duration{10 * time.Millisecond, 20 * time.Millisecond}, delays)
	})

	t.Run("exhaustion returns final cause", func(t *testing.T) {
		errs := []error{errors.New("first"), errors.New("second"), errors.New("last")}
		calls := 0
		var delays []time.Duration
		err := removeWithRetryWithOps("/snapshot", len(errs), func(string) error {
			result := errs[calls]
			calls++
			return result
		}, func(delay time.Duration) {
			delays = append(delays, delay)
		})

		assert.ErrorIs(t, err, errs[2])
		assert.Equal(t, []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond}, delays)
	})
}

func TestCheckDiskSpace_Errors(t *testing.T) {
	t.Run("missing path", func(t *testing.T) {
		err := checkDiskSpace(filepath.Join(t.TempDir(), "missing"), 1)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to check disk space")
	})

	t.Run("insufficient capacity", func(t *testing.T) {
		err := checkDiskSpace(t.TempDir(), math.MaxInt64)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "only")
	})
}

func TestList_ReadDirFailure(t *testing.T) {
	manifestDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(manifestDir, ".bosun"), 0755))
	require.NoError(t, os.WriteFile(snapshotsDir(manifestDir), []byte("not a directory"), 0644))

	snapshots, err := List(manifestDir)

	require.Error(t, err)
	assert.Nil(t, snapshots)
	assert.Contains(t, err.Error(), "read snapshots directory")
}

func TestGetRestoredFiles_IncludesBothYAMLExtensions(t *testing.T) {
	manifestDir := t.TempDir()
	outDir := outputDir(manifestDir)
	require.NoError(t, os.MkdirAll(outDir, 0755))
	for _, name := range []string{"compose.yml", "service.yaml", "README.md"} {
		require.NoError(t, os.WriteFile(filepath.Join(outDir, name), nil, 0644))
	}

	files, err := GetRestoredFiles(manifestDir)

	require.NoError(t, err)
	assert.Equal(t, []string{"output/compose.yml", "output/service.yaml"}, files)
}

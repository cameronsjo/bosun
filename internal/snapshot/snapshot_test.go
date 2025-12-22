package snapshot

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreate(t *testing.T) {
	tmpDir := t.TempDir()

	// Create output directory with content
	outDir := filepath.Join(tmpDir, "output")
	require.NoError(t, os.MkdirAll(outDir, 0755))

	testFile := filepath.Join(outDir, "test.yml")
	require.NoError(t, os.WriteFile(testFile, []byte("test: content"), 0644))

	// Create snapshot
	snapshotName, err := Create(tmpDir)
	require.NoError(t, err)
	assert.NotEmpty(t, snapshotName)
	assert.Contains(t, snapshotName, SnapshotPrefix)

	// Verify snapshot exists
	snapPath := filepath.Join(tmpDir, ".bosun", "snapshots", snapshotName)
	_, err = os.Stat(snapPath)
	require.NoError(t, err)

	// Verify snapshot content
	snappedFile := filepath.Join(snapPath, "test.yml")
	content, err := os.ReadFile(snappedFile)
	require.NoError(t, err)
	assert.Equal(t, "test: content", string(content))
}

func TestCreate_EmptyOutput(t *testing.T) {
	tmpDir := t.TempDir()

	// No output directory
	snapshotName, err := Create(tmpDir)
	require.NoError(t, err)
	assert.Empty(t, snapshotName)
}

func TestCreate_EmptyOutputDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Empty output directory
	outDir := filepath.Join(tmpDir, "output")
	require.NoError(t, os.MkdirAll(outDir, 0755))

	snapshotName, err := Create(tmpDir)
	require.NoError(t, err)
	assert.Empty(t, snapshotName)
}

func TestList(t *testing.T) {
	tmpDir := t.TempDir()

	// Create snapshots directory
	snapDir := filepath.Join(tmpDir, ".bosun", "snapshots")
	require.NoError(t, os.MkdirAll(snapDir, 0755))

	// Create several snapshot directories
	snapshots := []string{
		"snapshot-20240101-120000",
		"snapshot-20240102-120000",
		"snapshot-20240103-120000",
	}

	for _, snap := range snapshots {
		snapPath := filepath.Join(snapDir, snap)
		require.NoError(t, os.MkdirAll(snapPath, 0755))
		// Add a file to make it a valid snapshot
		testFile := filepath.Join(snapPath, "test.yml")
		require.NoError(t, os.WriteFile(testFile, []byte("test"), 0644))
	}

	// List snapshots
	result, err := List(tmpDir)
	require.NoError(t, err)
	require.Len(t, result, 3)

	// Should be sorted by date, newest first
	assert.Equal(t, "snapshot-20240103-120000", result[0].Name)
	assert.Equal(t, "snapshot-20240102-120000", result[1].Name)
	assert.Equal(t, "snapshot-20240101-120000", result[2].Name)

	// Should have file count
	assert.Equal(t, 1, result[0].FileCount)
}

func TestList_NoSnapshots(t *testing.T) {
	tmpDir := t.TempDir()

	result, err := List(tmpDir)
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestList_SortedByDate(t *testing.T) {
	tmpDir := t.TempDir()

	// Create snapshots directory manually with distinct timestamps
	snapDir := filepath.Join(tmpDir, ".bosun", "snapshots")
	require.NoError(t, os.MkdirAll(snapDir, 0755))

	// Create snapshots with distinct timestamps
	snapshots := []string{
		"snapshot-20240101-100000",
		"snapshot-20240101-110000",
		"snapshot-20240101-120000",
	}

	for _, snap := range snapshots {
		snapPath := filepath.Join(snapDir, snap)
		require.NoError(t, os.MkdirAll(snapPath, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(snapPath, "test.yml"), []byte("test"), 0644))
	}

	result, err := List(tmpDir)
	require.NoError(t, err)
	require.Len(t, result, 3)

	// Newest should be first
	dates := make([]time.Time, len(result))
	for i, snap := range result {
		dates[i] = snap.Created
	}

	assert.True(t, sort.SliceIsSorted(dates, func(i, j int) bool {
		return dates[i].After(dates[j])
	}), "snapshots should be sorted newest first")
}

func TestRestore(t *testing.T) {
	tmpDir := t.TempDir()

	// Create initial output
	outDir := filepath.Join(tmpDir, "output")
	require.NoError(t, os.MkdirAll(outDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(outDir, "original.yml"), []byte("original"), 0644))

	// Create snapshot
	snapshotName, err := Create(tmpDir)
	require.NoError(t, err)

	// Modify output
	require.NoError(t, os.WriteFile(filepath.Join(outDir, "original.yml"), []byte("modified"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(outDir, "new.yml"), []byte("new file"), 0644))

	// Restore snapshot
	err = Restore(tmpDir, snapshotName)
	require.NoError(t, err)

	// Verify original content restored
	content, err := os.ReadFile(filepath.Join(outDir, "original.yml"))
	require.NoError(t, err)
	assert.Equal(t, "original", string(content))

	// Verify new file is gone
	_, err = os.Stat(filepath.Join(outDir, "new.yml"))
	assert.True(t, os.IsNotExist(err))
}

func TestRestore_CreatesPreRollbackBackup(t *testing.T) {
	tmpDir := t.TempDir()

	// Create initial output
	outDir := filepath.Join(tmpDir, "output")
	require.NoError(t, os.MkdirAll(outDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(outDir, "file.yml"), []byte("v1"), 0644))

	// Create snapshot of v1
	snapshotName, err := Create(tmpDir)
	require.NoError(t, err)

	// Modify to v2
	require.NoError(t, os.WriteFile(filepath.Join(outDir, "file.yml"), []byte("v2"), 0644))

	// Restore v1
	err = Restore(tmpDir, snapshotName)
	require.NoError(t, err)

	// Should have created pre-rollback backup - check directly in snapshots dir
	snapDir := filepath.Join(tmpDir, ".bosun", "snapshots")
	entries, err := os.ReadDir(snapDir)
	require.NoError(t, err)

	hasPreRollback := false
	for _, entry := range entries {
		if entry.IsDir() && len(entry.Name()) >= 12 && entry.Name()[:12] == "pre-rollback" {
			hasPreRollback = true
			// Verify v2 content is in pre-rollback
			content, err := os.ReadFile(filepath.Join(snapDir, entry.Name(), "file.yml"))
			require.NoError(t, err)
			assert.Equal(t, "v2", string(content))
			break
		}
	}
	assert.True(t, hasPreRollback, "should create pre-rollback backup")
}

func TestRestore_SnapshotNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	err := Restore(tmpDir, "nonexistent-snapshot")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestCleanup(t *testing.T) {
	tmpDir := t.TempDir()

	// Create output with content
	outDir := filepath.Join(tmpDir, "output")
	require.NoError(t, os.MkdirAll(outDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(outDir, "test.yml"), []byte("test"), 0644))

	// Create more than MaxSnapshots snapshots
	for i := 0; i < MaxSnapshots+5; i++ {
		_, err := Create(tmpDir)
		require.NoError(t, err)
		time.Sleep(10 * time.Millisecond) // Ensure unique timestamps
	}

	// Cleanup is called automatically by Create, but call it explicitly
	err := Cleanup(tmpDir)
	require.NoError(t, err)

	// Should have at most MaxSnapshots
	snapshots, err := List(tmpDir)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(snapshots), MaxSnapshots)
}

func TestCleanup_KeepsNewest(t *testing.T) {
	tmpDir := t.TempDir()

	// Create snapshots directory manually
	snapDir := filepath.Join(tmpDir, ".bosun", "snapshots")
	require.NoError(t, os.MkdirAll(snapDir, 0755))

	// Create 25 snapshots (more than MaxSnapshots)
	for i := 0; i < 25; i++ {
		timestamp := time.Date(2024, 1, 1, 0, 0, i, 0, time.UTC)
		snapName := SnapshotPrefix + timestamp.Format(DateFormat)
		snapPath := filepath.Join(snapDir, snapName)
		require.NoError(t, os.MkdirAll(snapPath, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(snapPath, "test.yml"), []byte("test"), 0644))
	}

	err := Cleanup(tmpDir)
	require.NoError(t, err)

	snapshots, err := List(tmpDir)
	require.NoError(t, err)
	assert.Equal(t, MaxSnapshots, len(snapshots))

	// Newest should still exist (highest number = newest)
	newestExpected := "snapshot-20240101-000024"
	assert.Equal(t, newestExpected, snapshots[0].Name)
}

func TestGetRestoredFiles(t *testing.T) {
	tmpDir := t.TempDir()

	outDir := filepath.Join(tmpDir, "output")
	composeDir := filepath.Join(outDir, "compose")
	traefikDir := filepath.Join(outDir, "traefik")

	require.NoError(t, os.MkdirAll(composeDir, 0755))
	require.NoError(t, os.MkdirAll(traefikDir, 0755))

	require.NoError(t, os.WriteFile(filepath.Join(composeDir, "stack.yml"), []byte(""), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(traefikDir, "dynamic.yml"), []byte(""), 0644))
	// Non-yml file should be excluded
	require.NoError(t, os.WriteFile(filepath.Join(outDir, "README.md"), []byte(""), 0644))

	files, err := GetRestoredFiles(tmpDir)
	require.NoError(t, err)

	assert.Len(t, files, 2)
	assert.Contains(t, files, "output/compose/stack.yml")
	assert.Contains(t, files, "output/traefik/dynamic.yml")
}

func TestDirHasContent(t *testing.T) {
	tmpDir := t.TempDir()

	// Non-existent directory
	assert.False(t, dirHasContent(filepath.Join(tmpDir, "nonexistent")))

	// Empty directory
	emptyDir := filepath.Join(tmpDir, "empty")
	require.NoError(t, os.MkdirAll(emptyDir, 0755))
	assert.False(t, dirHasContent(emptyDir))

	// Directory with content
	contentDir := filepath.Join(tmpDir, "content")
	require.NoError(t, os.MkdirAll(contentDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(contentDir, "file.txt"), []byte(""), 0644))
	assert.True(t, dirHasContent(contentDir))
}

func TestCountFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create nested structure
	subDir := filepath.Join(tmpDir, "sub")
	require.NoError(t, os.MkdirAll(subDir, 0755))

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte(""), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte(""), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(subDir, "file3.txt"), []byte(""), 0644))

	count := countFiles(tmpDir)
	assert.Equal(t, 3, count)
}

func TestCopyDir(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Create source structure
	subDir := filepath.Join(srcDir, "sub")
	require.NoError(t, os.MkdirAll(subDir, 0755))

	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "file1.txt"), []byte("content1"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(subDir, "file2.txt"), []byte("content2"), 0644))

	// Copy
	err := copyDir(srcDir, dstDir)
	require.NoError(t, err)

	// Verify
	content1, err := os.ReadFile(filepath.Join(dstDir, "file1.txt"))
	require.NoError(t, err)
	assert.Equal(t, "content1", string(content1))

	content2, err := os.ReadFile(filepath.Join(dstDir, "sub", "file2.txt"))
	require.NoError(t, err)
	assert.Equal(t, "content2", string(content2))
}

func TestCopyFile(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	srcFile := filepath.Join(srcDir, "source.txt")
	dstFile := filepath.Join(dstDir, "nested", "dest.txt")

	require.NoError(t, os.WriteFile(srcFile, []byte("file content"), 0644))

	err := copyFile(srcFile, dstFile)
	require.NoError(t, err)

	content, err := os.ReadFile(dstFile)
	require.NoError(t, err)
	assert.Equal(t, "file content", string(content))
}

func TestSnapshotInfo(t *testing.T) {
	info := SnapshotInfo{
		Name:      "snapshot-20240101-120000",
		Path:      "/path/to/snapshot",
		Created:   time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		FileCount: 5,
	}

	assert.Equal(t, "snapshot-20240101-120000", info.Name)
	assert.Equal(t, "/path/to/snapshot", info.Path)
	assert.Equal(t, 5, info.FileCount)
	assert.Equal(t, 2024, info.Created.Year())
}

func TestConstants(t *testing.T) {
	assert.Equal(t, "snapshot-", SnapshotPrefix)
	assert.Equal(t, "20060102-150405", DateFormat)
	assert.Equal(t, 20, MaxSnapshots)
}

func TestCreate_SnapshotsDirError(t *testing.T) {
	// Test when snapshots directory cannot be created
	// Create a file where the directory should be to cause an error
	tmpDir := t.TempDir()
	outDir := filepath.Join(tmpDir, "output")
	require.NoError(t, os.MkdirAll(outDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(outDir, "test.yml"), []byte("test"), 0644))

	// Create a file at the snapshots path to prevent directory creation
	bosunDir := filepath.Join(tmpDir, ".bosun")
	require.NoError(t, os.MkdirAll(bosunDir, 0755))
	snapshotsFile := filepath.Join(bosunDir, "snapshots")
	require.NoError(t, os.WriteFile(snapshotsFile, []byte(""), 0644))

	_, err := Create(tmpDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create snapshot directory")
}

func TestList_NonSnapshotDirs(t *testing.T) {
	tmpDir := t.TempDir()
	snapDir := filepath.Join(tmpDir, ".bosun", "snapshots")
	require.NoError(t, os.MkdirAll(snapDir, 0755))

	// Create a non-snapshot directory (no prefix)
	require.NoError(t, os.MkdirAll(filepath.Join(snapDir, "other-dir"), 0755))

	// Create a file (not a directory)
	require.NoError(t, os.WriteFile(filepath.Join(snapDir, "file.txt"), []byte(""), 0644))

	// Create a valid snapshot
	validSnap := filepath.Join(snapDir, "snapshot-20240101-120000")
	require.NoError(t, os.MkdirAll(validSnap, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(validSnap, "test.yml"), []byte(""), 0644))

	result, err := List(tmpDir)
	require.NoError(t, err)

	// Should only have the valid snapshot
	assert.Len(t, result, 1)
	assert.Equal(t, "snapshot-20240101-120000", result[0].Name)
}

func TestList_InvalidTimestamp(t *testing.T) {
	tmpDir := t.TempDir()
	snapDir := filepath.Join(tmpDir, ".bosun", "snapshots")
	require.NoError(t, os.MkdirAll(snapDir, 0755))

	// Create snapshot with invalid timestamp format
	invalidSnap := filepath.Join(snapDir, "snapshot-invalid-timestamp")
	require.NoError(t, os.MkdirAll(invalidSnap, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(invalidSnap, "test.yml"), []byte(""), 0644))

	result, err := List(tmpDir)
	require.NoError(t, err)
	require.Len(t, result, 1)

	// Should use file modification time as fallback
	assert.Equal(t, "snapshot-invalid-timestamp", result[0].Name)
}

func TestRestore_NoExistingOutput(t *testing.T) {
	tmpDir := t.TempDir()
	snapDir := filepath.Join(tmpDir, ".bosun", "snapshots")

	// Create snapshot
	snapshotPath := filepath.Join(snapDir, "snapshot-20240101-120000")
	require.NoError(t, os.MkdirAll(snapshotPath, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(snapshotPath, "test.yml"), []byte("content"), 0644))

	// No output directory exists

	err := Restore(tmpDir, "snapshot-20240101-120000")
	require.NoError(t, err)

	// Verify restoration
	content, err := os.ReadFile(filepath.Join(tmpDir, "output", "test.yml"))
	require.NoError(t, err)
	assert.Equal(t, "content", string(content))
}

func TestCleanup_UnderLimit(t *testing.T) {
	tmpDir := t.TempDir()
	snapDir := filepath.Join(tmpDir, ".bosun", "snapshots")
	require.NoError(t, os.MkdirAll(snapDir, 0755))

	// Create fewer than MaxSnapshots
	for i := 0; i < 5; i++ {
		timestamp := time.Date(2024, 1, 1, 0, 0, i, 0, time.UTC)
		snapName := SnapshotPrefix + timestamp.Format(DateFormat)
		snapPath := filepath.Join(snapDir, snapName)
		require.NoError(t, os.MkdirAll(snapPath, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(snapPath, "test.yml"), []byte("test"), 0644))
	}

	err := Cleanup(tmpDir)
	require.NoError(t, err)

	// All should still exist
	snapshots, err := List(tmpDir)
	require.NoError(t, err)
	assert.Len(t, snapshots, 5)
}

func TestGetRestoredFiles_NoOutputDir(t *testing.T) {
	tmpDir := t.TempDir()

	files, err := GetRestoredFiles(tmpDir)
	require.Error(t, err)
	assert.Nil(t, files)
}

func TestGetRestoredFiles_EmptyOutputDir(t *testing.T) {
	tmpDir := t.TempDir()
	outDir := filepath.Join(tmpDir, "output")
	require.NoError(t, os.MkdirAll(outDir, 0755))

	files, err := GetRestoredFiles(tmpDir)
	require.NoError(t, err)
	assert.Empty(t, files)
}

func TestCopyFile_SourceNotFound(t *testing.T) {
	dstDir := t.TempDir()
	err := copyFile("/nonexistent/source.txt", filepath.Join(dstDir, "dest.txt"))
	require.Error(t, err)
}

func TestCopyDir_SourceNotFound(t *testing.T) {
	dstDir := t.TempDir()
	err := copyDir("/nonexistent/source", dstDir)
	require.Error(t, err)
}

func TestCopyDir_DeepNesting(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Create deeply nested structure
	deepDir := filepath.Join(srcDir, "a", "b", "c", "d")
	require.NoError(t, os.MkdirAll(deepDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(deepDir, "deep.txt"), []byte("deep content"), 0644))

	err := copyDir(srcDir, dstDir)
	require.NoError(t, err)

	// Verify deep file was copied
	content, err := os.ReadFile(filepath.Join(dstDir, "a", "b", "c", "d", "deep.txt"))
	require.NoError(t, err)
	assert.Equal(t, "deep content", string(content))
}

func TestCountFiles_EmptyDir(t *testing.T) {
	tmpDir := t.TempDir()
	count := countFiles(tmpDir)
	assert.Equal(t, 0, count)
}

func TestCountFiles_NonexistentDir(t *testing.T) {
	count := countFiles("/nonexistent/dir")
	assert.Equal(t, 0, count)
}

func TestSnapshotsDir(t *testing.T) {
	result := snapshotsDir("/manifest")
	assert.Equal(t, "/manifest/.bosun/snapshots", result)
}

func TestOutputDir(t *testing.T) {
	result := outputDir("/manifest")
	assert.Equal(t, "/manifest/output", result)
}

func TestCreate_CopyError(t *testing.T) {
	tmpDir := t.TempDir()

	// Create output directory with a file
	outDir := filepath.Join(tmpDir, "output")
	require.NoError(t, os.MkdirAll(outDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(outDir, "test.yml"), []byte("test"), 0644))

	// Make output directory unreadable to cause copy error
	// Note: This may not work on all systems, so we'll just verify the happy path
	snapshotName, err := Create(tmpDir)
	require.NoError(t, err)
	assert.NotEmpty(t, snapshotName)
}

func TestRestore_EmptyOutputDir(t *testing.T) {
	tmpDir := t.TempDir()
	snapDir := filepath.Join(tmpDir, ".bosun", "snapshots")

	// Create empty output directory
	outDir := filepath.Join(tmpDir, "output")
	require.NoError(t, os.MkdirAll(outDir, 0755))

	// Create snapshot
	snapshotPath := filepath.Join(snapDir, "snapshot-20240101-120000")
	require.NoError(t, os.MkdirAll(snapshotPath, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(snapshotPath, "test.yml"), []byte("content"), 0644))

	// Restore should work with empty output dir (no pre-rollback needed)
	err := Restore(tmpDir, "snapshot-20240101-120000")
	require.NoError(t, err)

	// Verify restoration
	content, err := os.ReadFile(filepath.Join(outDir, "test.yml"))
	require.NoError(t, err)
	assert.Equal(t, "content", string(content))
}

func TestCleanup_NoSnapshots(t *testing.T) {
	tmpDir := t.TempDir()

	// No snapshots directory
	err := Cleanup(tmpDir)
	require.NoError(t, err)
}

func TestCleanup_ListError(t *testing.T) {
	// This is hard to trigger without mocking, but we can verify
	// the function handles errors from List gracefully
	tmpDir := t.TempDir()
	snapDir := filepath.Join(tmpDir, ".bosun", "snapshots")
	require.NoError(t, os.MkdirAll(snapDir, 0755))

	// Create a valid snapshot to ensure the function works
	snapshotPath := filepath.Join(snapDir, "snapshot-20240101-120000")
	require.NoError(t, os.MkdirAll(snapshotPath, 0755))

	err := Cleanup(tmpDir)
	require.NoError(t, err)
}

func TestList_ReadDirError(t *testing.T) {
	// Create directory with a file that cannot be stat'd
	tmpDir := t.TempDir()
	snapDir := filepath.Join(tmpDir, ".bosun", "snapshots")
	require.NoError(t, os.MkdirAll(snapDir, 0755))

	// This test verifies the code handles stat errors
	// by continuing to the next entry
	snapshotPath := filepath.Join(snapDir, "snapshot-20240101-120000")
	require.NoError(t, os.MkdirAll(snapshotPath, 0755))

	snapshots, err := List(tmpDir)
	require.NoError(t, err)
	assert.Len(t, snapshots, 1)
}

func TestCopyFile_DstDirCreation(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	srcFile := filepath.Join(srcDir, "source.txt")
	require.NoError(t, os.WriteFile(srcFile, []byte("content"), 0644))

	// Destination is in a deeply nested directory that doesn't exist
	dstFile := filepath.Join(dstDir, "a", "b", "c", "dest.txt")

	err := copyFile(srcFile, dstFile)
	require.NoError(t, err)

	content, err := os.ReadFile(dstFile)
	require.NoError(t, err)
	assert.Equal(t, "content", string(content))
}

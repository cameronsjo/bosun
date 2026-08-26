package fileutil_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cameronsjo/bosun/internal/fileutil"
	"github.com/cameronsjo/bosun/internal/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureJSONLogs runs fn with logging routed to a buffer at Debug level.
// Returns the captured JSON log lines (one per line).
func captureJSONLogs(t *testing.T, fn func()) []string {
	t.Helper()

	r, w, err := os.Pipe()
	require.NoError(t, err)

	oldStdout := os.Stdout
	os.Stdout = w

	log.Init(&log.Options{
		Format:   log.FormatJSON,
		Level:    log.DebugLevel,
		LevelSet: true,
	})

	fn()

	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	_ = r.Close()
	os.Stdout = oldStdout
	log.Init(&log.Options{Format: log.FormatConsole})

	lines := []string{}
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// findLogEntry parses each JSON line and returns the first whose "message" field
// matches the given value.
func findLogEntry(t *testing.T, lines []string, message string) map[string]any {
	t.Helper()
	for _, line := range lines {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if msg, _ := entry["message"].(string); msg == message {
			return entry
		}
	}
	return nil
}

func TestCopyFileIfChanged_EmitsWroteLogOnWrite(t *testing.T) {
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "source.txt")
	dst := filepath.Join(tmpDir, "dest.txt")
	require.NoError(t, os.WriteFile(src, []byte("hello world"), 0644))

	lines := captureJSONLogs(t, func() {
		changed, err := fileutil.CopyFileIfChanged(context.Background(), src, dst)
		require.NoError(t, err)
		assert.True(t, changed, "expected file to be written")
	})

	entry := findLogEntry(t, lines, "wrote")
	require.NotNil(t, entry, "expected 'wrote' log line, got: %v", lines)
	assert.Equal(t, src, entry["src"])
	assert.Equal(t, dst, entry["dst"])
	assert.Equal(t, float64(len("hello world")), entry["bytes"])
}

func TestCopyFileIfChanged_EmitsSkippedLogOnHashMatch(t *testing.T) {
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "source.txt")
	dst := filepath.Join(tmpDir, "dest.txt")
	content := []byte("identical content")
	require.NoError(t, os.WriteFile(src, content, 0644))
	require.NoError(t, os.WriteFile(dst, content, 0644))

	lines := captureJSONLogs(t, func() {
		changed, err := fileutil.CopyFileIfChanged(context.Background(), src, dst)
		require.NoError(t, err)
		assert.False(t, changed, "expected file to be skipped")
	})

	entry := findLogEntry(t, lines, "skipped")
	require.NotNil(t, entry, "expected 'skipped' log line, got: %v", lines)
	assert.Equal(t, src, entry["src"])
	assert.Equal(t, dst, entry["dst"])
	assert.Equal(t, "hash_match", entry["reason"])
}

func TestCopyDirIfChanged_EmitsPerFileLogs(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src")
	dstDir := filepath.Join(tmpDir, "dst")
	require.NoError(t, os.MkdirAll(srcDir, 0755))
	require.NoError(t, os.MkdirAll(dstDir, 0755))

	// Two files: one new (will be written), one identical (will be skipped).
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "new.txt"), []byte("new content"), 0644))

	sameContent := []byte("same content")
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "same.txt"), sameContent, 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dstDir, "same.txt"), sameContent, 0644))

	var written []string
	lines := captureJSONLogs(t, func() {
		var err error
		written, err = fileutil.CopyDirIfChanged(context.Background(), srcDir, dstDir)
		require.NoError(t, err)
	})

	assert.Equal(t, []string{"new.txt"}, written, "only new.txt should be reported as written")

	wroteEntry := findLogEntry(t, lines, "wrote")
	require.NotNil(t, wroteEntry, "expected 'wrote' log line for new.txt, got: %v", lines)
	assert.Equal(t, filepath.Join(srcDir, "new.txt"), wroteEntry["src"])

	skippedEntry := findLogEntry(t, lines, "skipped")
	require.NotNil(t, skippedEntry, "expected 'skipped' log line for same.txt, got: %v", lines)
	assert.Equal(t, filepath.Join(srcDir, "same.txt"), skippedEntry["src"])
	assert.Equal(t, "hash_match", skippedEntry["reason"])
}

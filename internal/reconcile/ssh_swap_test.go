package reconcile

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Command construction (#343) ---

func TestBuildRemoteSwapCommand(t *testing.T) {
	t.Run("orders move-aside before move-in before cleanup", func(t *testing.T) {
		cmd := buildRemoteSwapCommand("/appdata/compose", "/appdata/.deploy-tmp-1", "/appdata/compose.bosun-old.1", false)

		moveAside := strings.Index(cmd, "mv /appdata/compose /appdata/compose.bosun-old.1")
		moveIn := strings.Index(cmd, "mv /appdata/.deploy-tmp-1 /appdata/compose")
		cleanup := strings.Index(cmd, "rm -rf /appdata/compose.bosun-old.1")

		require.GreaterOrEqual(t, moveAside, 0, "move-aside step missing")
		require.GreaterOrEqual(t, moveIn, 0, "move-in step missing")
		require.GreaterOrEqual(t, cleanup, 0, "cleanup step missing")
		assert.Less(t, moveAside, moveIn, "old tree must be moved aside before the staged tree moves in")
		assert.Less(t, moveIn, cleanup, "old tree must be removed only after the staged tree is in place")
		assert.True(t, strings.HasPrefix(cmd, "set -e; "), "swap must run under set -e")
	})

	t.Run("never deletes the target before the replacement is in place", func(t *testing.T) {
		cmd := buildRemoteSwapCommand("/appdata/compose", "/tmp/stage", "/appdata/compose.bosun-old.1", false)

		moveIn := strings.Index(cmd, "mv /tmp/stage /appdata/compose")
		// The only `rm -rf <target>` allowed before the move-in would be the
		// #343 defect. It exists solely in the rollback branch (after `else`).
		rmTarget := strings.Index(cmd, "rm -rf /appdata/compose;")
		elsePos := strings.Index(cmd, "else")
		require.GreaterOrEqual(t, rmTarget, 0)
		assert.Greater(t, rmTarget, elsePos, "rm -rf <target> may appear only in the rollback branch")
		assert.Greater(t, rmTarget, moveIn, "target is never removed before the move-in runs")
	})

	t.Run("rollback clears a partial target before restoring (nest-into guard)", func(t *testing.T) {
		cmd := buildRemoteSwapCommand("/a/t", "/a/x", "/a/t.old", false)

		rollback := cmd[strings.Index(cmd, "else"):]
		clearPartial := strings.Index(rollback, "rm -rf /a/t")
		restore := strings.Index(rollback, "mv /a/t.old /a/t")
		require.GreaterOrEqual(t, clearPartial, 0, "rollback must clear a partial target")
		require.GreaterOrEqual(t, restore, 0, "rollback must restore the old tree")
		assert.Less(t, clearPartial, restore, "partial target must be cleared before the restore, or mv nests-into it")
		assert.Contains(t, rollback, "exit $status", "rollback must propagate the move-in's failure status")
	})

	t.Run("quotes paths with shell metacharacters", func(t *testing.T) {
		cmd := buildRemoteSwapCommand("/mnt/user/app data", "/mnt/user/.tmp 1", "/mnt/user/app data.bosun-old.1", false)

		assert.Contains(t, cmd, "'/mnt/user/app data'")
		assert.Contains(t, cmd, "'/mnt/user/.tmp 1'")
		assert.NotContains(t, cmd, "mv /mnt/user/app data ", "unquoted spaced path would split into two argv words")
	})

	t.Run("sync included only under FUSE settle discipline", func(t *testing.T) {
		assert.Contains(t, buildRemoteSwapCommand("/mnt/user/appdata", "/t", "/o", true), "sync; ")
		assert.NotContains(t, buildRemoteSwapCommand("/srv/appdata", "/t", "/o", false), "sync; ")
	})
}

func TestBuildRemoteRecoverCommand(t *testing.T) {
	cmd := buildRemoteRecoverCommand("/appdata/compose")

	t.Run("promotes only when the target is missing", func(t *testing.T) {
		assert.Contains(t, cmd, "if [ ! -e /appdata/compose ]")
		assert.Contains(t, cmd, "sort | tail -n 1", "newest retained copy wins (fixed-width unix-nano timestamps sort lexically)")
	})

	t.Run("glob star stays outside the quoting", func(t *testing.T) {
		spaced := buildRemoteRecoverCommand("/mnt/user/app data")
		assert.Contains(t, spaced, "'/mnt/user/app data.bosun-old.'*", "a quoted star would never expand on the remote shell")
	})

	t.Run("orphan cleanup follows the promotion under set -e", func(t *testing.T) {
		require.True(t, strings.HasPrefix(cmd, "set -e; "), "a failed promotion must abort before the cleanup deletes the only surviving copy")
		promote := strings.Index(cmd, "mv \"$newest\"")
		cleanup := strings.Index(cmd, "rm -rf /appdata/compose.bosun-old.*")
		require.GreaterOrEqual(t, promote, 0)
		require.GreaterOrEqual(t, cleanup, 0)
		assert.Less(t, promote, cleanup)
	})
}

// --- Behavioral tests: execute the built shell against real directories ---

// runSwapShell executes a built command with /bin/sh, returning its exit error.
// The shell interpreter is deliberate: the string under test IS a shell command
// (production ships it over ssh, where the remote shell parses it), and the
// shellquote quoting these tests assert is the injection defense being verified.
// All paths originate from t.TempDir(), never user input.
func runSwapShell(t *testing.T, cmd string, extraPath string) error {
	t.Helper()
	c := exec.Command("/bin/sh", "-c", cmd)
	if extraPath != "" {
		c.Env = append(os.Environ(), "PATH="+extraPath+":"+os.Getenv("PATH"))
	}
	out, err := c.CombinedOutput()
	if len(out) > 0 {
		t.Logf("shell output: %s", out)
	}
	return err
}

func writeMarker(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}

func readMarker(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	require.NoError(t, err)
	return string(b)
}

func TestRemoteSwapCommand_Behavior(t *testing.T) {
	t.Run("happy path replaces the target and removes the retained copy", func(t *testing.T) {
		base := t.TempDir()
		target := filepath.Join(base, "compose")
		tmp := filepath.Join(base, ".deploy-tmp-1")
		old := target + bosunOldSuffix + "1111111111111111111"
		writeMarker(t, target, "marker", "old")
		writeMarker(t, tmp, "marker", "new")

		require.NoError(t, runSwapShell(t, buildRemoteSwapCommand(target, tmp, old, false), ""))

		assert.Equal(t, "new", readMarker(t, target, "marker"))
		assert.NoDirExists(t, old, "retained copy must be cleaned up after a successful swap")
		assert.NoDirExists(t, tmp)
	})

	t.Run("first deploy with no existing target succeeds", func(t *testing.T) {
		base := t.TempDir()
		target := filepath.Join(base, "compose")
		tmp := filepath.Join(base, ".deploy-tmp-1")
		old := target + bosunOldSuffix + "1111111111111111111"
		writeMarker(t, tmp, "marker", "new")

		require.NoError(t, runSwapShell(t, buildRemoteSwapCommand(target, tmp, old, false), ""))

		assert.Equal(t, "new", readMarker(t, target, "marker"))
	})

	t.Run("failed move-in restores the old target and exits nonzero", func(t *testing.T) {
		base := t.TempDir()
		target := filepath.Join(base, "compose")
		tmp := filepath.Join(base, ".deploy-tmp-missing") // does not exist -> mv fails
		old := target + bosunOldSuffix + "1111111111111111111"
		writeMarker(t, target, "marker", "old")

		err := runSwapShell(t, buildRemoteSwapCommand(target, tmp, old, false), "")

		require.Error(t, err, "a failed move-in must propagate a nonzero exit")
		assert.Equal(t, "old", readMarker(t, target, "marker"), "old tree must be restored")
		assert.NoDirExists(t, old, "the retained copy was moved back, not leaked")
	})

	t.Run("rollback clears a partial target instead of nesting the old tree into it", func(t *testing.T) {
		base := t.TempDir()
		target := filepath.Join(base, "compose")
		tmp := filepath.Join(base, ".deploy-tmp-1")
		old := target + bosunOldSuffix + "1111111111111111111"
		writeMarker(t, target, "marker", "old")
		writeMarker(t, tmp, "marker", "new")

		// PATH-shimmed mv: the move-in (tmp -> target) creates a PARTIAL
		// destination then fails, reproducing a cross-device shfs mv dying
		// mid-copy. Every other mv call passes through to the real binary.
		shimDir := t.TempDir()
		shim := "#!/bin/sh\n" +
			"if [ \"$1\" = \"" + tmp + "\" ] && [ \"$2\" = \"" + target + "\" ]; then\n" +
			"  mkdir -p \"" + target + "\"\n" +
			"  echo partial > \"" + target + "/marker\"\n" +
			"  exit 1\n" +
			"fi\n" +
			"exec /bin/mv \"$@\"\n"
		require.NoError(t, os.WriteFile(filepath.Join(shimDir, "mv"), []byte(shim), 0o755))

		err := runSwapShell(t, buildRemoteSwapCommand(target, tmp, old, false), shimDir)

		require.Error(t, err)
		assert.Equal(t, "old", readMarker(t, target, "marker"), "the restored target must hold the OLD tree, not the partial one")
		assert.NoDirExists(t, filepath.Join(target, filepath.Base(old)), "old tree must not be nested inside the partial target")
		assert.NoDirExists(t, old)
	})
}

func TestRemoteRecoverCommand_Behavior(t *testing.T) {
	t.Run("promotes the newest retained copy when the target is missing", func(t *testing.T) {
		base := t.TempDir()
		target := filepath.Join(base, "compose")
		older := target + bosunOldSuffix + "1111111111111111111"
		newer := target + bosunOldSuffix + "2222222222222222222"
		writeMarker(t, older, "marker", "older")
		writeMarker(t, newer, "marker", "newer")

		require.NoError(t, runSwapShell(t, buildRemoteRecoverCommand(target), ""))

		assert.Equal(t, "newer", readMarker(t, target, "marker"), "the newest retained copy must be promoted")
		assert.NoDirExists(t, older, "remaining orphans must be cleaned")
		assert.NoDirExists(t, newer)
	})

	t.Run("existing target is untouched, orphans still cleaned", func(t *testing.T) {
		base := t.TempDir()
		target := filepath.Join(base, "compose")
		orphan := target + bosunOldSuffix + "1111111111111111111"
		writeMarker(t, target, "marker", "live")
		writeMarker(t, orphan, "marker", "stale")

		require.NoError(t, runSwapShell(t, buildRemoteRecoverCommand(target), ""))

		assert.Equal(t, "live", readMarker(t, target, "marker"))
		assert.NoDirExists(t, orphan)
	})

	t.Run("no target and no retained copies is a no-op", func(t *testing.T) {
		base := t.TempDir()
		target := filepath.Join(base, "compose")

		require.NoError(t, runSwapShell(t, buildRemoteRecoverCommand(target), ""))

		assert.NoDirExists(t, target)
	})
}

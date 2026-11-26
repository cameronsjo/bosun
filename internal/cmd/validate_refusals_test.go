package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/reconcile"
)

// withoutAgeIdentity leaves no Age identity anywhere the SOPS preflight looks,
// so a configuration that decrypts secrets cannot start.
func withoutAgeIdentity(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the Age identity lookup uses the Unix home directory")
	}
	t.Setenv("SOPS_AGE_KEY", "")
	t.Setenv("SOPS_AGE_KEY_FILE", "")
	t.Setenv("HOME", t.TempDir())
}

// TestValidateReconcileConfig_ReportsWhatReconcileRefuses pins that the quick
// check reports the configurations `bosun reconcile` refuses to start on, the
// ones the help text and docs promise: a missing Age identity for the secrets
// files, targets that cannot be resolved, and a malformed BOSUN_TARGETS that
// reconcile would otherwise drop with a warning, leaving validation to look at
// the implicit default target only. Before the fix none of them was reported.
func TestValidateReconcileConfig_ReportsWhatReconcileRefuses(t *testing.T) {
	t.Run("secrets configured and no Age identity", func(t *testing.T) {
		setValidateEnv(t)
		withoutAgeIdentity(t)
		t.Setenv("BOSUN_SECRETS_FILE", "secrets.sops.yaml")
		chdirValidateProject(t, "")

		assert.Positive(t, validateReconcileConfig())
	})

	t.Run("the reserved default target beside another target", func(t *testing.T) {
		setValidateEnv(t)
		setValidatePaths(t)
		t.Setenv("BOSUN_TARGETS", `[{"name":"default"},{"name":"b"}]`)
		chdirValidateProject(t, "")

		assert.Positive(t, validateReconcileConfig())
	})

	t.Run("targets whose staging paths nest", func(t *testing.T) {
		setValidateEnv(t)
		staging, _, _ := setValidatePaths(t)
		t.Setenv("BOSUN_TARGETS", fmt.Sprintf(
			`[{"name":"a","project_name":"proj-a","local_appdata_path":"/mnt/appdata/a","staging_dir":%q},`+
				`{"name":"b","project_name":"proj-b","local_appdata_path":"/mnt/appdata/b","staging_dir":%q}]`,
			filepath.Join(staging, "x"), filepath.Join(staging, "x", "y")))
		chdirValidateProject(t, "")

		assert.Positive(t, validateReconcileConfig())
	})

	t.Run("BOSUN_TARGETS that is not valid JSON", func(t *testing.T) {
		setValidateEnv(t)
		setValidatePaths(t)
		t.Setenv("BOSUN_TARGETS", `[{"name":`)
		chdirValidateProject(t, "")

		assert.Positive(t, validateReconcileConfig())
	})

	t.Run("two valid targets report nothing", func(t *testing.T) {
		setValidateEnv(t)
		setValidatePaths(t)
		t.Setenv("BOSUN_TARGETS", validateTwoTargets)
		chdirValidateProject(t, "")

		assert.Zero(t, validateReconcileConfig())
	})
}

// TestRunFullDryRunWith_RefusesBeforeAnyTargetRuns pins that the full dry run
// makes the startup checks `bosun reconcile` makes before it touches Git, and
// that a malformed BOSUN_TARGETS is an error rather than a silent fall back to
// the default target. Before the fix the runner was called in both cases.
func TestRunFullDryRunWith_RefusesBeforeAnyTargetRuns(t *testing.T) {
	t.Run("secrets configured and no Age identity", func(t *testing.T) {
		setValidateEnv(t)
		setValidatePaths(t)
		withoutAgeIdentity(t)
		t.Setenv("BOSUN_SECRETS_FILE", "secrets.sops.yaml")
		chdirValidateProject(t, "")

		var runs []recordedValidateRun
		err := runFullDryRunWith(recordingValidateRunner(&runs, ""))

		require.Error(t, err)
		assert.Empty(t, runs)
	})

	t.Run("BOSUN_TARGETS that is not valid JSON", func(t *testing.T) {
		setValidateEnv(t)
		setValidatePaths(t)
		t.Setenv("BOSUN_TARGETS", `[{"name":`)
		chdirValidateProject(t, "")

		var runs []recordedValidateRun
		err := runFullDryRunWith(recordingValidateRunner(&runs, ""))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "BOSUN_TARGETS")
		assert.Empty(t, runs)
	})
}

// TestRunFullDryRunWith_FileSystemFailures injects the two failures the full dry
// run can hit around the reconciler: a staging tree that cannot be secured or
// removed, and no place to put the scratch copy of the deploy state. Each is an
// error and no runner is called for it.
func TestRunFullDryRunWith_FileSystemFailures(t *testing.T) {
	t.Run("a staging tree that cannot be secured or removed", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("file modes do not restrict access on Windows")
		}
		if os.Geteuid() == 0 {
			t.Skip("root ignores file modes")
		}
		setValidateEnv(t)
		staging, _, _ := setValidatePaths(t)
		locked := filepath.Join(staging, "locked")
		require.NoError(t, os.MkdirAll(locked, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(locked, "plaintext"), []byte("leftover"), 0o600))
		require.NoError(t, os.Chmod(locked, 0))
		t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
		chdirValidateProject(t, "")

		var runs []recordedValidateRun
		err := runFullDryRunWith(recordingValidateRunner(&runs, ""))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "staging evidence preflight")
		assert.Empty(t, runs)
	})

	t.Run("no directory for the scratch state copy", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("the temporary directory is not read from TMPDIR on Windows")
		}
		t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))

		called := false
		err := runFullDryRunTarget(reconcile.DefaultConfig(), func(_ context.Context, _ *reconcile.Config) error {
			called = true
			return nil
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "prepare state file")
		assert.False(t, called)
	})
}

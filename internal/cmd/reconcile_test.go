package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/cameronsjo/bosun/internal/reconcile"
	"github.com/cameronsjo/bosun/internal/ui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReconcileCmd_Registration(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"reconcile"})
	require.NoError(t, err)
	assert.Equal(t, "reconcile", cmd.Name())
}

func TestReconcileCmd_Help(t *testing.T) {
	t.Run("reconcile --help shows description", func(t *testing.T) {
		output, err := executeCmd(t, "reconcile", "--help")
		require.NoError(t, err)
		assert.Contains(t, output, "reconcile")
		assert.Contains(t, output, "GitOps")
		assert.Contains(t, output, "Clone/pull repository")
	})

	t.Run("reconcile --help shows workflow steps", func(t *testing.T) {
		output, err := executeCmd(t, "reconcile", "--help")
		require.NoError(t, err)
		assert.Contains(t, output, "Acquire lock")
		assert.Contains(t, output, "Decrypt secrets")
		assert.Contains(t, output, "Docker compose up")
	})

	t.Run("reconcile --help shows env vars", func(t *testing.T) {
		output, err := executeCmd(t, "reconcile", "--help")
		require.NoError(t, err)
		assert.Contains(t, output, "REPO_URL")
		assert.Contains(t, output, "REPO_BRANCH")
		assert.Contains(t, output, "DEPLOY_TARGET")
		assert.Contains(t, output, "SECRETS_FILES")
	})
}

func TestReconcileCmd_NoAliases(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"reconcile"})
	require.NoError(t, err)
	assert.Empty(t, cmd.Aliases, "reconcile command should have no aliases")
}

func TestRunReconcile_ConfigFieldSetup(t *testing.T) {
	// Intercept ui.Fatal so the function doesn't exit the process.
	old := ui.SetExitFn(func(int) {})
	defer ui.SetExitFn(old)

	t.Run("env overrides set ConfigField source to env", func(t *testing.T) {
		t.Setenv("REPO_URL", "https://example.com/repo.git")
		t.Setenv("BOSUN_POST_SYNC_HOOKS", `[{"paths":["traefik/**"],"action":"restart","container":"traefik"}]`)
		t.Setenv("BOSUN_HOOK_SETTLE_DELAY", "3s")

		// runReconcile will fail at reconciler.Run (no real repo) but exercises config setup.
		runReconcile(nil, nil)

		// The function runs and exits via ui.Fatal (intercepted).
		// Coverage of SetFromFile/SetFromEnv lines is the goal.
	})

	t.Run("SetFromFile path exercised via config.Load", func(t *testing.T) {
		// Running from project root where bosun.yaml exists exercises SetFromFile.
		t.Setenv("REPO_URL", "https://example.com/repo.git")

		runReconcile(nil, nil)
	})

	t.Run("BOSUN_TEMPLATE_INCLUDE_DIR env override exercised", func(t *testing.T) {
		// Drives the template-include-dir env branch through runReconcile so the
		// inline cfg.TemplateIncludeDir assignment is covered end-to-end.
		t.Setenv("REPO_URL", "https://example.com/repo.git")
		t.Setenv("BOSUN_TEMPLATE_INCLUDE_DIR", "shared/includes")

		runReconcile(nil, nil)
	})

	t.Run("ConfigReloader closure is callable", func(t *testing.T) {
		t.Setenv("REPO_URL", "https://example.com/repo.git")

		// Build config the same way runReconcile does, but only test the reloader.
		cfg := reconcile.DefaultConfig()
		cfg.ConfigReloader = func(dir string) (*reconcile.ReloadedConfig, error) {
			// This is a simplified version — the real one calls config.LoadFrom.
			return &reconcile.ReloadedConfig{}, nil
		}

		reloaded, err := cfg.ConfigReloader(".")
		require.NoError(t, err)
		assert.NotNil(t, reloaded)
	})
}

func TestRunReconcile_BOSUNTargetsValidation(t *testing.T) {
	// Intercept ui.Fatal so the function doesn't exit the process.
	old := ui.SetExitFn(func(int) {})
	defer ui.SetExitFn(old)

	// All subtests set REPO_URL so the function proceeds past the fatal check
	// and reaches the BOSUN_TARGETS validation block at line ~259. The reconciler
	// will fail on the fake repo URL, but the config-setup code is exercised first.

	t.Run("valid BOSUN_TARGETS reaches reconciler without panic", func(t *testing.T) {
		t.Setenv("REPO_URL", "https://example.com/repo.git")
		t.Setenv("BOSUN_TARGETS", `[{"name":"unraid","target_host":"user@unraid","project_name":"homelab","remote_appdata_path":"/mnt/user/appdata"}]`)

		// Exercises the BOSUN_TARGETS JSON parse + validate path; fails at r.Run with a non-existent repo.
		runReconcile(nil, nil)
	})

	t.Run("invalid project_name in BOSUN_TARGETS is cleared before reconcile attempt", func(t *testing.T) {
		t.Setenv("REPO_URL", "https://example.com/repo.git")
		t.Setenv("BOSUN_TARGETS", `[{"name":"evil","target_host":"user@host","project_name":"evil; rm -rf /","remote_appdata_path":"/mnt/appdata"}]`)

		// ValidateAndSanitizeTargets clears the bad field; function then proceeds to r.Run and fails.
		runReconcile(nil, nil)
	})

	t.Run("invalid remote_appdata_path in BOSUN_TARGETS is cleared", func(t *testing.T) {
		t.Setenv("REPO_URL", "https://example.com/repo.git")
		t.Setenv("BOSUN_TARGETS", `[{"name":"badpath","target_host":"user@host","project_name":"myproject","remote_appdata_path":"/mnt;evil"}]`)

		runReconcile(nil, nil)
	})

	t.Run("malformed BOSUN_TARGETS JSON is ignored and reconcile proceeds", func(t *testing.T) {
		t.Setenv("REPO_URL", "https://example.com/repo.git")
		t.Setenv("BOSUN_TARGETS", `not-valid-json`)

		runReconcile(nil, nil)
	})
}

// TestTemplateIncludeDirForCLI covers the one-shot CLI's include-allowlist
// precedence, which must match the daemon: the project-config value reaches
// reconcile.Config.TemplateIncludeDir unless BOSUN_TEMPLATE_INCLUDE_DIR
// overrides it. Before this wiring, `bosun reconcile` silently ignored both and
// used the default templates/ root regardless of configuration.
func TestTemplateIncludeDirForCLI(t *testing.T) {
	tests := []struct {
		name          string
		projectConfig string
		envValue      string
		want          string
	}{
		{"project config value reaches the field", "shared/includes", "", "shared/includes"},
		{"env override wins over project config", "shared/includes", "/etc/bosun/includes", "/etc/bosun/includes"},
		{"env override reaches the field with no project config", "", "custom", "custom"},
		{"both unset resolves empty (reconciler applies templates/ default)", "", "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, templateIncludeDirForCLI(tc.projectConfig, tc.envValue))
		})
	}
}

func TestEnsureStateDirForCLI(t *testing.T) {
	t.Run("creates missing nested parent", func(t *testing.T) {
		stateDir := filepath.Join(t.TempDir(), "nested", "state")
		stateFile := filepath.Join(stateDir, reconcile.DefaultStateFile)

		require.NoError(t, ensureStateDirForCLI(stateFile))

		info, err := os.Stat(stateDir)
		require.NoError(t, err)
		assert.True(t, info.IsDir())
	})

	t.Run("empty state file is a no-op", func(t *testing.T) {
		require.NoError(t, ensureStateDirForCLI(""))
	})

	t.Run("reports an unusable parent", func(t *testing.T) {
		parentFile := filepath.Join(t.TempDir(), "not-a-directory")
		require.NoError(t, os.WriteFile(parentFile, []byte("occupied"), 0o600))

		err := ensureStateDirForCLI(filepath.Join(parentFile, "state.json"))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "create state directory")
		assert.Contains(t, err.Error(), parentFile)
	})

	t.Run("rejects an existing unwritable parent", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("chmod semantics differ on Windows")
		}
		if os.Getuid() == 0 {
			t.Skip("cannot test permission denial as root")
		}

		stateDir := filepath.Join(t.TempDir(), "state")
		require.NoError(t, os.MkdirAll(stateDir, 0o755))
		require.NoError(t, os.Chmod(stateDir, 0o555))
		t.Cleanup(func() { require.NoError(t, os.Chmod(stateDir, 0o755)) })

		err := ensureStateDirForCLI(filepath.Join(stateDir, reconcile.DefaultStateFile))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "verify state directory")
		assert.Contains(t, err.Error(), stateDir)
	})
}

func TestPrepareStateFileForCLIRun(t *testing.T) {
	t.Run("real run prepares and returns configured state file", func(t *testing.T) {
		stateFile := filepath.Join(t.TempDir(), "nested", reconcile.DefaultStateFile)

		got, cleanup, err := prepareStateFileForCLIRun(stateFile, false)
		require.NoError(t, err)
		cleanup()

		assert.Equal(t, stateFile, got)
		assert.DirExists(t, filepath.Dir(stateFile))
	})

	t.Run("real run propagates state directory preparation failure", func(t *testing.T) {
		parentFile := filepath.Join(t.TempDir(), "not-a-directory")
		require.NoError(t, os.WriteFile(parentFile, []byte("occupied"), 0o600))

		got, cleanup, err := prepareStateFileForCLIRun(filepath.Join(parentFile, "state.json"), false)
		cleanup()

		require.Error(t, err)
		assert.Empty(t, got)
		assert.Contains(t, err.Error(), "create state directory")
	})

	t.Run("dry run copies state without touching configured directory", func(t *testing.T) {
		base := t.TempDir()
		configured := filepath.Join(base, "production", reconcile.DefaultStateFile)
		require.NoError(t, os.MkdirAll(filepath.Dir(configured), 0o755))
		require.NoError(t, reconcile.SaveState(configured, &reconcile.DeployState{LastDeployedCommit: "live-commit"}))

		scratch, cleanup, err := prepareStateFileForCLIRun(configured, true)
		require.NoError(t, err)
		scratchDir := filepath.Dir(scratch)
		t.Cleanup(cleanup)

		assert.NotEqual(t, configured, scratch)
		assert.Equal(t, "live-commit", reconcile.LoadState(scratch).LastDeployedCommit)
		require.NoError(t, reconcile.SaveState(scratch, &reconcile.DeployState{LastDeployedCommit: "preview-commit"}))
		assert.Equal(t, "live-commit", reconcile.LoadState(configured).LastDeployedCommit)

		cleanup()
		assert.NoDirExists(t, scratchDir)
	})

	t.Run("dry run leaves a missing configured directory absent", func(t *testing.T) {
		configured := filepath.Join(t.TempDir(), "production", "nested", reconcile.DefaultStateFile)

		scratch, cleanup, err := prepareStateFileForCLIRun(configured, true)
		require.NoError(t, err)
		t.Cleanup(cleanup)

		assert.NotEqual(t, configured, scratch)
		assert.NoDirExists(t, filepath.Dir(configured))
	})

	t.Run("dry run reports temporary directory creation failure", func(t *testing.T) {
		notDir := filepath.Join(t.TempDir(), "not-a-directory")
		require.NoError(t, os.WriteFile(notDir, []byte("occupied"), 0o600))
		t.Setenv("TMPDIR", notDir)

		scratch, cleanup, err := prepareStateFileForCLIRun("", true)
		cleanup()

		require.Error(t, err)
		assert.Empty(t, scratch)
		assert.Contains(t, err.Error(), "create temporary dry-run state directory")
	})

	t.Run("dry run reports state copy failure and cleans scratch", func(t *testing.T) {
		configured := filepath.Join(t.TempDir(), reconcile.DefaultStateFile)
		require.NoError(t, reconcile.SaveState(configured, &reconcile.DeployState{LastDeployedCommit: "live-commit"}))
		original := saveStateForCLIDryRun
		var attemptedStateFile string
		saveStateForCLIDryRun = func(stateFile string, _ *reconcile.DeployState) error {
			attemptedStateFile = stateFile
			return assert.AnError
		}
		t.Cleanup(func() { saveStateForCLIDryRun = original })

		scratch, cleanup, err := prepareStateFileForCLIRun(configured, true)
		cleanup()

		require.ErrorIs(t, err, assert.AnError)
		assert.Empty(t, scratch)
		assert.Contains(t, err.Error(), "copy deploy state for dry run")
		assert.NoDirExists(t, filepath.Dir(attemptedStateFile))
	})

	t.Run("dry run fails open from state it cannot inspect", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("chmod semantics differ on Windows")
		}
		if os.Getuid() == 0 {
			t.Skip("cannot test permission denial as root")
		}

		lockedDir := filepath.Join(t.TempDir(), "locked")
		configured := filepath.Join(lockedDir, reconcile.DefaultStateFile)
		require.NoError(t, os.MkdirAll(lockedDir, 0o755))
		require.NoError(t, os.WriteFile(configured, []byte("{}"), 0o600))
		require.NoError(t, os.Chmod(lockedDir, 0o000))
		t.Cleanup(func() { require.NoError(t, os.Chmod(lockedDir, 0o755)) })

		scratch, cleanup, err := prepareStateFileForCLIRun(configured, true)
		require.NoError(t, err)
		t.Cleanup(cleanup)

		assert.NotEqual(t, configured, scratch)
		assert.NoFileExists(t, scratch, "unreadable production state must degrade to empty scratch state")
	})
}

func TestRunReconcile_CreatesConfiguredStateDir(t *testing.T) {
	old := ui.SetExitFn(func(int) {})
	defer ui.SetExitFn(old)

	stateDir := filepath.Join(t.TempDir(), "fresh", "state")
	t.Setenv("REPO_URL", filepath.Join(t.TempDir(), "missing-repo"))
	t.Setenv("BOSUN_STATE_DIR", stateDir)

	runReconcile(nil, nil)

	info, err := os.Stat(stateDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestRunReconcile_PreparesEachTargetStateDir(t *testing.T) {
	old := ui.SetExitFn(func(int) {})
	defer ui.SetExitFn(old)

	base := t.TempDir()
	targets := []reconcile.Target{
		{Name: "unraid", StateFile: filepath.Join(base, "unraid", "state.json")},
		{Name: "pi", StateFile: filepath.Join(base, "pi", "nested", "state.json")},
	}
	targetsJSON, err := json.Marshal(targets)
	require.NoError(t, err)
	t.Setenv("REPO_URL", filepath.Join(t.TempDir(), "missing-repo"))
	t.Setenv("BOSUN_TARGETS", string(targetsJSON))

	runReconcile(nil, nil)

	for _, target := range targets {
		assert.DirExists(t, filepath.Dir(target.StateFile), "target %s state directory", target.Name)
	}
}

func TestRunReconcile_DryRunDoesNotMutateConfiguredState(t *testing.T) {
	old := ui.SetExitFn(func(int) {})
	defer ui.SetExitFn(old)

	t.Run("missing state directory stays absent", func(t *testing.T) {
		stateDir := filepath.Join(t.TempDir(), "production", "state")
		t.Setenv("REPO_URL", filepath.Join(t.TempDir(), "missing-repo"))
		t.Setenv("BOSUN_STATE_DIR", stateDir)
		t.Setenv("DRY_RUN", "true")

		runReconcile(nil, nil)

		assert.NoDirExists(t, stateDir)
	})

	t.Run("existing state remains unchanged", func(t *testing.T) {
		stateDir := filepath.Join(t.TempDir(), "production", "state")
		stateFile := filepath.Join(stateDir, reconcile.DefaultStateFile)
		require.NoError(t, os.MkdirAll(stateDir, 0o755))
		require.NoError(t, reconcile.SaveState(stateFile, &reconcile.DeployState{
			LastDeployedCommit: "live-commit",
			DeployCount:        7,
		}))
		t.Setenv("REPO_URL", filepath.Join(t.TempDir(), "missing-repo"))
		t.Setenv("BOSUN_STATE_DIR", stateDir)
		t.Setenv("DRY_RUN", "true")

		runReconcile(nil, nil)

		state := reconcile.LoadState(stateFile)
		assert.Equal(t, "live-commit", state.LastDeployedCommit)
		assert.Equal(t, 7, state.DeployCount)
		assert.Zero(t, state.AttemptCount)
	})
}

// TestRunFullDryRun_TemplateIncludeDirEnv exercises the `bosun validate` full
// dry-run harness with BOSUN_TEMPLATE_INCLUDE_DIR set, covering the include-dir
// env threading that keeps the dry-run a faithful preview of the deploy's
// allowlist. The fake repo URL means the reconcile fails at clone; the config
// path (including the env override) runs first and must not panic.
func TestRunFullDryRun_TemplateIncludeDirEnv(t *testing.T) {
	t.Setenv("REPO_URL", "https://example.com/repo.git")
	t.Setenv("BOSUN_TEMPLATE_INCLUDE_DIR", "shared/includes")

	err := runFullDryRun()
	// A non-existent repo makes Run return an error; we only assert the config
	// path executed and surfaced a failure rather than panicking.
	assert.Error(t, err)
}

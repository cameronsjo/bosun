package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/config"
)

// chdirValidateProject writes a project with the given bosun.yaml and switches
// into it, so the project config the reconcile builder loads is that file.
func chdirValidateProject(t *testing.T, bosunYAML string) {
	t.Helper()

	projectDir := evalSymlinks(t, t.TempDir())
	require.NoError(t, os.Mkdir(filepath.Join(projectDir, "manifest"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "bosun.yaml"), []byte(bosunYAML), 0o644))

	origDir, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(projectDir))
}

// setValidateEnv points validate at a reachable-looking HTTPS repository with no
// credentials, and unsets the variables the reconcile builder would otherwise
// pick up from the machine running the test.
func setValidateEnv(t *testing.T) {
	t.Helper()
	t.Setenv("BOSUN_REPO_URL", "https://example.com/repo.git")
	t.Setenv("REPO_URL", "")
	t.Setenv("BOSUN_GIT_USERNAME", "")
	t.Setenv("BOSUN_GIT_TOKEN", "")
	t.Setenv("BOSUN_TARGETS", "")
	for _, key := range []string{"BOSUN_SECRETS_FILE", "SECRETS_FILES"} {
		if value, present := os.LookupEnv(key); present {
			t.Setenv(key, value)
		}
		require.NoError(t, os.Unsetenv(key))
	}
}

const invalidPreDeployHookYAML = "pre_deploy_hooks:\n  - name: gate\n    command: []\n"

// TestRunFullDryRun_ReadsProjectConfig pins that `bosun validate --full` builds
// its reconciler configuration from the same environment and bosun.yaml as
// `bosun reconcile`. Before the fix it read four variables by hand and never
// loaded the project config, so a bosun.yaml that reconcile refuses to start on
// went unnoticed until the real run.
func TestRunFullDryRun_ReadsProjectConfig(t *testing.T) {
	setValidateEnv(t)
	chdirValidateProject(t, invalidPreDeployHookYAML)

	err := runFullDryRun()

	require.Error(t, err)
	assert.ErrorIs(t, err, config.ErrInvalidPreDeployHooks)
}

// TestValidateReconcileConfig_ReportsConfigThatReconcileRejects pins the same for
// the quick checks: a configuration the reconcile command refuses to start on is
// reported by `bosun validate`. Before the fix neither case was seen.
func TestValidateReconcileConfig_ReportsConfigThatReconcileRejects(t *testing.T) {
	t.Run("pre_deploy_hooks entry that cannot run", func(t *testing.T) {
		setValidateEnv(t)
		chdirValidateProject(t, invalidPreDeployHookYAML)

		assert.Positive(t, validateReconcileConfig())
	})

	t.Run("BOSUN_SECRETS_FILE set but naming no file", func(t *testing.T) {
		setValidateEnv(t)
		t.Setenv("BOSUN_SECRETS_FILE", " , ")
		chdirValidateProject(t, "")

		assert.Positive(t, validateReconcileConfig())
	})

	t.Run("valid configuration reports nothing", func(t *testing.T) {
		setValidateEnv(t)
		chdirValidateProject(t, "")

		assert.Zero(t, validateReconcileConfig())
	})
}

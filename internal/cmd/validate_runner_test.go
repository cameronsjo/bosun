package cmd

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/config"
	"github.com/cameronsjo/bosun/internal/reconcile"
)

// recordedValidateRun is what a validateRunner received for one target.
type recordedValidateRun struct {
	cfg      *reconcile.Config
	optCount int
}

// recordingValidateRunner records every call and fails for the named target.
func recordingValidateRunner(runs *[]recordedValidateRun, failTarget string) validateRunner {
	return func(_ context.Context, cfg *reconcile.Config, opts ...reconcile.ReconcilerOption) error {
		*runs = append(*runs, recordedValidateRun{cfg: cfg, optCount: len(opts)})
		if failTarget != "" && cfg.TargetName == failTarget {
			return errors.New("simulated failure")
		}
		return nil
	}
}

// validateHookProjectYAML declares a pre-deploy hook and a post-sync hook, so a
// test can tell whether validation carries project hooks into the reconciler.
const validateHookProjectYAML = `pre_deploy_hooks:
  - name: gate
    command: ["true"]
post_sync_hooks:
  - paths: ["appdata/**"]
    action: restart
    container: web
`

// validateTwoTargets declares two targets; the first has a pre-deploy hook of
// its own, which ConfigForTarget copies onto that target's config.
const validateTwoTargets = `[{"name":"a","pre_deploy_hooks":[{"name":"target-gate","command":["true"]}]},{"name":"b"}]`

// setValidatePaths points the path variables at temporary directories and
// returns them.
func setValidatePaths(t *testing.T) (staging, repo, state string) {
	t.Helper()
	staging = evalSymlinks(t, t.TempDir())
	repo = evalSymlinks(t, t.TempDir())
	state = evalSymlinks(t, t.TempDir())
	t.Setenv("STAGING_DIR", staging)
	t.Setenv("REPO_DIR", repo)
	t.Setenv("BOSUN_STATE_DIR", state)
	t.Setenv("BOSUN_INFRA_DIR", "infra")
	return staging, repo, state
}

// TestBuildValidateReconcileConfig pins what `bosun validate` builds: the
// reconcile configuration from the same environment and project config, with the
// parts a validation must not run or send removed.
func TestBuildValidateReconcileConfig(t *testing.T) {
	setValidateEnv(t)
	staging, repo, state := setValidatePaths(t)
	secrets := filepath.Join(t.TempDir(), "secrets.sops.yaml")
	t.Setenv("BOSUN_SECRETS_FILE", secrets)
	t.Setenv("BOSUN_TARGETS", validateTwoTargets)
	t.Setenv("DEPLOY_TARGET", "")
	t.Setenv("DRY_RUN", "false")
	t.Setenv("FORCE", "true")
	chdirValidateProject(t, validateHookProjectYAML)

	// A flag variable left set by an earlier command in the same process must
	// not reach validate.
	defaults := reconcile.DefaultConfig()
	origDryRun, origForce, origLocal, origRemote := reconcileDryRun, reconcileForce, reconcileLocal, reconcileRemote
	t.Cleanup(func() {
		reconcileDryRun, reconcileForce, reconcileLocal, reconcileRemote = origDryRun, origForce, origLocal, origRemote
	})
	reconcileForce, reconcileLocal, reconcileRemote = true, true, "root@leaked-flag"

	cfg, err := buildValidateReconcileConfig()
	require.NoError(t, err)

	assert.Equal(t, staging, cfg.StagingDir)
	assert.Equal(t, repo, cfg.RepoDir)
	assert.Equal(t, "infra", cfg.InfraSubDir)
	assert.Equal(t, filepath.Join(state, reconcile.DefaultStateFile), cfg.StateFile)
	assert.Equal(t, []string{secrets}, cfg.SecretsFiles)
	require.Len(t, cfg.Targets, 2)
	assert.Equal(t, "a", cfg.Targets[0].Name)
	assert.Equal(t, "b", cfg.Targets[1].Name)

	assert.True(t, cfg.DryRun, "validate always runs a dry run")
	assert.False(t, cfg.Force, "FORCE in the environment must not reach validate")
	assert.Equal(t, defaults.DeployMode, cfg.DeployMode, "--local must not reach validate")
	assert.Empty(t, cfg.TargetHost, "--remote must not reach validate")
	assert.Empty(t, cfg.PreDeployHooks.Value, "validate runs no pre-deploy hooks")
	assert.Nil(t, cfg.ConfigReloader, "validate must not pull a hook list out of the cloned repository")
}

// TestRunFullDryRunWith_RunsEachTargetWithTheReconcileConfig pins that
// `bosun validate --full` runs the reconciler it is handed with the configuration
// built from the environment and project config, once per target, with no hooks,
// no reloader and no reconciler options (so no alerter).
func TestRunFullDryRunWith_RunsEachTargetWithTheReconcileConfig(t *testing.T) {
	setValidateEnv(t)
	staging, repo, state := setValidatePaths(t)
	t.Setenv("BOSUN_TARGETS", validateTwoTargets)
	t.Setenv("DEPLOY_TARGET", "")
	t.Setenv("FORCE", "true")
	chdirValidateProject(t, validateHookProjectYAML)

	var runs []recordedValidateRun
	err := runFullDryRunWith(recordingValidateRunner(&runs, ""))

	require.NoError(t, err)
	require.Len(t, runs, 2)
	names := []string{runs[0].cfg.TargetName, runs[1].cfg.TargetName}
	assert.ElementsMatch(t, []string{"a", "b"}, names)

	stateFiles := map[string]bool{}
	for _, run := range runs {
		name := run.cfg.TargetName
		assert.Zero(t, run.optCount, "target %s: validation attaches no reconciler options, so no alerter", name)
		assert.True(t, run.cfg.DryRun, "target %s", name)
		assert.False(t, run.cfg.Force, "target %s", name)
		assert.Empty(t, run.cfg.PreDeployHooks.Value, "target %s: no pre-deploy hooks, including the target's own", name)
		assert.Nil(t, run.cfg.ConfigReloader, "target %s", name)
		assert.Equal(t, repo, run.cfg.RepoDir, "target %s", name)
		assert.Equal(t, "infra", run.cfg.InfraSubDir, "target %s", name)
		assert.True(t, strings.HasPrefix(run.cfg.StagingDir, staging), "target %s: staging %q is not under STAGING_DIR %q", name, run.cfg.StagingDir, staging)
		assert.False(t, strings.HasPrefix(run.cfg.StateFile, state), "target %s: state file %q must be a scratch copy, not under BOSUN_STATE_DIR", name, run.cfg.StateFile)
		stateFiles[run.cfg.StateFile] = true
	}
	assert.Len(t, stateFiles, 2, "each target gets its own scratch state file")
}

// TestRunFullDryRunWith_TargetFailures covers the error returns: one failing
// target does not stop the others, and a configuration the reconcile command
// refuses is reported before any target runs.
func TestRunFullDryRunWith_TargetFailures(t *testing.T) {
	t.Run("a failing target is reported and the others still run", func(t *testing.T) {
		setValidateEnv(t)
		setValidatePaths(t)
		t.Setenv("BOSUN_TARGETS", validateTwoTargets)
		chdirValidateProject(t, "")

		var runs []recordedValidateRun
		err := runFullDryRunWith(recordingValidateRunner(&runs, "a"))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "target a")
		assert.NotContains(t, err.Error(), "target b")
		assert.Len(t, runs, 2)
	})

	t.Run("targets that cannot be resolved run nothing", func(t *testing.T) {
		setValidateEnv(t)
		setValidatePaths(t)
		t.Setenv("BOSUN_TARGETS", `[{"name":"default"},{"name":"b"}]`)
		chdirValidateProject(t, "")

		var runs []recordedValidateRun
		err := runFullDryRunWith(recordingValidateRunner(&runs, ""))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "resolve targets")
		assert.Empty(t, runs)
	})

	t.Run("project config that reconcile rejects runs nothing", func(t *testing.T) {
		setValidateEnv(t)
		setValidatePaths(t)
		chdirValidateProject(t, invalidPreDeployHookYAML)

		var runs []recordedValidateRun
		err := runFullDryRunWith(recordingValidateRunner(&runs, ""))

		require.Error(t, err)
		assert.ErrorIs(t, err, config.ErrInvalidPreDeployHooks)
		assert.Empty(t, runs)
	})

	t.Run("an unsafe Git credential setup runs nothing and names no token", func(t *testing.T) {
		setValidateEnv(t)
		setValidatePaths(t)
		t.Setenv("BOSUN_GIT_USERNAME", "configured-user")
		t.Setenv("BOSUN_GIT_TOKEN", "configured-token")
		t.Setenv("BOSUN_REPO_URL", "http://example.com/repo.git")
		chdirValidateProject(t, "")

		var runs []recordedValidateRun
		err := runFullDryRunWith(recordingValidateRunner(&runs, ""))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "absolute https://")
		assert.NotContains(t, err.Error(), "configured-token")
		assert.Empty(t, runs)
	})
}

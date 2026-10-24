package reconcile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// preDeployAlerter records failure alerts for the pre-deploy gate tests.
type preDeployAlerter struct {
	failures []string
}

func (a *preDeployAlerter) SendDeploySuccess(context.Context, string, string, []string, time.Duration) error {
	return nil
}

func (a *preDeployAlerter) SendDeployFailure(_ context.Context, _, _, reason string, _ []string, _ time.Duration) error {
	a.failures = append(a.failures, reason)
	return nil
}

func (a *preDeployAlerter) SendDeployRecovery(context.Context, string, string, int) error {
	return nil
}

func (a *preDeployAlerter) SendUnhealthyContainers(context.Context, string, []string) error {
	return nil
}
func (a *preDeployAlerter) SendRollbackSuccess(context.Context, string, string) error { return nil }
func (a *preDeployAlerter) SendRollbackFailure(context.Context, string, string) error { return nil }

func TestValidatePreDeployHooks(t *testing.T) {
	tests := []struct {
		name    string
		hooks   []PreDeployHook
		wantErr string
	}{
		{name: "none", hooks: nil},
		{name: "valid", hooks: []PreDeployHook{{Name: "no-value", Command: []string{"sh", "-c", "true"}, Timeout: Duration{Duration: time.Second}}}},
		{name: "empty command", hooks: []PreDeployHook{{Name: "x"}}, wantErr: "non-empty list"},
		{name: "blank program", hooks: []PreDeployHook{{Command: []string{"  ", "arg"}}}, wantErr: "must name a program"},
		{name: "negative timeout", hooks: []PreDeployHook{{Command: []string{"true"}, Timeout: Duration{Duration: -time.Second}}}, wantErr: "must not be negative"},
		{name: "NUL in argument", hooks: []PreDeployHook{{Command: []string{"grep", "a\x00b"}}}, wantErr: "command[1] contains a NUL byte"},
		{name: "NUL in name", hooks: []PreDeployHook{{Name: "a\x00", Command: []string{"true"}}}, wantErr: "name contains a NUL byte"},
		{name: "second hook invalid", hooks: []PreDeployHook{{Command: []string{"true"}}, {}}, wantErr: "pre_deploy_hooks[1]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePreDeployHooks(tt.hooks)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrInvalidPreDeployHooks)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestPreDeployHookDefaults(t *testing.T) {
	assert.Equal(t, DefaultPreDeployHookTimeout, PreDeployHook{Command: []string{"x"}}.timeout())
	assert.Equal(t, 3*time.Second, PreDeployHook{Timeout: Duration{Duration: 3 * time.Second}}.timeout())
	assert.Equal(t, "check", PreDeployHook{Name: "check", Command: []string{"x"}}.label())
	assert.Equal(t, "x", PreDeployHook{Command: []string{"x"}}.label())
	assert.Equal(t, "unnamed", PreDeployHook{}.label())
}

func TestCappedBufferKeepsTail(t *testing.T) {
	b := &cappedBuffer{limit: 4}
	_, _ = b.Write([]byte("ab"))
	assert.Equal(t, "ab", b.String())
	_, _ = b.Write([]byte("cdef"))
	assert.Equal(t, "[truncated] cdef", b.String())
}

func TestClonePreDeployHooksDoesNotAlias(t *testing.T) {
	assert.Nil(t, ClonePreDeployHooks(nil))
	assert.NotNil(t, ClonePreDeployHooks([]PreDeployHook{}), "explicit [] must stay distinct from unset")
	src := []PreDeployHook{{Name: "a", Command: []string{"x", "y"}}}
	cp := ClonePreDeployHooks(src)
	cp[0].Command[0] = "changed"
	assert.Equal(t, "x", src[0].Command[0])
}

func skipWithoutPOSIXShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("pre-deploy hook pipeline tests use a POSIX shell")
	}
}

// preDeployHarness reuses the changed-only Run() harness: live b.yml differs
// from the repo, so a deploy that gets past the gate syncs b.yml and runs
// compose up on it.
func preDeployHarness(t *testing.T, hooks []PreDeployHook) (r *Reconciler, upCalls *[][]string, dockerLog, appdata string, alerter *preDeployAlerter) {
	t.Helper()
	r, upCalls, dockerLog, appdata = changedOnlyHarness(t, "services:\n  svc-b:\n    image: alpine:OLD\n", steadyPrior(), []string{"svc-a", "svc-b"})
	r.config.PreDeployHooks.Value = hooks
	r.config.OnFailure = true
	alerter = &preDeployAlerter{}
	r.alerter = alerter
	return r, upCalls, dockerLog, appdata, alerter
}

// assertDeployBlocked checks everything a blocked deploy must leave alone.
func assertDeployBlocked(t *testing.T, r *Reconciler, upCalls *[][]string, dockerLog, appdata string, alerter *preDeployAlerter, hookName string) {
	t.Helper()
	assert.Empty(t, *upCalls, "a blocked deploy must not run compose up")
	assert.Empty(t, readDockerLog(t, dockerLog), "a blocked deploy must not shell out to docker (no orphan pass, no rollback)")

	live, err := os.ReadFile(filepath.Join(appdata, "compose", "b.yml"))
	require.NoError(t, err)
	assert.Contains(t, string(live), "alpine:OLD", "a blocked deploy must not sync files to the target")

	state := LoadState(r.config.StateFile)
	assert.Equal(t, "prevcommit", state.LastDeployedCommit, "state must not advance past a blocked deploy")
	assert.False(t, state.NeedsRedeploy, "the gate runs before the pre-deploy marker is written")
	assert.Equal(t, []string{"compose/a.yml", "compose/b.yml"}, state.DeployedFiles)
	assert.Equal(t, 1, state.AttemptCount, "a blocked deploy counts toward the circuit breaker")

	require.Len(t, alerter.failures, 1, "a blocked deploy must send one failure alert")
	assert.Contains(t, alerter.failures[0], hookName)
}

func TestRun_PreDeployHook_PassingHookProceeds(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Setenv("BOSUN_GIT_TOKEN", "must-not-leak")
	out := filepath.Join(t.TempDir(), "hook.out")
	script := "pwd -P > '" + out + "' && env >> '" + out + "' && test -f compose/b.yml"
	r, upCalls, _, appdata, alerter := preDeployHarness(t, []PreDeployHook{{Name: "check", Command: []string{"sh", "-c", script}}})

	require.NoError(t, r.Run(context.Background()))

	assert.Equal(t, []string{"b.yml"}, upFileBases(*upCalls), "a passing gate lets the deploy run")
	assert.Empty(t, alerter.failures)
	assert.Equal(t, "newcommit", LoadState(r.config.StateFile).LastDeployedCommit)
	live, err := os.ReadFile(filepath.Join(appdata, "compose", "b.yml"))
	require.NoError(t, err)
	assert.Contains(t, string(live), "alpine:2")

	data, err := os.ReadFile(out)
	require.NoError(t, err, "the hook must have run")
	lines := strings.Split(string(data), "\n")
	// Staging is removed after a successful run, so resolve its parent.
	assert.Equal(t, filepath.Join(evalSymlinks(t, filepath.Dir(r.config.StagingDir)), "staging"), lines[0], "cwd must be the staging tree")
	env := string(data)
	assert.Contains(t, env, "BOSUN_COMMIT=newcommit")
	assert.Contains(t, env, "BOSUN_STAGING_DIR=")
	assert.Contains(t, env, "BOSUN_TARGET=")
	assert.NotContains(t, env, "must-not-leak", "bosun's environment must not reach the hook")
}

func TestRun_PreDeployHook_FailingHookBlocksDeploy(t *testing.T) {
	skipWithoutPOSIXShell(t)
	second := filepath.Join(t.TempDir(), "second.ran")
	r, upCalls, dockerLog, appdata, alerter := preDeployHarness(t, []PreDeployHook{
		{Name: "no-value-gate", Command: []string{"sh", "-c", "echo found sentinel >&2; exit 3"}},
		{Name: "later", Command: []string{"sh", "-c", "touch '" + second + "'"}},
	})

	err := r.Run(context.Background())

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPreDeployHookFailed)
	assert.Contains(t, err.Error(), `pre-deploy hook "no-value-gate" exited with status 3`)
	assertDeployBlocked(t, r, upCalls, dockerLog, appdata, alerter, "no-value-gate")
	_, statErr := os.Stat(second)
	assert.True(t, os.IsNotExist(statErr), "hooks stop at the first failure")
}

func TestRun_PreDeployHook_MissingProgramBlocksDeploy(t *testing.T) {
	skipWithoutPOSIXShell(t)
	r, upCalls, dockerLog, appdata, alerter := preDeployHarness(t, []PreDeployHook{
		{Name: "missing", Command: []string{"bosun-no-such-validator-477"}},
	})

	err := r.Run(context.Background())

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPreDeployHookFailed)
	assert.Contains(t, err.Error(), "could not run")
	assertDeployBlocked(t, r, upCalls, dockerLog, appdata, alerter, "missing")
}

func TestRun_PreDeployHook_TimeoutBlocksDeploy(t *testing.T) {
	skipWithoutPOSIXShell(t)
	r, upCalls, dockerLog, appdata, alerter := preDeployHarness(t, []PreDeployHook{
		{Name: "slow", Command: []string{"sh", "-c", "sleep 30"}, Timeout: Duration{Duration: 200 * time.Millisecond}},
	})

	start := time.Now()
	err := r.Run(context.Background())

	require.Error(t, err)
	assert.Less(t, time.Since(start), 15*time.Second, "the hook timeout must end the run, not the sleep")
	assert.ErrorIs(t, err, ErrPreDeployHookFailed)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Contains(t, err.Error(), `pre-deploy hook "slow" timed out after 200ms`)
	assertDeployBlocked(t, r, upCalls, dockerLog, appdata, alerter, "slow")
}

func TestRun_PreDeployHook_CycleDeadlineBlocksDeploy(t *testing.T) {
	skipWithoutPOSIXShell(t)
	r, upCalls, dockerLog, appdata, _ := preDeployHarness(t, []PreDeployHook{
		{Name: "slow", Command: []string{"sh", "-c", "sleep 30"}, Timeout: Duration{Duration: time.Minute}},
	})
	// The hook's own timeout is a minute; the cycle context must still stop it.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	start := time.Now()
	err := r.Run(ctx)

	require.Error(t, err)
	assert.Less(t, time.Since(start), 15*time.Second)
	assert.ErrorIs(t, err, ErrPreDeployHookFailed)
	assert.True(t, errors.Is(err, context.DeadlineExceeded), "the cycle deadline must be the reported cause: %v", err)
	assert.Contains(t, err.Error(), "reconcile cycle ended")
	assert.Empty(t, *upCalls)
	assert.Empty(t, readDockerLog(t, dockerLog))
	live, readErr := os.ReadFile(filepath.Join(appdata, "compose", "b.yml"))
	require.NoError(t, readErr)
	assert.Contains(t, string(live), "alpine:OLD")
	assert.Equal(t, "prevcommit", LoadState(r.config.StateFile).LastDeployedCommit)
}

func TestRun_PreDeployHook_InvalidConfigFailsBeforeAnything(t *testing.T) {
	r, upCalls, dockerLog, _, _ := preDeployHarness(t, []PreDeployHook{{Name: "empty"}})

	err := r.Run(context.Background())

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidPreDeployHooks)
	assert.Empty(t, *upCalls)
	assert.Empty(t, readDockerLog(t, dockerLog))
}

func TestConfigForTarget_PreDeployHooks(t *testing.T) {
	root := []PreDeployHook{{Name: "root", Command: []string{"true"}}}
	base := &Config{}
	base.PreDeployHooks.SetFromFile(root)

	t.Run("target inherits root", func(t *testing.T) {
		cp := base.ConfigForTarget(Target{Name: "nas"})
		require.Len(t, cp.PreDeployHooks.Value, 1)
		assert.Equal(t, "root", cp.PreDeployHooks.Value[0].Name)
		cp.PreDeployHooks.Value[0].Command[0] = "changed"
		assert.Equal(t, "true", base.PreDeployHooks.Value[0].Command[0], "the copy must not alias the base")
	})
	t.Run("target overrides root", func(t *testing.T) {
		cp := base.ConfigForTarget(Target{Name: "nas", PreDeployHooks: []PreDeployHook{{Name: "own", Command: []string{"x"}}}})
		require.Len(t, cp.PreDeployHooks.Value, 1)
		assert.Equal(t, "own", cp.PreDeployHooks.Value[0].Name)
	})
	t.Run("explicit empty list disables for the target", func(t *testing.T) {
		cp := base.ConfigForTarget(Target{Name: "nas", PreDeployHooks: []PreDeployHook{}})
		assert.Empty(t, cp.PreDeployHooks.Value)
	})
	t.Run("BOSUN_TARGETS list is env-owned", func(t *testing.T) {
		envBase := &Config{TargetsFromEnv: true}
		envBase.PreDeployHooks.SetFromFile(root)
		cp := envBase.ConfigForTarget(Target{Name: "nas", PreDeployHooks: []PreDeployHook{{Name: "env", Command: []string{"x"}}}})
		assert.True(t, cp.PreDeployHooks.FromEnv())
	})
}

func TestReloadProjectConfig_PreDeployHooks(t *testing.T) {
	newReloader := func(rc *ReloadedConfig, err error) *Reconciler {
		cfg := &Config{RepoDir: t.TempDir(), ConfigReloader: func(string) (*ReloadedConfig, error) { return rc, err }}
		cfg.PreDeployHooks.SetFromFile([]PreDeployHook{{Name: "old", Command: []string{"true"}}})
		return &Reconciler{config: cfg}
	}

	t.Run("reload applies the new list", func(t *testing.T) {
		r := newReloader(&ReloadedConfig{PreDeployHooks: []PreDeployHook{{Name: "new", Command: []string{"x"}}}}, nil)
		require.NoError(t, r.reloadProjectConfig())
		require.Len(t, r.config.PreDeployHooks.Value, 1)
		assert.Equal(t, "new", r.config.PreDeployHooks.Value[0].Name)
	})
	t.Run("absent key clears the gate", func(t *testing.T) {
		r := newReloader(&ReloadedConfig{PreDeployHooks: []PreDeployHook{}}, nil)
		require.NoError(t, r.reloadProjectConfig())
		assert.Empty(t, r.config.PreDeployHooks.Value)
	})
	t.Run("invalid root list aborts", func(t *testing.T) {
		r := newReloader(&ReloadedConfig{PreDeployHooks: []PreDeployHook{{Name: "bad"}}}, nil)
		err := r.reloadProjectConfig()
		assert.ErrorIs(t, err, ErrInvalidPreDeployHooks)
		assert.Equal(t, "old", r.config.PreDeployHooks.Value[0].Name, "an invalid reload must not replace the gate")
	})
	t.Run("invalid target list aborts", func(t *testing.T) {
		r := newReloader(&ReloadedConfig{Targets: []Target{{Name: "nas", PreDeployHooks: []PreDeployHook{{Name: "bad"}}}}}, nil)
		assert.ErrorIs(t, r.reloadProjectConfig(), ErrInvalidPreDeployHooks)
	})
	t.Run("loader rejection aborts", func(t *testing.T) {
		r := newReloader(nil, ErrInvalidPreDeployHooks)
		assert.ErrorIs(t, r.reloadProjectConfig(), ErrInvalidPreDeployHooks)
	})
	t.Run("target override replaces root", func(t *testing.T) {
		r := newReloader(&ReloadedConfig{
			PreDeployHooks: []PreDeployHook{{Name: "root", Command: []string{"x"}}},
			Targets:        []Target{{Name: "nas", PreDeployHooks: []PreDeployHook{{Name: "nas-only", Command: []string{"y"}}}}},
		}, nil)
		r.config.TargetName = "nas"
		require.NoError(t, r.reloadProjectConfig())
		require.Len(t, r.config.PreDeployHooks.Value, 1)
		assert.Equal(t, "nas-only", r.config.PreDeployHooks.Value[0].Name)
	})
	t.Run("env-owned list survives reload", func(t *testing.T) {
		r := newReloader(&ReloadedConfig{PreDeployHooks: []PreDeployHook{}}, nil)
		r.config.PreDeployHooks = EnvConfigField([]PreDeployHook{{Name: "env", Command: []string{"x"}}})
		require.NoError(t, r.reloadProjectConfig())
		require.Len(t, r.config.PreDeployHooks.Value, 1)
		assert.Equal(t, "env", r.config.PreDeployHooks.Value[0].Name)
	})
}

func TestRun_PreDeployHook_DryRunBlockSendsNoAlertAndIsNotCounted(t *testing.T) {
	skipWithoutPOSIXShell(t)
	r, upCalls, dockerLog, appdata, alerter := preDeployHarness(t, []PreDeployHook{
		{Name: "gate", Command: []string{"sh", "-c", "exit 1"}},
	})
	r.config.DryRun = true

	err := r.Run(context.Background())

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPreDeployHookFailed)
	assert.Contains(t, err.Error(), "dry run")
	assert.Empty(t, alerter.failures, "a dry-run block is a preview result, not a failed deploy")
	state := LoadState(r.config.StateFile)
	assert.Equal(t, 0, state.AttemptCount, "a dry-run block must not count toward the circuit breaker")
	assert.Empty(t, state.LastAttemptedCommit)
	assert.Equal(t, "prevcommit", state.LastDeployedCommit)
	assert.Empty(t, *upCalls)
	assert.Empty(t, readDockerLog(t, dockerLog))
	live, readErr := os.ReadFile(filepath.Join(appdata, "compose", "b.yml"))
	require.NoError(t, readErr)
	assert.Contains(t, string(live), "alpine:OLD")
}

// TestRunPreDeployHooks_TimeoutKillsProcessGroup pins that a timeout reaches
// the hook's children, not only the direct process: the background subshell
// would write its marker after 2s if it survived.
func TestRunPreDeployHooks_TimeoutKillsProcessGroup(t *testing.T) {
	skipWithoutPOSIXShell(t)
	dir := t.TempDir()
	marker := filepath.Join(dir, "survived")
	hooks := []PreDeployHook{{
		Name:    "spawner",
		Command: []string{"sh", "-c", "(sleep 2; touch '" + marker + "') & sleep 30"},
		Timeout: Duration{Duration: 200 * time.Millisecond},
	}}

	start := time.Now()
	err := runPreDeployHooks(context.Background(), hooks, dir, "t", "c")

	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	time.Sleep(time.Until(start.Add(3 * time.Second)))
	_, statErr := os.Stat(marker)
	assert.True(t, os.IsNotExist(statErr), "a child of a timed-out hook must not survive")
}

// TestRunPreDeployHooks_PassingHookLeavesNoBackgroundChild pins that the
// group is killed after a hook exits 0, so a background child cannot keep
// running while the deploy proceeds.
func TestRunPreDeployHooks_PassingHookLeavesNoBackgroundChild(t *testing.T) {
	skipWithoutPOSIXShell(t)
	dir := t.TempDir()
	marker := filepath.Join(dir, "survived")
	hooks := []PreDeployHook{{
		Name:    "detacher",
		Command: []string{"sh", "-c", "(sleep 2; touch '" + marker + "') >/dev/null 2>&1 & exit 0"},
	}}

	start := time.Now()
	require.NoError(t, runPreDeployHooks(context.Background(), hooks, dir, "t", "c"))

	time.Sleep(time.Until(start.Add(3 * time.Second)))
	_, statErr := os.Stat(marker)
	assert.True(t, os.IsNotExist(statErr), "a background child of a passing hook must be killed")
}

func TestPreDeployHookLabelAndOutputAreSanitized(t *testing.T) {
	label := PreDeployHook{Name: "gate\x1b[31m\nfake line\u202e", Command: []string{"x"}}.label()
	assert.NotContains(t, label, "\x1b")
	assert.NotContains(t, label, "\n")
	assert.NotContains(t, label, "\u202e")
	assert.Equal(t, "unnamed", PreDeployHook{Name: "\x1b\n"}.label(), "a name of only control characters falls back")
	assert.LessOrEqual(t, len([]rune(PreDeployHook{Name: strings.Repeat("a", 500)}.label())), preDeployHookLabelLimit)

	b := &cappedBuffer{limit: 1024}
	_, _ = b.Write([]byte("first\x1b[2J\r\nsecond\n"))
	assert.Equal(t, "first[2J | second", b.String())
}

func TestPreDeployHooksFromBOSUNTargetsSurviveReload(t *testing.T) {
	targets, override, err := ParseTargetsOverride(`[{"name":"nas","pre_deploy_hooks":[{"name":"env-gate","command":["sh","-c","true"],"timeout":"30s"}]}]`)
	require.NoError(t, err)
	require.True(t, override)
	require.Len(t, targets, 1)
	require.Len(t, targets[0].PreDeployHooks, 1)
	assert.Equal(t, 30*time.Second, targets[0].PreDeployHooks[0].Timeout.Duration)

	base := &Config{Targets: targets, TargetsFromEnv: true}
	base.PreDeployHooks.SetFromFile([]PreDeployHook{{Name: "file-root", Command: []string{"true"}}})
	cfg := base.ConfigForTarget(targets[0])
	require.True(t, cfg.PreDeployHooks.FromEnv())

	cfg.RepoDir = t.TempDir()
	cfg.ConfigReloader = func(string) (*ReloadedConfig, error) {
		return &ReloadedConfig{
			PreDeployHooks: []PreDeployHook{{Name: "repo-root", Command: []string{"x"}}},
			Targets:        []Target{{Name: "nas", PreDeployHooks: []PreDeployHook{{Name: "repo-nas", Command: []string{"y"}}}}},
		}, nil
	}
	r := &Reconciler{config: cfg}
	require.NoError(t, r.reloadProjectConfig())
	require.Len(t, r.config.PreDeployHooks.Value, 1)
	assert.Equal(t, "env-gate", r.config.PreDeployHooks.Value[0].Name, "a BOSUN_TARGETS list must win over the repo file on reload")
}

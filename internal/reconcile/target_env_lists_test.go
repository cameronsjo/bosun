package reconcile

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/docker"
)

// envListField describes one of the three per-target lists whose environment
// ownership these tests pin: critical_containers, deploy_sync_paths and
// deploy_sync_exclude. Each has a global environment variable and a per-target
// override, and the repo's bosun.yaml must never replace an environment value.
type envListField struct {
	name        string
	setTarget   func(*Target, []string)
	field       func(*Config) *ConfigField[[]string]
	setReloaded func(*ReloadedConfig, []string)
}

var envListFields = []envListField{
	{
		name:        "critical_containers",
		setTarget:   func(t *Target, v []string) { t.CriticalContainers = v },
		field:       func(c *Config) *ConfigField[[]string] { return &c.CriticalContainers },
		setReloaded: func(r *ReloadedConfig, v []string) { r.CriticalContainers = v },
	},
	{
		name:        "deploy_sync_paths",
		setTarget:   func(t *Target, v []string) { t.DeploySyncPaths = v },
		field:       func(c *Config) *ConfigField[[]string] { return &c.DeploySyncPaths },
		setReloaded: func(r *ReloadedConfig, v []string) { r.DeploySyncPaths = v },
	},
	{
		name:        "deploy_sync_exclude",
		setTarget:   func(t *Target, v []string) { t.DeploySyncExclude = v },
		field:       func(c *Config) *ConfigField[[]string] { return &c.DeploySyncExclude },
		setReloaded: func(r *ReloadedConfig, v []string) { r.DeploySyncExclude = v },
	},
}

// reloadWith builds a reconciler from cfg, reloads the given repo snapshot into
// it, and returns it so the caller can read the effective field.
func reloadWith(t *testing.T, cfg *Config, reloaded *ReloadedConfig, opts ...ReconcilerOption) *Reconciler {
	t.Helper()
	cfg.ConfigReloader = func(string) (*ReloadedConfig, error) { return reloaded, nil }
	r := NewReconciler(cfg, opts...)
	require.NoError(t, r.reloadProjectConfig())
	return r
}

func TestConfigForTarget_EnvTargetListsAreEnvOwned(t *testing.T) {
	for _, f := range envListFields {
		t.Run(f.name, func(t *testing.T) {
			for _, tt := range []struct {
				name     string
				list     []string
				wantEnv  bool
				wantList []string
			}{
				{name: "a BOSUN_TARGETS list is env-owned", list: []string{"a"}, wantEnv: true, wantList: []string{"a"}},
				{name: "an explicit empty BOSUN_TARGETS list is env-owned and stays empty", list: []string{}, wantEnv: true, wantList: []string{}},
				{name: "an omitted key claims nothing", list: nil, wantEnv: false, wantList: nil},
			} {
				t.Run(tt.name, func(t *testing.T) {
					target := Target{Name: "nas"}
					f.setTarget(&target, tt.list)
					base := &Config{TargetsFromEnv: true, Targets: []Target{target}}

					got := f.field(base.ConfigForTarget(target))

					assert.Equal(t, tt.wantEnv, got.FromEnv())
					assert.Equal(t, tt.wantList, got.Value)
					if tt.wantList != nil {
						assert.NotNil(t, got.Value)
					}
				})
			}
		})
	}
}

func TestReloadProjectConfig_EnvTargetListsSurviveReload(t *testing.T) {
	for _, f := range envListFields {
		t.Run(f.name, func(t *testing.T) {
			run := func(t *testing.T, envTarget Target, reloaded *ReloadedConfig) []string {
				t.Helper()
				base := &Config{TargetsFromEnv: true, Targets: []Target{envTarget}}
				return f.field(reloadWith(t, base.ConfigForTarget(envTarget), reloaded).config).Value
			}
			withList := func(name string, list []string) Target {
				target := Target{Name: name}
				f.setTarget(&target, list)
				return target
			}
			reloadedRoot := func(list []string) *ReloadedConfig {
				r := &ReloadedConfig{}
				f.setReloaded(r, list)
				return r
			}

			t.Run("repo root does not replace the env list", func(t *testing.T) {
				got := run(t, withList("nas", []string{"env"}), reloadedRoot([]string{"repo"}))
				assert.Equal(t, []string{"env"}, got)
			})

			t.Run("an env empty list is not refilled by the repo root", func(t *testing.T) {
				got := run(t, withList("nas", []string{}), reloadedRoot([]string{"repo"}))
				assert.NotNil(t, got)
				assert.Empty(t, got)
			})

			t.Run("a repo target override does not replace the env list", func(t *testing.T) {
				reloaded := reloadedRoot([]string{"repo-root"})
				reloaded.Targets = []Target{withList("nas", []string{"repo-target"})}
				got := run(t, withList("nas", []string{"env"}), reloaded)
				assert.Equal(t, []string{"env"}, got)
			})

			t.Run("a lone env default target keeps its list", func(t *testing.T) {
				reloaded := reloadedRoot([]string{"repo-root"})
				reloaded.Targets = []Target{withList("default", []string{"repo-target"})}
				got := run(t, withList("default", []string{"env"}), reloaded)
				assert.Equal(t, []string{"env"}, got)
			})

			t.Run("an env target without the key inherits the repo root (control)", func(t *testing.T) {
				got := run(t, Target{Name: "nas"}, reloadedRoot([]string{"repo"}))
				assert.Equal(t, []string{"repo"}, got)
			})
		})
	}
}

// A target declared in bosun.yaml is repo input. It must not replace a value the
// operator set through the global environment variable, in ConfigForTarget or in
// any later reload.
func TestConfigForTarget_FileTargetNeverOverridesGlobalEnvList(t *testing.T) {
	for _, f := range envListFields {
		t.Run(f.name, func(t *testing.T) {
			for _, fileList := range [][]string{{}, {"evil/**"}} {
				fileTarget := Target{Name: "nas"}
				f.setTarget(&fileTarget, fileList)
				base := &Config{Targets: []Target{fileTarget}}
				*f.field(base) = EnvConfigField([]string{"authelia"})

				effective := base.ConfigForTarget(fileTarget)
				got := f.field(effective)
				assert.Equal(t, []string{"authelia"}, got.Value, "file target %v", fileList)
				assert.True(t, got.FromEnv(), "the env label must match the env value")

				reloaded := &ReloadedConfig{}
				f.setReloaded(reloaded, []string{"repo-root"})
				reloaded.Targets = []Target{fileTarget}
				f.setTarget(&reloaded.Targets[0], []string{"repo-target"})
				r := reloadWith(t, effective, reloaded)
				assert.Equal(t, []string{"authelia"}, f.field(r.config).Value, "reload with file target %v", fileList)
			}
		})
	}
}

// Both sources are environment, so the more specific one wins: a BOSUN_TARGETS
// list replaces the global variable for that target. This is the precedence
// ConfigForTarget already had at startup, now pinned (it passes on the original).
func TestConfigForTarget_EnvTargetListBeatsGlobalEnvList(t *testing.T) {
	for _, f := range envListFields {
		t.Run(f.name, func(t *testing.T) {
			envTarget := Target{Name: "nas"}
			f.setTarget(&envTarget, []string{"specific"})
			base := &Config{TargetsFromEnv: true, Targets: []Target{envTarget}}
			*f.field(base) = EnvConfigField([]string{"global"})

			effective := base.ConfigForTarget(envTarget)
			assert.Equal(t, []string{"specific"}, f.field(effective).Value)
			assert.True(t, f.field(effective).FromEnv())

			reloaded := &ReloadedConfig{}
			f.setReloaded(reloaded, []string{"repo"})
			r := reloadWith(t, effective, reloaded)
			assert.Equal(t, []string{"specific"}, f.field(r.config).Value)
		})
	}
}

// Consumer rows: the deploy path reads these exact fields, so assert what the
// consumer receives, not only the ownership flag.

func TestEnvDeploySyncLists_ReachDiscovery(t *testing.T) {
	staging := t.TempDir()
	for _, name := range []string{"keep", "private"} {
		require.NoError(t, os.MkdirAll(filepath.Join(staging, name), 0o755))
	}
	relPaths := func(targets []DeployTarget) []string {
		var out []string
		for _, target := range targets {
			out = append(out, target.RelPath)
		}
		return out
	}

	t.Run("a file target with an empty list cannot clear the env blocklist", func(t *testing.T) {
		fileTarget := Target{Name: "nas", DeploySyncExclude: []string{}}
		base := &Config{Targets: []Target{fileTarget}}
		base.DeploySyncExclude = EnvConfigField([]string{"private"})

		r := reloadWith(t, base.ConfigForTarget(fileTarget), &ReloadedConfig{DeploySyncExclude: []string{}})
		targets, err := discoverDeployTargets(staging, r.config.DeploySyncPaths.Value, r.config.DeploySyncExclude.Value)

		require.NoError(t, err)
		assert.Equal(t, []string{"keep"}, relPaths(targets))
	})

	t.Run("a file target cannot widen the env allowlist", func(t *testing.T) {
		fileTarget := Target{Name: "nas", DeploySyncPaths: []string{"keep", "private"}}
		base := &Config{Targets: []Target{fileTarget}}
		base.DeploySyncPaths = EnvConfigField([]string{"keep"})

		r := reloadWith(t, base.ConfigForTarget(fileTarget), &ReloadedConfig{DeploySyncPaths: []string{"keep", "private"}})
		targets, err := discoverDeployTargets(staging, r.config.DeploySyncPaths.Value, r.config.DeploySyncExclude.Value)

		require.NoError(t, err)
		assert.Equal(t, []string{"keep"}, relPaths(targets))
	})

	t.Run("a reload cannot replace a BOSUN_TARGETS blocklist", func(t *testing.T) {
		envTarget := Target{Name: "nas", DeploySyncExclude: []string{"private"}}
		base := &Config{TargetsFromEnv: true, Targets: []Target{envTarget}}

		r := reloadWith(t, base.ConfigForTarget(envTarget), &ReloadedConfig{DeploySyncExclude: []string{}})
		targets, err := discoverDeployTargets(staging, r.config.DeploySyncPaths.Value, r.config.DeploySyncExclude.Value)

		require.NoError(t, err)
		assert.Equal(t, []string{"keep"}, relPaths(targets))
	})
}

func TestEnvCriticalContainers_ReachHealthGate(t *testing.T) {
	mockAPI := newReconcileMockDockerAPI()
	mockAPI.containerInspectFunc = func(_ context.Context, name string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
		return makeInspectResponse(name, "running", &container.Health{Status: "unhealthy"}), nil
	}
	dockerOpt := WithDockerClient(docker.NewClientWithAPI(mockAPI))

	newBase := func(t *testing.T, fileTarget Target) *Config {
		t.Helper()
		tmp := t.TempDir()
		return &Config{
			Targets:             []Target{fileTarget},
			StateFile:           filepath.Join(tmp, "state.json"),
			LocalAppdataPath:    tmp, // Ensures isLocalMode() returns true.
			HealthGateTimeout:   100 * time.Millisecond,
			HealthCheckInterval: 10 * time.Millisecond,
		}
	}

	t.Run("a file target with an empty list cannot switch off the env gate", func(t *testing.T) {
		fileTarget := Target{Name: "nas", CriticalContainers: []string{}}
		base := newBase(t, fileTarget)
		base.CriticalContainers = EnvConfigField([]string{"authelia"})

		r := reloadWith(t, base.ConfigForTarget(fileTarget), &ReloadedConfig{CriticalContainers: []string{}}, dockerOpt)
		_, err := r.runHealthGate(context.Background(), &DeployState{}, true, nil)

		require.Error(t, err, "the gate must still watch authelia")
		assert.Contains(t, err.Error(), "authelia")
	})

	t.Run("a reload cannot switch off a BOSUN_TARGETS gate", func(t *testing.T) {
		envTarget := Target{Name: "nas", CriticalContainers: []string{"authelia"}}
		base := newBase(t, envTarget)
		base.TargetsFromEnv = true

		r := reloadWith(t, base.ConfigForTarget(envTarget), &ReloadedConfig{CriticalContainers: []string{}}, dockerOpt)
		_, err := r.runHealthGate(context.Background(), &DeployState{}, true, nil)

		require.Error(t, err, "the gate must still watch authelia")
		assert.Contains(t, err.Error(), "authelia")
	})
}

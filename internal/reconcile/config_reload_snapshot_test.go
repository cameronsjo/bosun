package reconcile

import (
	"bytes"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	logpkg "github.com/cameronsjo/bosun/internal/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func restartHook(container string) PostSyncHook {
	return PostSyncHook{
		Paths:     []string{"appdata/" + container + "/**"},
		Action:    "restart",
		Container: container,
	}
}

func TestReloadProjectConfigHookSnapshotPresence(t *testing.T) {
	tests := []struct {
		name            string
		initialHooks    ConfigField[[]PostSyncHook]
		initialDelay    ConfigField[time.Duration]
		reloaded        *ReloadedConfig
		reloadErr       error
		wantContainer   string
		wantHooksNil    bool
		wantDelay       time.Duration
		wantHookSource  ConfigSource
		wantDelaySource ConfigSource
	}{
		{
			name:            "successful omission clears file hooks and retains delay",
			initialHooks:    FileConfigField([]PostSyncHook{restartHook("old")}),
			initialDelay:    FileConfigField(5 * time.Second),
			reloaded:        &ReloadedConfig{},
			wantHooksNil:    false,
			wantDelay:       5 * time.Second,
			wantHookSource:  SourceFile,
			wantDelaySource: SourceFile,
		},
		{
			name:            "explicit zero clears settle delay",
			initialHooks:    FileConfigField([]PostSyncHook{restartHook("old")}),
			initialDelay:    FileConfigField(5 * time.Second),
			reloaded:        &ReloadedConfig{HookSettleDelay: durationPointer(0)},
			wantHooksNil:    false,
			wantDelay:       0,
			wantHookSource:  SourceFile,
			wantDelaySource: SourceFile,
		},
		{
			name:            "environment replacements remain authoritative",
			initialHooks:    EnvConfigField([]PostSyncHook{restartHook("env")}),
			initialDelay:    EnvConfigField(7 * time.Second),
			reloaded:        &ReloadedConfig{PostSyncHooks: []PostSyncHook{restartHook("repo")}, HookSettleDelay: durationPointer(0)},
			wantContainer:   "env",
			wantDelay:       7 * time.Second,
			wantHookSource:  SourceEnv,
			wantDelaySource: SourceEnv,
		},
		{
			name:            "missing file retains current state",
			initialHooks:    FileConfigField([]PostSyncHook{restartHook("old")}),
			initialDelay:    FileConfigField(5 * time.Second),
			reloaded:        nil,
			wantContainer:   "old",
			wantDelay:       5 * time.Second,
			wantHookSource:  SourceFile,
			wantDelaySource: SourceFile,
		},
		{
			name:            "malformed file retains current state",
			initialHooks:    FileConfigField([]PostSyncHook{restartHook("old")}),
			initialDelay:    FileConfigField(5 * time.Second),
			reloadErr:       fmt.Errorf("malformed YAML"),
			wantContainer:   "old",
			wantDelay:       5 * time.Second,
			wantHookSource:  SourceFile,
			wantDelaySource: SourceFile,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				PostSyncHooks:   tt.initialHooks,
				HookSettleDelay: tt.initialDelay,
				ConfigReloader: func(string) (*ReloadedConfig, error) {
					return tt.reloaded, tt.reloadErr
				},
			}
			r := NewReconciler(cfg)
			require.NoError(t, r.reloadProjectConfig())

			if tt.wantContainer == "" {
				assert.Empty(t, r.config.PostSyncHooks.Value)
				assert.Equal(t, tt.wantHooksNil, r.config.PostSyncHooks.Value == nil)
			} else {
				require.Len(t, r.config.PostSyncHooks.Value, 1)
				assert.Equal(t, tt.wantContainer, r.config.PostSyncHooks.Value[0].Container)
			}
			assert.Equal(t, tt.wantDelay, r.config.HookSettleDelay.Value)
			assert.Equal(t, tt.wantHookSource, r.config.PostSyncHooks.Source)
			assert.Equal(t, tt.wantDelaySource, r.config.HookSettleDelay.Source)
		})
	}
}

func TestReloadProjectConfigRejectsInvalidHookSnapshotAtomically(t *testing.T) {
	oldHook := restartHook("old")
	oldHook.Command = []string{"do-not-mutate"}
	cfg := &Config{
		PostSyncHooks:   FileConfigField([]PostSyncHook{oldHook}),
		HookSettleDelay: FileConfigField(5 * time.Second),
		ConfigReloader: func(string) (*ReloadedConfig, error) {
			return &ReloadedConfig{
				PostSyncHooks:   []PostSyncHook{restartHook("new")},
				HookSettleDelay: durationPointer(0),
				Targets: []Target{{
					Name:          "nas",
					PostSyncHooks: []PostSyncHook{{Action: "exec", Container: "nas"}},
				}},
			}, nil
		},
	}
	r := NewReconciler(cfg)

	err := r.reloadProjectConfig()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidPostSyncHooks)
	require.Len(t, r.config.PostSyncHooks.Value, 1)
	assert.Equal(t, "old", r.config.PostSyncHooks.Value[0].Container)
	assert.Equal(t, []string{"do-not-mutate"}, r.config.PostSyncHooks.Value[0].Command)
	assert.Equal(t, 5*time.Second, r.config.HookSettleDelay.Value)
}

func TestReloadProjectConfigTargetHookSnapshot(t *testing.T) {
	tests := []struct {
		name          string
		targets       []Target
		wantHooks     int
		wantContainer string
	}{
		{
			name:          "omitted target hook key inherits root",
			targets:       []Target{{Name: "nas"}},
			wantHooks:     1,
			wantContainer: "root-new",
		},
		{
			name:      "explicit empty target hooks clear inheritance",
			targets:   []Target{{Name: "nas", PostSyncHooks: []PostSyncHook{}}},
			wantHooks: 0,
		},
		{
			name:          "removed target descriptor discards stale override",
			targets:       []Target{{Name: "pi", PostSyncHooks: []PostSyncHook{restartHook("pi")}}},
			wantHooks:     1,
			wantContainer: "root-new",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				TargetName:    "nas",
				PostSyncHooks: FileConfigField([]PostSyncHook{restartHook("nas-old")}),
				ConfigReloader: func(string) (*ReloadedConfig, error) {
					return &ReloadedConfig{
						PostSyncHooks: []PostSyncHook{restartHook("root-new")},
						Targets:       tt.targets,
					}, nil
				},
			}
			r := NewReconciler(cfg)
			require.NoError(t, r.reloadProjectConfig())
			require.Len(t, r.config.PostSyncHooks.Value, tt.wantHooks)
			if tt.wantHooks > 0 {
				assert.Equal(t, tt.wantContainer, r.config.PostSyncHooks.Value[0].Container)
			}
			assert.NotNil(t, r.config.PostSyncHooks.Value)
		})
	}
}

func TestConfigForTargetEnvironmentHookOwnership(t *testing.T) {
	base := DefaultConfig()
	base.TargetsFromEnv = true
	target := Target{Name: "nas", PostSyncHooks: []PostSyncHook{restartHook("env-target")}}
	targetConfig := base.ConfigForTarget(target)
	require.True(t, targetConfig.PostSyncHooks.FromEnv())
	targetConfig.ConfigReloader = func(string) (*ReloadedConfig, error) {
		return &ReloadedConfig{
			PostSyncHooks: []PostSyncHook{restartHook("repo-root")},
			Targets:       []Target{{Name: "nas", PostSyncHooks: []PostSyncHook{restartHook("repo-target")}}},
		}, nil
	}

	r := NewReconciler(targetConfig)
	require.NoError(t, r.reloadProjectConfig())
	require.Len(t, r.config.PostSyncHooks.Value, 1)
	assert.Equal(t, "env-target", r.config.PostSyncHooks.Value[0].Container)
	assert.True(t, r.config.PostSyncHooks.FromEnv())
}

func TestReloadProjectConfigOwnsNestedHookSlices(t *testing.T) {
	reloaded := &ReloadedConfig{
		PostSyncHooks: []PostSyncHook{{
			Paths:     []string{"appdata/root/**"},
			Action:    "exec",
			Container: "root",
			Command:   []string{"reload", "root"},
		}},
		Targets: []Target{{
			Name: "nas",
			PostSyncHooks: []PostSyncHook{{
				Paths:     []string{"appdata/nas/**"},
				Action:    "exec",
				Container: "nas",
				Command:   []string{"reload", "nas"},
			}},
		}},
	}
	cfg := &Config{TargetName: "nas", ConfigReloader: func(string) (*ReloadedConfig, error) { return reloaded, nil }}
	r := NewReconciler(cfg)
	require.NoError(t, r.reloadProjectConfig())

	reloaded.PostSyncHooks[0].Paths[0] = "mutated-root/**"
	reloaded.PostSyncHooks[0].Command[0] = "mutated-root"
	reloaded.Targets[0].PostSyncHooks[0].Paths[0] = "mutated-target/**"
	reloaded.Targets[0].PostSyncHooks[0].Command[0] = "mutated-target"

	require.Len(t, r.config.PostSyncHooks.Value, 1)
	assert.Equal(t, "appdata/nas/**", r.config.PostSyncHooks.Value[0].Paths[0])
	assert.Equal(t, "reload", r.config.PostSyncHooks.Value[0].Command[0])
}

func TestReloadProjectConfigLoggingIsSourceAwareAndRedacted(t *testing.T) {
	var logs bytes.Buffer
	logpkg.Init(&logpkg.Options{
		Format:            logpkg.FormatJSON,
		Level:             logpkg.DebugLevel,
		LevelSet:          true,
		AdditionalWriters: []io.Writer{&logs},
	})
	t.Cleanup(func() { logpkg.Init(nil) })

	const secretArgument = "super-secret-reload-token"
	cfg := &Config{
		TargetName: "nas",
		ConfigReloader: func(string) (*ReloadedConfig, error) {
			return &ReloadedConfig{PostSyncHooks: []PostSyncHook{{
				Paths:     []string{"appdata/nas/**"},
				Action:    "exec",
				Container: "nas",
				Command:   []string{"reload", secretArgument},
			}}}, nil
		},
	}
	r := NewReconciler(cfg)
	require.NoError(t, r.reloadProjectConfig())

	output := logs.String()
	assert.Contains(t, output, `"hooks_outcome":"applied"`)
	assert.Contains(t, output, `"hooks_source":"file"`)
	assert.Contains(t, output, `"hooks":1`)
	assert.Contains(t, output, `"target":"nas"`)
	assert.NotContains(t, output, secretArgument)
}

func TestReloadProjectConfigConcurrentTargetsDoNotAlias(t *testing.T) {
	shared := &ReloadedConfig{
		PostSyncHooks: []PostSyncHook{restartHook("root")},
		Targets: []Target{
			{Name: "nas", PostSyncHooks: []PostSyncHook{restartHook("nas")}},
			{Name: "pi", PostSyncHooks: []PostSyncHook{restartHook("pi")}},
		},
	}
	newTargetReconciler := func(name string) *Reconciler {
		cfg := &Config{
			TargetName: name,
			ConfigReloader: func(string) (*ReloadedConfig, error) {
				return shared, nil
			},
		}
		return NewReconciler(cfg)
	}
	nas := newTargetReconciler("nas")
	pi := newTargetReconciler("pi")

	var wg sync.WaitGroup
	for _, reconciler := range []*Reconciler{nas, pi} {
		wg.Add(1)
		go func(r *Reconciler) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				if err := r.reloadProjectConfig(); err != nil {
					t.Errorf("reload project config: %v", err)
					return
				}
			}
		}(reconciler)
	}
	wg.Wait()

	nas.config.PostSyncHooks.Value[0].Paths[0] = "mutated/**"
	assert.Equal(t, "appdata/pi/**", pi.config.PostSyncHooks.Value[0].Paths[0])
	assert.Equal(t, "appdata/nas/**", shared.Targets[0].PostSyncHooks[0].Paths[0])
}

func durationPointer(value time.Duration) *time.Duration { return &value }

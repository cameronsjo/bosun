package reconcile

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReloadProjectConfig_RootSlicesDoNotAliasReloadedConfig(t *testing.T) {
	reloaded := &ReloadedConfig{
		PostSyncHooks: []PostSyncHook{{
			Container: "traefik",
			Paths:     []string{"config/**"},
			Command:   []string{"reload", "traefik"},
		}},
		DeployPaths:        []string{"infra/**"},
		DeploySyncPaths:    []string{"appdata/**"},
		DeploySyncExclude:  []string{"logs/**"},
		CriticalContainers: []string{"traefik"},
		DriftIgnore:        []DriftIgnoreRule{{Service: "traefik", Type: "unhealthy"}},
	}
	cfg := &Config{ConfigReloader: func(string) (*ReloadedConfig, error) {
		return reloaded, nil
	}}
	r := NewReconciler(cfg)
	r.reloadProjectConfig()

	r.config.PostSyncHooks.Value[0].Container = "mutated-container"
	r.config.PostSyncHooks.Value[0].Paths[0] = "mutated-path/**"
	r.config.PostSyncHooks.Value[0].Command[0] = "mutated-command"
	r.config.DeployPaths.Value[0] = "mutated-deploy/**"
	r.config.DeploySyncPaths.Value[0] = "mutated-sync/**"
	r.config.DeploySyncExclude.Value[0] = "mutated-exclude/**"
	r.config.CriticalContainers.Value[0] = "mutated-critical"
	r.config.DriftIgnore.Value[0].Service = "mutated-service"

	assert.Equal(t, "traefik", reloaded.PostSyncHooks[0].Container)
	assert.Equal(t, []string{"config/**"}, reloaded.PostSyncHooks[0].Paths)
	assert.Equal(t, []string{"reload", "traefik"}, reloaded.PostSyncHooks[0].Command)
	assert.Equal(t, []string{"infra/**"}, reloaded.DeployPaths)
	assert.Equal(t, []string{"appdata/**"}, reloaded.DeploySyncPaths)
	assert.Equal(t, []string{"logs/**"}, reloaded.DeploySyncExclude)
	assert.Equal(t, []string{"traefik"}, reloaded.CriticalContainers)
	assert.Equal(t, []DriftIgnoreRule{{Service: "traefik", Type: "unhealthy"}}, reloaded.DriftIgnore)
}

func TestReloadProjectConfig_ClonedEmptySlicesStillClear(t *testing.T) {
	reloaded := &ReloadedConfig{
		PostSyncHooks:      []PostSyncHook{},
		DeployPaths:        []string{},
		DeploySyncPaths:    []string{},
		DeploySyncExclude:  []string{},
		CriticalContainers: []string{},
		DriftIgnore:        []DriftIgnoreRule{},
		Targets: []Target{{
			Name:               "nas",
			PostSyncHooks:      []PostSyncHook{},
			CriticalContainers: []string{},
			DeploySyncPaths:    []string{},
			DeploySyncExclude:  []string{},
		}},
	}
	cfg := &Config{
		TargetName:         "nas",
		PostSyncHooks:      NewConfigField([]PostSyncHook{{Container: "old"}}),
		DeployPaths:        NewConfigField([]string{"old/**"}),
		DeploySyncPaths:    NewConfigField([]string{"old/**"}),
		DeploySyncExclude:  NewConfigField([]string{"old/**"}),
		CriticalContainers: NewConfigField([]string{"old"}),
		DriftIgnore:        NewConfigField([]DriftIgnoreRule{{Service: "old", Type: "missing"}}),
		ConfigReloader: func(string) (*ReloadedConfig, error) {
			return reloaded, nil
		},
	}
	r := NewReconciler(cfg)
	r.reloadProjectConfig()

	assert.NotNil(t, r.config.PostSyncHooks.Value)
	assert.NotNil(t, r.config.DeployPaths.Value)
	assert.NotNil(t, r.config.DeploySyncPaths.Value)
	assert.NotNil(t, r.config.DeploySyncExclude.Value)
	assert.NotNil(t, r.config.CriticalContainers.Value)
	assert.NotNil(t, r.config.DriftIgnore.Value)
	assert.Empty(t, r.config.PostSyncHooks.Value)
	assert.Empty(t, r.config.DeployPaths.Value)
	assert.Empty(t, r.config.DeploySyncPaths.Value)
	assert.Empty(t, r.config.DeploySyncExclude.Value)
	assert.Empty(t, r.config.CriticalContainers.Value)
	assert.Empty(t, r.config.DriftIgnore.Value)
}

func TestReloadProjectConfig_TargetOverrideSlicesAreIndependent(t *testing.T) {
	sharedHooks := []PostSyncHook{{
		Container: "traefik",
		Paths:     []string{"config/**"},
		Command:   []string{"reload", "traefik"},
	}}
	sharedContainers := []string{"traefik"}
	sharedSyncPaths := []string{"appdata/**"}
	sharedSyncExclude := []string{"logs/**"}
	reloaded := &ReloadedConfig{Targets: []Target{
		{
			Name:               "nas",
			PostSyncHooks:      sharedHooks,
			CriticalContainers: sharedContainers,
			DeploySyncPaths:    sharedSyncPaths,
			DeploySyncExclude:  sharedSyncExclude,
		},
		{
			Name:               "pi",
			PostSyncHooks:      sharedHooks,
			CriticalContainers: sharedContainers,
			DeploySyncPaths:    sharedSyncPaths,
			DeploySyncExclude:  sharedSyncExclude,
		},
	}}

	newTargetReconciler := func(name string) *Reconciler {
		cfg := &Config{
			TargetName: name,
			ConfigReloader: func(string) (*ReloadedConfig, error) {
				return reloaded, nil
			},
		}
		r := NewReconciler(cfg)
		r.reloadProjectConfig()
		return r
	}

	nas := newTargetReconciler("nas")
	pi := newTargetReconciler("pi")

	nas.config.PostSyncHooks.Value[0].Container = "mutated-container"
	nas.config.PostSyncHooks.Value[0].Paths[0] = "mutated-path/**"
	nas.config.PostSyncHooks.Value[0].Command[0] = "mutated-command"
	nas.config.CriticalContainers.Value[0] = "mutated-critical"
	nas.config.DeploySyncPaths.Value[0] = "mutated-sync/**"
	nas.config.DeploySyncExclude.Value[0] = "mutated-exclude/**"

	assert.Equal(t, "traefik", pi.config.PostSyncHooks.Value[0].Container)
	assert.Equal(t, []string{"config/**"}, pi.config.PostSyncHooks.Value[0].Paths)
	assert.Equal(t, []string{"reload", "traefik"}, pi.config.PostSyncHooks.Value[0].Command)
	assert.Equal(t, []string{"traefik"}, pi.config.CriticalContainers.Value)
	assert.Equal(t, []string{"appdata/**"}, pi.config.DeploySyncPaths.Value)
	assert.Equal(t, []string{"logs/**"}, pi.config.DeploySyncExclude.Value)

	assert.Equal(t, "traefik", sharedHooks[0].Container)
	assert.Equal(t, []string{"config/**"}, sharedHooks[0].Paths)
	assert.Equal(t, []string{"reload", "traefik"}, sharedHooks[0].Command)
	assert.Equal(t, []string{"traefik"}, sharedContainers)
	assert.Equal(t, []string{"appdata/**"}, sharedSyncPaths)
	assert.Equal(t, []string{"logs/**"}, sharedSyncExclude)
}

func TestReloadProjectConfig_TargetOverrideSlicesDoNotRace(t *testing.T) {
	shared := []string{"initial"}
	reloaded := &ReloadedConfig{Targets: []Target{
		{Name: "nas", CriticalContainers: shared},
		{Name: "pi", CriticalContainers: shared},
	}}

	newTargetReconciler := func(name string) *Reconciler {
		cfg := &Config{
			TargetName: name,
			ConfigReloader: func(string) (*ReloadedConfig, error) {
				return reloaded, nil
			},
		}
		r := NewReconciler(cfg)
		r.reloadProjectConfig()
		return r
	}

	nas := newTargetReconciler("nas")
	pi := newTargetReconciler("pi")

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 1_000 {
			nas.config.CriticalContainers.Value[0] = "nas"
		}
	}()
	go func() {
		defer wg.Done()
		for range 1_000 {
			pi.config.CriticalContainers.Value[0] = "pi"
		}
	}()
	wg.Wait()

	assert.Equal(t, []string{"initial"}, shared)
}

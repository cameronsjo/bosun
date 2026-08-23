package reconcile

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfigForTarget_TargetSlicesPreserveNilAndEmpty(t *testing.T) {
	base := DefaultConfig()
	base.Targets = []Target{
		{
			Name:               "empty",
			CriticalContainers: []string{},
			PostSyncHooks:      []PostSyncHook{},
			DeploySyncPaths:    []string{},
			DeploySyncExclude:  []string{},
		},
		{Name: "nil"},
		{Name: "nested-empty", PostSyncHooks: []PostSyncHook{{Paths: []string{}, Command: []string{}}}},
		{Name: "nested-nil", PostSyncHooks: []PostSyncHook{{}}},
	}

	cloned := base.ConfigForTarget(Target{Name: "destination"})

	assert.NotNil(t, cloned.Targets)
	assert.NotNil(t, cloned.Targets[0].CriticalContainers)
	assert.NotNil(t, cloned.Targets[0].PostSyncHooks)
	assert.NotNil(t, cloned.Targets[0].DeploySyncPaths)
	assert.NotNil(t, cloned.Targets[0].DeploySyncExclude)
	assert.Nil(t, cloned.Targets[1].CriticalContainers)
	assert.Nil(t, cloned.Targets[1].PostSyncHooks)
	assert.Nil(t, cloned.Targets[1].DeploySyncPaths)
	assert.Nil(t, cloned.Targets[1].DeploySyncExclude)
	assert.NotNil(t, cloned.Targets[2].PostSyncHooks[0].Paths)
	assert.NotNil(t, cloned.Targets[2].PostSyncHooks[0].Command)
	assert.Nil(t, cloned.Targets[3].PostSyncHooks[0].Paths)
	assert.Nil(t, cloned.Targets[3].PostSyncHooks[0].Command)

	emptyBase := DefaultConfig()
	emptyBase.Targets = []Target{}
	assert.NotNil(t, emptyBase.ConfigForTarget(Target{Name: "destination"}).Targets)

	nilBase := DefaultConfig()
	nilBase.Targets = nil
	assert.Nil(t, nilBase.ConfigForTarget(Target{Name: "destination"}).Targets)
}

func TestConfigForTarget_TargetSlicesDoNotRace(t *testing.T) {
	base := DefaultConfig()
	base.Targets = []Target{{
		Name:               "source",
		CriticalContainers: []string{"critical"},
		PostSyncHooks: []PostSyncHook{{
			Container: "hook",
			Paths:     []string{"config/**"},
			Command:   []string{"reload", "service"},
		}},
		DeploySyncPaths:   []string{"appdata/**"},
		DeploySyncExclude: []string{"logs/**"},
	}}

	nas := base.ConfigForTarget(Target{Name: "nas"})
	pi := base.ConfigForTarget(Target{Name: "pi"})

	mutate := func(cfg *Config, value string) {
		for range 1_000 {
			cfg.Targets[0].CriticalContainers[0] = value
			cfg.Targets[0].PostSyncHooks[0].Paths[0] = value
			cfg.Targets[0].PostSyncHooks[0].Command[0] = value
			cfg.Targets[0].DeploySyncPaths[0] = value
			cfg.Targets[0].DeploySyncExclude[0] = value
		}
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		mutate(nas, "nas")
	}()
	go func() {
		defer wg.Done()
		mutate(pi, "pi")
	}()
	wg.Wait()

	assert.Equal(t, []string{"critical"}, base.Targets[0].CriticalContainers)
	assert.Equal(t, []string{"config/**"}, base.Targets[0].PostSyncHooks[0].Paths)
	assert.Equal(t, []string{"reload", "service"}, base.Targets[0].PostSyncHooks[0].Command)
	assert.Equal(t, []string{"appdata/**"}, base.Targets[0].DeploySyncPaths)
	assert.Equal(t, []string{"logs/**"}, base.Targets[0].DeploySyncExclude)
}

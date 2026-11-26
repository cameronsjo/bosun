package reconcile

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// changedFixture is a repo snapshot that sets every field a reload can touch.
func changedFixture() *ReloadedConfig {
	delay := 5 * time.Second
	onFailure, onSuccess, onRecovery, removeOrphans := false, true, false, false
	projectName := "homelab"
	return &ReloadedConfig{
		PostSyncHooks:      []PostSyncHook{restartHook("traefik")},
		HookSettleDelay:    &delay,
		DeployPaths:        []string{"unraid/**"},
		DeploySyncPaths:    []string{"appdata/**"},
		DeploySyncExclude:  []string{"logs/**"},
		CriticalContainers: []string{"traefik"},
		DriftIgnore:        []DriftIgnoreRule{{Service: "traefik", Type: "unhealthy"}},
		ComposeIgnore:      []string{"handmade"},
		PreDeployHooks:     []PreDeployHook{{Name: "lint", Command: []string{"true"}}},
		OnFailure:          &onFailure,
		OnSuccess:          &onSuccess,
		OnRecovery:         &onRecovery,
		RemoveOrphans:      &removeOrphans,
		ProjectName:        &projectName,
	}
}

// configMatching returns a config that already holds exactly what rc says, as a
// previous reload from the same repo would have left it.
func configMatching(rc *ReloadedConfig) *Config {
	cfg := &Config{
		PostSyncHooks:      FileConfigField(clonePostSyncHooks(rc.PostSyncHooks)),
		HookSettleDelay:    FileConfigField(*rc.HookSettleDelay),
		DeployPaths:        FileConfigField(cloneSlice(rc.DeployPaths)),
		DeploySyncPaths:    FileConfigField(cloneSlice(rc.DeploySyncPaths)),
		DeploySyncExclude:  FileConfigField(cloneSlice(rc.DeploySyncExclude)),
		CriticalContainers: FileConfigField(cloneSlice(rc.CriticalContainers)),
		DriftIgnore:        FileConfigField(cloneSlice(rc.DriftIgnore)),
		ComposeIgnore:      FileConfigField(cloneSlice(rc.ComposeIgnore)),
		PreDeployHooks:     FileConfigField(ClonePreDeployHooks(rc.PreDeployHooks)),
		OnFailure:          *rc.OnFailure,
		OnSuccess:          *rc.OnSuccess,
		OnRecovery:         *rc.OnRecovery,
		RemoveOrphans:      FileConfigField(*rc.RemoveOrphans),
		ProjectName:        *rc.ProjectName,
	}
	return cfg
}

func reloadChanged(t *testing.T, cfg *Config, reloaded *ReloadedConfig) bool {
	t.Helper()
	cfg.ConfigReloader = func(string) (*ReloadedConfig, error) { return reloaded, nil }
	changed, err := NewReconciler(cfg).reloadProjectConfigChanged()
	require.NoError(t, err)
	return changed
}

// A reload that finds the repo unchanged must say so. Reporting a change on every
// run makes the "Reloaded project config from repo" line fire every reconcile.
func TestReloadProjectConfig_IdenticalReloadReportsNoChange(t *testing.T) {
	rc := changedFixture()

	assert.False(t, reloadChanged(t, configMatching(rc), rc))
}

// Negative control for the test above: the same fixture, with one field really
// changed in the repo, must report a change, so "false" cannot be the answer to
// every input.
func TestReloadProjectConfig_ChangedFieldReportsChange(t *testing.T) {
	flip := func(v *bool) *bool { n := !*v; return &n }
	tests := []struct {
		name   string
		mutate func(*ReloadedConfig)
	}{
		{"post_sync_hooks", func(rc *ReloadedConfig) { rc.PostSyncHooks = []PostSyncHook{restartHook("gatus")} }},
		{"hook_settle_delay", func(rc *ReloadedConfig) { d := 6 * time.Second; rc.HookSettleDelay = &d }},
		{"deploy_paths", func(rc *ReloadedConfig) { rc.DeployPaths = []string{"infra/**"} }},
		{"deploy_sync_paths", func(rc *ReloadedConfig) { rc.DeploySyncPaths = []string{"other/**"} }},
		{"deploy_sync_exclude", func(rc *ReloadedConfig) { rc.DeploySyncExclude = []string{"tmp/**"} }},
		{"critical_containers", func(rc *ReloadedConfig) { rc.CriticalContainers = []string{"authelia"} }},
		{"drift_ignore", func(rc *ReloadedConfig) { rc.DriftIgnore = []DriftIgnoreRule{{Service: "gatus", Type: "missing"}} }},
		{"compose_ignore", func(rc *ReloadedConfig) { rc.ComposeIgnore = []string{"other"} }},
		{"pre_deploy_hooks", func(rc *ReloadedConfig) {
			rc.PreDeployHooks = []PreDeployHook{{Name: "lint", Command: []string{"false"}}}
		}},
		{"on_failure", func(rc *ReloadedConfig) { rc.OnFailure = flip(rc.OnFailure) }},
		{"on_success", func(rc *ReloadedConfig) { rc.OnSuccess = flip(rc.OnSuccess) }},
		{"on_recovery", func(rc *ReloadedConfig) { rc.OnRecovery = flip(rc.OnRecovery) }},
		{"remove_orphans", func(rc *ReloadedConfig) { rc.RemoveOrphans = flip(rc.RemoveOrphans) }},
		{"project_name", func(rc *ReloadedConfig) { n := "other"; rc.ProjectName = &n }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := configMatching(changedFixture())
			reloaded := changedFixture()
			tt.mutate(reloaded)

			assert.True(t, reloadChanged(t, cfg, reloaded))
		})
	}
}

// Environment-owned fields are never applied, so a reload that differs from them
// only there changes nothing.
func TestReloadProjectConfig_EnvOwnedFieldsReportNoChange(t *testing.T) {
	rc := changedFixture()
	cfg := configMatching(rc)
	cfg.PostSyncHooks = EnvConfigField([]PostSyncHook{restartHook("env-hook")})
	cfg.HookSettleDelay = EnvConfigField(9 * time.Second)

	assert.False(t, reloadChanged(t, cfg, rc))
	assert.Equal(t, "env-hook", cfg.PostSyncHooks.Value[0].Container)
}

// The per-target override path must also stay quiet when the repo repeats what
// the reconciler already holds.
func TestReloadProjectConfig_TargetOverridesReportChangeOnlyWhenChanged(t *testing.T) {
	newTarget := func() Target {
		return Target{
			Name:               "nas",
			ProjectName:        "nas-project",
			CriticalContainers: []string{"traefik"},
			DeploySyncPaths:    []string{"appdata/**"},
			DeploySyncExclude:  []string{"logs/**"},
			ComposeIgnore:      []string{"handmade"},
			PreDeployHooks:     []PreDeployHook{{Name: "lint", Command: []string{"true"}}},
		}
	}
	build := func(mutate func(*Target)) (*Config, *ReloadedConfig) {
		rc := changedFixture()
		rc.ProjectName = nil // the root name only reaches the default target
		target := newTarget()
		if mutate != nil {
			mutate(&target)
		}
		rc.Targets = []Target{target}

		cfg := configMatching(changedFixture())
		cfg.TargetName = "nas"
		cfg.ProjectName = "nas-project"
		return cfg, rc
	}

	t.Run("an identical target override reports no change", func(t *testing.T) {
		cfg, rc := build(nil)
		assert.False(t, reloadChanged(t, cfg, rc))
	})

	for name, mutate := range map[string]func(*Target){
		"project_name":        func(tg *Target) { tg.ProjectName = "renamed" },
		"critical_containers": func(tg *Target) { tg.CriticalContainers = []string{"authelia"} },
		"deploy_sync_paths":   func(tg *Target) { tg.DeploySyncPaths = []string{"other/**"} },
		"deploy_sync_exclude": func(tg *Target) { tg.DeploySyncExclude = []string{"tmp/**"} },
		"compose_ignore":      func(tg *Target) { tg.ComposeIgnore = []string{"other"} },
		"pre_deploy_hooks":    func(tg *Target) { tg.PreDeployHooks = []PreDeployHook{{Name: "lint", Command: []string{"false"}}} },
	} {
		t.Run("a changed "+name+" reports a change", func(t *testing.T) {
			cfg, rc := build(mutate)
			assert.True(t, reloadChanged(t, cfg, rc))
		})
	}
}

package reconcile

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #391 regression: a lone `name: default` target is the implicit default's
// configuration — its project_name (and paths) must reach the deploy instead
// of being discarded with a warning, which deployed project-less and collided
// containers. Each case declares the flat config, the explicit target, and a
// verify closure over the resolved default.
func TestResolveTargets_HonorsLoneDefaultTarget(t *testing.T) {
	tests := []struct {
		name    string
		flat    func(*Config)
		targets []Target
		verify  func(*testing.T, Target)
	}{
		{
			name:    "project_name reaches the resolved default target",
			flat:    func(*Config) {},
			targets: []Target{{Name: "default", ProjectName: "homelab"}},
			verify: func(t *testing.T, def Target) {
				assert.True(t, def.IsDefault())
				assert.Equal(t, "homelab", def.ProjectName, "lone default target's project_name must be honored (#391)")
			},
		},
		{
			name: "explicit fields win, empty fields fall back to flat config",
			flat: func(c *Config) {
				c.TargetHost = "user@flat-host"
				c.ProjectName = "flat-project"
				c.StateFile = "/var/lib/bosun/deploy-state.json"
				c.StagingDir = "/app/staging"
			},
			targets: []Target{{
				Name:        "default",
				ProjectName: "homelab",
				StateFile:   "/custom/state.json",
				StagingDir:  "/custom/staging",
			}},
			verify: func(t *testing.T, def Target) {
				assert.Equal(t, "homelab", def.ProjectName, "explicit project_name wins")
				assert.Equal(t, "/custom/state.json", def.StateFile, "explicit state_file wins")
				assert.Equal(t, "/custom/staging", def.StagingDir, "explicit staging_dir wins")
				assert.Equal(t, "user@flat-host", def.TargetHost, "unset target fields fall back to flat config")
			},
		},
		{
			name:    "case-variant name normalizes to the canonical default",
			flat:    func(*Config) {},
			targets: []Target{{Name: "Default", ProjectName: "homelab"}},
			verify: func(t *testing.T, def Target) {
				// The canonical name keeps ConfigForTarget on the legacy
				// state/lock/staging paths shared with pre-multi-target daemons (#228).
				assert.Equal(t, DefaultTargetName, def.Name)
				assert.Equal(t, "homelab", def.ProjectName)
			},
		},
		{
			name: "explicit empty slice clears the inherited flat value",
			flat: func(c *Config) {
				c.CriticalContainers = NewConfigField([]string{"traefik"})
			},
			targets: []Target{{Name: "default", CriticalContainers: []string{}}},
			verify: func(t *testing.T, def Target) {
				assert.NotNil(t, def.CriticalContainers)
				assert.Empty(t, def.CriticalContainers, "explicit empty list must clear the flat value, matching ConfigForTarget semantics")
			},
		},
		{
			// Every explicit-field branch of the merge in one case: each
			// non-zero field on the explicit target wins over its flat counterpart.
			name: "full merge — every explicit field wins",
			flat: func(c *Config) {
				c.TargetHost = "user@flat"
				c.LocalAppdataPath = "/flat/local"
				c.RemoteAppdataPath = "/flat/remote"
				c.ProjectName = "flat-project"
				c.CriticalContainers = NewConfigField([]string{"flat-cc"})
				c.PostSyncHooks = NewConfigField([]PostSyncHook{{Container: "flat-hook"}})
				c.DeploySyncPaths = NewConfigField([]string{"flat/**"})
				c.DeploySyncExclude = NewConfigField([]string{"flat.bak"})
			},
			targets: []Target{{
				Name:               "default",
				TargetHost:         "user@explicit",
				LocalAppdataPath:   "/explicit/local",
				RemoteAppdataPath:  "/explicit/remote",
				ProjectName:        "explicit-project",
				StateFile:          "/explicit/state.json",
				StagingDir:         "/explicit/staging",
				SecretsScope:       "explicit-scope",
				CriticalContainers: []string{"explicit-cc"},
				PostSyncHooks:      []PostSyncHook{{Container: "explicit-hook"}},
				DeploySyncPaths:    []string{"explicit/**"},
				DeploySyncExclude:  []string{"explicit.bak"},
			}},
			verify: func(t *testing.T, def Target) {
				assert.Equal(t, "user@explicit", def.TargetHost)
				assert.Equal(t, "/explicit/local", def.LocalAppdataPath)
				assert.Equal(t, "/explicit/remote", def.RemoteAppdataPath)
				assert.Equal(t, "explicit-project", def.ProjectName)
				assert.Equal(t, "/explicit/state.json", def.StateFile)
				assert.Equal(t, "/explicit/staging", def.StagingDir)
				assert.Equal(t, "explicit-scope", def.SecretsScope)
				assert.Equal(t, []string{"explicit-cc"}, def.CriticalContainers)
				require.Len(t, def.PostSyncHooks, 1)
				assert.Equal(t, "explicit-hook", def.PostSyncHooks[0].Container)
				assert.Equal(t, []string{"explicit/**"}, def.DeploySyncPaths)
				assert.Equal(t, []string{"explicit.bak"}, def.DeploySyncExclude)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.flat(cfg)
			cfg.Targets = tt.targets

			targets, err := cfg.ResolveTargets()
			require.NoError(t, err)
			require.Len(t, targets, 1)
			tt.verify(t, targets[0])
		})
	}
}

// The resolved lone-default target must flow through ConfigForTarget onto the
// per-target config the deploy actually uses.
func TestResolveTargets_LoneDefaultFlowsThroughConfigForTarget(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Targets = []Target{{Name: "default", ProjectName: "homelab"}}

	targets, err := cfg.ResolveTargets()
	require.NoError(t, err)
	require.Len(t, targets, 1)

	targetCfg := cfg.ConfigForTarget(targets[0])
	assert.Equal(t, "homelab", targetCfg.ProjectName, "project_name must reach the per-target config the deploy uses")
	assert.Equal(t, cfg.StagingDir, targetCfg.StagingDir, "default target keeps the base staging dir")
}

func TestResolveTargets_ExplicitEmptyMatchesAbsent(t *testing.T) {
	absent := DefaultConfig()
	absent.ProjectName = "homelab"
	explicitEmpty := DefaultConfig()
	explicitEmpty.ProjectName = "homelab"
	explicitEmpty.Targets = []Target{}

	want, err := absent.ResolveTargets()
	require.NoError(t, err)
	got, err := explicitEmpty.ResolveTargets()
	require.NoError(t, err)

	assert.Equal(t, want, got)
	require.Len(t, got, 1)
	assert.Equal(t, DefaultTargetName, got[0].Name)
}

// #391: a multi-target config carrying a reserved default-named target (any
// case variant) is a hard error naming the offender and the remedy — silently
// dropping the target was the container-collision vector.
func TestResolveTargets_MultiTargetDefaultFailsLoud(t *testing.T) {
	tests := []struct {
		name            string
		targets         []Target
		wantErrContains []string
	}{
		{
			name: "default listed after a named target",
			targets: []Target{
				{Name: "unraid", TargetHost: "user@unraid"},
				{Name: "default", ProjectName: "homelab"},
			},
			wantErrContains: []string{"default", "rename"},
		},
		{
			name: "case-variant default listed first",
			targets: []Target{
				{Name: "Default", TargetHost: "user@a"},
				{Name: "unraid", TargetHost: "user@unraid"},
			},
			wantErrContains: []string{"Default"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Targets = tt.targets

			targets, err := cfg.ResolveTargets()

			require.Error(t, err, "multi-target config with a reserved default name must fail loud")
			assert.Nil(t, targets)
			for _, want := range tt.wantErrContains {
				assert.Contains(t, err.Error(), want, "error must name the offender and the remedy")
			}
		})
	}
}

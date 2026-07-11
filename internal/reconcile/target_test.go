package reconcile

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #391 regression: a lone `name: default` target is the implicit default's
// configuration — its project_name (and paths) must reach the deploy instead
// of being discarded with a warning, which deployed project-less and collided
// containers. Multi-target configs carrying a `default` fail loud instead
// (see TestResolveTargets_FailsLoudOnMultiTargetDefault).
func TestResolveTargets_HonorsLoneDefaultTarget(t *testing.T) {
	t.Run("project_name reaches the resolved default target", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Targets = []Target{
			{Name: "default", ProjectName: "homelab"},
		}

		targets, err := cfg.ResolveTargets()
		require.NoError(t, err)
		require.Len(t, targets, 1)

		def := targets[0]
		assert.True(t, def.IsDefault())
		assert.Equal(t, "homelab", def.ProjectName, "lone default target's project_name must be honored (#391)")
	})

	t.Run("explicit fields win, empty fields fall back to flat config", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.TargetHost = "user@flat-host"
		cfg.ProjectName = "flat-project"
		cfg.StateFile = "/var/lib/bosun/deploy-state.json"
		cfg.StagingDir = "/app/staging"
		cfg.Targets = []Target{
			{
				Name:        "default",
				ProjectName: "homelab",
				StateFile:   "/custom/state.json",
				StagingDir:  "/custom/staging",
			},
		}

		targets, err := cfg.ResolveTargets()
		require.NoError(t, err)
		require.Len(t, targets, 1)

		def := targets[0]
		assert.Equal(t, "homelab", def.ProjectName, "explicit project_name wins")
		assert.Equal(t, "/custom/state.json", def.StateFile, "explicit state_file wins")
		assert.Equal(t, "/custom/staging", def.StagingDir, "explicit staging_dir wins")
		assert.Equal(t, "user@flat-host", def.TargetHost, "unset target fields fall back to flat config")
	})

	t.Run("case-variant name normalizes to the canonical default", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Targets = []Target{
			{Name: "Default", ProjectName: "homelab"},
		}

		targets, err := cfg.ResolveTargets()
		require.NoError(t, err)
		require.Len(t, targets, 1)
		// The canonical name keeps ConfigForTarget on the legacy state/lock/
		// staging paths shared with pre-multi-target daemons (#228).
		assert.Equal(t, DefaultTargetName, targets[0].Name)
		assert.Equal(t, "homelab", targets[0].ProjectName)
	})

	t.Run("explicit empty slice clears the inherited flat value", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.CriticalContainers = NewConfigField([]string{"traefik"})
		cfg.Targets = []Target{
			{Name: "default", CriticalContainers: []string{}},
		}

		targets, err := cfg.ResolveTargets()
		require.NoError(t, err)
		require.Len(t, targets, 1)
		assert.NotNil(t, targets[0].CriticalContainers)
		assert.Empty(t, targets[0].CriticalContainers, "explicit empty list must clear the flat value, matching ConfigForTarget semantics")
	})

	t.Run("resolved default flows through ConfigForTarget", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Targets = []Target{
			{Name: "default", ProjectName: "homelab"},
		}

		targets, err := cfg.ResolveTargets()
		require.NoError(t, err)
		require.Len(t, targets, 1)

		targetCfg := cfg.ConfigForTarget(targets[0])
		assert.Equal(t, "homelab", targetCfg.ProjectName, "project_name must reach the per-target config the deploy uses")
		assert.Equal(t, cfg.StagingDir, targetCfg.StagingDir, "default target keeps the base staging dir")
	})
}

func TestResolveTargets_MultiTargetDefaultErrorMentionsRemedy(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Targets = []Target{
		{Name: "unraid", TargetHost: "user@unraid"},
		{Name: "default", ProjectName: "homelab"},
	}

	targets, err := cfg.ResolveTargets()
	require.Error(t, err)
	assert.Nil(t, targets)
	assert.Contains(t, err.Error(), "rename", "error must state the remedy, not just the failure")
}

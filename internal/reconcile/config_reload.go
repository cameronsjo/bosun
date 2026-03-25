package reconcile

import (
	"github.com/cameronsjo/bosun/internal/log"
)

// reloadProjectConfig re-reads bosun.yaml from the repo working directory
// and updates operational config fields (hooks, deploy paths, alert gates,
// critical containers, drift ignore rules, remove-orphans) if the file changed.
// Fields overridden by environment variables are not updated.
func (r *Reconciler) reloadProjectConfig() {
	if r.config.ConfigReloader == nil {
		return
	}

	logger := log.Component(log.ComponentReconcile) // No ctx available in this method.

	reloaded, err := r.config.ConfigReloader(r.config.RepoDir)
	if err != nil {
		logger.Warn().Err(err).Msg("Failed to reload project config from repo, keeping existing config")
		return
	}
	if reloaded == nil {
		return
	}

	// If no field has any value from the repo, there's nothing to reload.
	// Use nil checks (not len==0) for slices so explicitly empty lists (e.g. `deploy_sync_paths: []`)
	// can clear in-memory filters during hot-reload.
	if reloaded.PostSyncHooks == nil && reloaded.HookSettleDelay == nil && reloaded.DeployPaths == nil && reloaded.DeploySyncPaths == nil && reloaded.DeploySyncExclude == nil && reloaded.CriticalContainers == nil && reloaded.DriftIgnore == nil && reloaded.OnFailure == nil && reloaded.OnSuccess == nil && reloaded.RemoveOrphans == nil && reloaded.Targets == nil {
		return
	}

	changed := false

	changed = reloadField(&r.config.PostSyncHooks, reloaded.PostSyncHooks, func(v []PostSyncHook) bool { return v != nil }) || changed

	if !r.config.HookSettleDelay.FromEnv() && reloaded.HookSettleDelay != nil {
		r.config.HookSettleDelay.SetFromFile(*reloaded.HookSettleDelay)
		changed = true
	}

	sliceSet := func(v []string) bool { return v != nil }
	changed = reloadField(&r.config.DeployPaths, reloaded.DeployPaths, sliceSet) || changed
	changed = reloadField(&r.config.DeploySyncPaths, reloaded.DeploySyncPaths, sliceSet) || changed
	changed = reloadField(&r.config.DeploySyncExclude, reloaded.DeploySyncExclude, sliceSet) || changed
	changed = reloadField(&r.config.CriticalContainers, reloaded.CriticalContainers, sliceSet) || changed
	changed = reloadField(&r.config.DriftIgnore, reloaded.DriftIgnore, func(v []DriftIgnoreRule) bool { return v != nil }) || changed

	if reloaded.OnFailure != nil {
		r.config.OnFailure = *reloaded.OnFailure
		changed = true
	}

	if reloaded.OnSuccess != nil {
		r.config.OnSuccess = *reloaded.OnSuccess
		changed = true
	}

	if reloaded.RemoveOrphans != nil {
		if reloadField(&r.config.RemoveOrphans, *reloaded.RemoveOrphans, func(v bool) bool { return true }) {
			if r.deploy != nil {
				r.deploy.RemoveOrphans = r.config.RemoveOrphans.Value
			}
			changed = true
		}
	}

	// Apply per-target operational overrides (hooks, critical containers, sync paths).
	// Named targets can override these fields in bosun.yaml; the default target uses root-level values.
	if r.config.TargetName != "" && r.config.TargetName != DefaultTargetName && reloaded.Targets != nil {
		for _, t := range reloaded.Targets {
			if t.Name == r.config.TargetName {
				changed = applyTargetOverrides(r, t) || changed
				break
			}
		}
	}

	if changed {
		logger.Info().
			Int("hooks", len(r.config.PostSyncHooks.Value)).
			Dur("settle_delay", r.config.HookSettleDelay.Value).
			Int("deploy_paths", len(r.config.DeployPaths.Value)).
			Bool("on_failure", r.config.OnFailure).
			Bool("on_success", r.config.OnSuccess).
			Bool("remove_orphans", r.config.RemoveOrphans.Value).
			Msg("Reloaded project config from repo")
	}
}

// applyTargetOverrides overlays per-target field overrides onto the reconciler's config.
// Only fields that the Target struct can override are applied. Fields set from
// environment variables are never overwritten (reloadField checks FromEnv).
func applyTargetOverrides(r *Reconciler, t Target) bool {
	changed := false
	sliceSet := func(v []string) bool { return v != nil }

	if t.PostSyncHooks != nil {
		changed = reloadField(&r.config.PostSyncHooks, t.PostSyncHooks, func(v []PostSyncHook) bool { return v != nil }) || changed
	}
	if t.CriticalContainers != nil {
		changed = reloadField(&r.config.CriticalContainers, t.CriticalContainers, sliceSet) || changed
	}
	if t.DeploySyncPaths != nil {
		changed = reloadField(&r.config.DeploySyncPaths, t.DeploySyncPaths, sliceSet) || changed
	}
	if t.DeploySyncExclude != nil {
		changed = reloadField(&r.config.DeploySyncExclude, t.DeploySyncExclude, sliceSet) || changed
	}

	return changed
}

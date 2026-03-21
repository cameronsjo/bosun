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
	if reloaded.PostSyncHooks == nil && reloaded.HookSettleDelay == 0 && reloaded.DeployPaths == nil && reloaded.DeploySyncPaths == nil && reloaded.DeploySyncExclude == nil && reloaded.CriticalContainers == nil && reloaded.DriftIgnore == nil && reloaded.OnFailure == nil && reloaded.OnSuccess == nil && reloaded.RemoveOrphans == nil {
		return
	}

	changed := false

	if !r.config.PostSyncHooksFromEnv && reloaded.PostSyncHooks != nil {
		r.config.PostSyncHooks = reloaded.PostSyncHooks
		changed = true
	}

	if !r.config.HookSettleDelayFromEnv && reloaded.HookSettleDelay > 0 {
		r.config.HookSettleDelay = reloaded.HookSettleDelay
		changed = true
	}

	if !r.config.DeployPathsFromEnv && reloaded.DeployPaths != nil {
		r.config.DeployPaths = reloaded.DeployPaths
		changed = true
	}

	if !r.config.DeploySyncPathsFromEnv && reloaded.DeploySyncPaths != nil {
		r.config.DeploySyncPaths = reloaded.DeploySyncPaths
		changed = true
	}

	if !r.config.DeploySyncExcludeFromEnv && reloaded.DeploySyncExclude != nil {
		r.config.DeploySyncExclude = reloaded.DeploySyncExclude
		changed = true
	}

	if !r.config.CriticalContainersFromEnv && reloaded.CriticalContainers != nil {
		r.config.CriticalContainers = reloaded.CriticalContainers
		changed = true
	}

	if !r.config.DriftIgnoreFromEnv && reloaded.DriftIgnore != nil {
		r.config.DriftIgnore = reloaded.DriftIgnore
		changed = true
	}

	if reloaded.OnFailure != nil {
		r.config.OnFailure = *reloaded.OnFailure
		changed = true
	}

	if reloaded.OnSuccess != nil {
		r.config.OnSuccess = *reloaded.OnSuccess
		changed = true
	}

	if !r.config.RemoveOrphansFromEnv && reloaded.RemoveOrphans != nil {
		r.config.RemoveOrphans = *reloaded.RemoveOrphans
		if r.deploy != nil {
			r.deploy.RemoveOrphans = *reloaded.RemoveOrphans
		}
		changed = true
	}

	if changed {
		logger.Info().
			Int("hooks", len(r.config.PostSyncHooks)).
			Dur("settle_delay", r.config.HookSettleDelay).
			Int("deploy_paths", len(r.config.DeployPaths)).
			Bool("on_failure", r.config.OnFailure).
			Bool("on_success", r.config.OnSuccess).
			Bool("remove_orphans", r.config.RemoveOrphans).
			Msg("Reloaded project config from repo")
	}
}

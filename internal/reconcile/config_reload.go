package reconcile

import (
	"errors"
	"fmt"
	"strings"

	"github.com/cameronsjo/bosun/internal/log"
)

// reloadProjectConfig re-reads bosun.yaml from the repo working directory
// and updates operational config fields (hooks, deploy paths, alert gates,
// critical containers, drift ignore rules, remove-orphans) if the file changed.
// Fields overridden by environment variables are not updated.
func (r *Reconciler) reloadProjectConfig() error {
	if r.config.ConfigReloader == nil {
		return nil
	}

	logger := log.Component(log.ComponentReconcile) // No ctx available in this method.

	logger.Debug().
		Str("repo_dir", r.config.RepoDir).
		Msg("Preparing to reload project config from bosun.yaml")

	reloaded, err := r.config.ConfigReloader(r.config.RepoDir)
	if err != nil {
		if errors.Is(err, ErrInvalidPostSyncHooks) {
			logger.Error().Err(err).Msg("Reloaded post-sync hooks are invalid, aborting reconciliation")
			return err
		}
		logger.Warn().Err(err).Msg("Failed to reload project config from repo, keeping existing config")
		return nil
	}
	if reloaded == nil {
		return nil
	}

	if err := ValidatePostSyncHooks(reloaded.PostSyncHooks); err != nil {
		logger.Error().Err(err).Msg("Reloaded root post-sync hooks are invalid, aborting reconciliation")
		return err
	}
	for _, target := range reloaded.Targets {
		if err := ValidatePostSyncHooks(target.PostSyncHooks); err != nil {
			targetErr := fmt.Errorf("target %q: %w", target.Name, err)
			logger.Error().Err(targetErr).Msg("Reloaded target post-sync hooks are invalid, aborting reconciliation")
			return targetErr
		}
	}

	// If no field has any value from the repo, there's nothing to reload.
	// Use nil checks (not len==0) for slices so explicitly empty lists (e.g. `deploy_sync_paths: []`)
	// can clear in-memory filters during hot-reload.
	if reloaded.PostSyncHooks == nil && reloaded.HookSettleDelay == nil && reloaded.DeployPaths == nil && reloaded.DeploySyncPaths == nil && reloaded.DeploySyncExclude == nil && reloaded.CriticalContainers == nil && reloaded.DriftIgnore == nil && reloaded.OnFailure == nil && reloaded.OnSuccess == nil && reloaded.RemoveOrphans == nil && reloaded.ProjectName == nil && reloaded.Targets == nil {
		return nil
	}

	changed := false

	changed = reloadField(&r.config.PostSyncHooks, clonePostSyncHooks(reloaded.PostSyncHooks), func(v []PostSyncHook) bool { return v != nil }) || changed

	if !r.config.HookSettleDelay.FromEnv() && reloaded.HookSettleDelay != nil {
		r.config.HookSettleDelay.SetFromFile(*reloaded.HookSettleDelay)
		changed = true
	}

	sliceSet := func(v []string) bool { return v != nil }
	changed = reloadField(&r.config.DeployPaths, cloneSlice(reloaded.DeployPaths), sliceSet) || changed
	changed = reloadField(&r.config.DeploySyncPaths, cloneSlice(reloaded.DeploySyncPaths), sliceSet) || changed
	changed = reloadField(&r.config.DeploySyncExclude, cloneSlice(reloaded.DeploySyncExclude), sliceSet) || changed
	changed = reloadField(&r.config.CriticalContainers, cloneSlice(reloaded.CriticalContainers), sliceSet) || changed

	// Validate reloaded drift_ignore rules before applying them -- GitOps reload
	// is the normal path for changing drift_ignore in production, so an invalid
	// rule (unknown type, bad glob) must not silently take effect. On a hard
	// error, reject the reload and keep the previous in-memory rules; nil-ing
	// reloaded.DriftIgnore makes reloadField's isSet check treat it as "nothing
	// to apply" so the rest of this reload proceeds normally. A total-suppression
	// rule is still applied (it's valid config), but logs a loud warning, matching
	// the daemon-startup posture in ValidateConfig.
	if reloaded.DriftIgnore != nil {
		if warnings, err := ValidateDriftIgnoreRules(reloaded.DriftIgnore); err != nil {
			logger.Error().Err(err).Msg("Reloaded drift_ignore rules are invalid, keeping previous rules")
			reloaded.DriftIgnore = nil
		} else {
			for _, w := range warnings {
				logger.Warn().Msg(w)
			}
		}
	}
	changed = reloadField(&r.config.DriftIgnore, cloneSlice(reloaded.DriftIgnore), func(v []DriftIgnoreRule) bool { return v != nil }) || changed

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

	// Root-level project_name from the repo's bosun.yaml reaches the default
	// target before the first deploy (#390): a project-less compose up
	// collides containers. Applied before the per-target overrides below so a
	// lone `default` target's project_name wins over the root value. Env wins:
	// BOSUN_TARGETS-provided config is never overwritten by the repo.
	isDefaultTarget := r.config.TargetName == "" || strings.EqualFold(r.config.TargetName, DefaultTargetName)
	if isDefaultTarget && !r.config.TargetsFromEnv && reloaded.ProjectName != nil && *reloaded.ProjectName != "" {
		if r.setProjectName(*reloaded.ProjectName) {
			logger.Debug().
				Str("project_name", *reloaded.ProjectName).
				Msg("Reloaded root-level project_name from bosun.yaml for default target deployment")
			changed = true
		}
	}

	// Apply per-target operational overrides (hooks, critical containers, sync
	// paths, project_name). Named targets match by name; the default target
	// adopts a lone `default` target's overrides (#390/#391). An invalid list —
	// multi-target carrying a reserved `default` name, the misconfiguration
	// #391 fails loud on at startup — skips ALL overrides (named targets
	// included: startup would reject the whole config, so reload must not
	// half-apply it) and warns, since the reload path can't restart the daemon.
	if reloaded.Targets != nil {
		if offender := multiTargetDefaultName(reloaded.Targets); offender != "" {
			logger.Warn().
				Str("target", offender).
				Msg("Reloaded config declares a reserved default-named target in a multi-target list, expected every target to carry a distinct name — all target overrides are ignored (#391); rename it or make it the only target")
		} else if !isDefaultTarget {
			for _, t := range reloaded.Targets {
				if t.Name == r.config.TargetName {
					changed = applyTargetOverrides(r, t) || changed
					break
				}
			}
		} else if len(reloaded.Targets) == 1 && reloaded.Targets[0].IsDefault() {
			changed = applyTargetOverrides(r, reloaded.Targets[0]) || changed
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
	return nil
}

// applyTargetOverrides overlays per-target field overrides onto the reconciler's config.
// Only fields that the Target struct can override are applied. Fields set from
// environment variables are never overwritten (reloadField checks FromEnv;
// ProjectName checks TargetsFromEnv — it has no per-field env var, BOSUN_TARGETS
// is its only env vector).
func applyTargetOverrides(r *Reconciler, t Target) bool {
	changed := false
	sliceSet := func(v []string) bool { return v != nil }

	if t.PostSyncHooks != nil {
		changed = reloadField(&r.config.PostSyncHooks, clonePostSyncHooks(t.PostSyncHooks), func(v []PostSyncHook) bool { return v != nil }) || changed
	}
	if t.CriticalContainers != nil {
		changed = reloadField(&r.config.CriticalContainers, cloneSlice(t.CriticalContainers), sliceSet) || changed
	}
	if t.DeploySyncPaths != nil {
		changed = reloadField(&r.config.DeploySyncPaths, cloneSlice(t.DeploySyncPaths), sliceSet) || changed
	}
	if t.DeploySyncExclude != nil {
		changed = reloadField(&r.config.DeploySyncExclude, cloneSlice(t.DeploySyncExclude), sliceSet) || changed
	}
	if t.ProjectName != "" && !r.config.TargetsFromEnv {
		if r.setProjectName(t.ProjectName) {
			logger := log.Component(log.ComponentReconcile)
			logger.Debug().
				Str("target", t.Name).
				Str("project_name", t.ProjectName).
				Msg("Reloaded project_name override from target config")
			changed = true
		}
	}

	return changed
}

// multiTargetDefaultName returns the name of a reserved default-named target
// inside a multi-target list, or "" when the list is valid. Mirrors the
// startup rejection in ResolveTargets (#391) for the hot-reload path.
func multiTargetDefaultName(targets []Target) string {
	if len(targets) <= 1 {
		return ""
	}
	for _, t := range targets {
		if t.IsDefault() {
			return t.Name
		}
	}
	return ""
}

// setProjectName updates the compose project name on the config and pushes it
// onto the live deploy ops (the same double-write RemoveOrphans needs — the
// reconciler copies config onto r.deploy at construction, so a reloaded value
// must reach both). Returns true when the value changed.
func (r *Reconciler) setProjectName(name string) bool {
	if r.config.ProjectName == name {
		return false
	}
	r.config.ProjectName = name
	if r.deploy != nil {
		r.deploy.ProjectName = name
	}
	return true
}

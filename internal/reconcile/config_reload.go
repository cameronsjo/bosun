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
			logger.Error().
				Err(err).
				Str("hooks_outcome", "rejected").
				Str("hooks_source", "file").
				Msg("Reloaded post-sync hooks are invalid, aborting reconciliation")
			return err
		}
		logger.Warn().
			Str("hooks_outcome", "retained").
			Str("hooks_source", configSourceName(r.config.PostSyncHooks.Source)).
			Msg("Failed to reload project config from repo, keeping existing config; error detail redacted")
		return nil
	}
	if reloaded == nil {
		logger.Debug().
			Str("hooks_outcome", "retained").
			Str("hooks_source", configSourceName(r.config.PostSyncHooks.Source)).
			Msg("No project config snapshot available; keeping existing hook config")
		return nil
	}

	if err := ValidatePostSyncHooks(reloaded.PostSyncHooks); err != nil {
		logger.Error().
			Err(err).
			Str("hooks_outcome", "rejected").
			Str("hooks_source", "file").
			Msg("Reloaded root post-sync hooks are invalid, aborting reconciliation")
		return err
	}
	for _, target := range reloaded.Targets {
		if err := ValidatePostSyncHooks(target.PostSyncHooks); err != nil {
			targetErr := fmt.Errorf("target %q: %w", target.Name, err)
			logger.Error().
				Err(targetErr).
				Str("hooks_outcome", "rejected").
				Str("hooks_source", "file").
				Str("target", target.Name).
				Msg("Reloaded target post-sync hooks are invalid, aborting reconciliation")
			return targetErr
		}
	}
	if offender := multiTargetDefaultName(reloaded.Targets); offender != "" {
		logger.Warn().
			Str("target", offender).
			Msg("Reloaded config declares a reserved default-named target in a multi-target list; rejecting snapshot and keeping existing config (#391)")
		return nil
	}

	changed := false

	// Prepare the complete hook snapshot before committing either hooks or
	// settle delay. A non-nil ReloadedConfig means a project config file was
	// present and decoded successfully, so an absent root post_sync_hooks key is
	// authoritative and clears file-sourced hooks. HookSettleDelay uses pointer
	// presence: nil retains, including after a valid empty-file reload.
	hooksOutcome := "retained"
	hooksSource := configSourceName(r.config.PostSyncHooks.Source)
	preparedHooks := clonePostSyncHooks(r.config.PostSyncHooks.Value)
	preparedHookSource := r.config.PostSyncHooks.Source
	if !r.config.PostSyncHooks.FromEnv() {
		preparedHooks = clonePostSyncHooks(reloaded.PostSyncHooks)
		if preparedHooks == nil {
			preparedHooks = []PostSyncHook{}
		}
		preparedHookSource = SourceFile
		hooksSource = configSourceName(SourceFile)
		if len(preparedHooks) == 0 {
			hooksOutcome = "cleared"
		} else {
			hooksOutcome = "applied"
		}

		// Apply the current target's operational hook override to the prepared
		// root snapshot. Missing target/key means inheritance; explicit [] means
		// clear. BOSUN_TARGETS topology is authoritative and is not consulted
		// from the repo during reload.
		if !r.config.TargetsFromEnv {
			if target, found := matchingReloadTarget(r.config.TargetName, reloaded.Targets); found {
				if target.PostSyncHooks != nil {
					preparedHooks = clonePostSyncHooks(target.PostSyncHooks)
					if len(preparedHooks) == 0 {
						hooksOutcome = "cleared"
					} else {
						hooksOutcome = "applied"
					}
				}
			} else if r.config.TargetName != "" && !strings.EqualFold(r.config.TargetName, DefaultTargetName) {
				logger.Warn().
					Str("target", r.config.TargetName).
					Msg("Reloaded config removed this target descriptor; discarded stale target hook override and inherited root hooks (restart daemon to apply structural target removal)")
			}
		}
	}

	// Commit hooks and delay together only after root and every target hook have
	// passed validation above. Assign owned slices so concurrent target
	// reconcilers cannot alias the loader or one another.
	if !r.config.PostSyncHooks.FromEnv() {
		r.config.PostSyncHooks = ConfigField[[]PostSyncHook]{
			Value:  preparedHooks,
			Source: preparedHookSource,
		}
		changed = true
	}
	delayOutcome := "retained"
	if !r.config.HookSettleDelay.FromEnv() && reloaded.HookSettleDelay != nil {
		r.config.HookSettleDelay.SetFromFile(*reloaded.HookSettleDelay)
		delayOutcome = "applied"
		changed = true
	}
	logger.Debug().
		Str("hooks_outcome", hooksOutcome).
		Str("hooks_source", hooksSource).
		Int("hooks", len(r.config.PostSyncHooks.Value)).
		Str("settle_delay_outcome", delayOutcome).
		Str("settle_delay_source", configSourceName(r.config.HookSettleDelay.Source)).
		Dur("settle_delay", r.config.HookSettleDelay.Value).
		Str("target", r.config.TargetName).
		Msg("Processed project hook config snapshot")

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
		if !isDefaultTarget {
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

func matchingReloadTarget(targetName string, targets []Target) (Target, bool) {
	isDefaultTarget := targetName == "" || strings.EqualFold(targetName, DefaultTargetName)
	if isDefaultTarget {
		if len(targets) == 1 && targets[0].IsDefault() {
			return targets[0], true
		}
		return Target{}, false
	}
	for _, target := range targets {
		if target.Name == targetName {
			return target, true
		}
	}
	return Target{}, false
}

func configSourceName(source ConfigSource) string {
	switch source {
	case SourceFile:
		return "file"
	case SourceEnv:
		return "environment"
	default:
		return "default"
	}
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

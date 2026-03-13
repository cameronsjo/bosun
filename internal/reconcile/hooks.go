package reconcile

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/log"
	"github.com/cameronsjo/bosun/internal/ui"
)

// PostSyncHook defines a container action triggered when specific file paths change.
type PostSyncHook struct {
	// Paths are glob patterns matched against changed files (relative to repo root).
	Paths []string `json:"paths" yaml:"paths"`
	// Action is the operation to perform: "restart" (default) or "exec".
	Action string `json:"action" yaml:"action"`
	// Container is the name of the container to act on.
	Container string `json:"container" yaml:"container"`
	// Command is the command to execute inside the container (for action: "exec").
	// Accepts a list of strings (e.g., ["nginx", "-s", "reload"]).
	Command []string `json:"command,omitempty" yaml:"command,omitempty"`
	// Delay is an optional pause before executing this hook's action.
	// Useful when a container needs extra time for config propagation.
	Delay Duration `json:"delay,omitempty" yaml:"delay,omitempty"`
}

// hookKey returns a deduplication key for a hook.
// Different actions on the same container are distinct hooks (e.g., restart + exec).
// For exec hooks, the command is included so two different commands on the same
// container are treated as separate hooks.
func hookKey(h PostSyncHook) string {
	action := h.Action
	if action == "" {
		action = "restart"
	}
	key := h.Container + ":" + action
	if action == "exec" && len(h.Command) > 0 {
		key += ":" + strings.Join(h.Command, " ")
	}
	return key
}

// EvaluatePostSyncHooks matches changed file paths against hook glob patterns
// and returns the hooks that should be executed.
// Deduplication keys on container+action, so a container can have both a restart
// and an exec hook fire from the same change set.
func EvaluatePostSyncHooks(changedFiles []string, hooks []PostSyncHook) []PostSyncHook {
	if len(hooks) == 0 || len(changedFiles) == 0 {
		return nil
	}

	var matched []PostSyncHook
	seen := make(map[string]bool)

	for _, hook := range hooks {
		key := hookKey(hook)
		if seen[key] {
			continue
		}
		if hookMatchesAny(hook, changedFiles) {
			matched = append(matched, hook)
			seen[key] = true
		}
	}

	return matched
}

// dedupeHooksByContainer returns hooks with duplicate container+action pairs removed
// (first wins). Used as a fallback when DiffFiles fails and all hooks should fire
// unconditionally.
func dedupeHooksByContainer(hooks []PostSyncHook) []PostSyncHook {
	var result []PostSyncHook
	seen := make(map[string]bool)
	for _, hook := range hooks {
		key := hookKey(hook)
		if seen[key] {
			continue
		}
		result = append(result, hook)
		seen[key] = true
	}
	return result
}

// hookMatchesAny returns true if any changed file matches any of the hook's path patterns.
func hookMatchesAny(hook PostSyncHook, changedFiles []string) bool {
	for _, pattern := range hook.Paths {
		for _, file := range changedFiles {
			if matchGlob(pattern, file) {
				return true
			}
		}
	}
	return false
}

// matchGlob checks if a file path matches a glob pattern.
// Supports ** for recursive directory matching by checking if the file
// starts with the pattern prefix (before **).
func matchGlob(pattern, file string) bool {
	// Handle ** patterns: "dir/**" matches anything under dir/
	if strings.Contains(pattern, "**") {
		prefix := strings.SplitN(pattern, "**", 2)[0]
		prefix = strings.TrimRight(prefix, "/")
		if prefix == "" {
			return true
		}
		return strings.HasPrefix(file, prefix+"/") || file == prefix
	}

	// Use filepath.Match for simple glob patterns.
	matched, err := filepath.Match(pattern, file)
	if err != nil {
		return false
	}
	return matched
}

// matchAnyPath returns true if any file matches any of the glob patterns.
// Used by deploy_paths to check if any changed files are deploy-relevant.
func matchAnyPath(files []string, patterns []string) bool {
	for _, file := range files {
		for _, pattern := range patterns {
			if matchGlob(pattern, file) {
				return true
			}
		}
	}
	return false
}

// ExecutePostSyncHooks runs matched hooks by performing the specified action on containers.
// settleDelay is a global pause before any hooks run (filesystem propagation).
func ExecutePostSyncHooks(ctx context.Context, client *docker.Client, hooks []PostSyncHook, settleDelay time.Duration) error {
	if len(hooks) == 0 {
		return nil
	}

	logger := log.ComponentCtx(ctx, log.ComponentReconcile)

	// Global settle delay: wait for filesystem propagation before running hooks.
	if settleDelay > 0 {
		logger.Info().Dur("settle_delay", settleDelay).Msg("Waiting for filesystem settle before post-sync hooks")
		ui.Info("  Waiting %s for filesystem settle...", settleDelay)
		select {
		case <-time.After(settleDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	var errs []string
	for _, hook := range hooks {
		// Per-hook delay: wait before executing this hook's action.
		if hook.Delay.Duration > 0 {
			logger.Info().Dur("delay", hook.Delay.Duration).Str("container", hook.Container).Str("action", hook.Action).Msg("Waiting before hook execution")
			ui.Info("  Waiting %s before %s on %s...", hook.Delay.Duration, hook.Action, hook.Container)
			select {
			case <-time.After(hook.Delay.Duration):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		switch hook.Action {
		case "restart":
			logger.Info().
				Str("container", hook.Container).
				Strs("patterns", hook.Paths).
				Msg("Executing post-sync hook: restarting container")
			ui.Info("  Restarting %s (config files changed)...", hook.Container)

			if err := client.RestartContainer(ctx, hook.Container); err != nil {
				logger.Error().
					Err(err).
					Str("container", hook.Container).
					Msg("Post-sync hook failed: container restart error")
				errs = append(errs, fmt.Sprintf("%s: %v", hook.Container, err))
			} else {
				ui.Success("  Restarted %s", hook.Container)
			}

		case "exec":
			if len(hook.Command) == 0 {
				logger.Warn().
					Str("container", hook.Container).
					Msg("Exec hook has no command, skipping")
				continue
			}

			logger.Info().
				Str("container", hook.Container).
				Int("command_args", len(hook.Command)).
				Strs("patterns", hook.Paths).
				Msg("Executing post-sync hook: exec in container")
			ui.Info("  Executing command in %s (config files changed)...", hook.Container)

			if err := client.ExecContainer(ctx, hook.Container, hook.Command); err != nil {
				logger.Error().
					Err(err).
					Str("container", hook.Container).
					Msg("Post-sync hook failed: container exec error")
				errs = append(errs, fmt.Sprintf("%s exec: %v", hook.Container, err))
			} else {
				ui.Success("  Executed command in %s", hook.Container)
			}

		default:
			logger.Warn().
				Str("action", hook.Action).
				Str("container", hook.Container).
				Msg("Unsupported post-sync hook action, skipping")
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("post-sync hook failures: %s", strings.Join(errs, "; "))
	}
	return nil
}

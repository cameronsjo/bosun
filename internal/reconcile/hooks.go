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
	// Action is the operation to perform (currently only "restart" is supported).
	Action string `json:"action" yaml:"action"`
	// Container is the name of the container to act on.
	Container string `json:"container" yaml:"container"`
	// Delay is an optional pause before restarting this specific container.
	// Useful when a container needs extra time for config propagation.
	Delay Duration `json:"delay,omitempty" yaml:"delay,omitempty"`
}

// EvaluatePostSyncHooks matches changed file paths against hook glob patterns
// and returns the hooks that should be executed.
func EvaluatePostSyncHooks(changedFiles []string, hooks []PostSyncHook) []PostSyncHook {
	if len(hooks) == 0 || len(changedFiles) == 0 {
		return nil
	}

	var matched []PostSyncHook
	seen := make(map[string]bool)

	for _, hook := range hooks {
		if seen[hook.Container] {
			continue
		}
		if hookMatchesAny(hook, changedFiles) {
			matched = append(matched, hook)
			seen[hook.Container] = true
		}
	}

	return matched
}

// dedupeHooksByContainer returns hooks with duplicate containers removed (first wins).
// Used as a fallback when DiffFiles fails and all hooks should fire unconditionally.
func dedupeHooksByContainer(hooks []PostSyncHook) []PostSyncHook {
	var result []PostSyncHook
	seen := make(map[string]bool)
	for _, hook := range hooks {
		if seen[hook.Container] {
			continue
		}
		result = append(result, hook)
		seen[hook.Container] = true
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

// MatchAnyPath returns true if any file matches any of the glob patterns.
// Used by deploy_paths to check if any changed files are deploy-relevant.
func MatchAnyPath(files []string, patterns []string) bool {
	for _, file := range files {
		for _, pattern := range patterns {
			if matchGlob(pattern, file) {
				return true
			}
		}
	}
	return false
}

// ExecutePostSyncHooks runs matched hooks by restarting the specified containers.
// settleDelay is a global pause before any hooks run (filesystem propagation).
func ExecutePostSyncHooks(ctx context.Context, client *docker.Client, hooks []PostSyncHook, settleDelay time.Duration) error {
	if len(hooks) == 0 {
		return nil
	}

	logger := log.Component(log.ComponentReconcile)

	// Global settle delay: wait for filesystem propagation before restarting anything.
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
		if hook.Action != "restart" {
			logger.Warn().
				Str("action", hook.Action).
				Str("container", hook.Container).
				Msg("Unsupported post-sync hook action, skipping")
			continue
		}

		// Per-hook delay: wait before restarting this specific container.
		if hook.Delay.Duration > 0 {
			logger.Info().Dur("delay", hook.Delay.Duration).Str("container", hook.Container).Msg("Waiting before container restart")
			ui.Info("  Waiting %s before restarting %s...", hook.Delay.Duration, hook.Container)
			select {
			case <-time.After(hook.Delay.Duration):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

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
	}

	if len(errs) > 0 {
		return fmt.Errorf("post-sync hook failures: %s", strings.Join(errs, "; "))
	}
	return nil
}

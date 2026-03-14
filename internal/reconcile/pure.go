package reconcile

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// shouldSkipDeploy returns true if the deploy should be skipped because the
// current commit has already been deployed, force mode is not enabled, and
// there is no pending redeploy from a previous partial failure.
func shouldSkipDeploy(lastDeployed, currentCommit string, force, needsRedeploy bool) bool {
	if force || needsRedeploy {
		return false
	}
	return lastDeployed == currentCommit
}

// shouldTriggerCircuitBreaker returns true if the circuit breaker should
// activate: the same commit has been attempted too many times and force
// mode is not enabled.
func shouldTriggerCircuitBreaker(lastAttempted, current string, attempts, maxAttempts int, force bool) bool {
	return lastAttempted == current && attempts >= maxAttempts && !force
}

// nextAttemptState computes the updated attempt tracking fields.
// If the commit being attempted is the same as the last attempt, the counter
// increments. Otherwise, the counter resets to 1 for the new commit.
func nextAttemptState(lastAttempted, current string, currentCount int) (newLastAttempted string, newCount int) {
	if lastAttempted == current {
		return current, currentCount + 1
	}
	return current, 1
}

// resolveTargetHost determines the SSH target host from config and secrets.
// Returns the config host if set, otherwise extracts network.unraid_ip from
// the secrets map and prepends "root@".
func resolveTargetHost(configHost string, secrets map[string]any) string {
	if configHost != "" {
		return configHost
	}

	if network, ok := secrets["network"].(map[string]any); ok {
		if ip, ok := network["unraid_ip"].(string); ok {
			return "root@" + ip
		}
	}

	return ""
}

// buildComposeArgs constructs the docker compose argument list with an
// optional project name and file flags.
func buildComposeArgs(projectName string, files []string) []string {
	args := []string{"compose"}
	if projectName != "" {
		args = append(args, "-p", projectName)
	}
	for _, f := range files {
		args = append(args, "-f", f)
	}
	return args
}

// classifySSHError categorizes an SSH stderr string into a known error class.
// Returns one of: "auth", "connection", "host_key", "dns", "timeout", "unknown".
func classifySSHError(stderr string) string {
	lower := strings.ToLower(stderr)

	switch {
	case strings.Contains(lower, "permission denied"):
		return "auth"
	case strings.Contains(lower, "connection refused"):
		return "connection"
	case strings.Contains(lower, "host key verification failed"):
		return "host_key"
	case strings.Contains(lower, "name or service not known"):
		return "dns"
	case strings.Contains(lower, "connection timed out"):
		return "timeout"
	case strings.Contains(lower, "no route to host"):
		return "connection"
	default:
		return "unknown"
	}
}

// buildKnownHostsPaths returns the candidate known_hosts file paths in
// priority order. Paths are constructed from the environment variable and
// home directory; no filesystem checks are performed.
func buildKnownHostsPaths(envKnownHosts, homeDir string) []string {
	paths := []string{
		envKnownHosts,
		filepath.Join(homeDir, ".ssh", "known_hosts"),
		"/config/known_hosts",
	}

	// Filter out empty strings from unset env vars.
	var result []string
	for _, p := range paths {
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// buildSSHKeyPaths returns the candidate SSH key file paths in priority order.
// Paths are constructed from the environment variable and home directory;
// no filesystem checks are performed.
func buildSSHKeyPaths(envKey, homeDir string) []string {
	paths := []string{
		envKey,
		"/config/deploy-key",
		"/config/ssh-key",
		filepath.Join(homeDir, ".ssh", "id_ed25519"),
		filepath.Join(homeDir, ".ssh", "id_rsa"),
	}

	// Filter out empty strings from unset env vars.
	var result []string
	for _, p := range paths {
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// composeFailureKind indicates whether a compose-up failure is recoverable.
type composeFailureKind int

const (
	// failureStartFailure means one or more containers failed to start.
	// This is the zero value so that an uninitialized result defaults to
	// the conservative "trigger rollback" path (fail-safe).
	failureStartFailure composeFailureKind = iota
	// failureUnhealthyOnly means all containers are running but some are unhealthy.
	failureUnhealthyOnly
)

// composeFailureResult holds the classification of a compose-up failure.
type composeFailureResult struct {
	Kind      composeFailureKind
	Unhealthy []string // container names that are running but unhealthy
	Failed    []string // container names that failed to start
}

// composePSEntry represents a single container from `docker compose ps --format json`.
// Docker Compose v2 emits one JSON object per line (NDJSON).
type composePSEntry struct {
	Name   string `json:"Name"`
	State  string `json:"State"`
	Health string `json:"Health"`
}

// classifyComposePS inspects parsed `docker compose ps` output to classify the
// failure. Each container is categorized as running-healthy, running-unhealthy,
// or failed (exited/restarting/dead/absent).
//
// Returns failureUnhealthyOnly when every container is running (some unhealthy).
// Returns failureStartFailure when any container is not running.
func classifyComposePS(entries []composePSEntry) composeFailureResult {
	var unhealthy []string
	var failed []string

	for _, e := range entries {
		switch strings.ToLower(e.State) {
		case "running":
			health := strings.ToLower(e.Health)
			if health == "unhealthy" || health == "starting" {
				unhealthy = append(unhealthy, e.Name)
			}
			// "healthy" or "" (no healthcheck) are fine
		default:
			// exited, restarting, dead, created, paused, removing, etc.
			failed = append(failed, e.Name)
		}
	}

	if len(failed) > 0 {
		return composeFailureResult{Kind: failureStartFailure, Unhealthy: unhealthy, Failed: failed}
	}
	if len(unhealthy) > 0 {
		return composeFailureResult{Kind: failureUnhealthyOnly, Unhealthy: unhealthy, Failed: failed}
	}
	// All containers running and healthy but compose still exited non-zero.
	return composeFailureResult{Kind: failureStartFailure}
}

// parseComposePSOutput parses output from `docker compose ps --format json`.
// Handles both JSON array format (Docker Compose docs spec) and NDJSON
// (one JSON object per line, emitted by Compose v2.21.0+).
func parseComposePSOutput(output []byte) ([]composePSEntry, error) {
	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return nil, nil
	}

	// JSON array format: [{...},{...}]
	if strings.HasPrefix(trimmed, "[") {
		var entries []composePSEntry
		if err := json.Unmarshal([]byte(trimmed), &entries); err != nil {
			return nil, err
		}
		if len(entries) == 0 {
			return nil, nil
		}
		return entries, nil
	}

	// NDJSON format: one JSON object per line
	var entries []composePSEntry
	lines := strings.Split(trimmed, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry composePSEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

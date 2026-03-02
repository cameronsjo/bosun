package reconcile

import (
	"path/filepath"
	"strings"
)

// shouldSkipDeploy returns true if the deploy should be skipped because the
// current commit has already been deployed and force mode is not enabled.
func shouldSkipDeploy(lastDeployed, currentCommit string, force bool) bool {
	return lastDeployed == currentCommit && !force
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

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

// hasMissingDeclaredServices returns true if any declared service has no
// corresponding running container in the actual state. This is used to detect
// the case where a commit was marked as deployed but one or more services were
// never actually started (e.g., the service was added to the compose file in a
// commit that was already recorded as deployed due to a prior run).
//
// When declared is empty, returns false — there is nothing to compare against.
func hasMissingDeclaredServices(declared []DeclaredService, actual []ActualService) bool {
	if len(declared) == 0 {
		return false
	}

	running := make(map[string]bool, len(actual))
	for _, svc := range actual {
		if svc.State == "running" {
			running[svc.Name] = true
		}
	}

	for _, decl := range declared {
		if !running[decl.Name] {
			return true
		}
	}
	return false
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

// FilterIgnoredDriftItems returns only the drift items that do not match any ignore rule.
// This is the exported entry point for callers outside the reconcile package.
func FilterIgnoredDriftItems(items []DriftItem, rules []DriftIgnoreRule) []DriftItem {
	return filterIgnoredDrift(items, rules)
}

// filterIgnoredDrift returns only the drift items that do not match any ignore rule.
// Service matching uses filepath.Match for glob support. Type matching is exact
// or "*" for wildcard. Invalid glob patterns in rules are silently skipped
// (the rule never matches).
func filterIgnoredDrift(items []DriftItem, rules []DriftIgnoreRule) []DriftItem {
	if len(rules) == 0 {
		return items
	}

	var kept []DriftItem
	for _, item := range items {
		if !driftItemMatchesAnyRule(item, rules) {
			kept = append(kept, item)
		}
	}
	return kept
}

// driftItemMatchesAnyRule returns true if the item matches at least one ignore rule.
func driftItemMatchesAnyRule(item DriftItem, rules []DriftIgnoreRule) bool {
	for _, rule := range rules {
		if driftItemMatchesRule(item, rule) {
			return true
		}
	}
	return false
}

// driftItemMatchesRule returns true if the item matches the given ignore rule.
func driftItemMatchesRule(item DriftItem, rule DriftIgnoreRule) bool {
	// Match service name using glob.
	matched, err := filepath.Match(rule.Service, item.Service)
	if err != nil || !matched {
		return false
	}

	// Match type: "*" matches all, otherwise exact match.
	return rule.Type == "*" || rule.Type == string(item.Type)
}

// ComposeFileResult records the outcome of a single compose file's deployment.
type ComposeFileResult struct {
	File       string // absolute path to the compose file
	Success    bool   // true if compose up succeeded (including unhealthy-only)
	RolledBack bool   // true if the file was rolled back from backup after failure
	Err        error  // nil on success, the compose-up error on failure
}

// ComposeUpSummary aggregates per-file compose up results.
type ComposeUpSummary struct {
	Results    []ComposeFileResult
	Succeeded  int
	Failed     int
	RolledBack int
}

// classifyComposeResults tallies per-file outcomes into a summary.
func classifyComposeResults(results []ComposeFileResult) ComposeUpSummary {
	s := ComposeUpSummary{Results: results}
	for _, r := range results {
		if r.Success {
			s.Succeeded++
		} else {
			s.Failed++
			if r.RolledBack {
				s.RolledBack++
			}
		}
	}
	return s
}

// buildOrphanPassFiles constructs the file list for the orphan-reconciliation pass.
// Succeeded files use their original (new) path. Rolled-back files use the backup
// copy so Docker sees the previous service set. Failed files with no backup use
// the original path (best-effort).
func buildOrphanPassFiles(results []ComposeFileResult, backupPath string) []string {
	var files []string
	for _, r := range results {
		if r.Success {
			files = append(files, r.File)
			continue
		}
		if r.RolledBack && backupPath != "" {
			backupFile := filepath.Join(backupPath, filepath.Base(r.File))
			files = append(files, backupFile)
			continue
		}
		// Failed without rollback — include original so orphan pass at least knows about it.
		files = append(files, r.File)
	}
	return files
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

package reconcile

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Validation patterns for security-sensitive inputs.
var (
	// hostPattern validates SSH host format: user@hostname or just hostname.
	// Allows alphanumeric, underscore, hyphen for username and alphanumeric, dot, hyphen for hostname.
	hostPattern = regexp.MustCompile(`^([a-zA-Z0-9_-]+@)?[a-zA-Z0-9.-]+$`)

	// branchPattern validates git branch names.
	// Allows alphanumeric, underscore, slash, dot, and hyphen.
	branchPattern = regexp.MustCompile(`^[a-zA-Z0-9_/.-]+$`)

	// containerNamePattern validates Docker container names.
	// Must start with alphanumeric and can contain alphanumeric, underscore, dot, and hyphen.
	containerNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

	// projectNamePattern validates docker compose project names.
	// Must start with alphanumeric and can contain alphanumeric, underscore, hyphen, and dot.
	// Length cap of 128 matches docker compose's own limit.
	projectNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

	// remotePathPattern validates remote filesystem paths passed to SSH commands.
	// Allows absolute paths (starting with /) or relative paths using alphanumeric,
	// underscore, hyphen, dot, space, and forward slash. Spaces are permitted because
	// real-world Unraid NAS paths frequently include them (e.g. "/mnt/user/My Media/appdata");
	// shellquote.Join handles quoting correctly. Rejects path traversal (../).
	remotePathPattern = regexp.MustCompile(`^[a-zA-Z0-9/_. -]+$`)

	// shellMetachars contains shell metacharacters that could enable command injection.
	shellMetachars = []string{";", "&", "|", "$", "`", "(", ")", "{", "}", "<", ">", "\\", "\n", "\r", "'", "\""}
)

// allowedSignals is the allowlist of valid signals for Docker containers.
var allowedSignals = map[string]bool{
	"SIGHUP":  true,
	"SIGTERM": true,
	"SIGKILL": true,
	"SIGUSR1": true,
	"SIGUSR2": true,
	"HUP":     true,
	"TERM":    true,
	"KILL":    true,
	"USR1":    true,
	"USR2":    true,
}

// validateHost validates an SSH host string for security.
// Accepts formats: "user@hostname" or "hostname".
// Rejects hosts containing shell metacharacters or starting with "-" (SSH option injection).
func validateHost(host string) error {
	if host == "" {
		return fmt.Errorf("host cannot be empty")
	}

	// Reject SSH option injection (arguments starting with -)
	if strings.HasPrefix(host, "-") {
		return fmt.Errorf("invalid host: cannot start with '-' (potential SSH option injection)")
	}

	// Reject shell metacharacters
	for _, char := range shellMetachars {
		if strings.Contains(host, char) {
			return fmt.Errorf("invalid host: contains shell metacharacter %q", char)
		}
	}

	// Validate format
	if !hostPattern.MatchString(host) {
		return fmt.Errorf("invalid host format: must match user@hostname or hostname pattern")
	}

	return nil
}

// validateBranch validates a git branch name for security.
// Accepts standard git ref format: alphanumeric, underscore, slash, dot, and hyphen.
// Rejects branch names starting with "-" (git option injection).
func validateBranch(branch string) error {
	if branch == "" {
		return fmt.Errorf("branch cannot be empty")
	}

	// Reject git option injection
	if strings.HasPrefix(branch, "-") {
		return fmt.Errorf("invalid branch: cannot start with '-' (potential git option injection)")
	}

	// Reject shell metacharacters
	for _, char := range shellMetachars {
		if strings.Contains(branch, char) {
			return fmt.Errorf("invalid branch: contains shell metacharacter %q", char)
		}
	}

	// Validate format
	if !branchPattern.MatchString(branch) {
		return fmt.Errorf("invalid branch format: must match git ref pattern (alphanumeric, underscore, slash, dot, hyphen)")
	}

	return nil
}

// validateSignal validates a Docker signal against an allowlist.
// Accepts: SIGHUP, SIGTERM, SIGKILL, SIGUSR1, SIGUSR2, HUP, TERM, KILL, USR1, USR2.
func validateSignal(signal string) error {
	if signal == "" {
		return fmt.Errorf("signal cannot be empty")
	}

	// Normalize to uppercase for comparison
	upperSignal := strings.ToUpper(signal)

	if !allowedSignals[upperSignal] {
		return fmt.Errorf("invalid signal %q: must be one of SIGHUP, SIGTERM, SIGKILL, SIGUSR1, SIGUSR2 (or without SIG prefix)", signal)
	}

	return nil
}

// validateContainerName validates a Docker container name.
// Must start with alphanumeric and can contain alphanumeric, underscore, dot, and hyphen.
func validateContainerName(name string) error {
	if name == "" {
		return fmt.Errorf("container name cannot be empty")
	}

	// Reject names starting with "-" (docker option injection)
	if strings.HasPrefix(name, "-") {
		return fmt.Errorf("invalid container name: cannot start with '-' (potential docker option injection)")
	}

	// Reject shell metacharacters
	for _, char := range shellMetachars {
		if strings.Contains(name, char) {
			return fmt.Errorf("invalid container name: contains shell metacharacter %q", char)
		}
	}

	// Validate format
	if !containerNamePattern.MatchString(name) {
		return fmt.Errorf("invalid container name format: must start with alphanumeric and contain only alphanumeric, underscore, dot, or hyphen")
	}

	return nil
}

// ValidateProjectName validates a docker compose project name for use in shell commands.
// Must start with alphanumeric and may contain alphanumeric, underscore, hyphen, or dot.
// Maximum length is 128 characters (docker compose's own limit).
// Rejects shell metacharacters that could enable command injection.
func ValidateProjectName(name string) error {
	if name == "" {
		return fmt.Errorf("project name cannot be empty")
	}

	// Reject names starting with "-" (option injection)
	if strings.HasPrefix(name, "-") {
		return fmt.Errorf("invalid project name: cannot start with '-' (potential option injection)")
	}

	// Reject shell metacharacters
	for _, char := range shellMetachars {
		if strings.Contains(name, char) {
			return fmt.Errorf("invalid project name: contains shell metacharacter %q", char)
		}
	}

	// Validate format and length
	if !projectNamePattern.MatchString(name) {
		return fmt.Errorf("invalid project name format: must start with alphanumeric and contain only alphanumeric, underscore, hyphen, or dot (max 128 chars)")
	}

	return nil
}

// ValidateRemotePath validates a filesystem path intended for use in SSH-executed shell commands.
// Accepts absolute paths (starting with /) and relative paths using alphanumeric, underscore,
// hyphen, dot, space, and forward slash. Spaces are permitted to support Unraid NAS paths
// (e.g. "/mnt/user/My Media/appdata"); shellquote.Join handles quoting at call sites.
// Rejects path traversal (../), shell metacharacters, and paths starting with "-" (CLI flag injection).
func ValidateRemotePath(path string) error {
	if path == "" {
		return fmt.Errorf("remote path cannot be empty")
	}

	// Reject paths starting with "-" to prevent CLI flag injection via SSH
	if strings.HasPrefix(path, "-") {
		return fmt.Errorf("invalid remote path: cannot start with '-' (potential CLI flag injection)")
	}

	// Reject path traversal
	if strings.Contains(path, "..") {
		return fmt.Errorf("invalid remote path: path traversal (contains '..')")
	}

	// Reject shell metacharacters
	for _, char := range shellMetachars {
		if strings.Contains(path, char) {
			return fmt.Errorf("invalid remote path: contains shell metacharacter %q", char)
		}
	}

	// Validate format — spaces are allowed (shellquote.Join quotes them correctly)
	if !remotePathPattern.MatchString(path) {
		return fmt.Errorf("invalid remote path format: must contain only alphanumeric, underscore, hyphen, dot, space, and forward slash")
	}

	return nil
}

// ValidateDriftIgnoreRules validates drift_ignore rules -- from the config
// file or the BOSUN_DRIFT_IGNORE environment override -- against the
// implemented DriftType enum and glob syntax.
//
// A rule with an unknown type or an invalid service glob is returned as an
// error, because such a rule silently never matches and leaves drift
// unreported; the offending rule's index is named so operators can find it.
// A rule that suppresses all drift (service "*" and type "*") is syntactically
// valid but disables drift detection entirely, so it is reported separately as
// a non-fatal warning: callers decide whether to treat it as an error (e.g.
// `bosun validate`, where it should block) or a loud warning (daemon startup,
// where an intentional full mute is allowed but never silent).
func ValidateDriftIgnoreRules(rules []DriftIgnoreRule) (warnings []string, err error) {
	validTypes := validDriftTypes()
	var errs []string

	for i, rule := range rules {
		if rule.Type != driftIgnoreWildcard && !validTypes[DriftType(rule.Type)] {
			errs = append(errs, fmt.Sprintf(
				"drift_ignore[%d]: unknown type %q (must be one of missing, image_mismatch, unhealthy, or %q)",
				i, rule.Type, driftIgnoreWildcard,
			))
			continue
		}

		// The "" target is a syntax probe, not a real match attempt -- filepath.Match
		// validates pattern syntax before it ever compares against a name, so this
		// only checks whether rule.Service itself parses; the returned bool is unused.
		if _, matchErr := filepath.Match(rule.Service, ""); matchErr != nil {
			errs = append(errs, fmt.Sprintf(
				"drift_ignore[%d]: invalid service glob %q: %v", i, rule.Service, matchErr,
			))
			continue
		}

		if rule.Service == driftIgnoreWildcard && rule.Type == driftIgnoreWildcard {
			warnings = append(warnings, fmt.Sprintf(
				"drift_ignore[%d]: service and type are both %q -- this suppresses ALL drift detection",
				i, driftIgnoreWildcard,
			))
		}
	}

	if len(errs) > 0 {
		return warnings, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return warnings, nil
}

// ValidateAndSanitizeTargets validates security-sensitive fields on each target,
// clearing invalid fields with a warning rather than dropping the whole target.
// This mirrors the YAML-load semantics in extractTargets — a single bad field
// must not block deployments to that host.
//
// The logger parameter receives structured warn entries; pass nil to suppress logging.
func ValidateAndSanitizeTargets(targets []Target, warn func(target, field string, err error)) []Target {
	out := make([]Target, len(targets))
	copy(out, targets)
	for i := range out {
		if out[i].ProjectName != "" {
			if err := ValidateProjectName(out[i].ProjectName); err != nil {
				if warn != nil {
					warn(out[i].Name, "project_name", err)
				}
				out[i].ProjectName = ""
			}
		}
		if out[i].RemoteAppdataPath != "" {
			if err := ValidateRemotePath(out[i].RemoteAppdataPath); err != nil {
				if warn != nil {
					warn(out[i].Name, "remote_appdata_path", err)
				}
				out[i].RemoteAppdataPath = ""
			}
		}
	}
	return out
}

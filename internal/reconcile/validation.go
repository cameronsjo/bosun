package reconcile

import (
	"fmt"
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

// Package preflight provides pre-flight validation for required binaries and system checks.
package preflight

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DefaultLookPathTimeout is the default timeout for exec.LookPath operations.
const DefaultLookPathTimeout = 5 * time.Second

// ErrEmptyBinaryName indicates an empty binary name was provided.
var ErrEmptyBinaryName = errors.New("empty binary name")

// BinaryCheck represents a required binary and its purpose.
type BinaryCheck struct {
	Name        string
	Required    bool   // false = warning only
	InstallHint string // e.g., "brew install sops" or "https://..."
	Error       error  // The underlying error from LookPath if lookup failed
}

// requiredBinaries defines binaries that must be present for bosun to function.
// Note: git is no longer required as we use go-git library for git operations.
var requiredBinaries = []BinaryCheck{
	{
		Name:        "docker",
		Required:    true,
		InstallHint: "Install Docker: https://docs.docker.com/get-docker/",
	},
}

// optionalBinaries defines binaries that enhance bosun functionality but are not strictly required.
// Note: sops binary is no longer required - we use the go-sops library for in-process decryption.
// The age binary is still optional for key generation (age-keygen).
var optionalBinaries = []BinaryCheck{
	{
		Name:        "age",
		Required:    false,
		InstallHint: "Install age: brew install age (needed for key generation with age-keygen)",
	},
}

// lookPathWithTimeout wraps exec.LookPath with a context timeout.
// Returns the path and any error, including context deadline exceeded.
func lookPathWithTimeout(ctx context.Context, name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", ErrEmptyBinaryName
	}

	type result struct {
		path string
		err  error
	}

	ch := make(chan result, 1)
	go func() {
		path, err := exec.LookPath(name)
		ch <- result{path, err}
	}()

	select {
	case <-ctx.Done():
		return "", fmt.Errorf("lookup %s: %w", name, ctx.Err())
	case r := <-ch:
		return r.path, r.err
	}
}

// CheckBinaries validates all required and optional binaries are available.
// Returns list of missing binaries with install hints and error details.
func CheckBinaries() []BinaryCheck {
	return CheckBinariesWithTimeout(DefaultLookPathTimeout)
}

// CheckBinariesWithTimeout validates all binaries with a custom timeout.
func CheckBinariesWithTimeout(timeout time.Duration) []BinaryCheck {
	var missing []BinaryCheck

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	allBinaries := append(requiredBinaries, optionalBinaries...)
	for _, bin := range allBinaries {
		if _, err := lookPathWithTimeout(ctx, bin.Name); err != nil {
			bin.Error = err
			missing = append(missing, bin)
		}
	}

	return missing
}

// CheckRequiredBinaries validates only required binaries are available.
// Returns list of missing required binaries with error details.
func CheckRequiredBinaries() []BinaryCheck {
	return CheckRequiredBinariesWithTimeout(DefaultLookPathTimeout)
}

// CheckRequiredBinariesWithTimeout validates required binaries with a custom timeout.
func CheckRequiredBinariesWithTimeout(timeout time.Duration) []BinaryCheck {
	var missing []BinaryCheck

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for _, bin := range requiredBinaries {
		if _, err := lookPathWithTimeout(ctx, bin.Name); err != nil {
			bin.Error = err
			missing = append(missing, bin)
		}
	}

	return missing
}

// CheckOptionalBinaries validates optional binaries and returns missing ones.
// Returns list of missing optional binaries with error details.
func CheckOptionalBinaries() []BinaryCheck {
	return CheckOptionalBinariesWithTimeout(DefaultLookPathTimeout)
}

// CheckOptionalBinariesWithTimeout validates optional binaries with a custom timeout.
func CheckOptionalBinariesWithTimeout(timeout time.Duration) []BinaryCheck {
	var missing []BinaryCheck

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for _, bin := range optionalBinaries {
		if _, err := lookPathWithTimeout(ctx, bin.Name); err != nil {
			bin.Error = err
			missing = append(missing, bin)
		}
	}

	return missing
}

// CheckAll performs all pre-flight checks and returns warnings and errors.
// Errors are for missing required binaries, warnings are for missing optional binaries.
// Error messages include the underlying error details.
func CheckAll() (warnings []string, errs []string) {
	return CheckAllWithTimeout(DefaultLookPathTimeout)
}

// CheckAllWithTimeout performs all pre-flight checks with a custom timeout.
func CheckAllWithTimeout(timeout time.Duration) (warnings []string, errs []string) {
	// Check required binaries
	missingRequired := CheckRequiredBinariesWithTimeout(timeout)
	for _, bin := range missingRequired {
		errMsg := bin.Name + ": " + bin.InstallHint
		if bin.Error != nil {
			errMsg += fmt.Sprintf(" (%v)", bin.Error)
		}
		errs = append(errs, errMsg)
	}

	// Check optional binaries
	missingOptional := CheckOptionalBinariesWithTimeout(timeout)
	for _, bin := range missingOptional {
		warnMsg := bin.Name + ": " + bin.InstallHint
		if bin.Error != nil {
			warnMsg += fmt.Sprintf(" (%v)", bin.Error)
		}
		warnings = append(warnings, warnMsg)
	}

	return warnings, errs
}

// IsBinaryAvailable checks if a specific binary is available in PATH.
// Returns false for empty binary names.
func IsBinaryAvailable(name string) bool {
	return IsBinaryAvailableWithTimeout(name, DefaultLookPathTimeout)
}

// IsBinaryAvailableWithTimeout checks if a specific binary is available with a custom timeout.
func IsBinaryAvailableWithTimeout(name string, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, err := lookPathWithTimeout(ctx, name)
	return err == nil
}

// GetAllBinaries returns all configured binaries (required and optional).
func GetAllBinaries() []BinaryCheck {
	return append(requiredBinaries, optionalBinaries...)
}

// GetRequiredBinaries returns only required binaries.
func GetRequiredBinaries() []BinaryCheck {
	return requiredBinaries
}

// GetOptionalBinaries returns only optional binaries.
func GetOptionalBinaries() []BinaryCheck {
	return optionalBinaries
}

// SSHKeyPermResult is the result of an SSH key permission check.
type SSHKeyPermResult struct {
	// Path is the key file that was checked (empty if no key file was found).
	Path string
	// Mode is the actual permission bits of the key file.
	Mode os.FileMode
	// Err is non-nil when the key file exists but has unsafe permissions.
	Err error
}

// sshKeyCandidates returns the ordered list of SSH key paths to check.
// Mirrors the resolution order used by the reconcile package's git.go so
// that preflight validates exactly the key that would be used at runtime.
func sshKeyCandidates() []string {
	candidates := []string{
		os.Getenv("BOSUN_SSH_KEY"),
		"/config/deploy-key",
		"/config/ssh-key",
	}
	if home := os.Getenv("HOME"); home != "" {
		candidates = append(candidates,
			filepath.Join(home, ".ssh", "id_ed25519"),
			filepath.Join(home, ".ssh", "id_rsa"),
		)
	}

	// Filter empty strings from unset env vars.
	var result []string
	for _, p := range candidates {
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// CheckSSHKeyPermissions validates that the first SSH key file found has
// safe permissions (0400 or 0600). Returns a result with an empty Path when
// no key file is found — that is not treated as an error because SSH auth
// may use an agent or an HTTPS URL.
func CheckSSHKeyPermissions() SSHKeyPermResult {
	for _, path := range sshKeyCandidates() {
		info, err := os.Stat(path)
		if err != nil {
			// File does not exist — try the next candidate.
			continue
		}

		mode := info.Mode().Perm()
		// Only 0400 (read-only owner) and 0600 (read-write owner) are safe.
		// SSH will refuse a key with group or world read bits set.
		if mode != 0400 && mode != 0600 {
			return SSHKeyPermResult{
				Path: path,
				Mode: mode,
				Err: fmt.Errorf(
					"SSH deploy key has unsafe permissions %04o (want 0400 or 0600): chmod 600 %s",
					mode, path,
				),
			}
		}

		return SSHKeyPermResult{Path: path, Mode: mode}
	}

	// No key file found — not an error here; runtime will use agent or HTTPS.
	return SSHKeyPermResult{}
}

// Package preflight provides pre-flight validation for required binaries and system checks.
package preflight

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// DefaultLookPathTimeout is the default timeout for exec.LookPath operations.
const DefaultLookPathTimeout = 5 * time.Second

// ErrEmptyBinaryName indicates an empty binary name was provided.
var ErrEmptyBinaryName = errors.New("empty binary name")

type pathLookup func(string) (string, error)
type contextPathLookup func(context.Context, string) (string, error)
type fileStat func(string) (os.FileInfo, error)

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
	return lookPathWith(ctx, name, exec.LookPath)
}

func lookPathWith(ctx context.Context, name string, lookup pathLookup) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", ErrEmptyBinaryName
	}

	type result struct {
		path string
		err  error
	}

	ch := make(chan result, 1)
	go func() {
		path, err := lookup(name)
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
	allBinaries := make([]BinaryCheck, 0, len(requiredBinaries)+len(optionalBinaries))
	allBinaries = append(allBinaries, requiredBinaries...)
	allBinaries = append(allBinaries, optionalBinaries...)
	return checkBinariesWithTimeout(allBinaries, timeout, lookPathWithTimeout)
}

// CheckRequiredBinaries validates only required binaries are available.
// Returns list of missing required binaries with error details.
func CheckRequiredBinaries() []BinaryCheck {
	return CheckRequiredBinariesWithTimeout(DefaultLookPathTimeout)
}

// CheckRequiredBinariesWithTimeout validates required binaries with a custom timeout.
func CheckRequiredBinariesWithTimeout(timeout time.Duration) []BinaryCheck {
	return checkBinariesWithTimeout(requiredBinaries, timeout, lookPathWithTimeout)
}

// CheckOptionalBinaries validates optional binaries and returns missing ones.
// Returns list of missing optional binaries with error details.
func CheckOptionalBinaries() []BinaryCheck {
	return CheckOptionalBinariesWithTimeout(DefaultLookPathTimeout)
}

// CheckOptionalBinariesWithTimeout validates optional binaries with a custom timeout.
func CheckOptionalBinariesWithTimeout(timeout time.Duration) []BinaryCheck {
	return checkBinariesWithTimeout(optionalBinaries, timeout, lookPathWithTimeout)
}

func checkBinariesWithTimeout(
	binaries []BinaryCheck,
	timeout time.Duration,
	lookup contextPathLookup,
) []BinaryCheck {
	var missing []BinaryCheck

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for _, bin := range binaries {
		if _, err := lookup(ctx, bin.Name); err != nil {
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
	return checkAllWithTimeout(timeout, lookPathWithTimeout)
}

func checkAllWithTimeout(timeout time.Duration, lookup contextPathLookup) (warnings []string, errs []string) {
	// Check required binaries
	missingRequired := checkBinariesWithTimeout(requiredBinaries, timeout, lookup)
	for _, bin := range missingRequired {
		errMsg := bin.Name + ": " + bin.InstallHint
		if bin.Error != nil {
			errMsg += fmt.Sprintf(" (%v)", bin.Error)
		}
		errs = append(errs, errMsg)
	}

	// Check optional binaries
	missingOptional := checkBinariesWithTimeout(optionalBinaries, timeout, lookup)
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
	// PermissionsChecked reports whether POSIX permission bits were validated.
	// Windows uses ACLs instead, which this check does not inspect.
	PermissionsChecked bool
	// Err is non-nil when the key path cannot be used safely as a key file.
	Err error
}

// sshKeyCandidates returns the ordered list of conventional SSH key paths to
// check. BOSUN_SSH_KEY is handled separately because reconcile treats an
// explicit path as authoritative rather than falling back when it is unusable.
func sshKeyCandidates(home string) []string {
	return []string{
		"/config/deploy-key",
		"/config/ssh-key",
		filepath.Join(home, ".ssh", "id_ed25519"),
		filepath.Join(home, ".ssh", "id_rsa"),
	}
}

// CheckSSHKeyPermissions validates the explicit SSH key or the first usable
// conventional candidate. The key must be a non-empty regular file and, on
// POSIX systems, have safe permissions (0400 or 0600). An empty Path means no
// candidate was found; SSH auth may instead use an agent or an HTTPS URL.
func CheckSSHKeyPermissions() SSHKeyPermResult {
	return checkSSHKeyPermissions(
		os.Getenv("BOSUN_SSH_KEY"),
		sshKeyCandidates(os.Getenv("HOME")),
		runtime.GOOS,
		os.Stat,
	)
}

func checkSSHKeyPermissions(explicitPath string, candidates []string, goos string, stat fileStat) SSHKeyPermResult {
	if explicitPath != "" {
		return inspectSSHKey(explicitPath, goos, stat)
	}

	var firstUnusable SSHKeyPermResult
	for _, path := range candidates {
		result := inspectSSHKey(path, goos, stat)
		if errors.Is(result.Err, os.ErrNotExist) {
			continue
		}
		if result.Err != nil && !result.PermissionsChecked {
			if firstUnusable.Path == "" {
				firstUnusable = result
			}
			continue
		}
		return result
	}

	// Like reconcile, report the first unusable conventional candidate only if
	// no later candidate can be used. Otherwise runtime can use an agent or HTTPS.
	return firstUnusable
}

func inspectSSHKey(path, goos string, stat fileStat) SSHKeyPermResult {
	info, err := stat(path)
	if err != nil {
		return SSHKeyPermResult{Path: path, Err: fmt.Errorf("reading SSH key metadata: %w", err)}
	}

	mode := info.Mode().Perm()
	if !info.Mode().IsRegular() {
		return SSHKeyPermResult{
			Path: path,
			Mode: mode,
			Err:  fmt.Errorf("SSH deploy key is not a regular file: %s", path),
		}
	}
	if info.Size() == 0 {
		return SSHKeyPermResult{
			Path: path,
			Mode: mode,
			Err:  fmt.Errorf("SSH deploy key is empty: %s", path),
		}
	}

	// Windows protects private keys with ACLs rather than POSIX mode bits.
	// The regular/non-empty checks above still catch paths runtime will reject.
	if goos == "windows" {
		return SSHKeyPermResult{Path: path, Mode: mode}
	}

	// Bosun requires private keys to be owner-readable without group or world
	// access. Keep the accepted modes intentionally narrow and actionable.
	if mode != 0400 && mode != 0600 {
		return SSHKeyPermResult{
			Path:               path,
			Mode:               mode,
			PermissionsChecked: true,
			Err: fmt.Errorf(
				"SSH deploy key has unsafe permissions %04o (want 0400 or 0600): chmod 600 %s",
				mode, path,
			),
		}
	}

	return SSHKeyPermResult{Path: path, Mode: mode, PermissionsChecked: true}
}

// Package preflight provides pre-flight validation for required binaries and system checks.
package preflight

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
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

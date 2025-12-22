// Package preflight provides pre-flight validation for required binaries and system checks.
package preflight

import (
	"os/exec"
)

// BinaryCheck represents a required binary and its purpose.
type BinaryCheck struct {
	Name        string
	Required    bool   // false = warning only
	InstallHint string // e.g., "brew install sops" or "https://..."
}

// requiredBinaries defines binaries that must be present for bosun to function.
var requiredBinaries = []BinaryCheck{
	{
		Name:        "docker",
		Required:    true,
		InstallHint: "Install Docker: https://docs.docker.com/get-docker/",
	},
	{
		Name:        "git",
		Required:    true,
		InstallHint: "Install git: https://git-scm.com/downloads",
	},
}

// optionalBinaries defines binaries that enhance bosun functionality but are not strictly required.
var optionalBinaries = []BinaryCheck{
	{
		Name:        "sops",
		Required:    false,
		InstallHint: "Install sops: brew install sops",
	},
	{
		Name:        "age",
		Required:    false,
		InstallHint: "Install age: brew install age",
	},
	{
		Name:        "chezmoi",
		Required:    false,
		InstallHint: "Install chezmoi: brew install chezmoi",
	},
	{
		Name:        "rsync",
		Required:    false,
		InstallHint: "Install rsync: brew install rsync",
	},
}

// CheckBinaries validates all required and optional binaries are available.
// Returns list of missing binaries with install hints.
func CheckBinaries() []BinaryCheck {
	var missing []BinaryCheck

	allBinaries := append(requiredBinaries, optionalBinaries...)
	for _, bin := range allBinaries {
		if _, err := exec.LookPath(bin.Name); err != nil {
			missing = append(missing, bin)
		}
	}

	return missing
}

// CheckRequiredBinaries validates only required binaries are available.
// Returns list of missing required binaries.
func CheckRequiredBinaries() []BinaryCheck {
	var missing []BinaryCheck

	for _, bin := range requiredBinaries {
		if _, err := exec.LookPath(bin.Name); err != nil {
			missing = append(missing, bin)
		}
	}

	return missing
}

// CheckOptionalBinaries validates optional binaries and returns missing ones.
// Returns list of missing optional binaries as warnings.
func CheckOptionalBinaries() []BinaryCheck {
	var missing []BinaryCheck

	for _, bin := range optionalBinaries {
		if _, err := exec.LookPath(bin.Name); err != nil {
			missing = append(missing, bin)
		}
	}

	return missing
}

// CheckAll performs all pre-flight checks and returns warnings and errors.
// Errors are for missing required binaries, warnings are for missing optional binaries.
func CheckAll() (warnings []string, errors []string) {
	// Check required binaries
	missingRequired := CheckRequiredBinaries()
	for _, bin := range missingRequired {
		errors = append(errors, bin.Name+": "+bin.InstallHint)
	}

	// Check optional binaries
	missingOptional := CheckOptionalBinaries()
	for _, bin := range missingOptional {
		warnings = append(warnings, bin.Name+": "+bin.InstallHint)
	}

	return warnings, errors
}

// IsBinaryAvailable checks if a specific binary is available in PATH.
func IsBinaryAvailable(name string) bool {
	_, err := exec.LookPath(name)
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

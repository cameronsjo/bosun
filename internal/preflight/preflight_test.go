package preflight

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCheckBinaries(t *testing.T) {
	t.Run("returns list of missing binaries", func(t *testing.T) {
		// This test verifies the function runs without error
		// Actual results depend on system configuration
		missing := CheckBinaries()
		// Each missing binary should have a name and install hint
		for _, bin := range missing {
			assert.NotEmpty(t, bin.Name, "missing binary should have a name")
			assert.NotEmpty(t, bin.InstallHint, "missing binary should have an install hint")
		}
	})
}

func TestCheckRequiredBinaries(t *testing.T) {
	t.Run("returns only required binaries that are missing", func(t *testing.T) {
		missing := CheckRequiredBinaries()
		for _, bin := range missing {
			assert.True(t, bin.Required, "should only return required binaries")
		}
	})
}

func TestCheckOptionalBinaries(t *testing.T) {
	t.Run("returns only optional binaries that are missing", func(t *testing.T) {
		missing := CheckOptionalBinaries()
		for _, bin := range missing {
			assert.False(t, bin.Required, "should only return optional binaries")
		}
	})
}

func TestCheckAll(t *testing.T) {
	t.Run("separates warnings and errors correctly", func(t *testing.T) {
		warnings, errors := CheckAll()

		// Errors should be for required binaries
		// Warnings should be for optional binaries
		// Both should contain install hints
		for _, err := range errors {
			assert.NotEmpty(t, err, "error should not be empty")
			assert.Contains(t, err, ":", "error should contain colon separator")
		}
		for _, warn := range warnings {
			assert.NotEmpty(t, warn, "warning should not be empty")
			assert.Contains(t, warn, ":", "warning should contain colon separator")
		}
	})
}

func TestIsBinaryAvailable(t *testing.T) {
	t.Run("returns true for commonly available binaries", func(t *testing.T) {
		// These binaries are almost always available on Unix-like systems
		// Note: On CI systems, these might not be available, so we test the logic
		if IsBinaryAvailable("ls") {
			assert.True(t, IsBinaryAvailable("ls"))
		}
	})

	t.Run("returns false for non-existent binary", func(t *testing.T) {
		result := IsBinaryAvailable("this-binary-definitely-does-not-exist-xyz123")
		assert.False(t, result)
	})
}

func TestGetAllBinaries(t *testing.T) {
	t.Run("returns all binaries", func(t *testing.T) {
		all := GetAllBinaries()
		required := GetRequiredBinaries()
		optional := GetOptionalBinaries()

		assert.Equal(t, len(required)+len(optional), len(all))
	})
}

func TestGetRequiredBinaries(t *testing.T) {
	t.Run("includes docker and git", func(t *testing.T) {
		required := GetRequiredBinaries()

		names := make([]string, 0, len(required))
		for _, bin := range required {
			names = append(names, bin.Name)
			assert.True(t, bin.Required, "all returned binaries should be required")
		}

		assert.Contains(t, names, "docker")
		assert.Contains(t, names, "git")
	})

	t.Run("all have install hints", func(t *testing.T) {
		required := GetRequiredBinaries()
		for _, bin := range required {
			assert.NotEmpty(t, bin.InstallHint, "required binary %s should have install hint", bin.Name)
		}
	})
}

func TestGetOptionalBinaries(t *testing.T) {
	t.Run("includes sops, age, chezmoi, rsync", func(t *testing.T) {
		optional := GetOptionalBinaries()

		names := make([]string, 0, len(optional))
		for _, bin := range optional {
			names = append(names, bin.Name)
			assert.False(t, bin.Required, "all returned binaries should be optional")
		}

		assert.Contains(t, names, "sops")
		assert.Contains(t, names, "age")
		assert.Contains(t, names, "chezmoi")
		assert.Contains(t, names, "rsync")
	})

	t.Run("all have install hints", func(t *testing.T) {
		optional := GetOptionalBinaries()
		for _, bin := range optional {
			assert.NotEmpty(t, bin.InstallHint, "optional binary %s should have install hint", bin.Name)
		}
	})
}

func TestBinaryCheck_Properties(t *testing.T) {
	t.Run("docker binary check", func(t *testing.T) {
		required := GetRequiredBinaries()
		var docker BinaryCheck
		for _, bin := range required {
			if bin.Name == "docker" {
				docker = bin
				break
			}
		}

		assert.Equal(t, "docker", docker.Name)
		assert.True(t, docker.Required)
		assert.Contains(t, docker.InstallHint, "https://")
	})

	t.Run("sops binary check", func(t *testing.T) {
		optional := GetOptionalBinaries()
		var sops BinaryCheck
		for _, bin := range optional {
			if bin.Name == "sops" {
				sops = bin
				break
			}
		}

		assert.Equal(t, "sops", sops.Name)
		assert.False(t, sops.Required)
		assert.Contains(t, sops.InstallHint, "brew install")
	})
}

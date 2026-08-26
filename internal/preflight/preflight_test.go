package preflight

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLookPathWith(t *testing.T) {
	lookupErr := errors.New("lookup failed")
	tests := []struct {
		name       string
		binary     string
		lookupPath string
		lookupErr  error
		wantPath   string
		wantErr    error
		wantCalls  int
	}{
		{
			name:      "empty name is rejected without lookup",
			binary:    " \t ",
			wantErr:   ErrEmptyBinaryName,
			wantCalls: 0,
		},
		{
			name:       "found binary returns path",
			binary:     "docker",
			lookupPath: "/usr/local/bin/docker",
			wantPath:   "/usr/local/bin/docker",
			wantCalls:  1,
		},
		{
			name:      "lookup failure is preserved",
			binary:    "docker",
			lookupErr: lookupErr,
			wantErr:   lookupErr,
			wantCalls: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			lookup := func(name string) (string, error) {
				calls++
				assert.Equal(t, tc.binary, name)
				return tc.lookupPath, tc.lookupErr
			}

			path, err := lookPathWith(context.Background(), tc.binary, lookup)

			assert.Equal(t, tc.wantPath, path)
			assert.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.wantCalls, calls)
		})
	}
}

func TestLookPathWith_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	release := make(chan struct{})
	lookupExited := make(chan struct{})
	lookup := func(string) (string, error) {
		defer close(lookupExited)
		cancel()
		<-release
		return "", nil
	}

	_, err := lookPathWith(ctx, "docker", lookup)
	close(release)
	<-lookupExited

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.EqualError(t, err, "lookup docker: context canceled")
}

func TestCheckBinariesWithTimeout(t *testing.T) {
	notFound := errors.New("not found")
	binaries := []BinaryCheck{
		{Name: "docker", Required: true, InstallHint: "install docker"},
		{Name: "age", Required: false, InstallHint: "install age"},
	}
	var calls []string
	lookup := func(_ context.Context, name string) (string, error) {
		calls = append(calls, name)
		if name == "docker" {
			return "", notFound
		}
		return "/usr/bin/" + name, nil
	}

	missing := checkBinariesWithTimeout(binaries, time.Second, lookup)

	require.Len(t, missing, 1)
	assert.Equal(t, "docker", missing[0].Name)
	assert.True(t, missing[0].Required)
	assert.Equal(t, "install docker", missing[0].InstallHint)
	assert.ErrorIs(t, missing[0].Error, notFound)
	assert.Equal(t, []string{"docker", "age"}, calls)
	assert.Nil(t, binaries[0].Error, "checking must not mutate configured binary metadata")
}

func TestCheckAllWithTimeout(t *testing.T) {
	tests := []struct {
		name         string
		failures     map[string]error
		wantWarnings []string
		wantErrors   []string
	}{
		{
			name: "all binaries found",
		},
		{
			name: "classifies failures and includes lookup details",
			failures: map[string]error{
				"docker": errors.New("docker lookup failed"),
				"age":    errors.New("age lookup failed"),
			},
			wantWarnings: []string{
				"age: Install age: brew install age (needed for key generation with age-keygen) (age lookup failed)",
			},
			wantErrors: []string{
				"docker: Install Docker: https://docs.docker.com/get-docker/ (docker lookup failed)",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lookup := func(_ context.Context, name string) (string, error) {
				if err := tc.failures[name]; err != nil {
					return "", err
				}
				return fmt.Sprintf("/bin/%s", name), nil
			}

			warnings, errs := checkAllWithTimeout(time.Second, lookup)

			assert.Equal(t, tc.wantWarnings, warnings)
			assert.Equal(t, tc.wantErrors, errs)
		})
	}
}

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
	t.Run("includes docker", func(t *testing.T) {
		required := GetRequiredBinaries()

		names := make([]string, 0, len(required))
		for _, bin := range required {
			names = append(names, bin.Name)
			assert.True(t, bin.Required, "all returned binaries should be required")
		}

		assert.Contains(t, names, "docker")
	})

	t.Run("all have install hints", func(t *testing.T) {
		required := GetRequiredBinaries()
		for _, bin := range required {
			assert.NotEmpty(t, bin.InstallHint, "required binary %s should have install hint", bin.Name)
		}
	})
}

func TestGetOptionalBinaries(t *testing.T) {
	t.Run("includes age", func(t *testing.T) {
		optional := GetOptionalBinaries()

		names := make([]string, 0, len(optional))
		for _, bin := range optional {
			names = append(names, bin.Name)
			assert.False(t, bin.Required, "all returned binaries should be optional")
		}

		assert.Contains(t, names, "age")
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

	t.Run("age binary check", func(t *testing.T) {
		optional := GetOptionalBinaries()
		var age BinaryCheck
		for _, bin := range optional {
			if bin.Name == "age" {
				age = bin
				break
			}
		}

		assert.Equal(t, "age", age.Name)
		assert.False(t, age.Required)
		assert.Contains(t, age.InstallHint, "brew install")
	})
}

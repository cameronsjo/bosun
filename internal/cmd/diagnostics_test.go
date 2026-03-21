package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/config"
)

func TestStatusCmd_Help(t *testing.T) {
	t.Run("status --help", func(t *testing.T) {
		output, err := executeCmd(t, "status", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "status")
	})
}

func TestStatusCmd_Aliases(t *testing.T) {
	t.Run("bridge alias", func(t *testing.T) {
		_, err := executeCmd(t, "bridge", "--help")
		assert.NoError(t, err)
	})
}

func TestLogCmd_Help(t *testing.T) {
	t.Run("log --help", func(t *testing.T) {
		output, err := executeCmd(t, "log", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "manifest")
	})
}

func TestLogCmd_Aliases(t *testing.T) {
	t.Run("ledger alias", func(t *testing.T) {
		_, err := executeCmd(t, "ledger", "--help")
		assert.NoError(t, err)
	})
}

func TestDriftCmd_Help(t *testing.T) {
	t.Run("drift --help", func(t *testing.T) {
		output, err := executeCmd(t, "drift", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "declared")
		assert.Contains(t, output, "containers")
	})
}

func TestFormatBytes(t *testing.T) {
	testCases := []struct {
		name     string
		bytes    int64
		expected string
	}{
		{"negative_small", -1, "N/A"},
		{"negative_large", -12345, "N/A"},
		{"zero", 0, "0 B"},
		{"512_bytes", 512, "512 B"},
		{"1_kb", 1024, "1.0 KB"},
		{"1.5_kb", 1536, "1.5 KB"},
		{"1_mb", 1048576, "1.0 MB"},
		{"1_gb", 1073741824, "1.0 GB"},
		{"1_tb", 1099511627776, "1.0 TB"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := formatBytes(tc.bytes)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestDefaultInfraContainers(t *testing.T) {
	// Test that config package's default infra containers include expected values
	// Load config from a directory without config file to get defaults
	tmpDir := t.TempDir()

	// Create manifest directory to enable config loading
	manifestDir := filepath.Join(tmpDir, "manifest")
	require.NoError(t, os.MkdirAll(manifestDir, 0755))

	// Create bosun directory with docker-compose.yml
	bosunDir := filepath.Join(tmpDir, "bosun")
	require.NoError(t, os.MkdirAll(bosunDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(bosunDir, "docker-compose.yml"), []byte("version: '3'"), 0644))

	// Change to project root
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(originalWd) }()

	require.NoError(t, os.Chdir(tmpDir))

	cfg, err := config.Load()
	require.NoError(t, err)

	containers := cfg.InfraContainers()
	assert.Contains(t, containers, "traefik")
	assert.Contains(t, containers, "authelia")
	assert.Contains(t, containers, "gatus")
}

func TestCheckResult_Add(t *testing.T) {
	t.Run("add two results", func(t *testing.T) {
		r1 := CheckResult{Passed: 2, Failed: 1, Warned: 3}
		r2 := CheckResult{Passed: 1, Failed: 2, Warned: 1}

		r1.Add(r2)

		assert.Equal(t, 3, r1.Passed)
		assert.Equal(t, 3, r1.Failed)
		assert.Equal(t, 4, r1.Warned)
	})

	t.Run("add empty result", func(t *testing.T) {
		r1 := CheckResult{Passed: 2, Failed: 1, Warned: 3}
		r2 := CheckResult{}

		r1.Add(r2)

		assert.Equal(t, 2, r1.Passed)
		assert.Equal(t, 1, r1.Failed)
		assert.Equal(t, 3, r1.Warned)
	})

	t.Run("add to empty result", func(t *testing.T) {
		r1 := CheckResult{}
		r2 := CheckResult{Passed: 2, Failed: 1, Warned: 3}

		r1.Add(r2)

		assert.Equal(t, 2, r1.Passed)
		assert.Equal(t, 1, r1.Failed)
		assert.Equal(t, 3, r1.Warned)
	})
}

func TestShowProvisionTimestamps(t *testing.T) {
	t.Run("lists yml files with timestamps", func(t *testing.T) {
		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")
		require.NoError(t, os.MkdirAll(outputDir, 0755))

		require.NoError(t, os.WriteFile(filepath.Join(outputDir, "test.yml"), []byte("test"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(outputDir, "test2.yml"), []byte("test2"), 0644))

		// Should not panic -- output goes to stdout.
		showProvisionTimestamps(outputDir, tmpDir)
	})

	t.Run("handles empty directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")
		require.NoError(t, os.MkdirAll(outputDir, 0755))

		showProvisionTimestamps(outputDir, tmpDir)
	})

	t.Run("handles non-existent directory", func(t *testing.T) {
		showProvisionTimestamps("/nonexistent/dir", "/tmp")
	})
}

// TestFormatBytes_AdditionalCases tests additional edge cases for formatBytes.
func TestFormatBytes_AdditionalCases(t *testing.T) {
	testCases := []struct {
		name     string
		bytes    int64
		expected string
	}{
		{
			name:     "max int64",
			bytes:    9223372036854775807,
			expected: "8.0 EB",
		},
		{
			name:     "1023 bytes (just under 1KB)",
			bytes:    1023,
			expected: "1023 B",
		},
		{
			name:     "1025 bytes (just over 1KB)",
			bytes:    1025,
			expected: "1.0 KB",
		},
		{
			name:     "petabyte",
			bytes:    1125899906842624,
			expected: "1.0 PB",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := formatBytes(tc.bytes)
			assert.Equal(t, tc.expected, result)
		})
	}
}


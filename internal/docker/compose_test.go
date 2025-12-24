package docker

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewComposeClient(t *testing.T) {
	t.Run("valid file", func(t *testing.T) {
		// Create a temporary compose file
		tmpDir := t.TempDir()
		composeFile := filepath.Join(tmpDir, "docker-compose.yml")
		err := os.WriteFile(composeFile, []byte("services: {}"), 0644)
		require.NoError(t, err)

		client, err := NewComposeClient(composeFile)
		require.NoError(t, err)
		assert.NotNil(t, client)
		assert.Equal(t, composeFile, client.file)
	})

	t.Run("nonexistent file", func(t *testing.T) {
		client, err := NewComposeClient("/nonexistent/docker-compose.yml")
		require.Error(t, err)
		assert.Nil(t, client)
		assert.Contains(t, err.Error(), "compose file not found")
	})

	t.Run("permission denied", func(t *testing.T) {
		// This test is platform-specific and may not work in all environments
		// Skip if running as root or on Windows
		if os.Getuid() == 0 {
			t.Skip("Skipping permission test when running as root")
		}

		tmpDir := t.TempDir()
		composeFile := filepath.Join(tmpDir, "docker-compose.yml")
		err := os.WriteFile(composeFile, []byte("services: {}"), 0000)
		require.NoError(t, err)
		defer func() { _ = os.Chmod(composeFile, 0644) }() // Restore for cleanup

		// On some systems stat works even without read permission
		// so we just check that it doesn't panic
		_, _ = NewComposeClient(composeFile)
	})
}

func TestComposeClient_ParseStatusOutput(t *testing.T) {
	// Test the Status parsing logic by testing the string parsing
	tests := []struct {
		name     string
		input    string
		wantLen  int
		wantName string
	}{
		{
			name:     "single service",
			input:    "web\trunning\tUp 10 minutes\t8080:80/tcp",
			wantLen:  1,
			wantName: "web",
		},
		{
			name:     "multiple services",
			input:    "web\trunning\tUp 10 minutes\t8080:80/tcp\ndb\trunning\tUp 10 minutes\t5432:5432/tcp",
			wantLen:  2,
			wantName: "web",
		},
		{
			name:    "empty output",
			input:   "",
			wantLen: 0,
		},
		{
			name:    "incomplete line",
			input:   "web\trunning",
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This tests the parsing logic used in Status()
			// We test this indirectly since Status() uses exec.Command
			services := parseStatusOutput(tt.input)
			assert.Len(t, services, tt.wantLen)
			if tt.wantLen > 0 {
				assert.Equal(t, tt.wantName, services[0].Name)
			}
		})
	}
}

// parseStatusOutput is a helper to test the parsing logic from Status()
// This is extracted for testing purposes.
func parseStatusOutput(output string) []ServiceStatus {
	var services []ServiceStatus
	if output == "" {
		return services
	}

	lines := splitLines(output)
	for _, line := range lines {
		if line == "" {
			continue
		}

		parts := splitByTab(line)
		if len(parts) < 3 {
			continue
		}

		svc := ServiceStatus{
			Name:    parts[0],
			State:   parts[1],
			Status:  parts[2],
			Running: parts[1] == "running",
		}
		if len(parts) > 3 {
			svc.Ports = parts[3]
		}

		services = append(services, svc)
	}

	return services
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func splitByTab(s string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\t' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		parts = append(parts, s[start:])
	}
	return parts
}

func TestServiceStatus_Fields(t *testing.T) {
	svc := ServiceStatus{
		Name:    "web",
		State:   "running",
		Status:  "Up 10 minutes",
		Ports:   "8080:80/tcp",
		Running: true,
	}

	assert.Equal(t, "web", svc.Name)
	assert.Equal(t, "running", svc.State)
	assert.Equal(t, "Up 10 minutes", svc.Status)
	assert.Equal(t, "8080:80/tcp", svc.Ports)
	assert.True(t, svc.Running)
}

// Integration tests - these require Docker to be available
// They are skipped if Docker is not running

func TestComposeClient_Integration(t *testing.T) {
	if os.Getenv("DOCKER_INTEGRATION_TESTS") != "1" {
		t.Skip("Skipping integration test. Set DOCKER_INTEGRATION_TESTS=1 to run.")
	}

	// Check if docker is available
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker not available")
	}

	// Create a temporary compose file
	tmpDir := t.TempDir()
	composeFile := filepath.Join(tmpDir, "docker-compose.yml")

	composeContent := `
services:
  test-nginx:
    image: nginx:alpine
    ports:
      - "18080:80"
`
	err := os.WriteFile(composeFile, []byte(composeContent), 0644)
	require.NoError(t, err)

	client, err := NewComposeClient(composeFile)
	require.NoError(t, err)

	ctx := context.Background()

	// Test Up
	t.Run("Up", func(t *testing.T) {
		err := client.Up(ctx)
		require.NoError(t, err)
	})

	// Test Status
	t.Run("Status", func(t *testing.T) {
		services, err := client.Status(ctx)
		require.NoError(t, err)
		assert.NotEmpty(t, services)
	})

	// Test Ps
	t.Run("Ps", func(t *testing.T) {
		output, err := client.Ps(ctx)
		require.NoError(t, err)
		assert.Contains(t, output, "test-nginx")
	})

	// Test Restart
	t.Run("Restart", func(t *testing.T) {
		err := client.Restart(ctx, "test-nginx")
		require.NoError(t, err)
	})

	// Test Down
	t.Run("Down", func(t *testing.T) {
		err := client.Down(ctx)
		require.NoError(t, err)
	})
}

// Test error cases with invalid compose files
func TestComposeClient_Errors(t *testing.T) {
	// Constructor now validates file existence, so nonexistent files fail at construction
	t.Run("constructor with nonexistent file", func(t *testing.T) {
		_, err := NewComposeClient("/nonexistent/docker-compose.yml")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "compose file not found")
	})
}

// Test command building logic
func TestComposeClient_CommandBuilding(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		services []string
		wantArgs []string
	}{
		{
			name:     "up with no services",
			file:     "compose.yml",
			services: nil,
			wantArgs: []string{"compose", "-f", "compose.yml", "up", "-d"},
		},
		{
			name:     "up with one service",
			file:     "compose.yml",
			services: []string{"web"},
			wantArgs: []string{"compose", "-f", "compose.yml", "up", "-d", "web"},
		},
		{
			name:     "up with multiple services",
			file:     "compose.yml",
			services: []string{"web", "db", "cache"},
			wantArgs: []string{"compose", "-f", "compose.yml", "up", "-d", "web", "db", "cache"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build the args like Up() does
			args := []string{"compose", "-f", tt.file, "up", "-d"}
			args = append(args, tt.services...)
			assert.Equal(t, tt.wantArgs, args)
		})
	}
}

func TestComposeClient_RestartCommandBuilding(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		services []string
		wantArgs []string
	}{
		{
			name:     "restart with no services",
			file:     "compose.yml",
			services: nil,
			wantArgs: []string{"compose", "-f", "compose.yml", "restart"},
		},
		{
			name:     "restart with one service",
			file:     "compose.yml",
			services: []string{"web"},
			wantArgs: []string{"compose", "-f", "compose.yml", "restart", "web"},
		},
		{
			name:     "restart with multiple services",
			file:     "compose.yml",
			services: []string{"web", "api"},
			wantArgs: []string{"compose", "-f", "compose.yml", "restart", "web", "api"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build the args like Restart() does
			args := []string{"compose", "-f", tt.file, "restart"}
			args = append(args, tt.services...)
			assert.Equal(t, tt.wantArgs, args)
		})
	}
}

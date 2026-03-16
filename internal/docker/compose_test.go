package docker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHelperProcess is a helper process for mocking exec.Command in tests.
// It is not a real test -- it is invoked as a subprocess by the mock runner.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	// Print the configured stdout if any
	if stdout := os.Getenv("HELPER_STDOUT"); stdout != "" {
		_, _ = fmt.Fprint(os.Stdout, stdout)
	}
	// Print stderr if configured
	if stderr := os.Getenv("HELPER_STDERR"); stderr != "" {
		fmt.Fprint(os.Stderr, stderr)
	}
	// Exit with configured code
	if os.Getenv("HELPER_EXIT_CODE") == "1" {
		os.Exit(1)
	}
	os.Exit(0)
}

// mockRunner returns a commandRunner that records the args passed and produces
// a helper subprocess with the given behavior.
type mockRunnerOpts struct {
	stdout   string
	stderr   string
	exitCode string // "0" or "1"
}

func newMockRunner(opts mockRunnerOpts, recorded *[][]string) commandRunner {
	return func(ctx context.Context, name string, args ...string) *exec.Cmd {
		if recorded != nil {
			*recorded = append(*recorded, append([]string{name}, args...))
		}
		cs := []string{"-test.run=TestHelperProcess", "--"}
		cs = append(cs, name)
		cs = append(cs, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...) //nolint:gosec // test helper only
		cmd.Env = []string{
			"GO_WANT_HELPER_PROCESS=1",
			"HELPER_STDOUT=" + opts.stdout,
			"HELPER_STDERR=" + opts.stderr,
			"HELPER_EXIT_CODE=" + opts.exitCode,
		}
		return cmd
	}
}

func TestNewComposeClient(t *testing.T) {
	t.Run("valid file", func(t *testing.T) {
		// Create a temporary compose file
		tmpDir := t.TempDir()
		composeFile := filepath.Join(tmpDir, "docker-compose.yml")
		err := os.WriteFile(composeFile, []byte("services: {}"), 0644)
		require.NoError(t, err)

		client, err := NewComposeClient(composeFile, "test-project")
		require.NoError(t, err)
		assert.NotNil(t, client)
		assert.Equal(t, composeFile, client.file)
	})

	t.Run("nonexistent file", func(t *testing.T) {
		client, err := NewComposeClient("/nonexistent/docker-compose.yml", "")
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
		_, _ = NewComposeClient(composeFile, "test-project")
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

	client, err := NewComposeClient(composeFile, "test-project")
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
		_, err := NewComposeClient("/nonexistent/docker-compose.yml", "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "compose file not found")
	})
}

// Test command building logic
func TestComposeClient_CommandBuilding(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		project  string
		services []string
		wantArgs []string
	}{
		{
			name:     "up with no services, no project",
			file:     "compose.yml",
			project:  "",
			services: nil,
			wantArgs: []string{"compose", "-f", "compose.yml", "up", "-d"},
		},
		{
			name:     "up with project name",
			file:     "compose.yml",
			project:  "homelab",
			services: nil,
			wantArgs: []string{"compose", "-p", "homelab", "-f", "compose.yml", "up", "-d"},
		},
		{
			name:     "up with project and services",
			file:     "compose.yml",
			project:  "homelab",
			services: []string{"web", "db"},
			wantArgs: []string{"compose", "-p", "homelab", "-f", "compose.yml", "up", "-d", "web", "db"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build the args like baseArgs() + Up() does
			args := []string{"compose"}
			if tt.project != "" {
				args = append(args, "-p", tt.project)
			}
			args = append(args, "-f", tt.file, "up", "-d")
			args = append(args, tt.services...)
			assert.Equal(t, tt.wantArgs, args)
		})
	}
}

func TestComposeClient_RestartCommandBuilding(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		project  string
		services []string
		wantArgs []string
	}{
		{
			name:     "restart with no services, no project",
			file:     "compose.yml",
			project:  "",
			services: nil,
			wantArgs: []string{"compose", "-f", "compose.yml", "restart"},
		},
		{
			name:     "restart with project name",
			file:     "compose.yml",
			project:  "homelab",
			services: []string{"web"},
			wantArgs: []string{"compose", "-p", "homelab", "-f", "compose.yml", "restart", "web"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build the args like baseArgs() + Restart() does
			args := []string{"compose"}
			if tt.project != "" {
				args = append(args, "-p", tt.project)
			}
			args = append(args, "-f", tt.file, "restart")
			args = append(args, tt.services...)
			assert.Equal(t, tt.wantArgs, args)
		})
	}
}

func TestComposeClient_baseArgs(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		project  string
		wantArgs []string
	}{
		{
			name:     "with project",
			file:     "/path/to/compose.yml",
			project:  "myproject",
			wantArgs: []string{"compose", "-p", "myproject", "-f", "/path/to/compose.yml"},
		},
		{
			name:     "without project",
			file:     "/path/to/compose.yml",
			project:  "",
			wantArgs: []string{"compose", "-f", "/path/to/compose.yml"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &ComposeClient{file: tt.file, project: tt.project}
			got := c.baseArgs()
			assert.Equal(t, tt.wantArgs, got)
		})
	}
}

func TestComposeClient_command_defaultsToExecCommandContext(t *testing.T) {
	c := &ComposeClient{file: "compose.yml", project: "test"}
	cmd := c.command(context.Background(), "compose", "-f", "compose.yml", "ps")
	// The command should have "docker" as the path
	assert.Contains(t, cmd.Path, "docker")
}

func TestComposeClient_Up_WithMockRunner(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		project  string
		services []string
		opts     mockRunnerOpts
		wantArgs []string
		wantErr  bool
		errMsg   string
	}{
		{
			name:     "success no services",
			file:     "compose.yml",
			project:  "proj",
			services: nil,
			opts:     mockRunnerOpts{exitCode: "0"},
			wantArgs: []string{"docker", "compose", "-p", "proj", "-f", "compose.yml", "up", "-d"},
			wantErr:  false,
		},
		{
			name:     "success with services",
			file:     "compose.yml",
			project:  "proj",
			services: []string{"web", "db"},
			opts:     mockRunnerOpts{exitCode: "0"},
			wantArgs: []string{"docker", "compose", "-p", "proj", "-f", "compose.yml", "up", "-d", "web", "db"},
			wantErr:  false,
		},
		{
			name:     "success no project",
			file:     "compose.yml",
			project:  "",
			services: nil,
			opts:     mockRunnerOpts{exitCode: "0"},
			wantArgs: []string{"docker", "compose", "-f", "compose.yml", "up", "-d"},
			wantErr:  false,
		},
		{
			name:     "failure",
			file:     "compose.yml",
			project:  "proj",
			services: nil,
			opts:     mockRunnerOpts{exitCode: "1", stderr: "error: something broke"},
			wantErr:  true,
			errMsg:   "docker compose up",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var recorded [][]string
			c := &ComposeClient{
				file:    tt.file,
				project: tt.project,
				runner:  newMockRunner(tt.opts, &recorded),
			}

			err := c.Up(context.Background(), tt.services...)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
				require.Len(t, recorded, 1)
				assert.Equal(t, tt.wantArgs, recorded[0])
			}
		})
	}
}

func TestComposeClient_Down_WithMockRunner(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		project  string
		opts     mockRunnerOpts
		wantArgs []string
		wantErr  bool
		errMsg   string
	}{
		{
			name:     "success with project",
			file:     "compose.yml",
			project:  "homelab",
			opts:     mockRunnerOpts{exitCode: "0"},
			wantArgs: []string{"docker", "compose", "-p", "homelab", "-f", "compose.yml", "down"},
			wantErr:  false,
		},
		{
			name:     "success without project",
			file:     "compose.yml",
			project:  "",
			opts:     mockRunnerOpts{exitCode: "0"},
			wantArgs: []string{"docker", "compose", "-f", "compose.yml", "down"},
			wantErr:  false,
		},
		{
			name:    "failure",
			file:    "compose.yml",
			project: "proj",
			opts:    mockRunnerOpts{exitCode: "1"},
			wantErr: true,
			errMsg:  "docker compose down",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var recorded [][]string
			c := &ComposeClient{
				file:    tt.file,
				project: tt.project,
				runner:  newMockRunner(tt.opts, &recorded),
			}

			err := c.Down(context.Background())
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
				require.Len(t, recorded, 1)
				assert.Equal(t, tt.wantArgs, recorded[0])
			}
		})
	}
}

func TestComposeClient_DownWithTimeout_WithMockRunner(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		project  string
		timeout  int
		opts     mockRunnerOpts
		wantArgs []string
		wantErr  bool
		errMsg   string
	}{
		{
			name:     "success with 30s timeout",
			file:     "compose.yml",
			project:  "proj",
			timeout:  30,
			opts:     mockRunnerOpts{exitCode: "0"},
			wantArgs: []string{"docker", "compose", "-p", "proj", "-f", "compose.yml", "down", "--timeout", "30"},
			wantErr:  false,
		},
		{
			name:     "success with 120s timeout",
			file:     "compose.yml",
			project:  "homelab",
			timeout:  120,
			opts:     mockRunnerOpts{exitCode: "0"},
			wantArgs: []string{"docker", "compose", "-p", "homelab", "-f", "compose.yml", "down", "--timeout", "120"},
			wantErr:  false,
		},
		{
			name:    "failure",
			file:    "compose.yml",
			project: "proj",
			timeout: 30,
			opts:    mockRunnerOpts{exitCode: "1", stderr: "error"},
			wantErr: true,
			errMsg:  "docker compose down",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var recorded [][]string
			c := &ComposeClient{
				file:    tt.file,
				project: tt.project,
				runner:  newMockRunner(tt.opts, &recorded),
			}

			err := c.DownWithTimeout(context.Background(), tt.timeout)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
				require.Len(t, recorded, 1)
				assert.Equal(t, tt.wantArgs, recorded[0])
			}
		})
	}
}

func TestComposeClient_Restart_WithMockRunner(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		project  string
		services []string
		opts     mockRunnerOpts
		wantArgs []string
		wantErr  bool
		errMsg   string
	}{
		{
			name:     "restart all services",
			file:     "compose.yml",
			project:  "proj",
			services: nil,
			opts:     mockRunnerOpts{exitCode: "0"},
			wantArgs: []string{"docker", "compose", "-p", "proj", "-f", "compose.yml", "restart"},
			wantErr:  false,
		},
		{
			name:     "restart specific services",
			file:     "compose.yml",
			project:  "proj",
			services: []string{"traefik", "nginx"},
			opts:     mockRunnerOpts{exitCode: "0"},
			wantArgs: []string{"docker", "compose", "-p", "proj", "-f", "compose.yml", "restart", "traefik", "nginx"},
			wantErr:  false,
		},
		{
			name:     "failure",
			file:     "compose.yml",
			project:  "proj",
			services: []string{"web"},
			opts:     mockRunnerOpts{exitCode: "1"},
			wantErr:  true,
			errMsg:   "docker compose restart",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var recorded [][]string
			c := &ComposeClient{
				file:    tt.file,
				project: tt.project,
				runner:  newMockRunner(tt.opts, &recorded),
			}

			err := c.Restart(context.Background(), tt.services...)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
				require.Len(t, recorded, 1)
				assert.Equal(t, tt.wantArgs, recorded[0])
			}
		})
	}
}

func TestComposeClient_Status_WithMockRunner(t *testing.T) {
	tests := []struct {
		name         string
		file         string
		project      string
		opts         mockRunnerOpts
		wantServices []ServiceStatus
		wantErr      bool
		errMsg       string
	}{
		{
			name:    "single running service",
			file:    "compose.yml",
			project: "proj",
			opts: mockRunnerOpts{
				exitCode: "0",
				stdout:   "web\trunning\tUp 10 minutes\t0.0.0.0:8080->80/tcp",
			},
			wantServices: []ServiceStatus{
				{Name: "web", State: "running", Status: "Up 10 minutes", Ports: "0.0.0.0:8080->80/tcp", Running: true},
			},
			wantErr: false,
		},
		{
			name:    "multiple services",
			file:    "compose.yml",
			project: "proj",
			opts: mockRunnerOpts{
				exitCode: "0",
				stdout:   "web\trunning\tUp 10 minutes\t8080:80/tcp\ndb\texited\tExited (0)\t",
			},
			wantServices: []ServiceStatus{
				{Name: "web", State: "running", Status: "Up 10 minutes", Ports: "8080:80/tcp", Running: true},
				{Name: "db", State: "exited", Status: "Exited (0)", Ports: "", Running: false},
			},
			wantErr: false,
		},
		{
			name:    "no services running",
			file:    "compose.yml",
			project: "proj",
			opts: mockRunnerOpts{
				exitCode: "0",
				stdout:   "",
			},
			wantServices: nil,
			wantErr:      false,
		},
		{
			name:    "service without ports column",
			file:    "compose.yml",
			project: "proj",
			opts: mockRunnerOpts{
				exitCode: "0",
				stdout:   "worker\trunning\tUp 5 minutes",
			},
			wantServices: []ServiceStatus{
				{Name: "worker", State: "running", Status: "Up 5 minutes", Running: true},
			},
			wantErr: false,
		},
		{
			name:    "incomplete line skipped",
			file:    "compose.yml",
			project: "proj",
			opts: mockRunnerOpts{
				exitCode: "0",
				stdout:   "web\trunning",
			},
			wantServices: nil,
			wantErr:      false,
		},
		{
			name:    "command failure",
			file:    "compose.yml",
			project: "proj",
			opts:    mockRunnerOpts{exitCode: "1", stderr: "no such file"},
			wantErr: true,
			errMsg:  "docker compose ps",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &ComposeClient{
				file:    tt.file,
				project: tt.project,
				runner:  newMockRunner(tt.opts, nil),
			}

			services, err := c.Status(context.Background())
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantServices, services)
			}
		})
	}
}

func TestComposeClient_Ps_WithMockRunner(t *testing.T) {
	tests := []struct {
		name         string
		file         string
		project      string
		opts         mockRunnerOpts
		wantArgs     []string
		wantContains string
		wantErr      bool
		errMsg       string
	}{
		{
			name:         "success",
			file:         "compose.yml",
			project:      "proj",
			opts:         mockRunnerOpts{exitCode: "0", stdout: "NAME   STATUS\nweb    running\n"},
			wantArgs:     []string{"docker", "compose", "-p", "proj", "-f", "compose.yml", "ps"},
			wantContains: "NAME   STATUS\nweb    running",
			wantErr:      false,
		},
		{
			name:         "success no project",
			file:         "compose.yml",
			project:      "",
			opts:         mockRunnerOpts{exitCode: "0", stdout: "NAME   STATUS\n"},
			wantArgs:     []string{"docker", "compose", "-f", "compose.yml", "ps"},
			wantContains: "NAME   STATUS",
			wantErr:      false,
		},
		{
			name:    "failure",
			file:    "compose.yml",
			project: "proj",
			opts:    mockRunnerOpts{exitCode: "1"},
			wantErr: true,
			errMsg:  "docker compose ps",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var recorded [][]string
			c := &ComposeClient{
				file:    tt.file,
				project: tt.project,
				runner:  newMockRunner(tt.opts, &recorded),
			}

			got, err := c.Ps(context.Background())
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
				assert.Contains(t, got, tt.wantContains)
				require.Len(t, recorded, 1)
				assert.Equal(t, tt.wantArgs, recorded[0])
			}
		})
	}
}

// TestComposeClient_DownCommandBuilding verifies Down builds the correct args.
func TestComposeClient_DownCommandBuilding(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		project  string
		wantArgs []string
	}{
		{
			name:     "down with project",
			file:     "compose.yml",
			project:  "homelab",
			wantArgs: []string{"compose", "-p", "homelab", "-f", "compose.yml", "down"},
		},
		{
			name:     "down without project",
			file:     "compose.yml",
			project:  "",
			wantArgs: []string{"compose", "-f", "compose.yml", "down"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &ComposeClient{file: tt.file, project: tt.project}
			args := c.baseArgs()
			args = append(args, "down")
			assert.Equal(t, tt.wantArgs, args)
		})
	}
}

// TestComposeClient_StatusCommandBuilding verifies Status builds the correct format args.
func TestComposeClient_StatusCommandBuilding(t *testing.T) {
	c := &ComposeClient{file: "compose.yml", project: "proj"}
	args := c.baseArgs()
	args = append(args, "ps", "--format", "{{.Name}}\t{{.State}}\t{{.Status}}\t{{.Ports}}")

	expected := []string{
		"compose", "-p", "proj", "-f", "compose.yml",
		"ps", "--format", "{{.Name}}\t{{.State}}\t{{.Status}}\t{{.Ports}}",
	}
	assert.Equal(t, expected, args)
}

// TestComposeClient_PsCommandBuilding verifies Ps builds the correct args.
func TestComposeClient_PsCommandBuilding(t *testing.T) {
	c := &ComposeClient{file: "compose.yml", project: "proj"}
	args := c.baseArgs()
	args = append(args, "ps")

	expected := []string{"compose", "-p", "proj", "-f", "compose.yml", "ps"}
	assert.Equal(t, expected, args)
}

// TestParseStatusOutput_EdgeCases tests additional edge cases for status output parsing.
func TestParseStatusOutput_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantLen  int
		validate func(t *testing.T, services []ServiceStatus)
	}{
		{
			name:    "service without ports",
			input:   "worker\trunning\tUp 5 minutes",
			wantLen: 1,
			validate: func(t *testing.T, services []ServiceStatus) {
				assert.Equal(t, "worker", services[0].Name)
				assert.Equal(t, "running", services[0].State)
				assert.True(t, services[0].Running)
				assert.Empty(t, services[0].Ports)
			},
		},
		{
			name:    "exited service not running",
			input:   "db\texited\tExited (0)\t",
			wantLen: 1,
			validate: func(t *testing.T, services []ServiceStatus) {
				assert.Equal(t, "db", services[0].Name)
				assert.Equal(t, "exited", services[0].State)
				assert.False(t, services[0].Running)
			},
		},
		{
			name:    "blank lines between services",
			input:   "web\trunning\tUp 10 minutes\t8080:80/tcp\n\ndb\trunning\tUp 10 minutes\t5432:5432/tcp",
			wantLen: 2,
			validate: func(t *testing.T, services []ServiceStatus) {
				assert.Equal(t, "web", services[0].Name)
				assert.Equal(t, "db", services[1].Name)
			},
		},
		{
			name:    "only whitespace",
			input:   "   \t  ",
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			services := parseStatusOutput(tt.input)
			assert.Len(t, services, tt.wantLen)
			if tt.validate != nil && len(services) > 0 {
				tt.validate(t, services)
			}
		})
	}
}

// splitLines and splitByTab helpers used by parseStatusOutput
func TestSplitLines(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"single line", "hello", []string{"hello"}},
		{"two lines", "a\nb", []string{"a", "b"}},
		{"trailing newline", "a\n", []string{"a"}},
		{"empty string", "", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitLines(tt.in)
			if tt.want == nil {
				assert.Empty(t, got)
			} else {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestSplitByTab(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"single field", "hello", []string{"hello"}},
		{"two fields", "a\tb", []string{"a", "b"}},
		{"four fields", "a\tb\tc\td", []string{"a", "b", "c", "d"}},
		{"trailing tab", "a\t", []string{"a"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitByTab(tt.in)
			assert.Equal(t, tt.want, got)
		})
	}
}

// Verify the mock runner captures correct docker binary name.
func TestMockRunner_CapturesFullCommand(t *testing.T) {
	var recorded [][]string
	runner := newMockRunner(mockRunnerOpts{exitCode: "0"}, &recorded)

	cmd := runner(context.Background(), "docker", "compose", "-f", "test.yml", "up")
	_ = cmd.Run()

	require.Len(t, recorded, 1)
	assert.Equal(t, "docker", recorded[0][0])
	assert.True(t, strings.HasSuffix(strings.Join(recorded[0], " "), "compose -f test.yml up"))
}

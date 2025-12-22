package docker

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// ServiceStatus represents the status of a docker compose service.
type ServiceStatus struct {
	Name    string
	State   string
	Status  string
	Ports   string
	Running bool
}

// ComposeClient handles docker compose operations.
type ComposeClient struct {
	file string
}

// NewComposeClient creates a new compose client for the given compose file.
func NewComposeClient(file string) *ComposeClient {
	return &ComposeClient{file: file}
}

// Up starts services defined in the compose file.
func (c *ComposeClient) Up(ctx context.Context, services ...string) error {
	args := []string{"compose", "-f", c.file, "up", "-d"}
	args = append(args, services...)

	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker compose up: %w\n%s", err, output)
	}

	return nil
}

// Down stops and removes services defined in the compose file.
func (c *ComposeClient) Down(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "compose", "-f", c.file, "down")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker compose down: %w\n%s", err, output)
	}

	return nil
}

// Restart restarts services defined in the compose file.
func (c *ComposeClient) Restart(ctx context.Context, services ...string) error {
	args := []string{"compose", "-f", c.file, "restart"}
	args = append(args, services...)

	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker compose restart: %w\n%s", err, output)
	}

	return nil
}

// Status returns the status of services in the compose file.
func (c *ComposeClient) Status(ctx context.Context) ([]ServiceStatus, error) {
	cmd := exec.CommandContext(ctx, "docker", "compose", "-f", c.file, "ps", "--format", "{{.Name}}\t{{.State}}\t{{.Status}}\t{{.Ports}}")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("docker compose ps: %w\n%s", err, stderr.String())
	}

	var services []ServiceStatus
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}

		parts := strings.Split(line, "\t")
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

	return services, nil
}

// Ps runs docker compose ps and returns the raw output.
func (c *ComposeClient) Ps(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "compose", "-f", c.file, "ps")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker compose ps: %w\n%s", err, output)
	}

	return string(output), nil
}

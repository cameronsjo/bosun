package docker

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/cameronsjo/bosun/internal/log"
)

// ServiceStatus represents the status of a docker compose service.
type ServiceStatus struct {
	Name    string
	State   string
	Status  string
	Ports   string
	Running bool
}

// commandRunner creates exec commands. Defaults to exec.CommandContext.
type commandRunner func(ctx context.Context, name string, args ...string) *exec.Cmd

// ComposeClient handles docker compose operations.
type ComposeClient struct {
	file    string
	project string
	runner  commandRunner // nil = exec.CommandContext
}

// command creates an exec.Cmd using the configured runner (or exec.CommandContext by default).
func (c *ComposeClient) command(ctx context.Context, args ...string) *exec.Cmd {
	r := c.runner
	if r == nil {
		r = exec.CommandContext
	}
	return r(ctx, "docker", args...)
}

// NewComposeClient creates a new compose client for the given compose file and project name.
// The project name ensures consistent container namespacing and proper orphan detection.
// Returns an error if the compose file does not exist.
func NewComposeClient(file, project string) (*ComposeClient, error) {
	if _, err := os.Stat(file); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("compose file not found: %s", file)
		}
		return nil, fmt.Errorf("cannot access compose file %s: %w", file, err)
	}
	return &ComposeClient{file: file, project: project}, nil
}

// baseArgs returns the common docker compose arguments including project name.
func (c *ComposeClient) baseArgs() []string {
	args := []string{"compose"}
	if c.project != "" {
		args = append(args, "-p", c.project)
	}
	args = append(args, "-f", c.file)
	return args
}

// Up starts services defined in the compose file.
func (c *ComposeClient) Up(ctx context.Context, services ...string) error {
	start := time.Now()
	logger := log.ComponentCtx(ctx, log.ComponentDocker)
	serviceCount := len(services)
	if serviceCount == 0 {
		logger.Debug().Str("project", c.project).Msg("Preparing to start all services with docker compose up")
	} else {
		logger.Debug().Str("project", c.project).Int("service_count", serviceCount).Msg("Preparing to start specific services with docker compose up")
	}

	args := c.baseArgs()
	args = append(args, "up", "-d")
	args = append(args, services...)

	cmd := c.command(ctx, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Error().
			Err(err).
			Str("project", c.project).
			Int("service_count", serviceCount).
			Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
			Msg("Failed to start services with docker compose up")
		return fmt.Errorf("docker compose up: %w\n%s", err, output)
	}

	logger.Info().
		Str("project", c.project).
		Int("service_count", serviceCount).
		Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
		Msg("Successfully started services with docker compose up")
	return nil
}

// Down stops and removes services defined in the compose file.
func (c *ComposeClient) Down(ctx context.Context) error {
	start := time.Now()
	logger := log.ComponentCtx(ctx, log.ComponentDocker)
	logger.Info().Str("project", c.project).Msg("Preparing to stop and remove services with docker compose down")

	args := c.baseArgs()
	args = append(args, "down")

	cmd := c.command(ctx, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Error().
			Err(err).
			Str("project", c.project).
			Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
			Msg("Failed to stop and remove services with docker compose down")
		return fmt.Errorf("docker compose down: %w\n%s", err, output)
	}

	logger.Info().
		Str("project", c.project).
		Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
		Msg("Successfully stopped and removed services with docker compose down")
	return nil
}

// DownWithTimeout stops and removes services with a custom shutdown timeout.
// The timeout is the grace period (in seconds) between SIGTERM and SIGKILL for each container.
func (c *ComposeClient) DownWithTimeout(ctx context.Context, timeoutSeconds int) error {
	start := time.Now()
	logger := log.ComponentCtx(ctx, log.ComponentDocker)
	logger.Info().
		Str("project", c.project).
		Int("timeout_seconds", timeoutSeconds).
		Msg("Preparing to stop and remove services with docker compose down")

	args := c.baseArgs()
	args = append(args, "down", "--timeout", fmt.Sprintf("%d", timeoutSeconds))

	cmd := c.command(ctx, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Error().
			Err(err).
			Str("project", c.project).
			Int("timeout_seconds", timeoutSeconds).
			Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
			Msg("Failed to stop and remove services with docker compose down")
		return fmt.Errorf("docker compose down: %w\n%s", err, output)
	}

	logger.Info().
		Str("project", c.project).
		Int("timeout_seconds", timeoutSeconds).
		Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
		Msg("Successfully stopped and removed services with docker compose down")
	return nil
}

// Restart restarts services defined in the compose file.
func (c *ComposeClient) Restart(ctx context.Context, services ...string) error {
	start := time.Now()
	logger := log.ComponentCtx(ctx, log.ComponentDocker)
	serviceCount := len(services)
	if serviceCount == 0 {
		logger.Debug().Str("project", c.project).Msg("Preparing to restart all services with docker compose restart")
	} else {
		logger.Debug().Str("project", c.project).Int("service_count", serviceCount).Msg("Preparing to restart specific services with docker compose restart")
	}

	args := c.baseArgs()
	args = append(args, "restart")
	args = append(args, services...)

	cmd := c.command(ctx, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Error().
			Err(err).
			Str("project", c.project).
			Int("service_count", serviceCount).
			Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
			Msg("Failed to restart services with docker compose restart")
		return fmt.Errorf("docker compose restart: %w\n%s", err, output)
	}

	logger.Info().
		Str("project", c.project).
		Int("service_count", serviceCount).
		Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
		Msg("Successfully restarted services with docker compose restart")
	return nil
}

// Status returns the status of services in the compose file.
func (c *ComposeClient) Status(ctx context.Context) ([]ServiceStatus, error) {
	start := time.Now()
	logger := log.ComponentCtx(ctx, log.ComponentDocker)
	logger.Debug().Str("project", c.project).Msg("Fetching service status with docker compose ps")

	args := c.baseArgs()
	args = append(args, "ps", "--format", "{{.Name}}\t{{.State}}\t{{.Status}}\t{{.Ports}}")
	cmd := c.command(ctx, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		logger.Error().
			Err(err).
			Str("project", c.project).
			Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
			Msg("Failed to fetch service status with docker compose ps")
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

	logger.Info().
		Str("project", c.project).
		Int("service_count", len(services)).
		Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
		Msg("Successfully fetched service status")
	return services, nil
}

// Ps runs docker compose ps and returns the raw output.
func (c *ComposeClient) Ps(ctx context.Context) (string, error) {
	start := time.Now()
	logger := log.ComponentCtx(ctx, log.ComponentDocker)
	logger.Debug().Str("project", c.project).Msg("Running docker compose ps")

	args := c.baseArgs()
	args = append(args, "ps")

	cmd := c.command(ctx, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Error().
			Err(err).
			Str("project", c.project).
			Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
			Msg("Failed to run docker compose ps")
		return "", fmt.Errorf("docker compose ps: %w\n%s", err, output)
	}

	logger.Debug().
		Str("project", c.project).
		Int("output_size", len(output)).
		Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
		Msg("Successfully ran docker compose ps")
	return string(output), nil
}

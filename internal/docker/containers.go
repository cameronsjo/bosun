package docker

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
)

// ContainerDetails holds detailed container information.
type ContainerDetails struct {
	ID           string
	Name         string
	Image        string
	State        string
	Status       string
	Health       string
	Ports        []PortBinding
	Networks     []string
	Volumes      []string
	Environment  []string
	Labels       map[string]string
	Created      time.Time
	Started      time.Time
	RestartCount int
	Platform     string
	Driver       string
}

// PortBinding represents a container port binding.
type PortBinding struct {
	ContainerPort string
	HostPort      string
	Protocol      string
}

// Logs returns a reader for container logs (streaming).
func (c *Client) Logs(ctx context.Context, name string, tail int, follow bool) (io.ReadCloser, error) {
	tailStr := "all"
	if tail > 0 {
		tailStr = fmt.Sprintf("%d", tail)
	}

	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Tail:       tailStr,
		Timestamps: false,
	}

	reader, err := c.cli.ContainerLogs(ctx, name, options)
	if err != nil {
		return nil, fmt.Errorf("get logs for %s: %w", name, err)
	}

	return reader, nil
}

// Inspect returns detailed information about a container.
func (c *Client) Inspect(ctx context.Context, name string) (*ContainerDetails, error) {
	info, err := c.cli.ContainerInspect(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("inspect container %s: %w", name, err)
	}

	details := &ContainerDetails{
		ID:           info.ID[:12],
		Name:         strings.TrimPrefix(info.Name, "/"),
		Image:        info.Config.Image,
		State:        info.State.Status,
		Status:       formatContainerStatus(info.State),
		RestartCount: info.RestartCount,
		Labels:       info.Config.Labels,
		Environment:  info.Config.Env,
		Driver:       info.Driver,
	}

	// Parse created time
	if created, err := time.Parse(time.RFC3339Nano, info.Created); err == nil {
		details.Created = created
	}

	// Parse started time
	if info.State.StartedAt != "" {
		if started, err := time.Parse(time.RFC3339Nano, info.State.StartedAt); err == nil {
			details.Started = started
		}
	}

	// Health status
	if info.State.Health != nil {
		details.Health = info.State.Health.Status
	}

	// Port bindings
	for port, bindings := range info.NetworkSettings.Ports {
		for _, binding := range bindings {
			details.Ports = append(details.Ports, PortBinding{
				ContainerPort: port.Port(),
				HostPort:      binding.HostPort,
				Protocol:      port.Proto(),
			})
		}
	}

	// Networks
	for network := range info.NetworkSettings.Networks {
		details.Networks = append(details.Networks, network)
	}

	// Volumes
	for _, mount := range info.Mounts {
		details.Volumes = append(details.Volumes, fmt.Sprintf("%s:%s", mount.Source, mount.Destination))
	}

	// Platform
	if info.Platform != "" {
		details.Platform = info.Platform
	}

	return details, nil
}

// Remove removes a container.
func (c *Client) Remove(ctx context.Context, name string, force bool) error {
	options := container.RemoveOptions{
		Force:         force,
		RemoveVolumes: false,
	}

	if err := c.cli.ContainerRemove(ctx, name, options); err != nil {
		return fmt.Errorf("remove container %s: %w", name, err)
	}

	return nil
}

// Start starts a stopped container.
func (c *Client) Start(ctx context.Context, name string) error {
	if err := c.cli.ContainerStart(ctx, name, container.StartOptions{}); err != nil {
		return fmt.Errorf("start container %s: %w", name, err)
	}
	return nil
}

// Exists checks if a container with the given name exists (running or stopped).
func (c *Client) Exists(ctx context.Context, name string) (bool, error) {
	containers, err := c.cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return false, fmt.Errorf("list containers: %w", err)
	}

	for _, ctr := range containers {
		for _, n := range ctr.Names {
			if strings.TrimPrefix(n, "/") == name {
				return true, nil
			}
		}
	}

	return false, nil
}

// formatContainerStatus formats container state for display.
func formatContainerStatus(state *types.ContainerState) string {
	if state.Running {
		return fmt.Sprintf("Up %s", formatDuration(time.Since(mustParseTime(state.StartedAt))))
	}
	if state.ExitCode != 0 {
		return fmt.Sprintf("Exited (%d)", state.ExitCode)
	}
	return state.Status
}

// mustParseTime parses a time string or returns zero time.
func mustParseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

// formatDuration formats a duration for display like "2 hours ago".
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d seconds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%d hours", int(d.Hours()))
	}
	return fmt.Sprintf("%d days", int(d.Hours()/24))
}

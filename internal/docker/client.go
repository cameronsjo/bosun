// Package docker provides Docker SDK client and operations.
package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/system"
	"github.com/docker/docker/client"

	"github.com/cameronsjo/bosun/internal/log"
)

// statsJSON is the internal representation for container stats.
type statsJSON struct {
	CPUStats struct {
		CPUUsage struct {
			TotalUsage  uint64   `json:"total_usage"`
			PercpuUsage []uint64 `json:"percpu_usage"`
		} `json:"cpu_usage"`
		SystemUsage uint64 `json:"system_cpu_usage"`
	} `json:"cpu_stats"`
	PreCPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemUsage uint64 `json:"system_cpu_usage"`
	} `json:"precpu_stats"`
	MemoryStats struct {
		Usage uint64 `json:"usage"`
		Limit uint64 `json:"limit"`
	} `json:"memory_stats"`
}

// Client wraps the Docker SDK client.
type Client struct {
	cli *client.Client
	api DockerAPI // interface for testing
}

// NewClient creates a new Docker client connection and validates daemon connectivity.
func NewClient() (*Client, error) {
	logger := log.Component(log.ComponentDocker)
	logger.Debug().Msg("Creating Docker client")

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		logger.Error().Err(err).Msg("Failed to create Docker client")
		return nil, fmt.Errorf("create docker client: %w", err)
	}

	c := &Client{cli: cli, api: cli}

	// Validate daemon is reachable before returning client
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := cli.Ping(ctx); err != nil {
		cli.Close()
		logger.Error().Err(err).Msg("Docker daemon not reachable")
		return nil, fmt.Errorf("docker daemon not reachable: %w", err)
	}

	logger.Debug().Msg("Docker client created successfully")
	return c, nil
}

// NewClientWithAPI creates a new Docker client with a custom API implementation.
// This is primarily used for testing with mock implementations.
func NewClientWithAPI(api DockerAPI) *Client {
	return &Client{api: api}
}

// Ping tests the connection to the Docker daemon.
func (c *Client) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := c.api.Ping(ctx)
	if err != nil {
		return fmt.Errorf("ping docker: %w", err)
	}

	return nil
}

// Info returns system-wide information about the Docker daemon.
func (c *Client) Info(ctx context.Context) (system.Info, error) {
	return c.api.Info(ctx)
}

// Close closes the Docker client connection.
func (c *Client) Close() error {
	if c.api != nil {
		return c.api.Close()
	}
	return nil
}

// Raw returns the underlying Docker client for advanced operations.
func (c *Client) Raw() *client.Client {
	return c.cli
}

// ContainerInfo holds summary information about a container.
type ContainerInfo struct {
	ID      string
	Name    string
	Image   string
	Status  string
	State   string
	Health  string
	Created time.Time
	Uptime  string
	Ports   []string
	Labels  map[string]string
}

// ContainerStats holds resource usage statistics.
type ContainerStats struct {
	Name       string
	CPUPercent float64
	MemUsage   uint64
	MemLimit   uint64
	MemPercent float64
}

// ListContainers returns all containers (running and stopped).
func (c *Client) ListContainers(ctx context.Context, runningOnly bool) ([]ContainerInfo, error) {
	start := time.Now()
	logger := log.ComponentCtx(ctx, log.ComponentDocker)
	logger.Debug().Bool("running_only", runningOnly).Msg("Listing containers")

	containers, err := c.api.ContainerList(ctx, container.ListOptions{
		All: !runningOnly,
	})
	if err != nil {
		logger.Error().Err(err).Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).Msg("Failed to list containers")
		return nil, fmt.Errorf("list containers: %w", err)
	}

	result := make([]ContainerInfo, 0, len(containers))
	for _, ctr := range containers {
		name := ""
		if len(ctr.Names) > 0 {
			name = strings.TrimPrefix(ctr.Names[0], "/")
		}

		health := parseHealthFromStatus(ctr.Status)

		ports := make([]string, 0, len(ctr.Ports))
		for _, p := range ctr.Ports {
			ports = append(ports, formatPortString(p.PublicPort, p.PrivatePort, p.Type))
		}

		result = append(result, ContainerInfo{
			ID:      ctr.ID[:12],
			Name:    name,
			Image:   ctr.Image,
			Status:  ctr.Status,
			State:   ctr.State,
			Health:  health,
			Created: time.Unix(ctr.Created, 0),
			Ports:   ports,
			Labels:  ctr.Labels,
		})
	}

	logger.Debug().
		Int("count", len(result)).
		Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
		Msg("Listed containers successfully")
	return result, nil
}

// CountContainers returns counts of running, total, and unhealthy containers.
func (c *Client) CountContainers(ctx context.Context) (running, total, unhealthy int, err error) {
	containers, err := c.ListContainers(ctx, false)
	if err != nil {
		return 0, 0, 0, err
	}

	total = len(containers)
	for _, ctr := range containers {
		if ctr.State == "running" {
			running++
		}
		if ctr.Health == "unhealthy" {
			unhealthy++
		}
	}

	return running, total, unhealthy, nil
}

// GetContainerByName returns a container by its name.
func (c *Client) GetContainerByName(ctx context.Context, name string) (*ContainerInfo, error) {
	containers, err := c.ListContainers(ctx, false)
	if err != nil {
		return nil, err
	}

	for _, ctr := range containers {
		if ctr.Name == name {
			return &ctr, nil
		}
	}

	return nil, fmt.Errorf("container not found: %s", name)
}

// IsContainerRunning checks if a container with the given name is running.
func (c *Client) IsContainerRunning(ctx context.Context, name string) bool {
	ctr, err := c.GetContainerByName(ctx, name)
	if err != nil {
		return false
	}
	return ctr.State == "running"
}

// GetContainerImage returns the image name for a running container.
func (c *Client) GetContainerImage(ctx context.Context, name string) (string, error) {
	ctr, err := c.GetContainerByName(ctx, name)
	if err != nil {
		return "", err
	}
	return ctr.Image, nil
}

// RemoveContainer forcefully removes a container by name.
func (c *Client) RemoveContainer(ctx context.Context, name string) error {
	logger := log.ComponentCtx(ctx, log.ComponentDocker)
	logger.Info().Str(log.FieldContainer, name).Msg("Removing container")

	err := c.api.ContainerRemove(ctx, name, container.RemoveOptions{
		Force: true,
	})
	if err != nil {
		logger.Error().Str(log.FieldContainer, name).Err(err).Msg("Failed to remove container")
		return err
	}

	logger.Info().Str(log.FieldContainer, name).Msg("Container removed successfully")
	return nil
}

// StopContainer gracefully stops a container with the given timeout.
func (c *Client) StopContainer(ctx context.Context, name string, timeout int) error {
	logger := log.ComponentCtx(ctx, log.ComponentDocker)
	logger.Info().Str(log.FieldContainer, name).Int("timeout", timeout).Msg("Stopping container")

	err := c.api.ContainerStop(ctx, name, container.StopOptions{Timeout: &timeout})
	if err != nil {
		logger.Error().Str(log.FieldContainer, name).Err(err).Msg("Failed to stop container")
		return fmt.Errorf("stop container %s: %w", name, err)
	}

	logger.Info().Str(log.FieldContainer, name).Msg("Container stopped successfully")
	return nil
}

// RestartContainer restarts a container by name with the given timeout (seconds).
// The timeout controls the grace period between SIGTERM and SIGKILL during the stop phase.
func (c *Client) RestartContainer(ctx context.Context, name string, timeout ...int) error {
	logger := log.ComponentCtx(ctx, log.ComponentDocker)

	t := 10
	if len(timeout) > 0 && timeout[0] > 0 {
		t = timeout[0]
	}

	logger.Info().Str(log.FieldContainer, name).Int("timeout", t).Msg("Restarting container")

	err := c.api.ContainerRestart(ctx, name, container.StopOptions{Timeout: &t})
	if err != nil {
		logger.Error().Str(log.FieldContainer, name).Err(err).Msg("Failed to restart container")
		return err
	}

	logger.Info().Str(log.FieldContainer, name).Msg("Container restarted successfully")
	return nil
}

// ExecContainer runs a command inside a running container and waits for completion.
// Returns an error if the command exits with a non-zero status.
func (c *Client) ExecContainer(ctx context.Context, name string, cmd []string) error {
	logger := log.ComponentCtx(ctx, log.ComponentDocker)
	logger.Info().Str(log.FieldContainer, name).Strs("command", cmd).Msg("Executing command in container")

	createResp, err := c.api.ContainerExecCreate(ctx, name, container.ExecOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		logger.Error().Str(log.FieldContainer, name).Err(err).Msg("Failed to create exec instance")
		return fmt.Errorf("create exec in %s: %w", name, err)
	}

	attachResp, err := c.api.ContainerExecAttach(ctx, createResp.ID, container.ExecAttachOptions{})
	if err != nil {
		logger.Error().Str(log.FieldContainer, name).Err(err).Msg("Failed to attach to exec instance")
		return fmt.Errorf("attach exec in %s: %w", name, err)
	}
	defer attachResp.Close()

	// Drain output to allow the exec to complete.
	if _, err := io.Copy(io.Discard, attachResp.Reader); err != nil {
		logger.Warn().Str(log.FieldContainer, name).Err(err).Msg("Error reading exec stream")
	}

	// Check exit code
	inspectResp, err := c.api.ContainerExecInspect(ctx, createResp.ID)
	if err != nil {
		logger.Error().Str(log.FieldContainer, name).Err(err).Msg("Failed to inspect exec instance")
		return fmt.Errorf("inspect exec in %s: %w", name, err)
	}

	if inspectResp.ExitCode != 0 {
		logger.Error().Str(log.FieldContainer, name).Int("exit_code", inspectResp.ExitCode).Msg("Exec command failed with non-zero exit code")
		return fmt.Errorf("exec in %s exited with code %d", name, inspectResp.ExitCode)
	}

	logger.Info().Str(log.FieldContainer, name).Msg("Exec command completed successfully")
	return nil
}

// MaxLogSize is the maximum size of logs to read (100MB).
const MaxLogSize = 100 * 1024 * 1024

// GetContainerLogs returns the last n lines of logs from a container.
// Logs are limited to MaxLogSize (100MB) to prevent memory exhaustion.
func (c *Client) GetContainerLogs(ctx context.Context, name string, tail int) (string, error) {
	logger := log.ComponentCtx(ctx, log.ComponentDocker)
	logger.Debug().Str(log.FieldContainer, name).Int("tail", tail).Msg("Fetching container logs")

	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       fmt.Sprintf("%d", tail),
	}

	reader, err := c.api.ContainerLogs(ctx, name, options)
	if err != nil {
		logger.Error().Str(log.FieldContainer, name).Err(err).Msg("Failed to get container logs")
		return "", fmt.Errorf("get container logs: %w", err)
	}
	defer reader.Close()

	// Read logs with size limit to prevent memory exhaustion
	limitedReader := io.LimitReader(reader, MaxLogSize)
	logs, err := io.ReadAll(limitedReader)
	if err != nil {
		logger.Error().Str(log.FieldContainer, name).Err(err).Msg("Failed to read container logs")
		return "", fmt.Errorf("read logs: %w", err)
	}

	logger.Debug().Str(log.FieldContainer, name).Int("bytes", len(logs)).Msg("Fetched container logs")
	// Strip Docker log header bytes (first 8 bytes per line for multiplexed streams)
	// This is a simplified version - logs may have control characters
	return string(logs), nil
}

// GetContainerStats returns resource usage for a container.
func (c *Client) GetContainerStats(ctx context.Context, name string) (*ContainerStats, error) {
	logger := log.ComponentCtx(ctx, log.ComponentDocker)
	logger.Debug().Str(log.FieldContainer, name).Msg("Fetching container stats")

	stats, err := c.api.ContainerStats(ctx, name, false)
	if err != nil {
		logger.Error().Str(log.FieldContainer, name).Err(err).Msg("Failed to get container stats")
		return nil, fmt.Errorf("get container stats: %w", err)
	}
	defer func() {
		// Drain any remaining data before closing to ensure proper connection reuse
		_, _ = io.Copy(io.Discard, stats.Body)
		stats.Body.Close()
	}()

	// Parse the stats JSON
	var v statsJSON
	if err := readJSONStats(stats.Body, &v); err != nil {
		return nil, fmt.Errorf("parse stats: %w", err)
	}

	// Calculate CPU percentage
	cpuDelta := float64(v.CPUStats.CPUUsage.TotalUsage - v.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(v.CPUStats.SystemUsage - v.PreCPUStats.SystemUsage)
	cpuPercent := calculateCPUPercent(cpuDelta, systemDelta, len(v.CPUStats.CPUUsage.PercpuUsage))

	// Calculate memory percentage
	memPercent := calculateMemPercent(v.MemoryStats.Usage, v.MemoryStats.Limit)

	return &ContainerStats{
		Name:       name,
		CPUPercent: cpuPercent,
		MemUsage:   v.MemoryStats.Usage,
		MemLimit:   v.MemoryStats.Limit,
		MemPercent: memPercent,
	}, nil
}

// GetAllContainerStats returns stats for all running containers.
// Containers that fail to return stats are logged and skipped.
func (c *Client) GetAllContainerStats(ctx context.Context) ([]ContainerStats, error) {
	containers, err := c.ListContainers(ctx, true)
	if err != nil {
		return nil, err
	}

	logger := log.ComponentCtx(ctx, log.ComponentDocker)
	stats := make([]ContainerStats, 0, len(containers))
	for _, ctr := range containers {
		s, err := c.GetContainerStats(ctx, ctr.Name)
		if err != nil {
			logger.Debug().
				Str(log.FieldContainer, ctr.Name).
				Err(err).
				Msg("Skipping container stats due to error")
			continue
		}
		stats = append(stats, *s)
	}

	return stats, nil
}

// DiskUsage returns Docker system disk usage information.
func (c *Client) DiskUsage(ctx context.Context) (types.DiskUsage, error) {
	return c.api.DiskUsage(ctx, types.DiskUsageOptions{})
}

// parseHealthFromStatus extracts health status from the Docker Status string.
// Docker's container list returns Status strings like:
//
//	"Up 2 hours (healthy)"
//	"Up 5 minutes (unhealthy)"
//	"Up 10 seconds (health: starting)"
//
// This avoids an N+1 ContainerInspect call per container.
func parseHealthFromStatus(status string) string {
	lower := strings.ToLower(status)
	switch {
	case strings.Contains(lower, "(healthy)"):
		return "healthy"
	case strings.Contains(lower, "(unhealthy)"):
		return "unhealthy"
	case strings.Contains(lower, "(health: starting)"):
		return "starting"
	default:
		return ""
	}
}

// calculateCPUPercent computes the CPU usage percentage from delta values.
// Returns 0 if either delta is non-positive (avoids divide-by-zero).
func calculateCPUPercent(cpuDelta, systemDelta float64, numCPUs int) float64 {
	if systemDelta <= 0 || cpuDelta <= 0 {
		return 0.0
	}
	return (cpuDelta / systemDelta) * float64(numCPUs) * 100.0
}

// calculateMemPercent computes the memory usage percentage.
// Returns 0 if the limit is zero (avoids divide-by-zero).
func calculateMemPercent(usage, limit uint64) float64 {
	if limit == 0 {
		return 0.0
	}
	return float64(usage) / float64(limit) * 100.0
}

// formatPortString formats a Docker port binding into a human-readable string.
// Public port of 0 means the port is not published to the host.
func formatPortString(publicPort, privatePort uint16, protocol string) string {
	if publicPort > 0 {
		return fmt.Sprintf("%d:%d/%s", publicPort, privatePort, protocol)
	}
	return fmt.Sprintf("%d/%s", privatePort, protocol)
}

// readJSONStats reads a single JSON stats object from the reader.
func readJSONStats(r io.Reader, v *statsJSON) error {
	return json.NewDecoder(r).Decode(v)
}

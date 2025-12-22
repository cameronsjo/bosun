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

// NewClient creates a new Docker client connection.
func NewClient() (*Client, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("create docker client: %w", err)
	}

	return &Client{cli: cli, api: cli}, nil
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
	containers, err := c.api.ContainerList(ctx, container.ListOptions{
		All: !runningOnly,
	})
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}

	result := make([]ContainerInfo, 0, len(containers))
	for _, ctr := range containers {
		name := ""
		if len(ctr.Names) > 0 {
			name = strings.TrimPrefix(ctr.Names[0], "/")
		}

		health := ""
		if ctr.State == "running" {
			// Get health status from inspection
			inspect, err := c.api.ContainerInspect(ctx, ctr.ID)
			if err == nil && inspect.State.Health != nil {
				health = inspect.State.Health.Status
			}
		}

		ports := make([]string, 0, len(ctr.Ports))
		for _, p := range ctr.Ports {
			if p.PublicPort > 0 {
				ports = append(ports, fmt.Sprintf("%d:%d/%s", p.PublicPort, p.PrivatePort, p.Type))
			} else {
				ports = append(ports, fmt.Sprintf("%d/%s", p.PrivatePort, p.Type))
			}
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
		})
	}

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
	return c.api.ContainerRemove(ctx, name, container.RemoveOptions{
		Force: true,
	})
}

// RestartContainer restarts a container by name.
func (c *Client) RestartContainer(ctx context.Context, name string) error {
	timeout := 10
	return c.api.ContainerRestart(ctx, name, container.StopOptions{Timeout: &timeout})
}

// GetContainerLogs returns the last n lines of logs from a container.
func (c *Client) GetContainerLogs(ctx context.Context, name string, tail int) (string, error) {
	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       fmt.Sprintf("%d", tail),
	}

	reader, err := c.api.ContainerLogs(ctx, name, options)
	if err != nil {
		return "", fmt.Errorf("get container logs: %w", err)
	}
	defer reader.Close()

	// Read all logs
	logs, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("read logs: %w", err)
	}

	// Strip Docker log header bytes (first 8 bytes per line for multiplexed streams)
	// This is a simplified version - logs may have control characters
	return string(logs), nil
}

// GetContainerStats returns resource usage for a container.
func (c *Client) GetContainerStats(ctx context.Context, name string) (*ContainerStats, error) {
	stats, err := c.api.ContainerStats(ctx, name, false)
	if err != nil {
		return nil, fmt.Errorf("get container stats: %w", err)
	}
	defer stats.Body.Close()

	// Parse the stats JSON
	var v statsJSON
	if err := readJSONStats(stats.Body, &v); err != nil {
		return nil, fmt.Errorf("parse stats: %w", err)
	}

	// Calculate CPU percentage
	cpuPercent := 0.0
	cpuDelta := float64(v.CPUStats.CPUUsage.TotalUsage - v.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(v.CPUStats.SystemUsage - v.PreCPUStats.SystemUsage)
	if systemDelta > 0 && cpuDelta > 0 {
		cpuPercent = (cpuDelta / systemDelta) * float64(len(v.CPUStats.CPUUsage.PercpuUsage)) * 100.0
	}

	// Calculate memory percentage
	memPercent := 0.0
	if v.MemoryStats.Limit > 0 {
		memPercent = float64(v.MemoryStats.Usage) / float64(v.MemoryStats.Limit) * 100.0
	}

	return &ContainerStats{
		Name:       name,
		CPUPercent: cpuPercent,
		MemUsage:   v.MemoryStats.Usage,
		MemLimit:   v.MemoryStats.Limit,
		MemPercent: memPercent,
	}, nil
}

// GetAllContainerStats returns stats for all running containers.
func (c *Client) GetAllContainerStats(ctx context.Context) ([]ContainerStats, error) {
	containers, err := c.ListContainers(ctx, true)
	if err != nil {
		return nil, err
	}

	stats := make([]ContainerStats, 0, len(containers))
	for _, ctr := range containers {
		s, err := c.GetContainerStats(ctx, ctr.Name)
		if err != nil {
			continue // Skip containers that fail
		}
		stats = append(stats, *s)
	}

	return stats, nil
}

// DiskUsage returns Docker system disk usage information.
func (c *Client) DiskUsage(ctx context.Context) (types.DiskUsage, error) {
	return c.api.DiskUsage(ctx, types.DiskUsageOptions{})
}

// readJSONStats reads a single JSON stats object from the reader.
func readJSONStats(r io.Reader, v *statsJSON) error {
	return json.NewDecoder(r).Decode(v)
}

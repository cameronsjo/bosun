# Docker Integration

This document describes bosun's Docker integration architecture, covering the client abstraction, container operations, compose management, and testing patterns.

## Overview

Bosun uses Docker to manage containers on Unraid servers. The integration provides:

- **Container lifecycle management**: Start, stop, restart, and remove containers
- **Container inspection**: Detailed container info, health checks, logs, and stats
- **Compose operations**: Manage multi-container applications via docker-compose
- **Testable architecture**: Interface-based design enabling comprehensive unit testing

The Docker package is located at `internal/docker/` and consists of four main files:

| File | Purpose |
|------|---------|
| `interface.go` | DockerAPI interface abstraction for testability |
| `client.go` | Docker client wrapper and container operations |
| `containers.go` | Extended container operations (inspect, logs, remove) |
| `compose.go` | Docker Compose CLI operations |

## Client Architecture

### Interface Abstraction

The `DockerAPI` interface abstracts the Docker SDK client, enabling dependency injection for testing:

```go
type DockerAPI interface {
    Ping(ctx context.Context) (types.Ping, error)
    ContainerList(ctx context.Context, options container.ListOptions) ([]types.Container, error)
    ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error)
    ContainerLogs(ctx context.Context, container string, options container.LogsOptions) (io.ReadCloser, error)
    ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error
    ContainerRestart(ctx context.Context, containerID string, options container.StopOptions) error
    ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error
    ContainerStats(ctx context.Context, containerID string, stream bool) (container.StatsResponseReader, error)
    DiskUsage(ctx context.Context, options types.DiskUsageOptions) (types.DiskUsage, error)
    Info(ctx context.Context) (system.Info, error)
    Close() error
}
```

This interface:

- Matches the Docker SDK client method signatures exactly
- Includes compile-time verification via `var _ DockerAPI = (dockerAPIAdapter)(nil)`
- Allows mock implementations for testing without a running Docker daemon

### Client Creation

**Production usage** - connects to the Docker daemon via environment configuration:

```go
client, err := docker.NewClient()
if err != nil {
    return fmt.Errorf("create docker client: %w", err)
}
defer client.Close()
```

The client uses `client.FromEnv` and `client.WithAPIVersionNegotiation()` to automatically detect the Docker socket and API version.

**Testing usage** - inject a mock implementation:

```go
mock := NewMockDockerAPI()
client := docker.NewClientWithAPI(mock)
```

### TestableClient

For advanced testing scenarios, `TestableClient` provides an alternative wrapper:

```go
type TestableClient struct {
    api DockerAPI
}

func NewTestableClient(api DockerAPI) *TestableClient
func (c *TestableClient) API() DockerAPI
func (c *TestableClient) Close() error
```

## Container Operations

### Data Types

**ContainerInfo** - summary information for container listings:

```go
type ContainerInfo struct {
    ID      string      // Short 12-character ID
    Name    string      // Container name (without leading /)
    Image   string      // Image name
    Status  string      // Human-readable status (e.g., "Up 10 minutes")
    State   string      // Container state (running, exited, etc.)
    Health  string      // Health check status (healthy, unhealthy, starting)
    Created time.Time   // Creation timestamp
    Uptime  string      // Formatted uptime
    Ports   []string    // Port mappings (e.g., "8080:80/tcp")
}
```

**ContainerDetails** - detailed inspection data:

```go
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
```

**ContainerStats** - resource usage statistics:

```go
type ContainerStats struct {
    Name       string
    CPUPercent float64
    MemUsage   uint64
    MemLimit   uint64
    MemPercent float64
}
```

### Listing Containers

```go
// List all containers (running and stopped)
containers, err := client.ListContainers(ctx, false)

// List only running containers
containers, err := client.ListContainers(ctx, true)
```

The listing automatically fetches health status for running containers via inspection.

### Container Counts

```go
running, total, unhealthy, err := client.CountContainers(ctx)
```

Returns counts useful for dashboards and monitoring.

### Finding Containers

```go
// Get container by exact name
container, err := client.GetContainerByName(ctx, "nginx")

// Check if container is running
isRunning := client.IsContainerRunning(ctx, "nginx")

// Check if container exists (running or stopped)
exists, err := client.Exists(ctx, "nginx")

// Get container image
image, err := client.GetContainerImage(ctx, "nginx")
```

### Viewing Logs

**Get last N lines as string:**

```go
logs, err := client.GetContainerLogs(ctx, "nginx", 100)
```

**Get streaming log reader:**

```go
// Get last 100 lines, no streaming
reader, err := client.Logs(ctx, "nginx", 100, false)

// Get all logs with streaming (follow mode)
reader, err := client.Logs(ctx, "nginx", 0, true)

defer reader.Close()
// Read from reader as needed
```

### Starting and Stopping

```go
// Start a stopped container
err := client.Start(ctx, "nginx")

// Restart a container (10-second timeout)
err := client.RestartContainer(ctx, "nginx")
```

### Removing Containers

```go
// Force remove (always uses force: true)
err := client.RemoveContainer(ctx, "nginx")

// Remove with force option
err := client.Remove(ctx, "nginx", true)  // force=true
err := client.Remove(ctx, "nginx", false) // force=false
```

### Health Checks

Health status is automatically populated during:

- `ListContainers()` - fetches health for running containers
- `Inspect()` - includes health in detailed inspection

Health values: `healthy`, `unhealthy`, `starting`, or empty string if no health check configured.

### Container Inspection

```go
details, err := client.Inspect(ctx, "nginx")

fmt.Printf("Name: %s\n", details.Name)
fmt.Printf("State: %s\n", details.State)
fmt.Printf("Health: %s\n", details.Health)
fmt.Printf("Networks: %v\n", details.Networks)
fmt.Printf("Volumes: %v\n", details.Volumes)
fmt.Printf("Restart Count: %d\n", details.RestartCount)
```

## Compose Operations

The `ComposeClient` manages multi-container applications via the `docker compose` CLI.

### Creating a Compose Client

```go
compose := docker.NewComposeClient("/path/to/docker-compose.yml")
```

### Starting Services

```go
// Start all services
err := compose.Up(ctx)

// Start specific services
err := compose.Up(ctx, "web", "db")
```

Services start in detached mode (`-d` flag).

### Stopping Services

```go
// Stop and remove all services
err := compose.Down(ctx)
```

### Restarting Services

```go
// Restart all services
err := compose.Restart(ctx)

// Restart specific services
err := compose.Restart(ctx, "web")
```

### Checking Status

**Structured status:**

```go
services, err := compose.Status(ctx)
for _, svc := range services {
    fmt.Printf("%s: %s (%v)\n", svc.Name, svc.State, svc.Running)
}
```

Returns `[]ServiceStatus`:

```go
type ServiceStatus struct {
    Name    string
    State   string  // "running", "exited", etc.
    Status  string  // Detailed status
    Ports   string  // Port mappings
    Running bool    // Convenience flag
}
```

**Raw output:**

```go
output, err := compose.Ps(ctx)
fmt.Println(output)
```

## Error Handling

### Connection Failures

The client wraps Docker SDK errors with context:

```go
client, err := docker.NewClient()
if err != nil {
    // "create docker client: ..."
}
```

### Ping Timeout

`Ping()` uses a 5-second timeout to detect unresponsive daemons:

```go
err := client.Ping(ctx)
if err != nil {
    // "ping docker: ..."
}
```

### Operation Errors

All operations wrap errors with descriptive prefixes:

| Operation | Error Prefix |
|-----------|--------------|
| `ListContainers` | `list containers:` |
| `ContainerLogs` | `get container logs:` |
| `GetContainerStats` | `get container stats:` |
| `Inspect` | `inspect container <name>:` |
| `Start` | `start container <name>:` |
| `Remove` | `remove container <name>:` |
| `Logs` | `get logs for <name>:` |

### Container Not Found

`GetContainerByName()` returns a specific error:

```go
container, err := client.GetContainerByName(ctx, "missing")
// err.Error() == "container not found: missing"
```

### Compose Errors

Compose operations include command output on failure:

```go
err := compose.Up(ctx)
// "docker compose up: exit status 1\n<command output>"
```

## Testing

### MockDockerAPI

The package includes a comprehensive mock implementation in `mock_test.go`:

```go
type MockDockerAPI struct {
    // Function overrides
    PingFunc             func(ctx context.Context) (types.Ping, error)
    ContainerListFunc    func(ctx context.Context, options container.ListOptions) ([]types.Container, error)
    ContainerInspectFunc func(ctx context.Context, containerID string) (types.ContainerJSON, error)
    ContainerLogsFunc    func(ctx context.Context, container string, options container.LogsOptions) (io.ReadCloser, error)
    ContainerStartFunc   func(ctx context.Context, containerID string, options container.StartOptions) error
    ContainerRestartFunc func(ctx context.Context, containerID string, options container.StopOptions) error
    ContainerRemoveFunc  func(ctx context.Context, containerID string, options container.RemoveOptions) error
    ContainerStatsFunc   func(ctx context.Context, containerID string, stream bool) (container.StatsResponseReader, error)
    DiskUsageFunc        func(ctx context.Context, options types.DiskUsageOptions) (types.DiskUsage, error)
    InfoFunc             func(ctx context.Context) (system.Info, error)
    CloseFunc            func() error

    // Call tracking
    PingCalls             int
    ContainerListCalls    int
    ContainerInspectCalls int
    // ... etc
}
```

### Writing Tests

**Basic test structure:**

```go
func TestClient_ListContainers(t *testing.T) {
    tests := []struct {
        name        string
        runningOnly bool
        setup       func(*MockDockerAPI)
        want        []ContainerInfo
        wantErr     bool
    }{
        {
            name:        "single running container",
            runningOnly: true,
            setup: func(m *MockDockerAPI) {
                m.ContainerListFunc = func(ctx context.Context, options container.ListOptions) ([]types.Container, error) {
                    return []types.Container{
                        makeTestContainer("abc123", "web", "nginx:latest", "running"),
                    }, nil
                }
                m.ContainerInspectFunc = func(ctx context.Context, containerID string) (types.ContainerJSON, error) {
                    return makeTestContainerJSONWithHealth("abc123", "web", "nginx:latest", "running", "healthy", true), nil
                }
            },
            want: []ContainerInfo{{Name: "web", State: "running", Health: "healthy"}},
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            mock := NewMockDockerAPI()
            tt.setup(mock)
            client := NewClientWithAPI(mock)

            got, err := client.ListContainers(context.Background(), tt.runningOnly)
            if tt.wantErr {
                require.Error(t, err)
            } else {
                require.NoError(t, err)
                // assertions...
            }
        })
    }
}
```

### Test Helpers

The mock package includes helpers for creating test data:

```go
// Create a test container for listing
container := makeTestContainer("abc123456789", "web", "nginx:latest", "running")

// Create a test ContainerJSON for inspection
json := makeTestContainerJSON("abc123456789", "web", "nginx:latest", "running", true)

// Create a test ContainerJSON with health check
json := makeTestContainerJSONWithHealth("abc123456789", "web", "nginx:latest", "running", "healthy", true)

// Create test stats JSON
statsData := makeStatsJSON(
    200000000,   // cpu total
    2000000000,  // cpu system
    100000000,   // pre-cpu total
    1000000000,  // pre-cpu system
    536870912,   // mem usage (512MB)
    1073741824,  // mem limit (1GB)
    4,           // cpu count
)
```

### Call Verification

Verify the mock was called correctly:

```go
mock := NewMockDockerAPI()
// ... run test ...

assert.Equal(t, 1, mock.ContainerListCalls)
assert.Equal(t, 2, mock.ContainerInspectCalls)
```

Reset counters between test cases:

```go
mock.Reset()
```

### Common Test Errors

Predefined error variables for consistent testing:

```go
var (
    errMockPing      = errors.New("mock: ping failed")
    errMockList      = errors.New("mock: container list failed")
    errMockInspect   = errors.New("mock: container inspect failed")
    errMockLogs      = errors.New("mock: container logs failed")
    errMockStart     = errors.New("mock: container start failed")
    errMockRestart   = errors.New("mock: container restart failed")
    errMockRemove    = errors.New("mock: container remove failed")
    errMockStats     = errors.New("mock: container stats failed")
    errMockDiskUsage = errors.New("mock: disk usage failed")
    errMockInfo      = errors.New("mock: info failed")
    errMockNotFound  = errors.New("mock: container not found")
)
```

## Performance

### Stats Collection

`GetContainerStats()` uses one-shot mode (no streaming) for efficient single-point stats:

```go
stats, err := c.api.ContainerStats(ctx, name, false) // stream=false
```

The stats JSON is parsed once and the body is immediately closed.

### CPU Calculation

CPU percentage is calculated using delta values between the current and previous sample:

```go
cpuDelta := float64(v.CPUStats.CPUUsage.TotalUsage - v.PreCPUStats.CPUUsage.TotalUsage)
systemDelta := float64(v.CPUStats.SystemUsage - v.PreCPUStats.SystemUsage)
cpuPercent := (cpuDelta / systemDelta) * float64(numCPUs) * 100.0
```

This matches the calculation used by `docker stats`.

### Memory Calculation

Memory percentage is straightforward:

```go
memPercent := float64(usage) / float64(limit) * 100.0
```

### Batch Stats Collection

`GetAllContainerStats()` collects stats for all running containers:

```go
stats, err := client.GetAllContainerStats(ctx)
```

This method:

- Lists only running containers (`runningOnly=true`)
- Collects stats for each container sequentially
- Skips containers that fail (does not abort on individual errors)
- Returns all successfully collected stats

For large deployments, consider parallelizing stats collection or implementing sampling.

### Disk Usage

System-wide disk usage is available via:

```go
usage, err := client.DiskUsage(ctx)
fmt.Printf("Layers: %d bytes\n", usage.LayersSize)
```

## System Information

Get Docker daemon information:

```go
info, err := client.Info(ctx)
fmt.Printf("Containers: %d\n", info.Containers)
fmt.Printf("Images: %d\n", info.Images)
```

Access the raw Docker SDK client for advanced operations:

```go
rawClient := client.Raw()
// Use rawClient for operations not covered by the wrapper
```

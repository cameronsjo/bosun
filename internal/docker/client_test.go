package docker

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/system"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ensure types import is used for DiskUsage tests.
var _ types.DiskUsage

func TestNewClientWithAPI(t *testing.T) {
	mock := NewMockDockerAPI()
	client := NewClientWithAPI(mock)

	assert.NotNil(t, client)
	assert.Equal(t, mock, client.api)
}

func TestClient_Ping(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*MockDockerAPI)
		wantErr bool
		errMsg  string
	}{
		{
			name: "success",
			setup: func(m *MockDockerAPI) {
				m.PingFunc = func(ctx context.Context) (types.Ping, error) {
					return types.Ping{APIVersion: "1.45"}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "failure",
			setup: func(m *MockDockerAPI) {
				m.PingFunc = func(ctx context.Context) (types.Ping, error) {
					return types.Ping{}, errMockPing
				}
			},
			wantErr: true,
			errMsg:  "ping docker",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := NewMockDockerAPI()
			tt.setup(mock)
			client := NewClientWithAPI(mock)

			err := client.Ping(context.Background())
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, 1, mock.PingCalls)
		})
	}
}

func TestClient_Info(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*MockDockerAPI)
		want    system.Info
		wantErr bool
	}{
		{
			name: "success",
			setup: func(m *MockDockerAPI) {
				m.InfoFunc = func(ctx context.Context) (system.Info, error) {
					return system.Info{
						ID:         "test-id",
						Containers: 5,
					}, nil
				}
			},
			want: system.Info{
				ID:         "test-id",
				Containers: 5,
			},
			wantErr: false,
		},
		{
			name: "failure",
			setup: func(m *MockDockerAPI) {
				m.InfoFunc = func(ctx context.Context) (system.Info, error) {
					return system.Info{}, errMockInfo
				}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := NewMockDockerAPI()
			tt.setup(mock)
			client := NewClientWithAPI(mock)

			got, err := client.Info(context.Background())
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestClient_Close(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mock := NewMockDockerAPI()
		mock.CloseFunc = func() error {
			return nil
		}
		client := NewClientWithAPI(mock)

		err := client.Close()
		require.NoError(t, err)
		assert.Equal(t, 1, mock.CloseCalls)
	})

	t.Run("nil api", func(t *testing.T) {
		client := &Client{api: nil}
		err := client.Close()
		require.NoError(t, err)
	})
}

func TestClient_ListContainers(t *testing.T) {
	tests := []struct {
		name        string
		runningOnly bool
		setup       func(*MockDockerAPI)
		want        []ContainerInfo
		wantErr     bool
	}{
		{
			name:        "empty list",
			runningOnly: false,
			setup: func(m *MockDockerAPI) {
				m.ContainerListFunc = func(ctx context.Context, options container.ListOptions) ([]container.Summary, error) {
					return []container.Summary{}, nil
				}
			},
			want:    nil,
			wantErr: false,
		},
		{
			name:        "single running container",
			runningOnly: true,
			setup: func(m *MockDockerAPI) {
				m.ContainerListFunc = func(ctx context.Context, options container.ListOptions) ([]container.Summary, error) {
					return []container.Summary{
						makeTestContainer("abc123456789", "web", "nginx:latest", "running"),
					}, nil
				}
				m.ContainerInspectFunc = func(ctx context.Context, containerID string) (container.InspectResponse, error) {
					return makeTestContainerJSONWithHealth("abc123456789", "web", "nginx:latest", "running", "healthy", true), nil
				}
			},
			want: []ContainerInfo{
				{
					ID:     "abc123456789",
					Name:   "web",
					Image:  "nginx:latest",
					State:  "running",
					Status: "Up 10 minutes",
					Health: "healthy",
					Ports:  []string{"8080:80/tcp"},
				},
			},
			wantErr: false,
		},
		{
			name:        "multiple containers with mixed states",
			runningOnly: false,
			setup: func(m *MockDockerAPI) {
				m.ContainerListFunc = func(ctx context.Context, options container.ListOptions) ([]container.Summary, error) {
					return []container.Summary{
						makeTestContainer("abc123456789", "web", "nginx:latest", "running"),
						makeTestContainer("def123456789", "db", "postgres:15", "exited"),
					}, nil
				}
				m.ContainerInspectFunc = func(ctx context.Context, containerID string) (container.InspectResponse, error) {
					if containerID == "abc1234567890000000000000000" {
						return makeTestContainerJSONWithHealth("abc123456789", "web", "nginx:latest", "running", "healthy", true), nil
					}
					return container.InspectResponse{}, nil
				}
			},
			wantErr: false,
		},
		{
			name:        "list error",
			runningOnly: false,
			setup: func(m *MockDockerAPI) {
				m.ContainerListFunc = func(ctx context.Context, options container.ListOptions) ([]container.Summary, error) {
					return nil, errMockList
				}
			},
			wantErr: true,
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
				if tt.want != nil {
					assert.Len(t, got, len(tt.want))
				}
			}
		})
	}
}

func TestClient_CountContainers(t *testing.T) {
	tests := []struct {
		name          string
		setup         func(*MockDockerAPI)
		wantRunning   int
		wantTotal     int
		wantUnhealthy int
		wantErr       bool
	}{
		{
			name: "no containers",
			setup: func(m *MockDockerAPI) {
				m.ContainerListFunc = func(ctx context.Context, options container.ListOptions) ([]container.Summary, error) {
					return []container.Summary{}, nil
				}
			},
			wantRunning:   0,
			wantTotal:     0,
			wantUnhealthy: 0,
			wantErr:       false,
		},
		{
			name: "mixed states",
			setup: func(m *MockDockerAPI) {
				m.ContainerListFunc = func(ctx context.Context, options container.ListOptions) ([]container.Summary, error) {
					return []container.Summary{
						makeTestContainer("abc123456789", "web", "nginx:latest", "running"),
						makeTestContainer("def123456789", "api", "app:latest", "running"),
						makeTestContainer("ghi123456789", "db", "postgres:15", "exited"),
					}, nil
				}
				m.ContainerInspectFunc = func(ctx context.Context, containerID string) (container.InspectResponse, error) {
					if containerID == "abc1234567890000000000000000" {
						return makeTestContainerJSONWithHealth("abc123456789", "web", "nginx:latest", "running", "healthy", true), nil
					}
					if containerID == "def1234567890000000000000000" {
						return makeTestContainerJSONWithHealth("def123456789", "api", "app:latest", "running", "unhealthy", true), nil
					}
					return container.InspectResponse{}, nil
				}
			},
			wantRunning:   2,
			wantTotal:     3,
			wantUnhealthy: 1,
			wantErr:       false,
		},
		{
			name: "list error",
			setup: func(m *MockDockerAPI) {
				m.ContainerListFunc = func(ctx context.Context, options container.ListOptions) ([]container.Summary, error) {
					return nil, errMockList
				}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := NewMockDockerAPI()
			tt.setup(mock)
			client := NewClientWithAPI(mock)

			running, total, unhealthy, err := client.CountContainers(context.Background())
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantRunning, running)
				assert.Equal(t, tt.wantTotal, total)
				assert.Equal(t, tt.wantUnhealthy, unhealthy)
			}
		})
	}
}

func TestClient_GetContainerByName(t *testing.T) {
	tests := []struct {
		name      string
		container string
		setup     func(*MockDockerAPI)
		wantName  string
		wantErr   bool
	}{
		{
			name:      "found",
			container: "web",
			setup: func(m *MockDockerAPI) {
				m.ContainerListFunc = func(ctx context.Context, options container.ListOptions) ([]container.Summary, error) {
					return []container.Summary{
						makeTestContainer("abc123456789", "web", "nginx:latest", "running"),
					}, nil
				}
				m.ContainerInspectFunc = func(ctx context.Context, containerID string) (container.InspectResponse, error) {
					return makeTestContainerJSON("abc123456789", "web", "nginx:latest", "running", true), nil
				}
			},
			wantName: "web",
			wantErr:  false,
		},
		{
			name:      "not found",
			container: "missing",
			setup: func(m *MockDockerAPI) {
				m.ContainerListFunc = func(ctx context.Context, options container.ListOptions) ([]container.Summary, error) {
					return []container.Summary{
						makeTestContainer("abc123456789", "web", "nginx:latest", "running"),
					}, nil
				}
				m.ContainerInspectFunc = func(ctx context.Context, containerID string) (container.InspectResponse, error) {
					return makeTestContainerJSON("abc123456789", "web", "nginx:latest", "running", true), nil
				}
			},
			wantErr: true,
		},
		{
			name:      "list error",
			container: "web",
			setup: func(m *MockDockerAPI) {
				m.ContainerListFunc = func(ctx context.Context, options container.ListOptions) ([]container.Summary, error) {
					return nil, errMockList
				}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := NewMockDockerAPI()
			tt.setup(mock)
			client := NewClientWithAPI(mock)

			got, err := client.GetContainerByName(context.Background(), tt.container)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantName, got.Name)
			}
		})
	}
}

func TestClient_IsContainerRunning(t *testing.T) {
	tests := []struct {
		name      string
		container string
		setup     func(*MockDockerAPI)
		want      bool
	}{
		{
			name:      "running",
			container: "web",
			setup: func(m *MockDockerAPI) {
				m.ContainerListFunc = func(ctx context.Context, options container.ListOptions) ([]container.Summary, error) {
					return []container.Summary{
						makeTestContainer("abc123456789", "web", "nginx:latest", "running"),
					}, nil
				}
				m.ContainerInspectFunc = func(ctx context.Context, containerID string) (container.InspectResponse, error) {
					return makeTestContainerJSON("abc123456789", "web", "nginx:latest", "running", true), nil
				}
			},
			want: true,
		},
		{
			name:      "stopped",
			container: "web",
			setup: func(m *MockDockerAPI) {
				m.ContainerListFunc = func(ctx context.Context, options container.ListOptions) ([]container.Summary, error) {
					return []container.Summary{
						makeTestContainer("abc123456789", "web", "nginx:latest", "exited"),
					}, nil
				}
			},
			want: false,
		},
		{
			name:      "not found",
			container: "missing",
			setup: func(m *MockDockerAPI) {
				m.ContainerListFunc = func(ctx context.Context, options container.ListOptions) ([]container.Summary, error) {
					return []container.Summary{}, nil
				}
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := NewMockDockerAPI()
			tt.setup(mock)
			client := NewClientWithAPI(mock)

			got := client.IsContainerRunning(context.Background(), tt.container)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestClient_GetContainerImage(t *testing.T) {
	tests := []struct {
		name      string
		container string
		setup     func(*MockDockerAPI)
		wantImage string
		wantErr   bool
	}{
		{
			name:      "found",
			container: "web",
			setup: func(m *MockDockerAPI) {
				m.ContainerListFunc = func(ctx context.Context, options container.ListOptions) ([]container.Summary, error) {
					return []container.Summary{
						makeTestContainer("abc123456789", "web", "nginx:latest", "running"),
					}, nil
				}
				m.ContainerInspectFunc = func(ctx context.Context, containerID string) (container.InspectResponse, error) {
					return makeTestContainerJSON("abc123456789", "web", "nginx:latest", "running", true), nil
				}
			},
			wantImage: "nginx:latest",
			wantErr:   false,
		},
		{
			name:      "not found",
			container: "missing",
			setup: func(m *MockDockerAPI) {
				m.ContainerListFunc = func(ctx context.Context, options container.ListOptions) ([]container.Summary, error) {
					return []container.Summary{}, nil
				}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := NewMockDockerAPI()
			tt.setup(mock)
			client := NewClientWithAPI(mock)

			got, err := client.GetContainerImage(context.Background(), tt.container)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantImage, got)
			}
		})
	}
}

func TestClient_RemoveContainer(t *testing.T) {
	tests := []struct {
		name      string
		container string
		setup     func(*MockDockerAPI)
		wantErr   bool
	}{
		{
			name:      "success",
			container: "web",
			setup: func(m *MockDockerAPI) {
				m.ContainerRemoveFunc = func(ctx context.Context, containerID string, options container.RemoveOptions) error {
					return nil
				}
			},
			wantErr: false,
		},
		{
			name:      "failure",
			container: "web",
			setup: func(m *MockDockerAPI) {
				m.ContainerRemoveFunc = func(ctx context.Context, containerID string, options container.RemoveOptions) error {
					return errMockRemove
				}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := NewMockDockerAPI()
			tt.setup(mock)
			client := NewClientWithAPI(mock)

			err := client.RemoveContainer(context.Background(), tt.container)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, 1, mock.ContainerRemoveCalls)
		})
	}
}

func TestClient_RestartContainer(t *testing.T) {
	tests := []struct {
		name      string
		container string
		setup     func(*MockDockerAPI)
		wantErr   bool
	}{
		{
			name:      "success",
			container: "web",
			setup: func(m *MockDockerAPI) {
				m.ContainerRestartFunc = func(ctx context.Context, containerID string, options container.StopOptions) error {
					return nil
				}
			},
			wantErr: false,
		},
		{
			name:      "failure",
			container: "web",
			setup: func(m *MockDockerAPI) {
				m.ContainerRestartFunc = func(ctx context.Context, containerID string, options container.StopOptions) error {
					return errMockRestart
				}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := NewMockDockerAPI()
			tt.setup(mock)
			client := NewClientWithAPI(mock)

			err := client.RestartContainer(context.Background(), tt.container)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, 1, mock.ContainerRestartCalls)
		})
	}
}

func TestClient_GetContainerLogs(t *testing.T) {
	tests := []struct {
		name      string
		container string
		tail      int
		setup     func(*MockDockerAPI)
		want      string
		wantErr   bool
	}{
		{
			name:      "success",
			container: "web",
			tail:      100,
			setup: func(m *MockDockerAPI) {
				m.ContainerLogsFunc = func(ctx context.Context, containerName string, options container.LogsOptions) (io.ReadCloser, error) {
					return io.NopCloser(bytes.NewReader([]byte("log line 1\nlog line 2\n"))), nil
				}
			},
			want:    "log line 1\nlog line 2\n",
			wantErr: false,
		},
		{
			name:      "failure",
			container: "web",
			tail:      100,
			setup: func(m *MockDockerAPI) {
				m.ContainerLogsFunc = func(ctx context.Context, containerName string, options container.LogsOptions) (io.ReadCloser, error) {
					return nil, errMockLogs
				}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := NewMockDockerAPI()
			tt.setup(mock)
			client := NewClientWithAPI(mock)

			got, err := client.GetContainerLogs(context.Background(), tt.container, tt.tail)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestClient_GetContainerStats(t *testing.T) {
	tests := []struct {
		name      string
		container string
		setup     func(*MockDockerAPI)
		wantCPU   float64
		wantMem   uint64
		wantErr   bool
	}{
		{
			name:      "success with usage",
			container: "web",
			setup: func(m *MockDockerAPI) {
				m.ContainerStatsFunc = func(ctx context.Context, containerID string, stream bool) (container.StatsResponseReader, error) {
					// CPU: 100 delta out of 1000 system = 10% per core, 4 cores = 40%
					// Memory: 512MB / 1GB = 50%
					stats := makeStatsJSON(
						200000000,       // cpu total
						2000000000,      // cpu system
						100000000,       // pre-cpu total
						1000000000,      // pre-cpu system
						536870912,       // mem usage (512MB)
						1073741824,      // mem limit (1GB)
						4,               // cpu count
					)
					return container.StatsResponseReader{
						Body: io.NopCloser(bytes.NewReader(stats)),
					}, nil
				}
			},
			wantCPU: 40.0, // (100M / 1000M) * 4 * 100
			wantMem: 536870912,
			wantErr: false,
		},
		{
			name:      "failure",
			container: "web",
			setup: func(m *MockDockerAPI) {
				m.ContainerStatsFunc = func(ctx context.Context, containerID string, stream bool) (container.StatsResponseReader, error) {
					return container.StatsResponseReader{}, errMockStats
				}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := NewMockDockerAPI()
			tt.setup(mock)
			client := NewClientWithAPI(mock)

			got, err := client.GetContainerStats(context.Background(), tt.container)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.InDelta(t, tt.wantCPU, got.CPUPercent, 0.1)
				assert.Equal(t, tt.wantMem, got.MemUsage)
			}
		})
	}
}

func TestClient_GetAllContainerStats(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(*MockDockerAPI)
		wantCount int
		wantErr   bool
	}{
		{
			name: "multiple containers",
			setup: func(m *MockDockerAPI) {
				m.ContainerListFunc = func(ctx context.Context, options container.ListOptions) ([]container.Summary, error) {
					return []container.Summary{
						makeTestContainer("abc123456789", "web", "nginx:latest", "running"),
						makeTestContainer("def123456789", "api", "app:latest", "running"),
					}, nil
				}
				m.ContainerInspectFunc = func(ctx context.Context, containerID string) (container.InspectResponse, error) {
					return makeTestContainerJSON("abc123456789", "web", "nginx:latest", "running", true), nil
				}
				m.ContainerStatsFunc = func(ctx context.Context, containerID string, stream bool) (container.StatsResponseReader, error) {
					stats := makeStatsJSON(100000000, 1000000000, 50000000, 500000000, 100000000, 200000000, 2)
					return container.StatsResponseReader{
						Body: io.NopCloser(bytes.NewReader(stats)),
					}, nil
				}
			},
			wantCount: 2,
			wantErr:   false,
		},
		{
			name: "skip failed stats",
			setup: func(m *MockDockerAPI) {
				callCount := 0
				m.ContainerListFunc = func(ctx context.Context, options container.ListOptions) ([]container.Summary, error) {
					return []container.Summary{
						makeTestContainer("abc123456789", "web", "nginx:latest", "running"),
						makeTestContainer("def123456789", "api", "app:latest", "running"),
					}, nil
				}
				m.ContainerInspectFunc = func(ctx context.Context, containerID string) (container.InspectResponse, error) {
					return makeTestContainerJSON("abc123456789", "web", "nginx:latest", "running", true), nil
				}
				m.ContainerStatsFunc = func(ctx context.Context, containerID string, stream bool) (container.StatsResponseReader, error) {
					callCount++
					if callCount == 1 {
						return container.StatsResponseReader{}, errMockStats
					}
					stats := makeStatsJSON(100000000, 1000000000, 50000000, 500000000, 100000000, 200000000, 2)
					return container.StatsResponseReader{
						Body: io.NopCloser(bytes.NewReader(stats)),
					}, nil
				}
			},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name: "list error",
			setup: func(m *MockDockerAPI) {
				m.ContainerListFunc = func(ctx context.Context, options container.ListOptions) ([]container.Summary, error) {
					return nil, errMockList
				}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := NewMockDockerAPI()
			tt.setup(mock)
			client := NewClientWithAPI(mock)

			got, err := client.GetAllContainerStats(context.Background())
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Len(t, got, tt.wantCount)
			}
		})
	}
}

func TestClient_DiskUsage(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*MockDockerAPI)
		want    types.DiskUsage
		wantErr bool
	}{
		{
			name: "success",
			setup: func(m *MockDockerAPI) {
				m.DiskUsageFunc = func(ctx context.Context, options types.DiskUsageOptions) (types.DiskUsage, error) {
					return types.DiskUsage{
						LayersSize: 1073741824, // 1GB
					}, nil
				}
			},
			want: types.DiskUsage{
				LayersSize: 1073741824,
			},
			wantErr: false,
		},
		{
			name: "failure",
			setup: func(m *MockDockerAPI) {
				m.DiskUsageFunc = func(ctx context.Context, options types.DiskUsageOptions) (types.DiskUsage, error) {
					return types.DiskUsage{}, errMockDiskUsage
				}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := NewMockDockerAPI()
			tt.setup(mock)
			client := NewClientWithAPI(mock)

			got, err := client.DiskUsage(context.Background())
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want.LayersSize, got.LayersSize)
			}
		})
	}
}

func TestReadJSONStats(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		wantErr bool
	}{
		{
			name:    "valid JSON",
			input:   makeStatsJSON(100, 200, 50, 100, 1000, 2000, 2),
			wantErr: false,
		},
		{
			name:    "invalid JSON",
			input:   []byte("not json"),
			wantErr: true,
		},
		{
			name:    "empty JSON",
			input:   []byte("{}"),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var v statsJSON
			err := readJSONStats(bytes.NewReader(tt.input), &v)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestReadJSONStats_ReaderError(t *testing.T) {
	var v statsJSON
	err := readJSONStats(&errorReader{}, &v)
	require.Error(t, err)
}

// errorReader is a reader that always returns an error.
type errorReader struct{}

func (e *errorReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("read error")
}

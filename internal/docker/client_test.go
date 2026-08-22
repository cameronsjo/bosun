package docker

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/system"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
				m.PingFunc = func(ctx context.Context, options client.PingOptions) (client.PingResult, error) {
					return client.PingResult{APIVersion: "1.45"}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "failure",
			setup: func(m *MockDockerAPI) {
				m.PingFunc = func(ctx context.Context, options client.PingOptions) (client.PingResult, error) {
					return client.PingResult{}, errMockPing
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

func TestClient_PingPreservesTighterCallerDeadline(t *testing.T) {
	parentDeadline := time.Now().Add(100 * time.Millisecond)
	ctx, cancel := context.WithDeadline(context.Background(), parentDeadline)
	defer cancel()

	mock := NewMockDockerAPI()
	mock.PingFunc = func(ctx context.Context, options client.PingOptions) (client.PingResult, error) {
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		assert.Equal(t, parentDeadline, deadline)
		return client.PingResult{}, nil
	}

	err := NewClientWithAPI(mock).Ping(ctx)
	require.NoError(t, err)
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
				m.InfoFunc = func(ctx context.Context, options client.InfoOptions) (client.SystemInfoResult, error) {
					return client.SystemInfoResult{
						Info: system.Info{
							ID:         "test-id",
							Containers: 5,
						},
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
				m.InfoFunc = func(ctx context.Context, options client.InfoOptions) (client.SystemInfoResult, error) {
					return client.SystemInfoResult{}, errMockInfo
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
				m.ContainerListFunc = func(ctx context.Context, options client.ContainerListOptions) (client.ContainerListResult, error) {
					return client.ContainerListResult{Items: []container.Summary{}}, nil
				}
			},
			want:    nil,
			wantErr: false,
		},
		{
			name:        "single running container",
			runningOnly: true,
			setup: func(m *MockDockerAPI) {
				m.ContainerListFunc = func(ctx context.Context, options client.ContainerListOptions) (client.ContainerListResult, error) {
					return client.ContainerListResult{Items: []container.Summary{
						makeTestContainerWithHealth("abc123456789", "web", "nginx:latest", "running", "healthy"),
					}}, nil
				}
			},
			want: []ContainerInfo{
				{
					ID:     "abc123456789",
					Name:   "web",
					Image:  "nginx:latest",
					State:  "running",
					Status: "Up 10 minutes (healthy)",
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
				m.ContainerListFunc = func(ctx context.Context, options client.ContainerListOptions) (client.ContainerListResult, error) {
					return client.ContainerListResult{Items: []container.Summary{
						makeTestContainerWithHealth("abc123456789", "web", "nginx:latest", "running", "healthy"),
						makeTestContainer("def123456789", "db", "postgres:15", "exited"),
					}}, nil
				}
			},
			wantErr: false,
		},
		{
			name:        "list error",
			runningOnly: false,
			setup: func(m *MockDockerAPI) {
				m.ContainerListFunc = func(ctx context.Context, options client.ContainerListOptions) (client.ContainerListResult, error) {
					return client.ContainerListResult{}, errMockList
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
				m.ContainerListFunc = func(ctx context.Context, options client.ContainerListOptions) (client.ContainerListResult, error) {
					return client.ContainerListResult{Items: []container.Summary{}}, nil
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
				m.ContainerListFunc = func(ctx context.Context, options client.ContainerListOptions) (client.ContainerListResult, error) {
					return client.ContainerListResult{Items: []container.Summary{
						makeTestContainerWithHealth("abc123456789", "web", "nginx:latest", "running", "healthy"),
						makeTestContainerWithHealth("def123456789", "api", "app:latest", "running", "unhealthy"),
						makeTestContainer("ghi123456789", "db", "postgres:15", "exited"),
					}}, nil
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
				m.ContainerListFunc = func(ctx context.Context, options client.ContainerListOptions) (client.ContainerListResult, error) {
					return client.ContainerListResult{}, errMockList
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
				m.ContainerListFunc = func(ctx context.Context, options client.ContainerListOptions) (client.ContainerListResult, error) {
					return client.ContainerListResult{Items: []container.Summary{
						makeTestContainer("abc123456789", "web", "nginx:latest", "running"),
					}}, nil
				}
				m.ContainerInspectFunc = func(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
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
				m.ContainerListFunc = func(ctx context.Context, options client.ContainerListOptions) (client.ContainerListResult, error) {
					return client.ContainerListResult{Items: []container.Summary{
						makeTestContainer("abc123456789", "web", "nginx:latest", "running"),
					}}, nil
				}
				m.ContainerInspectFunc = func(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
					return makeTestContainerJSON("abc123456789", "web", "nginx:latest", "running", true), nil
				}
			},
			wantErr: true,
		},
		{
			name:      "list error",
			container: "web",
			setup: func(m *MockDockerAPI) {
				m.ContainerListFunc = func(ctx context.Context, options client.ContainerListOptions) (client.ContainerListResult, error) {
					return client.ContainerListResult{}, errMockList
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
				m.ContainerListFunc = func(ctx context.Context, options client.ContainerListOptions) (client.ContainerListResult, error) {
					return client.ContainerListResult{Items: []container.Summary{
						makeTestContainer("abc123456789", "web", "nginx:latest", "running"),
					}}, nil
				}
				m.ContainerInspectFunc = func(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
					return makeTestContainerJSON("abc123456789", "web", "nginx:latest", "running", true), nil
				}
			},
			want: true,
		},
		{
			name:      "stopped",
			container: "web",
			setup: func(m *MockDockerAPI) {
				m.ContainerListFunc = func(ctx context.Context, options client.ContainerListOptions) (client.ContainerListResult, error) {
					return client.ContainerListResult{Items: []container.Summary{
						makeTestContainer("abc123456789", "web", "nginx:latest", "exited"),
					}}, nil
				}
			},
			want: false,
		},
		{
			name:      "not found",
			container: "missing",
			setup: func(m *MockDockerAPI) {
				m.ContainerListFunc = func(ctx context.Context, options client.ContainerListOptions) (client.ContainerListResult, error) {
					return client.ContainerListResult{Items: []container.Summary{}}, nil
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
				m.ContainerListFunc = func(ctx context.Context, options client.ContainerListOptions) (client.ContainerListResult, error) {
					return client.ContainerListResult{Items: []container.Summary{
						makeTestContainer("abc123456789", "web", "nginx:latest", "running"),
					}}, nil
				}
				m.ContainerInspectFunc = func(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
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
				m.ContainerListFunc = func(ctx context.Context, options client.ContainerListOptions) (client.ContainerListResult, error) {
					return client.ContainerListResult{Items: []container.Summary{}}, nil
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
				m.ContainerRemoveFunc = func(ctx context.Context, containerID string, options client.ContainerRemoveOptions) (client.ContainerRemoveResult, error) {
					return client.ContainerRemoveResult{}, nil
				}
			},
			wantErr: false,
		},
		{
			name:      "failure",
			container: "web",
			setup: func(m *MockDockerAPI) {
				m.ContainerRemoveFunc = func(ctx context.Context, containerID string, options client.ContainerRemoveOptions) (client.ContainerRemoveResult, error) {
					return client.ContainerRemoveResult{}, errMockRemove
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

func TestClient_StopContainer(t *testing.T) {
	tests := []struct {
		name      string
		container string
		timeout   int
		setup     func(*MockDockerAPI)
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "success",
			container: "web",
			timeout:   30,
			setup: func(m *MockDockerAPI) {
				m.ContainerStopFunc = func(ctx context.Context, containerID string, options client.ContainerStopOptions) (client.ContainerStopResult, error) {
					assert.Equal(t, "web", containerID)
					require.NotNil(t, options.Timeout)
					assert.Equal(t, 30, *options.Timeout)
					return client.ContainerStopResult{}, nil
				}
			},
			wantErr: false,
		},
		{
			name:      "custom timeout",
			container: "api",
			timeout:   120,
			setup: func(m *MockDockerAPI) {
				m.ContainerStopFunc = func(ctx context.Context, containerID string, options client.ContainerStopOptions) (client.ContainerStopResult, error) {
					require.NotNil(t, options.Timeout)
					assert.Equal(t, 120, *options.Timeout)
					return client.ContainerStopResult{}, nil
				}
			},
			wantErr: false,
		},
		{
			name:      "failure",
			container: "web",
			timeout:   10,
			setup: func(m *MockDockerAPI) {
				m.ContainerStopFunc = func(ctx context.Context, containerID string, options client.ContainerStopOptions) (client.ContainerStopResult, error) {
					return client.ContainerStopResult{}, errors.New("mock: container stop failed")
				}
			},
			wantErr: true,
			errMsg:  "stop container web",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := NewMockDockerAPI()
			tt.setup(mock)
			client := NewClientWithAPI(mock)

			err := client.StopContainer(context.Background(), tt.container, tt.timeout)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, 1, mock.ContainerStopCalls)
		})
	}
}

func TestClient_RestartContainer(t *testing.T) {
	tests := []struct {
		name        string
		container   string
		timeout     []int
		wantTimeout int
		setup       func(*MockDockerAPI)
		wantErr     bool
	}{
		{
			name:        "success with default timeout",
			container:   "web",
			timeout:     nil,
			wantTimeout: 10,
			setup: func(m *MockDockerAPI) {
				m.ContainerRestartFunc = func(ctx context.Context, containerID string, options client.ContainerRestartOptions) (client.ContainerRestartResult, error) {
					require.NotNil(t, options.Timeout)
					assert.Equal(t, 10, *options.Timeout)
					return client.ContainerRestartResult{}, nil
				}
			},
			wantErr: false,
		},
		{
			name:        "success with custom timeout",
			container:   "web",
			timeout:     []int{60},
			wantTimeout: 60,
			setup: func(m *MockDockerAPI) {
				m.ContainerRestartFunc = func(ctx context.Context, containerID string, options client.ContainerRestartOptions) (client.ContainerRestartResult, error) {
					require.NotNil(t, options.Timeout)
					assert.Equal(t, 60, *options.Timeout)
					return client.ContainerRestartResult{}, nil
				}
			},
			wantErr: false,
		},
		{
			name:        "failure",
			container:   "web",
			timeout:     nil,
			wantTimeout: 10,
			setup: func(m *MockDockerAPI) {
				m.ContainerRestartFunc = func(ctx context.Context, containerID string, options client.ContainerRestartOptions) (client.ContainerRestartResult, error) {
					return client.ContainerRestartResult{}, errMockRestart
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

			err := client.RestartContainer(context.Background(), tt.container, tt.timeout...)
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
				m.ContainerLogsFunc = func(ctx context.Context, containerName string, options client.ContainerLogsOptions) (client.ContainerLogsResult, error) {
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
				m.ContainerLogsFunc = func(ctx context.Context, containerName string, options client.ContainerLogsOptions) (client.ContainerLogsResult, error) {
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

func TestClient_GetContainerLogs_CancellationClosesBlockedStream(t *testing.T) {
	server, stream := net.Pipe()
	defer func() { _ = server.Close() }()

	streamReturned := make(chan struct{})
	mock := NewMockDockerAPI()
	mock.ContainerLogsFunc = func(ctx context.Context, containerName string, options client.ContainerLogsOptions) (client.ContainerLogsResult, error) {
		close(streamReturned)
		return stream, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := NewClientWithAPI(mock).GetContainerLogs(ctx, "web", 100)
		result <- err
	}()

	<-streamReturned
	cancel()
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("GetContainerLogs did not stop after cancellation")
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
				m.ContainerStatsFunc = func(ctx context.Context, containerID string, options client.ContainerStatsOptions) (client.ContainerStatsResult, error) {
					// CPU: 100 delta out of 1000 system = 10% per core, 4 cores = 40%
					// Memory: 512MB / 1GB = 50%
					stats := makeStatsJSON(
						200000000,  // cpu total
						2000000000, // cpu system
						100000000,  // pre-cpu total
						1000000000, // pre-cpu system
						536870912,  // mem usage (512MB)
						1073741824, // mem limit (1GB)
						4,          // cpu count
					)
					return client.ContainerStatsResult{
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
				m.ContainerStatsFunc = func(ctx context.Context, containerID string, options client.ContainerStatsOptions) (client.ContainerStatsResult, error) {
					return client.ContainerStatsResult{}, errMockStats
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

func TestClient_GetContainerStats_CancellationInterruptsDrain(t *testing.T) {
	server, stream := net.Pipe()
	defer func() { _ = server.Close() }()

	mock := NewMockDockerAPI()
	mock.ContainerStatsFunc = func(ctx context.Context, containerID string, options client.ContainerStatsOptions) (client.ContainerStatsResult, error) {
		return client.ContainerStatsResult{Body: stream}, nil
	}

	writeDone := make(chan struct{})
	go func() {
		_, _ = server.Write(makeStatsJSON(200, 2000, 100, 1000, 512, 1024, 2))
		close(writeDone)
	}()

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := NewClientWithAPI(mock).GetContainerStats(ctx, "web")
		result <- err
	}()

	<-writeDone
	cancel()
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("GetContainerStats did not stop draining after cancellation")
	}
}

func TestClient_ExecContainer_CancellationClosesBlockedStream(t *testing.T) {
	server, stream := net.Pipe()
	defer func() { _ = server.Close() }()

	streamReturned := make(chan struct{})
	mock := NewMockDockerAPI()
	mock.ExecAttachFunc = func(ctx context.Context, execID string, options client.ExecAttachOptions) (client.ExecAttachResult, error) {
		close(streamReturned)
		return client.ExecAttachResult{HijackedResponse: client.HijackedResponse{
			Conn:   stream,
			Reader: bufio.NewReader(stream),
		}}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- NewClientWithAPI(mock).ExecContainer(ctx, "web", []string{"true"})
	}()

	<-streamReturned
	cancel()
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("ExecContainer did not stop after cancellation")
	}
	assert.Zero(t, mock.ExecInspectCalls)
}

func TestClient_ExecContainer_DrainsThenInspects(t *testing.T) {
	mock := NewMockDockerAPI()

	err := NewClientWithAPI(mock).ExecContainer(context.Background(), "web", []string{"true"})

	require.NoError(t, err)
	assert.Equal(t, 1, mock.ExecCreateCalls)
	assert.Equal(t, 1, mock.ExecAttachCalls)
	assert.Equal(t, 1, mock.ExecInspectCalls)
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
				m.ContainerListFunc = func(ctx context.Context, options client.ContainerListOptions) (client.ContainerListResult, error) {
					return client.ContainerListResult{Items: []container.Summary{
						makeTestContainer("abc123456789", "web", "nginx:latest", "running"),
						makeTestContainer("def123456789", "api", "app:latest", "running"),
					}}, nil
				}
				m.ContainerInspectFunc = func(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
					return makeTestContainerJSON("abc123456789", "web", "nginx:latest", "running", true), nil
				}
				m.ContainerStatsFunc = func(ctx context.Context, containerID string, options client.ContainerStatsOptions) (client.ContainerStatsResult, error) {
					stats := makeStatsJSON(100000000, 1000000000, 50000000, 500000000, 100000000, 200000000, 2)
					return client.ContainerStatsResult{
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
				m.ContainerListFunc = func(ctx context.Context, options client.ContainerListOptions) (client.ContainerListResult, error) {
					return client.ContainerListResult{Items: []container.Summary{
						makeTestContainer("abc123456789", "web", "nginx:latest", "running"),
						makeTestContainer("def123456789", "api", "app:latest", "running"),
					}}, nil
				}
				m.ContainerInspectFunc = func(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
					return makeTestContainerJSON("abc123456789", "web", "nginx:latest", "running", true), nil
				}
				m.ContainerStatsFunc = func(ctx context.Context, containerID string, options client.ContainerStatsOptions) (client.ContainerStatsResult, error) {
					callCount++
					if callCount == 1 {
						return client.ContainerStatsResult{}, errMockStats
					}
					stats := makeStatsJSON(100000000, 1000000000, 50000000, 500000000, 100000000, 200000000, 2)
					return client.ContainerStatsResult{
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
				m.ContainerListFunc = func(ctx context.Context, options client.ContainerListOptions) (client.ContainerListResult, error) {
					return client.ContainerListResult{}, errMockList
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
		want    client.DiskUsageResult
		wantErr bool
	}{
		{
			name: "success",
			setup: func(m *MockDockerAPI) {
				m.DiskUsageFunc = func(ctx context.Context, options client.DiskUsageOptions) (client.DiskUsageResult, error) {
					return client.DiskUsageResult{
						Images: client.ImagesDiskUsage{TotalSize: 1073741824}, // 1GB
					}, nil
				}
			},
			want: client.DiskUsageResult{
				Images: client.ImagesDiskUsage{TotalSize: 1073741824},
			},
			wantErr: false,
		},
		{
			name: "failure",
			setup: func(m *MockDockerAPI) {
				m.DiskUsageFunc = func(ctx context.Context, options client.DiskUsageOptions) (client.DiskUsageResult, error) {
					return client.DiskUsageResult{}, errMockDiskUsage
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
				assert.Equal(t, tt.want.Images.TotalSize, got.Images.TotalSize)
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

func TestReadWithContext_AlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	closed := false
	read := false
	err := readWithContext(ctx, func() { closed = true }, func() error {
		read = true
		return nil
	})

	require.ErrorIs(t, err, context.Canceled)
	assert.True(t, closed)
	assert.False(t, read)
}

// errorReader is a reader that always returns an error.
type errorReader struct{}

func (e *errorReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("read error")
}

func TestClient_GetContainerLogs_ReadError(t *testing.T) {
	mock := NewMockDockerAPI()
	mock.ContainerLogsFunc = func(ctx context.Context, ctr string, options client.ContainerLogsOptions) (client.ContainerLogsResult, error) {
		return io.NopCloser(&errorReader{}), nil
	}
	client := NewClientWithAPI(mock)

	_, err := client.GetContainerLogs(context.Background(), "web", 100)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read logs")
}

func TestClient_GetContainerStats_ParseError(t *testing.T) {
	mock := NewMockDockerAPI()
	mock.ContainerStatsFunc = func(ctx context.Context, containerID string, options client.ContainerStatsOptions) (client.ContainerStatsResult, error) {
		return client.ContainerStatsResult{
			Body: io.NopCloser(bytes.NewReader([]byte("not json"))),
		}, nil
	}
	client := NewClientWithAPI(mock)

	_, err := client.GetContainerStats(context.Background(), "web")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse stats")
}

func TestClient_Close_Error(t *testing.T) {
	mock := NewMockDockerAPI()
	mock.CloseFunc = func() error {
		return errors.New("close failed")
	}
	client := NewClientWithAPI(mock)

	err := client.Close()
	require.Error(t, err)
	assert.Equal(t, "close failed", err.Error())
}

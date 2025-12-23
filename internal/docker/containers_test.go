package docker

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_Logs(t *testing.T) {
	tests := []struct {
		name      string
		container string
		tail      int
		follow    bool
		setup     func(*MockDockerAPI)
		want      string
		wantErr   bool
	}{
		{
			name:      "get logs with tail",
			container: "web",
			tail:      100,
			follow:    false,
			setup: func(m *MockDockerAPI) {
				m.ContainerLogsFunc = func(ctx context.Context, containerName string, options container.LogsOptions) (io.ReadCloser, error) {
					assert.Equal(t, "100", options.Tail)
					assert.False(t, options.Follow)
					return io.NopCloser(bytes.NewReader([]byte("line1\nline2\n"))), nil
				}
			},
			want:    "line1\nline2\n",
			wantErr: false,
		},
		{
			name:      "get all logs",
			container: "web",
			tail:      0,
			follow:    false,
			setup: func(m *MockDockerAPI) {
				m.ContainerLogsFunc = func(ctx context.Context, containerName string, options container.LogsOptions) (io.ReadCloser, error) {
					assert.Equal(t, "all", options.Tail)
					return io.NopCloser(bytes.NewReader([]byte("all logs\n"))), nil
				}
			},
			want:    "all logs\n",
			wantErr: false,
		},
		{
			name:      "follow logs",
			container: "web",
			tail:      50,
			follow:    true,
			setup: func(m *MockDockerAPI) {
				m.ContainerLogsFunc = func(ctx context.Context, containerName string, options container.LogsOptions) (io.ReadCloser, error) {
					assert.True(t, options.Follow)
					return io.NopCloser(bytes.NewReader([]byte("streaming\n"))), nil
				}
			},
			want:    "streaming\n",
			wantErr: false,
		},
		{
			name:      "logs error",
			container: "missing",
			tail:      100,
			follow:    false,
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

			reader, err := client.Logs(context.Background(), tt.container, tt.tail, tt.follow)
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, reader)
			} else {
				require.NoError(t, err)
				defer reader.Close()

				data, err := io.ReadAll(reader)
				require.NoError(t, err)
				assert.Equal(t, tt.want, string(data))
			}
		})
	}
}

func TestClient_Inspect(t *testing.T) {
	tests := []struct {
		name        string
		container   string
		setup       func(*MockDockerAPI)
		wantDetails *ContainerDetails
		wantErr     bool
	}{
		{
			name:      "inspect running container",
			container: "web",
			setup: func(m *MockDockerAPI) {
				m.ContainerInspectFunc = func(ctx context.Context, containerID string) (container.InspectResponse, error) {
					return container.InspectResponse{
						ContainerJSONBase: &container.ContainerJSONBase{
							ID:      "abc1234567890000",
							Name:    "/web",
							Created: "2024-01-01T00:00:00.000000000Z",
							State: &container.State{
								Status:    "running",
								Running:   true,
								StartedAt: "2024-01-01T00:00:00.000000000Z",
							},
							RestartCount: 2,
							Driver:       "overlay2",
						},
						Config: &container.Config{
							Image:  "nginx:latest",
							Labels: map[string]string{"env": "prod"},
							Env:    []string{"FOO=bar", "BAZ=qux"},
						},
						NetworkSettings: &container.NetworkSettings{
							NetworkSettingsBase: container.NetworkSettingsBase{
								Ports: nat.PortMap{
									"80/tcp": []nat.PortBinding{
										{HostPort: "8080"},
									},
								},
							},
							Networks: map[string]*network.EndpointSettings{
								"bridge": {},
								"custom": {},
							},
						},
						Mounts: []container.MountPoint{
							{Source: "/host/path", Destination: "/container/path"},
						},
					}, nil
				}
			},
			wantDetails: &ContainerDetails{
				ID:           "abc123456789",
				Name:         "web",
				Image:        "nginx:latest",
				State:        "running",
				RestartCount: 2,
				Driver:       "overlay2",
				Labels:       map[string]string{"env": "prod"},
				Environment:  []string{"FOO=bar", "BAZ=qux"},
			},
			wantErr: false,
		},
		{
			name:      "inspect with health check",
			container: "api",
			setup: func(m *MockDockerAPI) {
				m.ContainerInspectFunc = func(ctx context.Context, containerID string) (container.InspectResponse, error) {
					return container.InspectResponse{
						ContainerJSONBase: &container.ContainerJSONBase{
							ID:      "def1234567890000",
							Name:    "/api",
							Created: "2024-01-01T00:00:00.000000000Z",
							State: &container.State{
								Status:    "running",
								Running:   true,
								StartedAt: "2024-01-01T00:00:00.000000000Z",
								Health: &container.Health{
									Status: "healthy",
								},
							},
							Driver: "overlay2",
						},
						Config: &container.Config{
							Image:  "api:latest",
							Labels: map[string]string{},
							Env:    []string{},
						},
						NetworkSettings: &container.NetworkSettings{
							NetworkSettingsBase: container.NetworkSettingsBase{
								Ports: nat.PortMap{},
							},
							Networks: map[string]*network.EndpointSettings{},
						},
						Mounts: []container.MountPoint{},
					}, nil
				}
			},
			wantDetails: &ContainerDetails{
				ID:     "def123456789",
				Name:   "api",
				Image:  "api:latest",
				State:  "running",
				Health: "healthy",
			},
			wantErr: false,
		},
		{
			name:      "inspect error",
			container: "missing",
			setup: func(m *MockDockerAPI) {
				m.ContainerInspectFunc = func(ctx context.Context, containerID string) (container.InspectResponse, error) {
					return container.InspectResponse{}, errMockInspect
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

			got, err := client.Inspect(context.Background(), tt.container)
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, got)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantDetails.ID, got.ID)
				assert.Equal(t, tt.wantDetails.Name, got.Name)
				assert.Equal(t, tt.wantDetails.Image, got.Image)
				assert.Equal(t, tt.wantDetails.State, got.State)
				assert.Equal(t, tt.wantDetails.Health, got.Health)
			}
		})
	}
}

func TestClient_Remove(t *testing.T) {
	tests := []struct {
		name      string
		container string
		force     bool
		setup     func(*MockDockerAPI)
		wantErr   bool
	}{
		{
			name:      "remove with force",
			container: "web",
			force:     true,
			setup: func(m *MockDockerAPI) {
				m.ContainerRemoveFunc = func(ctx context.Context, containerID string, options container.RemoveOptions) error {
					assert.True(t, options.Force)
					assert.False(t, options.RemoveVolumes)
					return nil
				}
			},
			wantErr: false,
		},
		{
			name:      "remove without force",
			container: "web",
			force:     false,
			setup: func(m *MockDockerAPI) {
				m.ContainerRemoveFunc = func(ctx context.Context, containerID string, options container.RemoveOptions) error {
					assert.False(t, options.Force)
					return nil
				}
			},
			wantErr: false,
		},
		{
			name:      "remove error",
			container: "web",
			force:     true,
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

			err := client.Remove(context.Background(), tt.container, tt.force)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "remove container")
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, 1, mock.ContainerRemoveCalls)
		})
	}
}

func TestClient_Start(t *testing.T) {
	tests := []struct {
		name      string
		container string
		setup     func(*MockDockerAPI)
		wantErr   bool
	}{
		{
			name:      "start success",
			container: "web",
			setup: func(m *MockDockerAPI) {
				m.ContainerStartFunc = func(ctx context.Context, containerID string, options container.StartOptions) error {
					return nil
				}
			},
			wantErr: false,
		},
		{
			name:      "start error",
			container: "web",
			setup: func(m *MockDockerAPI) {
				m.ContainerStartFunc = func(ctx context.Context, containerID string, options container.StartOptions) error {
					return errMockStart
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

			err := client.Start(context.Background(), tt.container)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "start container")
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, 1, mock.ContainerStartCalls)
		})
	}
}

func TestClient_Exists(t *testing.T) {
	tests := []struct {
		name      string
		container string
		setup     func(*MockDockerAPI)
		want      bool
		wantErr   bool
	}{
		{
			name:      "container exists",
			container: "web",
			setup: func(m *MockDockerAPI) {
				m.ContainerListFunc = func(ctx context.Context, options container.ListOptions) ([]container.Summary, error) {
					assert.True(t, options.All)
					return []container.Summary{
						{Names: []string{"/web"}},
						{Names: []string{"/api"}},
					}, nil
				}
			},
			want:    true,
			wantErr: false,
		},
		{
			name:      "container does not exist",
			container: "missing",
			setup: func(m *MockDockerAPI) {
				m.ContainerListFunc = func(ctx context.Context, options container.ListOptions) ([]container.Summary, error) {
					return []container.Summary{
						{Names: []string{"/web"}},
					}, nil
				}
			},
			want:    false,
			wantErr: false,
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
		{
			name:      "empty list",
			container: "web",
			setup: func(m *MockDockerAPI) {
				m.ContainerListFunc = func(ctx context.Context, options container.ListOptions) ([]container.Summary, error) {
					return []container.Summary{}, nil
				}
			},
			want:    false,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := NewMockDockerAPI()
			tt.setup(mock)
			client := NewClientWithAPI(mock)

			got, err := client.Exists(context.Background(), tt.container)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestFormatContainerStatus(t *testing.T) {
	tests := []struct {
		name  string
		state *container.State
		want  string
	}{
		{
			name: "running container",
			state: &container.State{
				Running:   true,
				StartedAt: time.Now().Add(-2 * time.Hour).Format(time.RFC3339Nano),
			},
			want: "Up 2 hours",
		},
		{
			name: "exited with error",
			state: &container.State{
				Running:  false,
				ExitCode: 1,
			},
			want: "Exited (1)",
		},
		{
			name: "exited successfully",
			state: &container.State{
				Running:  false,
				ExitCode: 0,
				Status:   "exited",
			},
			want: "exited",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatContainerStatus(tt.state)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseTimeOrZero(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool // true if should be non-zero
	}{
		{
			name:  "valid RFC3339Nano",
			input: "2024-01-01T12:00:00.000000000Z",
			want:  true,
		},
		{
			name:  "invalid format",
			input: "not a time",
			want:  false,
		},
		{
			name:  "empty string",
			input: "",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseTimeOrZero(tt.input)
			if tt.want {
				assert.False(t, got.IsZero())
			} else {
				assert.True(t, got.IsZero())
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     string
	}{
		{
			name:     "seconds",
			duration: 45 * time.Second,
			want:     "45 seconds",
		},
		{
			name:     "minutes",
			duration: 15 * time.Minute,
			want:     "15 minutes",
		},
		{
			name:     "hours",
			duration: 5 * time.Hour,
			want:     "5 hours",
		},
		{
			name:     "days",
			duration: 3 * 24 * time.Hour,
			want:     "3 days",
		},
		{
			name:     "edge case - just under a minute",
			duration: 59 * time.Second,
			want:     "59 seconds",
		},
		{
			name:     "edge case - just under an hour",
			duration: 59 * time.Minute,
			want:     "59 minutes",
		},
		{
			name:     "edge case - just under a day",
			duration: 23 * time.Hour,
			want:     "23 hours",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDuration(tt.duration)
			assert.Equal(t, tt.want, got)
		})
	}
}

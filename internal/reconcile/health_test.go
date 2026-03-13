package reconcile

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/docker/dockertest"
	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassifyHealth(t *testing.T) {
	t.Run("all running no healthcheck", func(t *testing.T) {
		declared := []DeclaredService{{Name: "web"}, {Name: "db"}}
		actual := []ActualService{
			{Name: "web", State: "running", Health: ""},
			{Name: "db", State: "running", Health: ""},
		}
		assert.Empty(t, classifyHealth(declared, actual))
	})

	t.Run("all healthy with healthcheck", func(t *testing.T) {
		declared := []DeclaredService{{Name: "web"}}
		actual := []ActualService{
			{Name: "web", State: "running", Health: "healthy"},
		}
		assert.Empty(t, classifyHealth(declared, actual))
	})

	t.Run("unhealthy container", func(t *testing.T) {
		declared := []DeclaredService{{Name: "web"}}
		actual := []ActualService{
			{Name: "web", State: "running", Health: "unhealthy"},
		}
		result := classifyHealth(declared, actual)
		assert.Equal(t, []string{"web"}, result)
	})

	t.Run("starting container", func(t *testing.T) {
		declared := []DeclaredService{{Name: "web"}}
		actual := []ActualService{
			{Name: "web", State: "running", Health: "starting"},
		}
		result := classifyHealth(declared, actual)
		assert.Equal(t, []string{"web"}, result)
	})

	t.Run("missing container", func(t *testing.T) {
		declared := []DeclaredService{{Name: "web"}, {Name: "missing"}}
		actual := []ActualService{
			{Name: "web", State: "running", Health: ""},
		}
		result := classifyHealth(declared, actual)
		assert.Equal(t, []string{"missing"}, result)
	})

	t.Run("exited container", func(t *testing.T) {
		declared := []DeclaredService{{Name: "web"}}
		actual := []ActualService{
			{Name: "web", State: "exited", Health: ""},
		}
		result := classifyHealth(declared, actual)
		assert.Equal(t, []string{"web"}, result)
	})

	t.Run("mixed healthy and unhealthy", func(t *testing.T) {
		declared := []DeclaredService{{Name: "web"}, {Name: "db"}, {Name: "cache"}}
		actual := []ActualService{
			{Name: "web", State: "running", Health: "healthy"},
			{Name: "db", State: "running", Health: "unhealthy"},
			{Name: "cache", State: "running", Health: ""},
		}
		result := classifyHealth(declared, actual)
		assert.Equal(t, []string{"db"}, result)
	})
}

func TestPollContainerHealth(t *testing.T) {
	t.Run("all healthy on first poll", func(t *testing.T) {
		mockAPI := &dockertest.MockDockerAPI{
			ContainerListFunc: func(ctx context.Context, options container.ListOptions) ([]container.Summary, error) {
				return []container.Summary{
					{
						ID:     "abc123def456abc123def456abc123def456abc123def456abc123def456abcd",
						Names:  []string{"/test-web-1"},
						Image:  "nginx",
						State:  "running",
						Labels: map[string]string{"com.docker.compose.project": "test", "com.docker.compose.service": "web"},
					},
				}, nil
			},
		}
		client := docker.NewClientWithAPI(mockAPI)

		result, err := pollContainerHealth(
			context.Background(), client,
			[]DeclaredService{{Name: "web", Image: "nginx"}},
			"test", 10*time.Second, 50*time.Millisecond,
		)
		require.NoError(t, err)
		assert.True(t, result.Passed)
		assert.Equal(t, 1, result.Iterations)
		assert.Empty(t, result.Unhealthy)
	})

	t.Run("becomes healthy on 3rd poll", func(t *testing.T) {
		var callCount atomic.Int32
		mockAPI := &dockertest.MockDockerAPI{
			ContainerListFunc: func(ctx context.Context, options container.ListOptions) ([]container.Summary, error) {
				n := callCount.Add(1)
				var status string
				if n >= 3 {
					status = "Up 10 minutes (healthy)"
				} else {
					status = "Up 10 minutes (health: starting)"
				}
				return []container.Summary{
					{
						ID:     "abc123def456abc123def456abc123def456abc123def456abc123def456abcd",
						Names:  []string{"/test-web-1"},
						Image:  "nginx",
						State:  "running",
						Status: status,
						Labels: map[string]string{"com.docker.compose.project": "test", "com.docker.compose.service": "web"},
					},
				}, nil
			},
		}
		client := docker.NewClientWithAPI(mockAPI)

		result, err := pollContainerHealth(
			context.Background(), client,
			[]DeclaredService{{Name: "web", Image: "nginx"}},
			"test", 5*time.Second, 50*time.Millisecond,
		)
		require.NoError(t, err)
		assert.True(t, result.Passed)
		assert.Equal(t, 3, result.Iterations)
	})

	t.Run("timeout with unhealthy containers", func(t *testing.T) {
		mockAPI := &dockertest.MockDockerAPI{
			ContainerListFunc: func(ctx context.Context, options container.ListOptions) ([]container.Summary, error) {
				return []container.Summary{
					{
						ID:     "abc123def456abc123def456abc123def456abc123def456abc123def456abcd",
						Names:  []string{"/test-web-1"},
						Image:  "nginx",
						State:  "running",
						Status: "Up 10 minutes (unhealthy)",
						Labels: map[string]string{"com.docker.compose.project": "test", "com.docker.compose.service": "web"},
					},
				}, nil
			},
		}
		client := docker.NewClientWithAPI(mockAPI)

		result, err := pollContainerHealth(
			context.Background(), client,
			[]DeclaredService{{Name: "web", Image: "nginx"}},
			"test", 200*time.Millisecond, 50*time.Millisecond,
		)
		require.Error(t, err)
		assert.False(t, result.Passed)
		assert.Contains(t, result.Unhealthy, "web")
		assert.Contains(t, err.Error(), "timed out")
	})

	t.Run("containers without healthcheck treated as healthy", func(t *testing.T) {
		mockAPI := &dockertest.MockDockerAPI{
			ContainerListFunc: func(ctx context.Context, options container.ListOptions) ([]container.Summary, error) {
				return []container.Summary{
					{
						ID:     "abc123def456abc123def456abc123def456abc123def456abc123def456abcd",
						Names:  []string{"/test-redis-1"},
						Image:  "redis",
						State:  "running",
						Labels: map[string]string{"com.docker.compose.project": "test", "com.docker.compose.service": "redis"},
					},
				}, nil
			},
		}
		client := docker.NewClientWithAPI(mockAPI)

		result, err := pollContainerHealth(
			context.Background(), client,
			[]DeclaredService{{Name: "redis", Image: "redis"}},
			"test", 5*time.Second, 50*time.Millisecond,
		)
		require.NoError(t, err)
		assert.True(t, result.Passed)
		assert.Equal(t, 1, result.Iterations)
	})

	t.Run("context cancellation exits immediately", func(t *testing.T) {
		mockAPI := &dockertest.MockDockerAPI{
			ContainerListFunc: func(ctx context.Context, options container.ListOptions) ([]container.Summary, error) {
				return []container.Summary{
					{
						ID:     "abc123def456abc123def456abc123def456abc123def456abc123def456abcd",
						Names:  []string{"/test-web-1"},
						Image:  "nginx",
						State:  "running",
						Status: "Up 10 minutes (health: starting)",
						Labels: map[string]string{"com.docker.compose.project": "test", "com.docker.compose.service": "web"},
					},
				}, nil
			},
		}
		client := docker.NewClientWithAPI(mockAPI)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		result, err := pollContainerHealth(
			ctx, client,
			[]DeclaredService{{Name: "web", Image: "nginx"}},
			"test", 10*time.Second, 50*time.Millisecond,
		)
		require.Error(t, err)
		assert.False(t, result.Passed)
		assert.Contains(t, err.Error(), "cancelled")
	})
}

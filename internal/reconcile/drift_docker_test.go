package reconcile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/docker/dockertest"
)

func TestCollectActualState_MatchesByLabel(t *testing.T) {
	mock := dockertest.NewMockDockerAPI()
	mock.ContainerListFunc = func(_ context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
		return client.ContainerListResult{Items: []container.Summary{
			dockertest.MakeTestContainer("abc123", "myapp-web-1", "nginx:latest", "running", "myapp", "web"),
			dockertest.MakeTestContainer("def456", "myapp-api-1", "myapp:v1", "running", "myapp", "api"),
			dockertest.MakeTestContainer("ghi789", "other-db-1", "postgres:16", "running", "other", "db"),
		}}, nil
	}

	client := docker.NewClientWithAPI(mock)
	actual, err := CollectActualState(context.Background(), client, "myapp")
	require.NoError(t, err)

	assert.Len(t, actual, 2)
	names := make(map[string]bool)
	for _, s := range actual {
		names[s.Name] = true
	}
	assert.True(t, names["web"])
	assert.True(t, names["api"])
	assert.False(t, names["db"], "container from different project should not match")
}

func TestCollectActualState_NoMatch(t *testing.T) {
	mock := dockertest.NewMockDockerAPI()
	mock.ContainerListFunc = func(_ context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
		return client.ContainerListResult{Items: []container.Summary{
			dockertest.MakeTestContainer("abc123", "other-web-1", "nginx:latest", "running", "other", "web"),
		}}, nil
	}

	client := docker.NewClientWithAPI(mock)
	actual, err := CollectActualState(context.Background(), client, "myapp")
	require.NoError(t, err)
	assert.Empty(t, actual)
}

func TestCollectActualState_DockerError(t *testing.T) {
	mock := dockertest.NewMockDockerAPI()
	mock.ContainerListFunc = func(_ context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
		return client.ContainerListResult{}, errors.New("docker daemon unreachable")
	}

	client := docker.NewClientWithAPI(mock)
	_, err := CollectActualState(context.Background(), client, "myapp")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "list containers")
}

func TestRunDriftCheck_DetectsMissing(t *testing.T) {
	mock := dockertest.NewMockDockerAPI()
	mock.ContainerListFunc = func(_ context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
		return client.ContainerListResult{Items: []container.Summary{
			dockertest.MakeTestContainer("abc123", "myapp-web-1", "nginx:latest", "running", "myapp", "web"),
		}}, nil
	}

	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
	stateFile := filepath.Join(tmpDir, "state.json")

	// Write state with two declared services.
	state := &DeployState{
		LastDeployedCommit: "abc123",
		DeclaredServices: []DeclaredService{
			{Name: "api", Image: "myapp:v1"},
			{Name: "web", Image: "nginx:latest"},
		},
	}
	require.NoError(t, SaveState(stateFile, state))

	client := docker.NewClientWithAPI(mock)
	report, err := RunDriftCheck(context.Background(), client, stateFile, "myapp", nil)
	require.NoError(t, err)
	assert.True(t, report.HasDrift())

	// "api" should be missing.
	var found bool
	for _, item := range report.Items {
		if item.Service == "api" && item.Type == DriftMissing {
			found = true
		}
	}
	assert.True(t, found, "api should be detected as missing")
}

func TestRunDriftCheck_DetectsImageMismatch(t *testing.T) {
	mock := dockertest.NewMockDockerAPI()
	mock.ContainerListFunc = func(_ context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
		return client.ContainerListResult{Items: []container.Summary{
			dockertest.MakeTestContainer("abc123", "myapp-api-1", "myapp:v2", "running", "myapp", "api"),
		}}, nil
	}

	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
	stateFile := filepath.Join(tmpDir, "state.json")

	state := &DeployState{
		LastDeployedCommit: "abc123",
		DeclaredServices: []DeclaredService{
			{Name: "api", Image: "myapp:v1"},
		},
	}
	require.NoError(t, SaveState(stateFile, state))

	client := docker.NewClientWithAPI(mock)
	report, err := RunDriftCheck(context.Background(), client, stateFile, "myapp", nil)
	require.NoError(t, err)
	assert.True(t, report.HasDrift())

	var found bool
	for _, item := range report.Items {
		if item.Service == "api" && item.Type == DriftImageMismatch {
			found = true
			assert.Equal(t, "myapp:v1", item.Declared)
			assert.Equal(t, "myapp:v2", item.Actual)
		}
	}
	assert.True(t, found, "api should have image mismatch drift")
}

func TestRunDriftCheck_CleanState(t *testing.T) {
	mock := dockertest.NewMockDockerAPI()
	mock.ContainerListFunc = func(_ context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
		return client.ContainerListResult{Items: []container.Summary{
			dockertest.MakeTestContainer("abc123", "myapp-web-1", "nginx:latest", "running", "myapp", "web"),
			dockertest.MakeTestContainer("def456", "myapp-api-1", "myapp:v1", "running", "myapp", "api"),
		}}, nil
	}

	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
	stateFile := filepath.Join(tmpDir, "state.json")

	state := &DeployState{
		LastDeployedCommit: "abc123",
		DeclaredServices: []DeclaredService{
			{Name: "api", Image: "myapp:v1"},
			{Name: "web", Image: "nginx:latest"},
		},
	}
	require.NoError(t, SaveState(stateFile, state))

	client := docker.NewClientWithAPI(mock)
	report, err := RunDriftCheck(context.Background(), client, stateFile, "myapp", nil)
	require.NoError(t, err)
	assert.False(t, report.HasDrift(), "all services match — no drift expected")
}

func TestRunDriftCheck_NoDeclaredServices(t *testing.T) {
	mock := dockertest.NewMockDockerAPI()

	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
	stateFile := filepath.Join(tmpDir, "state.json")

	// Empty state file — no declared services.
	state := &DeployState{}
	require.NoError(t, SaveState(stateFile, state))

	client := docker.NewClientWithAPI(mock)
	_, err := RunDriftCheck(context.Background(), client, stateFile, "myapp", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no declared services")

	// Docker should never be called.
	assert.Equal(t, 0, mock.ContainerListCalls)
}

func TestRunDriftCheck_NoStateFile(t *testing.T) {
	mock := dockertest.NewMockDockerAPI()

	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
	stateFile := filepath.Join(tmpDir, "nonexistent.json")

	client := docker.NewClientWithAPI(mock)
	_, err := RunDriftCheck(context.Background(), client, stateFile, "myapp", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no declared services")
}

func TestCollectActualState_EmptyProjectName(t *testing.T) {
	mock := dockertest.NewMockDockerAPI()
	mock.ContainerListFunc = func(_ context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
		return client.ContainerListResult{Items: []container.Summary{
			dockertest.MakeTestContainer("abc123", "myapp-web-1", "nginx:latest", "running", "myapp", "web"),
			dockertest.MakeTestContainer("def456", "other-db-1", "postgres:16", "running", "other", "db"),
		}}, nil
	}

	client := docker.NewClientWithAPI(mock)
	// Empty project name matches all containers with compose labels.
	actual, err := CollectActualState(context.Background(), client, "")
	require.NoError(t, err)
	assert.Len(t, actual, 2, "empty project name should match all compose-labeled containers")
}

func TestRunDriftCheck_PersistsStateFile(t *testing.T) {
	mock := dockertest.NewMockDockerAPI()
	mock.ContainerListFunc = func(_ context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
		return client.ContainerListResult{Items: []container.Summary{
			dockertest.MakeTestContainer("abc123", "myapp-web-1", "nginx:latest", "running", "myapp", "web"),
		}}, nil
	}

	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
	stateFile := filepath.Join(tmpDir, "state.json")

	state := &DeployState{
		LastDeployedCommit: "abc123",
		DeclaredServices: []DeclaredService{
			{Name: "web", Image: "nginx:latest"},
		},
	}
	require.NoError(t, SaveState(stateFile, state))

	client := docker.NewClientWithAPI(mock)
	_, err := RunDriftCheck(context.Background(), client, stateFile, "myapp", nil)
	require.NoError(t, err)

	// State file should still exist.
	_, err = os.Stat(stateFile)
	assert.NoError(t, err, "state file should still exist after drift check")
}

// TestHasMissingDeclaredServices_Integration tests the Docker-backed missing
// service detection used to override the commit-hash skip in the reconcile pipeline.
func TestHasMissingDeclaredServices_Integration(t *testing.T) {
	t.Run("new service not yet running overrides skip", func(t *testing.T) {
		mock := dockertest.NewMockDockerAPI()
		mock.ContainerListFunc = func(_ context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
			// web and api are running, but newservice is not present.
			return client.ContainerListResult{Items: []container.Summary{
				dockertest.MakeTestContainer("a1", "myapp-web-1", "nginx:latest", "running", "myapp", "web"),
				dockertest.MakeTestContainer("a2", "myapp-api-1", "myapp:v1", "running", "myapp", "api"),
			}}, nil
		}

		client := docker.NewClientWithAPI(mock)
		actual, err := CollectActualState(context.Background(), client, "myapp")
		require.NoError(t, err)

		declared := []DeclaredService{
			{Name: "web", Image: "nginx:latest"},
			{Name: "api", Image: "myapp:v1"},
			{Name: "newservice", Image: "myapp:v2"},
		}

		assert.True(t, hasMissingDeclaredServices(declared, actual),
			"newservice missing from Docker should indicate missing services")
	})

	t.Run("all services running does not override skip", func(t *testing.T) {
		mock := dockertest.NewMockDockerAPI()
		mock.ContainerListFunc = func(_ context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
			return client.ContainerListResult{Items: []container.Summary{
				dockertest.MakeTestContainer("a1", "myapp-web-1", "nginx:latest", "running", "myapp", "web"),
				dockertest.MakeTestContainer("a2", "myapp-api-1", "myapp:v1", "running", "myapp", "api"),
			}}, nil
		}

		client := docker.NewClientWithAPI(mock)
		actual, err := CollectActualState(context.Background(), client, "myapp")
		require.NoError(t, err)

		declared := []DeclaredService{
			{Name: "web", Image: "nginx:latest"},
			{Name: "api", Image: "myapp:v1"},
		}

		assert.False(t, hasMissingDeclaredServices(declared, actual),
			"all declared services running — skip should proceed normally")
	})

	t.Run("empty declared state does not override skip", func(t *testing.T) {
		mock := dockertest.NewMockDockerAPI()
		mock.ContainerListFunc = func(_ context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
			return client.ContainerListResult{Items: []container.Summary{
				dockertest.MakeTestContainer("a1", "myapp-web-1", "nginx:latest", "running", "myapp", "web"),
			}}, nil
		}

		client := docker.NewClientWithAPI(mock)
		actual, err := CollectActualState(context.Background(), client, "myapp")
		require.NoError(t, err)

		// Empty declared — no deployment has been recorded yet.
		assert.False(t, hasMissingDeclaredServices(nil, actual),
			"empty declared services should not override skip")
	})
}

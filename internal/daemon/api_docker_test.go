package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/docker/dockertest"
	"github.com/cameronsjo/bosun/internal/reconcile"
)

// newDockerDaemon creates a daemon with an injected mock Docker client.
func newDockerDaemon(t *testing.T, mockAPI *dockertest.MockDockerAPI) *Daemon {
	t.Helper()
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	cfg := DefaultConfig()
	cfg.SocketPath = filepath.Join(tmpDir, "test.sock")
	cfg.EnableHTTP = false
	cfg.ReconcileConfig = reconcile.DefaultConfig()
	cfg.ReconcileConfig.RepoURL = "https://github.com/test/repo"
	cfg.ReconcileConfig.DryRun = true
	cfg.ReconcileConfig.RepoDir = filepath.Join(tmpDir, "repo")
	cfg.ReconcileConfig.LockFile = filepath.Join(tmpDir, "test.lock")
	cfg.ReconcileConfig.StateFile = filepath.Join(tmpDir, "state.json")

	d, err := New(cfg)
	require.NoError(t, err)

	// Inject mock Docker client via override.
	d.dockerClientOverride = docker.NewClientWithAPI(mockAPI)

	return d
}

func TestAPIContainers_Success(t *testing.T) {
	mock := dockertest.NewMockDockerAPI()
	mock.ContainerListFunc = func(_ context.Context, _ container.ListOptions) ([]container.Summary, error) {
		return []container.Summary{
			dockertest.MakeTestContainer("abc123", "web", "nginx:latest", "running", "myapp", "web"),
			dockertest.MakeTestContainer("def456", "api", "myapp:v1", "running", "myapp", "api"),
		}, nil
	}

	d := newDockerDaemon(t, mock)
	mux := http.NewServeMux()
	d.RegisterAPIRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/containers", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp APIContainersResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Len(t, resp.Containers, 2)
	assert.Equal(t, 2, resp.Summary.Total)
	assert.Equal(t, 2, resp.Summary.Running)
	assert.Equal(t, 0, resp.Summary.Stopped)
}

func TestAPIContainers_Empty(t *testing.T) {
	mock := dockertest.NewMockDockerAPI()
	d := newDockerDaemon(t, mock)
	mux := http.NewServeMux()
	d.RegisterAPIRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/containers", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp APIContainersResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Empty(t, resp.Containers)
	assert.Equal(t, 0, resp.Summary.Total)
}

func TestAPIContainers_DockerError(t *testing.T) {
	mock := dockertest.NewMockDockerAPI()
	mock.ContainerListFunc = func(_ context.Context, _ container.ListOptions) ([]container.Summary, error) {
		return nil, errors.New("docker daemon unreachable")
	}

	d := newDockerDaemon(t, mock)
	mux := http.NewServeMux()
	d.RegisterAPIRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/containers", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestAPIContainers_MethodNotAllowed(t *testing.T) {
	mock := dockertest.NewMockDockerAPI()
	d := newDockerDaemon(t, mock)
	mux := http.NewServeMux()
	d.RegisterAPIRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/containers", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestAPIContainerLogs_Success(t *testing.T) {
	mock := dockertest.NewMockDockerAPI()
	mock.ContainerLogsFunc = func(_ context.Context, _ string, _ container.LogsOptions) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewBufferString("line1\nline2\nline3\n")), nil
	}

	d := newDockerDaemon(t, mock)
	mux := http.NewServeMux()
	d.RegisterAPIRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/containers/web/logs?lines=50", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp APILogsResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "web", resp.Container)
	assert.Equal(t, 50, resp.Lines)
	assert.Contains(t, resp.Logs, "line1")
}

func TestAPIContainerLogs_LinesCapped(t *testing.T) {
	mock := dockertest.NewMockDockerAPI()
	mock.ContainerLogsFunc = func(_ context.Context, _ string, opts container.LogsOptions) (io.ReadCloser, error) {
		// Verify the lines were capped in the Docker API call.
		assert.Equal(t, "10000", opts.Tail)
		return io.NopCloser(bytes.NewBufferString("logs")), nil
	}

	d := newDockerDaemon(t, mock)
	mux := http.NewServeMux()
	d.RegisterAPIRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/containers/web/logs?lines=99999", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp APILogsResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, 10000, resp.Lines)
}

func TestAPIContainerLogs_DockerError(t *testing.T) {
	mock := dockertest.NewMockDockerAPI()
	mock.ContainerLogsFunc = func(_ context.Context, _ string, _ container.LogsOptions) (io.ReadCloser, error) {
		return nil, errors.New("container not found")
	}

	d := newDockerDaemon(t, mock)
	mux := http.NewServeMux()
	d.RegisterAPIRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/containers/web/logs", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestAPIContainerRestart_Success(t *testing.T) {
	mock := dockertest.NewMockDockerAPI()
	mock.ContainerRestartFunc = func(_ context.Context, _ string, _ container.StopOptions) error {
		return nil
	}

	d := newDockerDaemon(t, mock)
	mux := http.NewServeMux()
	d.RegisterAPIRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/containers/web/restart", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp APIRestartResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "success", resp.Status)
	assert.Equal(t, "web", resp.Container)
	assert.Equal(t, 1, mock.ContainerRestartCalls)
}

func TestAPIContainerRestart_DockerError(t *testing.T) {
	mock := dockertest.NewMockDockerAPI()
	mock.ContainerRestartFunc = func(_ context.Context, _ string, _ container.StopOptions) error {
		return errors.New("restart timeout")
	}

	d := newDockerDaemon(t, mock)
	mux := http.NewServeMux()
	d.RegisterAPIRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/containers/web/restart", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestAPIContainerRestart_MethodNotAllowed(t *testing.T) {
	mock := dockertest.NewMockDockerAPI()
	d := newDockerDaemon(t, mock)
	mux := http.NewServeMux()
	d.RegisterAPIRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/containers/web/restart", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

package reconcile

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		file    string
		want    bool
	}{
		{"exact match", "config.yaml", "config.yaml", true},
		{"exact no match", "config.yaml", "other.yaml", false},
		{"star glob", "*.yml", "compose.yml", true},
		{"star glob no match", "*.yml", "compose.yaml", false},
		{"doublestar prefix", "traefik/conf.d/**", "traefik/conf.d/dynamic.yml", true},
		{"doublestar nested", "traefik/conf.d/**", "traefik/conf.d/sub/deep.yml", true},
		{"doublestar no match", "traefik/conf.d/**", "gatus/config.yaml", false},
		{"doublestar root", "**", "anything/at/all.txt", true},
		{"simple dir match", "traefik/**", "traefik/traefik.yml", true},
		{"dir boundary", "traefik/**", "traefik-other/file.yml", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, matchGlob(tc.pattern, tc.file))
		})
	}
}

func TestMatchAnyPath(t *testing.T) {
	tests := []struct {
		name     string
		files    []string
		patterns []string
		want     bool
	}{
		{"matching file", []string{"unraid/compose/core.yml"}, []string{"unraid/**"}, true},
		{"no matching file", []string{"docs/README.md"}, []string{"unraid/**"}, false},
		{"multiple patterns one matches", []string{"infra/traefik.yml"}, []string{"unraid/**", "infra/**"}, true},
		{"mixed files one matches", []string{"docs/README.md", "unraid/compose/core.yml"}, []string{"unraid/**"}, true},
		{"empty files", nil, []string{"unraid/**"}, false},
		{"empty patterns", []string{"anything.txt"}, nil, false},
		{"both empty", nil, nil, false},
		{"exact match pattern", []string{"bosun.yaml"}, []string{"bosun.yaml"}, true},
		{"doublestar root matches all", []string{"any/file.txt"}, []string{"**"}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, matchAnyPath(tc.files, tc.patterns))
		})
	}
}

func TestEvaluatePostSyncHooks(t *testing.T) {
	hooks := []PostSyncHook{
		{Paths: []string{"traefik/conf.d/**"}, Action: "restart", Container: "traefik"},
		{Paths: []string{"gatus/config.yaml"}, Action: "restart", Container: "gatus"},
		{Paths: []string{"prometheus/**"}, Action: "restart", Container: "prometheus"},
	}

	t.Run("matches traefik hook", func(t *testing.T) {
		changed := []string{"traefik/conf.d/dynamic.yml"}
		matched := EvaluatePostSyncHooks(changed, hooks)
		assert.Len(t, matched, 1)
		assert.Equal(t, "traefik", matched[0].Container)
	})

	t.Run("matches multiple hooks", func(t *testing.T) {
		changed := []string{"traefik/conf.d/routers.yml", "gatus/config.yaml"}
		matched := EvaluatePostSyncHooks(changed, hooks)
		assert.Len(t, matched, 2)
	})

	t.Run("no matches for unrelated files", func(t *testing.T) {
		changed := []string{"docker-compose.yml", "README.md"}
		matched := EvaluatePostSyncHooks(changed, hooks)
		assert.Empty(t, matched)
	})

	t.Run("empty changed files returns nil", func(t *testing.T) {
		matched := EvaluatePostSyncHooks(nil, hooks)
		assert.Nil(t, matched)
	})

	t.Run("empty hooks returns nil", func(t *testing.T) {
		matched := EvaluatePostSyncHooks([]string{"anything.txt"}, nil)
		assert.Nil(t, matched)
	})

	t.Run("deduplicates by container", func(t *testing.T) {
		dupeHooks := []PostSyncHook{
			{Paths: []string{"traefik/conf.d/**"}, Action: "restart", Container: "traefik"},
			{Paths: []string{"traefik/traefik.yml"}, Action: "restart", Container: "traefik"},
		}
		changed := []string{"traefik/conf.d/x.yml", "traefik/traefik.yml"}
		matched := EvaluatePostSyncHooks(changed, dupeHooks)
		assert.Len(t, matched, 1)
	})

	t.Run("preserves delay field on matched hooks", func(t *testing.T) {
		hooksWithDelay := []PostSyncHook{
			{
				Paths:     []string{"traefik/conf.d/**"},
				Action:    "restart",
				Container: "traefik",
				Delay:     Duration{Duration: 5 * time.Second},
			},
			{
				Paths:     []string{"gatus/config.yaml"},
				Action:    "restart",
				Container: "gatus",
			},
		}
		changed := []string{"traefik/conf.d/dynamic.yml", "gatus/config.yaml"}
		matched := EvaluatePostSyncHooks(changed, hooksWithDelay)
		assert.Len(t, matched, 2)
		assert.Equal(t, 5*time.Second, matched[0].Delay.Duration)
		assert.Equal(t, time.Duration(0), matched[1].Delay.Duration)
	})
}

func TestDedupeHooksByContainer(t *testing.T) {
	tests := []struct {
		name  string
		hooks []PostSyncHook
		want  int
	}{
		{
			name:  "empty hooks",
			hooks: nil,
			want:  0,
		},
		{
			name: "no duplicates",
			hooks: []PostSyncHook{
				{Container: "traefik", Action: "restart"},
				{Container: "gatus", Action: "restart"},
			},
			want: 2,
		},
		{
			name: "duplicates removed first wins",
			hooks: []PostSyncHook{
				{Container: "traefik", Action: "restart", Paths: []string{"a/**"}},
				{Container: "traefik", Action: "restart", Paths: []string{"b/**"}},
				{Container: "gatus", Action: "restart"},
			},
			want: 2,
		},
		{
			name: "all same container",
			hooks: []PostSyncHook{
				{Container: "traefik", Action: "restart"},
				{Container: "traefik", Action: "restart"},
				{Container: "traefik", Action: "restart"},
			},
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := dedupeHooksByContainer(tt.hooks)
			assert.Len(t, result, tt.want)
		})
	}
}

func TestExecutePostSyncHooks(t *testing.T) {
	t.Run("empty hooks returns nil immediately", func(t *testing.T) {
		err := ExecutePostSyncHooks(context.Background(), nil, nil, 0)
		assert.NoError(t, err)
	})

	t.Run("single restart hook calls RestartContainer", func(t *testing.T) {
		var mu sync.Mutex
		restarted := []string{}

		mockAPI := newReconcileMockDockerAPI()
		mockAPI.containerRestartFunc = func(ctx context.Context, containerID string, _ container.StopOptions) error {
			mu.Lock()
			defer mu.Unlock()
			restarted = append(restarted, containerID)
			return nil
		}
		client := docker.NewClientWithAPI(mockAPI)

		hooks := []PostSyncHook{
			{Paths: []string{"traefik/**"}, Action: "restart", Container: "traefik"},
		}

		err := ExecutePostSyncHooks(context.Background(), client, hooks, 0)
		require.NoError(t, err)

		mu.Lock()
		defer mu.Unlock()
		assert.Equal(t, []string{"traefik"}, restarted)
	})

	t.Run("unsupported action is skipped with no error", func(t *testing.T) {
		mockAPI := newReconcileMockDockerAPI()
		restartCalled := false
		mockAPI.containerRestartFunc = func(ctx context.Context, containerID string, _ container.StopOptions) error {
			restartCalled = true
			return nil
		}
		client := docker.NewClientWithAPI(mockAPI)

		hooks := []PostSyncHook{
			{Paths: []string{"app/**"}, Action: "reload", Container: "myapp"},
		}

		err := ExecutePostSyncHooks(context.Background(), client, hooks, 0)
		assert.NoError(t, err)
		assert.False(t, restartCalled)
	})

	t.Run("RestartContainer failure returns aggregated error", func(t *testing.T) {
		mockAPI := newReconcileMockDockerAPI()
		mockAPI.containerRestartFunc = func(ctx context.Context, containerID string, _ container.StopOptions) error {
			return fmt.Errorf("container %s not found", containerID)
		}
		client := docker.NewClientWithAPI(mockAPI)

		hooks := []PostSyncHook{
			{Action: "restart", Container: "traefik"},
		}

		err := ExecutePostSyncHooks(context.Background(), client, hooks, 0)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "traefik")
		assert.Contains(t, err.Error(), "post-sync hook failures")
	})

	t.Run("multiple hooks with mixed success and failure", func(t *testing.T) {
		mockAPI := newReconcileMockDockerAPI()
		mockAPI.containerRestartFunc = func(ctx context.Context, containerID string, _ container.StopOptions) error {
			if containerID == "gatus" {
				return errors.New("restart failed")
			}
			return nil
		}
		client := docker.NewClientWithAPI(mockAPI)

		hooks := []PostSyncHook{
			{Action: "restart", Container: "traefik"},
			{Action: "restart", Container: "gatus"},
			{Action: "restart", Container: "prometheus"},
		}

		err := ExecutePostSyncHooks(context.Background(), client, hooks, 0)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "gatus")
		assert.NotContains(t, err.Error(), "traefik")
		assert.NotContains(t, err.Error(), "prometheus")
	})

	t.Run("settle delay with cancelled context returns context error", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		hooks := []PostSyncHook{
			{Action: "restart", Container: "traefik"},
		}

		err := ExecutePostSyncHooks(ctx, nil, hooks, 1*time.Second)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("per-hook delay with cancelled context returns context error", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		hooks := []PostSyncHook{
			{Action: "restart", Container: "traefik", Delay: Duration{Duration: 1 * time.Second}},
		}

		err := ExecutePostSyncHooks(ctx, nil, hooks, 0)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("per-hook delay applied before restart", func(t *testing.T) {
		mockAPI := newReconcileMockDockerAPI()
		var restartTime time.Time
		mockAPI.containerRestartFunc = func(ctx context.Context, containerID string, _ container.StopOptions) error {
			restartTime = time.Now()
			return nil
		}
		client := docker.NewClientWithAPI(mockAPI)

		hooks := []PostSyncHook{
			{Action: "restart", Container: "traefik", Delay: Duration{Duration: 5 * time.Millisecond}},
		}

		start := time.Now()
		err := ExecutePostSyncHooks(context.Background(), client, hooks, 0)
		require.NoError(t, err)

		// Restart should have happened after the delay
		assert.True(t, restartTime.After(start.Add(3*time.Millisecond)),
			"restart should have happened after the per-hook delay")
	})

	t.Run("settle delay applied before hooks run", func(t *testing.T) {
		mockAPI := newReconcileMockDockerAPI()
		var restartTime time.Time
		mockAPI.containerRestartFunc = func(ctx context.Context, containerID string, _ container.StopOptions) error {
			restartTime = time.Now()
			return nil
		}
		client := docker.NewClientWithAPI(mockAPI)

		hooks := []PostSyncHook{
			{Action: "restart", Container: "traefik"},
		}

		start := time.Now()
		err := ExecutePostSyncHooks(context.Background(), client, hooks, 5*time.Millisecond)
		require.NoError(t, err)

		assert.True(t, restartTime.After(start.Add(3*time.Millisecond)),
			"restart should have happened after the settle delay")
	})

	t.Run("multiple restart hooks track all containers", func(t *testing.T) {
		var mu sync.Mutex
		restarted := []string{}

		mockAPI := newReconcileMockDockerAPI()
		mockAPI.containerRestartFunc = func(ctx context.Context, containerID string, _ container.StopOptions) error {
			mu.Lock()
			defer mu.Unlock()
			restarted = append(restarted, containerID)
			return nil
		}
		client := docker.NewClientWithAPI(mockAPI)

		hooks := []PostSyncHook{
			{Action: "restart", Container: "traefik"},
			{Action: "restart", Container: "gatus"},
			{Action: "restart", Container: "prometheus"},
		}

		err := ExecutePostSyncHooks(context.Background(), client, hooks, 0)
		require.NoError(t, err)

		mu.Lock()
		defer mu.Unlock()
		assert.Equal(t, []string{"traefik", "gatus", "prometheus"}, restarted)
	})

	t.Run("mix of restart and unsupported actions", func(t *testing.T) {
		var mu sync.Mutex
		restarted := []string{}

		mockAPI := newReconcileMockDockerAPI()
		mockAPI.containerRestartFunc = func(ctx context.Context, containerID string, _ container.StopOptions) error {
			mu.Lock()
			defer mu.Unlock()
			restarted = append(restarted, containerID)
			return nil
		}
		client := docker.NewClientWithAPI(mockAPI)

		hooks := []PostSyncHook{
			{Action: "restart", Container: "traefik"},
			{Action: "signal", Container: "nginx"},
			{Action: "restart", Container: "gatus"},
		}

		err := ExecutePostSyncHooks(context.Background(), client, hooks, 0)
		require.NoError(t, err)

		mu.Lock()
		defer mu.Unlock()
		assert.Equal(t, []string{"traefik", "gatus"}, restarted)
	})

	t.Run("exec hook calls ExecContainer", func(t *testing.T) {
		var mu sync.Mutex
		var execCalls []struct {
			container string
			cmd       []string
		}

		mockAPI := newReconcileMockDockerAPI()
		mockAPI.containerExecCreateFunc = func(ctx context.Context, ctr string, config container.ExecOptions) (container.ExecCreateResponse, error) {
			mu.Lock()
			defer mu.Unlock()
			execCalls = append(execCalls, struct {
				container string
				cmd       []string
			}{ctr, config.Cmd})
			return container.ExecCreateResponse{ID: "exec-123"}, nil
		}
		client := docker.NewClientWithAPI(mockAPI)

		hooks := []PostSyncHook{
			{Paths: []string{"traefik/**"}, Action: "exec", Container: "traefik", Command: []string{"traefik", "reload"}},
		}

		err := ExecutePostSyncHooks(context.Background(), client, hooks, 0)
		require.NoError(t, err)

		mu.Lock()
		defer mu.Unlock()
		require.Len(t, execCalls, 1)
		assert.Equal(t, "traefik", execCalls[0].container)
		assert.Equal(t, []string{"traefik", "reload"}, execCalls[0].cmd)
	})

	t.Run("exec hook with empty command is skipped", func(t *testing.T) {
		mockAPI := newReconcileMockDockerAPI()
		execCalled := false
		mockAPI.containerExecCreateFunc = func(ctx context.Context, ctr string, config container.ExecOptions) (container.ExecCreateResponse, error) {
			execCalled = true
			return container.ExecCreateResponse{ID: "exec-123"}, nil
		}
		client := docker.NewClientWithAPI(mockAPI)

		hooks := []PostSyncHook{
			{Paths: []string{"app/**"}, Action: "exec", Container: "myapp"},
		}

		err := ExecutePostSyncHooks(context.Background(), client, hooks, 0)
		assert.NoError(t, err)
		assert.False(t, execCalled)
	})

	t.Run("exec hook failure returns aggregated error", func(t *testing.T) {
		mockAPI := newReconcileMockDockerAPI()
		mockAPI.containerExecCreateFunc = func(ctx context.Context, ctr string, config container.ExecOptions) (container.ExecCreateResponse, error) {
			return container.ExecCreateResponse{}, fmt.Errorf("container %s not found", ctr)
		}
		client := docker.NewClientWithAPI(mockAPI)

		hooks := []PostSyncHook{
			{Action: "exec", Container: "traefik", Command: []string{"traefik", "reload"}},
		}

		err := ExecutePostSyncHooks(context.Background(), client, hooks, 0)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "traefik exec")
		assert.Contains(t, err.Error(), "post-sync hook failures")
	})

	t.Run("exec hook with non-zero exit code returns error", func(t *testing.T) {
		mockAPI := newReconcileMockDockerAPI()
		mockAPI.containerExecInspectFunc = func(ctx context.Context, execID string) (container.ExecInspect, error) {
			return container.ExecInspect{ExitCode: 1}, nil
		}
		client := docker.NewClientWithAPI(mockAPI)

		hooks := []PostSyncHook{
			{Action: "exec", Container: "nginx", Command: []string{"nginx", "-s", "reload"}},
		}

		err := ExecutePostSyncHooks(context.Background(), client, hooks, 0)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nginx exec")
		assert.Contains(t, err.Error(), "exited with code 1")
	})

	t.Run("mix of restart and exec hooks", func(t *testing.T) {
		var mu sync.Mutex
		restarted := []string{}
		executed := []string{}

		mockAPI := newReconcileMockDockerAPI()
		mockAPI.containerRestartFunc = func(ctx context.Context, containerID string, _ container.StopOptions) error {
			mu.Lock()
			defer mu.Unlock()
			restarted = append(restarted, containerID)
			return nil
		}
		mockAPI.containerExecCreateFunc = func(ctx context.Context, ctr string, config container.ExecOptions) (container.ExecCreateResponse, error) {
			mu.Lock()
			defer mu.Unlock()
			executed = append(executed, ctr)
			return container.ExecCreateResponse{ID: "exec-123"}, nil
		}
		client := docker.NewClientWithAPI(mockAPI)

		hooks := []PostSyncHook{
			{Action: "restart", Container: "gatus"},
			{Action: "exec", Container: "traefik", Command: []string{"traefik", "reload"}},
			{Action: "exec", Container: "nginx", Command: []string{"nginx", "-s", "reload"}},
		}

		err := ExecutePostSyncHooks(context.Background(), client, hooks, 0)
		require.NoError(t, err)

		mu.Lock()
		defer mu.Unlock()
		assert.Equal(t, []string{"gatus"}, restarted)
		assert.Equal(t, []string{"traefik", "nginx"}, executed)
	})
}

func TestEvaluatePostSyncHooksWithExec(t *testing.T) {
	t.Run("same container with different actions both match", func(t *testing.T) {
		hooks := []PostSyncHook{
			{Paths: []string{"traefik/**"}, Action: "restart", Container: "traefik"},
			{Paths: []string{"traefik/**"}, Action: "exec", Container: "traefik", Command: []string{"traefik", "reload"}},
		}
		changed := []string{"traefik/conf.d/dynamic.yml"}
		matched := EvaluatePostSyncHooks(changed, hooks)
		assert.Len(t, matched, 2)
		assert.Equal(t, "restart", matched[0].Action)
		assert.Equal(t, "exec", matched[1].Action)
	})

	t.Run("same container same action deduplicates", func(t *testing.T) {
		hooks := []PostSyncHook{
			{Paths: []string{"traefik/conf.d/**"}, Action: "exec", Container: "traefik", Command: []string{"traefik", "reload"}},
			{Paths: []string{"traefik/traefik.yml"}, Action: "exec", Container: "traefik", Command: []string{"traefik", "reload"}},
		}
		changed := []string{"traefik/conf.d/x.yml", "traefik/traefik.yml"}
		matched := EvaluatePostSyncHooks(changed, hooks)
		assert.Len(t, matched, 1)
	})
}

func TestDedupeHooksByContainerWithExec(t *testing.T) {
	t.Run("different actions on same container are kept", func(t *testing.T) {
		hooks := []PostSyncHook{
			{Container: "traefik", Action: "restart"},
			{Container: "traefik", Action: "exec", Command: []string{"traefik", "reload"}},
		}
		result := dedupeHooksByContainer(hooks)
		assert.Len(t, result, 2)
	})

	t.Run("same action on same container deduplicates", func(t *testing.T) {
		hooks := []PostSyncHook{
			{Container: "traefik", Action: "exec", Command: []string{"cmd1"}},
			{Container: "traefik", Action: "exec", Command: []string{"cmd2"}},
		}
		result := dedupeHooksByContainer(hooks)
		assert.Len(t, result, 1)
	})
}

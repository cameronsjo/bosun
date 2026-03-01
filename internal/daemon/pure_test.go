package daemon

import (
	"errors"
	"testing"
	"time"

	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/reconcile"
	"github.com/stretchr/testify/assert"
)

func TestReconcileStateString(t *testing.T) {
	tests := []struct {
		name          string
		isReconciling bool
		want          string
	}{
		{"idle when not reconciling", false, "idle"},
		{"reconciling when active", true, "reconciling"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, reconcileStateString(tt.isReconciling))
		})
	}
}

func TestComputeDriftStatus(t *testing.T) {
	tests := []struct {
		name               string
		driftItemCount     int
		lastDeployedCommit string
		want               string
	}{
		{"unknown when no commit deployed", 0, "", "unknown"},
		{"unknown trumps drift items when no commit", 3, "", "unknown"},
		{"clean when deployed and no drift", 0, "abc123", "clean"},
		{"drifted when items exist", 2, "abc123", "drifted"},
		{"drifted with single item", 1, "abc123", "drifted"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, computeDriftStatus(tt.driftItemCount, tt.lastDeployedCommit))
		})
	}
}

func TestBuildAPIStatusResponse(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	someErr := errors.New("reconcile failed")

	tests := []struct {
		name          string
		lastReconcile time.Time
		lastErr       error
		reconciling   bool
		health        string
		uptime        time.Duration
		pollInterval  time.Duration
		check         func(t *testing.T, resp APIStatusResponse)
	}{
		{
			name:          "idle with no history",
			lastReconcile: time.Time{},
			lastErr:       nil,
			reconciling:   false,
			health:        "healthy",
			uptime:        5 * time.Minute,
			pollInterval:  0,
			check: func(t *testing.T, resp APIStatusResponse) {
				assert.Equal(t, "idle", resp.State)
				assert.Equal(t, "healthy", resp.Health)
				assert.Nil(t, resp.LastReconcile)
				assert.Empty(t, resp.LastError)
				assert.Nil(t, resp.NextPoll)
				assert.Equal(t, 0, resp.PollInterval)
			},
		},
		{
			name:          "reconciling with error",
			lastReconcile: now,
			lastErr:       someErr,
			reconciling:   true,
			health:        "degraded",
			uptime:        10 * time.Minute,
			pollInterval:  time.Hour,
			check: func(t *testing.T, resp APIStatusResponse) {
				assert.Equal(t, "reconciling", resp.State)
				assert.Equal(t, "degraded", resp.Health)
				assert.NotNil(t, resp.LastReconcile)
				assert.Equal(t, now, *resp.LastReconcile)
				assert.Equal(t, "reconcile failed", resp.LastError)
				assert.Equal(t, 3600, resp.PollInterval)
				assert.NotNil(t, resp.NextPoll)
				assert.Equal(t, now.Add(time.Hour), *resp.NextPoll)
			},
		},
		{
			name:          "poll interval set but no last reconcile omits next poll",
			lastReconcile: time.Time{},
			lastErr:       nil,
			reconciling:   false,
			health:        "healthy",
			uptime:        time.Second,
			pollInterval:  30 * time.Minute,
			check: func(t *testing.T, resp APIStatusResponse) {
				assert.Equal(t, 1800, resp.PollInterval)
				assert.Nil(t, resp.NextPoll)
			},
		},
		{
			name:          "uptime rounds to seconds",
			lastReconcile: time.Time{},
			lastErr:       nil,
			reconciling:   false,
			health:        "healthy",
			uptime:        5*time.Minute + 500*time.Millisecond,
			pollInterval:  0,
			check: func(t *testing.T, resp APIStatusResponse) {
				assert.Equal(t, "5m1s", resp.Uptime)
				assert.Equal(t, int64(300), resp.UptimeSeconds)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := buildAPIStatusResponse(tt.lastReconcile, tt.lastErr, tt.reconciling, tt.health, tt.uptime, tt.pollInterval)
			tt.check(t, resp)
		})
	}
}

func TestComputeContainerSummary(t *testing.T) {
	tests := []struct {
		name       string
		containers []docker.ContainerInfo
		want       ContainerSummary
	}{
		{
			name:       "empty list",
			containers: nil,
			want:       ContainerSummary{Total: 0},
		},
		{
			name: "all running and healthy",
			containers: []docker.ContainerInfo{
				{Name: "web", State: "running", Health: "healthy"},
				{Name: "db", State: "running", Health: "healthy"},
			},
			want: ContainerSummary{Total: 2, Running: 2, Stopped: 0, Unhealthy: 0},
		},
		{
			name: "mixed states",
			containers: []docker.ContainerInfo{
				{Name: "web", State: "running", Health: "healthy"},
				{Name: "worker", State: "running", Health: "unhealthy"},
				{Name: "db", State: "exited", Health: ""},
			},
			want: ContainerSummary{Total: 3, Running: 2, Stopped: 1, Unhealthy: 1},
		},
		{
			name: "all stopped",
			containers: []docker.ContainerInfo{
				{Name: "web", State: "exited"},
				{Name: "db", State: "exited"},
			},
			want: ContainerSummary{Total: 2, Running: 0, Stopped: 2, Unhealthy: 0},
		},
		{
			name: "unhealthy running container",
			containers: []docker.ContainerInfo{
				{Name: "web", State: "running", Health: "unhealthy"},
			},
			want: ContainerSummary{Total: 1, Running: 1, Stopped: 0, Unhealthy: 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, computeContainerSummary(tt.containers))
		})
	}
}

func TestShouldProcessGitHubEvent(t *testing.T) {
	tests := []struct {
		name       string
		eventType  string
		wantAction string
		wantReason string
	}{
		{"push event", "push", "process", "push event received"},
		{"ping event", "ping", "ping", "ping event received"},
		{"issues event ignored", "issues", "ignore", "Event type 'issues' not handled"},
		{"pull_request ignored", "pull_request", "ignore", "Event type 'pull_request' not handled"},
		{"empty event ignored", "", "ignore", "Event type '' not handled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action, reason := shouldProcessGitHubEvent(tt.eventType)
			assert.Equal(t, tt.wantAction, action)
			assert.Equal(t, tt.wantReason, reason)
		})
	}
}

func TestShouldProcessGitHubPush(t *testing.T) {
	tests := []struct {
		name           string
		pushRef        string
		trackingBranch string
		wantProcess    bool
		wantReason     string
	}{
		{
			name:           "matching branch",
			pushRef:        "refs/heads/main",
			trackingBranch: "main",
			wantProcess:    true,
			wantReason:     "push to tracked branch main",
		},
		{
			name:           "different branch",
			pushRef:        "refs/heads/develop",
			trackingBranch: "main",
			wantProcess:    false,
			wantReason:     "Push to refs/heads/develop ignored (tracking refs/heads/main)",
		},
		{
			name:           "tag ref ignored",
			pushRef:        "refs/tags/v1.0.0",
			trackingBranch: "main",
			wantProcess:    false,
			wantReason:     "Push to refs/tags/v1.0.0 ignored (tracking refs/heads/main)",
		},
		{
			name:           "custom tracking branch",
			pushRef:        "refs/heads/production",
			trackingBranch: "production",
			wantProcess:    true,
			wantReason:     "push to tracked branch production",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			process, reason := shouldProcessGitHubPush(tt.pushRef, tt.trackingBranch)
			assert.Equal(t, tt.wantProcess, process)
			assert.Equal(t, tt.wantReason, reason)
		})
	}
}

func TestBuildConfigResponse(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want ConfigResponse
	}{
		{
			name: "full config",
			cfg: &Config{
				WebhookSecret: "secret123",
				PollInterval:  time.Hour,
				ReconcileConfig: &reconcile.Config{
					RepoURL:    "https://github.com/example/repo.git",
					RepoBranch: "main",
				},
			},
			want: ConfigResponse{
				WebhookSecret: "secret123",
				PollInterval:  3600,
				RepoURL:       "https://github.com/example/repo.git",
				RepoBranch:    "main",
			},
		},
		{
			name: "no reconcile config",
			cfg: &Config{
				WebhookSecret: "s",
				PollInterval:  30 * time.Minute,
			},
			want: ConfigResponse{
				WebhookSecret: "s",
				PollInterval:  1800,
			},
		},
		{
			name: "zero poll interval",
			cfg: &Config{
				WebhookSecret: "x",
				PollInterval:  0,
			},
			want: ConfigResponse{
				WebhookSecret: "x",
				PollInterval:  0,
			},
		},
		{
			name: "empty config",
			cfg:  &Config{},
			want: ConfigResponse{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, buildConfigResponse(tt.cfg))
		})
	}
}

func TestConvertDriftItems(t *testing.T) {
	tests := []struct {
		name  string
		items []reconcile.DriftItem
		want  []APIDriftItem
	}{
		{
			name:  "nil items returns empty slice",
			items: nil,
			want:  []APIDriftItem{},
		},
		{
			name:  "empty items returns empty slice",
			items: []reconcile.DriftItem{},
			want:  []APIDriftItem{},
		},
		{
			name: "converts items",
			items: []reconcile.DriftItem{
				{Service: "web", Type: "image", Declared: "nginx:1.25", Actual: "nginx:1.24"},
				{Service: "db", Type: "missing", Declared: "postgres:16", Actual: ""},
			},
			want: []APIDriftItem{
				{Service: "web", Type: "image", Declared: "nginx:1.25", Actual: "nginx:1.24"},
				{Service: "db", Type: "missing", Declared: "postgres:16", Actual: ""},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, convertDriftItems(tt.items))
		})
	}
}

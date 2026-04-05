package reconcile

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShouldSkipDeploy(t *testing.T) {
	tests := []struct {
		name           string
		lastDeployed   string
		current        string
		force          bool
		needsRedeploy  bool
		want           bool
	}{
		{
			name:         "same commit without force skips",
			lastDeployed: "abc123",
			current:      "abc123",
			force:        false,
			want:         true,
		},
		{
			name:         "same commit with force does not skip",
			lastDeployed: "abc123",
			current:      "abc123",
			force:        true,
			want:         false,
		},
		{
			name:         "different commit does not skip",
			lastDeployed: "abc123",
			current:      "def456",
			force:        false,
			want:         false,
		},
		{
			name:         "empty last deployed never skips",
			lastDeployed: "",
			current:      "abc123",
			force:        false,
			want:         false,
		},
		{
			name:         "both empty without force skips",
			lastDeployed: "",
			current:      "",
			force:        false,
			want:         true,
		},
		{
			name:          "same commit with needsRedeploy does not skip",
			lastDeployed:  "abc123",
			current:       "abc123",
			needsRedeploy: true,
			want:          false,
		},
		{
			name:          "needsRedeploy overrides same commit match",
			lastDeployed:  "abc123",
			current:       "abc123",
			force:         false,
			needsRedeploy: true,
			want:          false,
		},
		{
			name:          "needsRedeploy false with same commit still skips",
			lastDeployed:  "abc123",
			current:       "abc123",
			force:         false,
			needsRedeploy: false,
			want:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldSkipDeploy(tt.lastDeployed, tt.current, tt.force, tt.needsRedeploy)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHasMissingDeclaredServices(t *testing.T) {
	tests := []struct {
		name     string
		declared []DeclaredService
		actual   []ActualService
		want     bool
	}{
		{
			name:     "empty declared returns false",
			declared: nil,
			actual:   []ActualService{{Name: "web", State: "running"}},
			want:     false,
		},
		{
			name:     "all declared services running",
			declared: []DeclaredService{{Name: "web"}, {Name: "api"}},
			actual: []ActualService{
				{Name: "web", State: "running"},
				{Name: "api", State: "running"},
			},
			want: false,
		},
		{
			name:     "one service missing",
			declared: []DeclaredService{{Name: "web"}, {Name: "db"}},
			actual:   []ActualService{{Name: "web", State: "running"}},
			want:     true,
		},
		{
			name:     "service exists but not running",
			declared: []DeclaredService{{Name: "web"}},
			actual:   []ActualService{{Name: "web", State: "exited"}},
			want:     true,
		},
		{
			name:     "no actual containers at all",
			declared: []DeclaredService{{Name: "web"}},
			actual:   nil,
			want:     true,
		},
		{
			name:     "extra actual services are ignored",
			declared: []DeclaredService{{Name: "web"}},
			actual: []ActualService{
				{Name: "web", State: "running"},
				{Name: "extra", State: "running"},
			},
			want: false,
		},
		{
			name:     "newly added service not yet running",
			declared: []DeclaredService{{Name: "web"}, {Name: "api"}, {Name: "newservice"}},
			actual: []ActualService{
				{Name: "web", State: "running"},
				{Name: "api", State: "running"},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasMissingDeclaredServices(tt.declared, tt.actual)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestShouldTriggerCircuitBreaker(t *testing.T) {
	tests := []struct {
		name          string
		lastAttempted string
		current       string
		attempts      int
		maxAttempts   int
		force         bool
		want          bool
	}{
		{
			name:          "triggers at max attempts on same commit",
			lastAttempted: "abc123",
			current:       "abc123",
			attempts:      3,
			maxAttempts:   3,
			force:         false,
			want:          true,
		},
		{
			name:          "triggers above max attempts on same commit",
			lastAttempted: "abc123",
			current:       "abc123",
			attempts:      5,
			maxAttempts:   3,
			force:         false,
			want:          true,
		},
		{
			name:          "does not trigger below max attempts",
			lastAttempted: "abc123",
			current:       "abc123",
			attempts:      2,
			maxAttempts:   3,
			force:         false,
			want:          false,
		},
		{
			name:          "does not trigger on different commit",
			lastAttempted: "abc123",
			current:       "def456",
			attempts:      5,
			maxAttempts:   3,
			force:         false,
			want:          false,
		},
		{
			name:          "force overrides circuit breaker",
			lastAttempted: "abc123",
			current:       "abc123",
			attempts:      5,
			maxAttempts:   3,
			force:         true,
			want:          false,
		},
		{
			name:          "zero attempts never triggers",
			lastAttempted: "abc123",
			current:       "abc123",
			attempts:      0,
			maxAttempts:   3,
			force:         false,
			want:          false,
		},
		{
			name:          "empty last attempted does not trigger",
			lastAttempted: "",
			current:       "abc123",
			attempts:      5,
			maxAttempts:   3,
			force:         false,
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldTriggerCircuitBreaker(tt.lastAttempted, tt.current, tt.attempts, tt.maxAttempts, tt.force)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNextAttemptState(t *testing.T) {
	tests := []struct {
		name             string
		lastAttempted    string
		current          string
		currentCount     int
		wantLastAttempt  string
		wantCount        int
	}{
		{
			name:            "same commit increments count",
			lastAttempted:   "abc123",
			current:         "abc123",
			currentCount:    2,
			wantLastAttempt: "abc123",
			wantCount:       3,
		},
		{
			name:            "new commit resets count to 1",
			lastAttempted:   "abc123",
			current:         "def456",
			currentCount:    5,
			wantLastAttempt: "def456",
			wantCount:       1,
		},
		{
			name:            "first attempt on empty last sets count to 1",
			lastAttempted:   "",
			current:         "abc123",
			currentCount:    0,
			wantLastAttempt: "abc123",
			wantCount:       1,
		},
		{
			name:            "same commit from zero increments to 1",
			lastAttempted:   "abc123",
			current:         "abc123",
			currentCount:    0,
			wantLastAttempt: "abc123",
			wantCount:       1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotAttempt, gotCount := nextAttemptState(tt.lastAttempted, tt.current, tt.currentCount)
			assert.Equal(t, tt.wantLastAttempt, gotAttempt)
			assert.Equal(t, tt.wantCount, gotCount)
		})
	}
}

func TestResolveTargetHost(t *testing.T) {
	tests := []struct {
		name       string
		configHost string
		secrets    map[string]any
		want       string
	}{
		{
			name:       "config host takes priority",
			configHost: "root@10.0.0.1",
			secrets:    map[string]any{"network": map[string]any{"unraid_ip": "10.0.0.2"}},
			want:       "root@10.0.0.1",
		},
		{
			name:       "falls back to secrets network.unraid_ip",
			configHost: "",
			secrets:    map[string]any{"network": map[string]any{"unraid_ip": "10.0.0.2"}},
			want:       "root@10.0.0.2",
		},
		{
			name:       "returns empty when no config and no secrets",
			configHost: "",
			secrets:    map[string]any{},
			want:       "",
		},
		{
			name:       "returns empty when secrets has wrong type",
			configHost: "",
			secrets:    map[string]any{"network": "not a map"},
			want:       "",
		},
		{
			name:       "returns empty when network missing unraid_ip",
			configHost: "",
			secrets:    map[string]any{"network": map[string]any{"other_key": "value"}},
			want:       "",
		},
		{
			name:       "returns empty with nil secrets",
			configHost: "",
			secrets:    nil,
			want:       "",
		},
		{
			name:       "handles unraid_ip with non-string value",
			configHost: "",
			secrets:    map[string]any{"network": map[string]any{"unraid_ip": 12345}},
			want:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveTargetHost(tt.configHost, tt.secrets)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolveDeployModeWithSecrets(t *testing.T) {
	existingStat := func(path string) (os.FileInfo, error) { return nil, nil }
	missingStat := func(path string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	secretsWithIP := map[string]any{"network": map[string]any{"unraid_ip": "192.168.1.100"}}

	tests := []struct {
		name           string
		configHost     string
		localAppdata   string
		secrets        map[string]any
		statFn         func(string) (os.FileInfo, error)
		wantLocal      bool
		wantErr        bool
	}{
		{
			name:       "config host set forces remote",
			configHost: "user@remote",
			statFn:     existingStat,
			wantLocal:  false,
		},
		{
			name:      "secrets host forces remote",
			secrets:   secretsWithIP,
			statFn:    existingStat,
			wantLocal: false,
		},
		{
			name:         "local path accessible and no host returns local",
			localAppdata: "/mnt/appdata",
			statFn:       existingStat,
			wantLocal:    true,
		},
		{
			name:         "local path inaccessible with secrets host falls back to remote",
			localAppdata: "/mnt/appdata",
			secrets:      secretsWithIP,
			statFn:       missingStat,
			wantLocal:    false,
		},
		{
			name:         "local path inaccessible with config host falls back to remote",
			configHost:   "user@remote",
			localAppdata: "/mnt/appdata",
			statFn:       missingStat,
			wantLocal:    false,
		},
		{
			name:         "local path inaccessible with no host returns error",
			localAppdata: "/mnt/appdata",
			statFn:       missingStat,
			wantErr:      true,
		},
		{
			name:      "no local path and no host returns remote",
			statFn:    existingStat,
			wantLocal: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			local, err := resolveDeployModeWithSecrets(tt.configHost, tt.localAppdata, tt.secrets, tt.statFn)
			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, ErrAppdataInaccessible)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantLocal, local)
		})
	}
}

func TestBuildComposeArgs(t *testing.T) {
	tests := []struct {
		name        string
		projectName string
		files       []string
		want        []string
	}{
		{
			name:        "with project name and files",
			projectName: "bosun",
			files:       []string{"core.yml", "monitoring.yml"},
			want:        []string{"compose", "-p", "bosun", "-f", "core.yml", "-f", "monitoring.yml"},
		},
		{
			name:        "without project name",
			projectName: "",
			files:       []string{"core.yml"},
			want:        []string{"compose", "-f", "core.yml"},
		},
		{
			name:        "no files",
			projectName: "bosun",
			files:       nil,
			want:        []string{"compose", "-p", "bosun"},
		},
		{
			name:        "empty project name and no files",
			projectName: "",
			files:       nil,
			want:        []string{"compose"},
		},
		{
			name:        "multiple files without project",
			projectName: "",
			files:       []string{"a.yml", "b.yml", "c.yml"},
			want:        []string{"compose", "-f", "a.yml", "-f", "b.yml", "-f", "c.yml"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildComposeArgs(tt.projectName, tt.files)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestClassifySSHError(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		want   string
	}{
		{
			name:   "permission denied",
			stderr: "user@host: Permission denied (publickey).",
			want:   "auth",
		},
		{
			name:   "connection refused",
			stderr: "ssh: connect to host 10.0.0.1 port 22: Connection refused",
			want:   "connection",
		},
		{
			name:   "host key verification",
			stderr: "Host key verification failed.",
			want:   "host_key",
		},
		{
			name:   "dns resolution failure",
			stderr: "ssh: Could not resolve hostname myhost: Name or service not known",
			want:   "dns",
		},
		{
			name:   "connection timed out",
			stderr: "ssh: connect to host 10.0.0.1 port 22: Connection timed out",
			want:   "timeout",
		},
		{
			name:   "no route to host",
			stderr: "ssh: connect to host 10.0.0.1 port 22: No route to host",
			want:   "connection",
		},
		{
			name:   "unknown error",
			stderr: "some unexpected error message",
			want:   "unknown",
		},
		{
			name:   "empty stderr",
			stderr: "",
			want:   "unknown",
		},
		{
			name:   "case insensitive matching",
			stderr: "PERMISSION DENIED (publickey)",
			want:   "auth",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifySSHError(tt.stderr)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildKnownHostsPaths(t *testing.T) {
	tests := []struct {
		name          string
		envKnownHosts string
		want          []string
	}{
		{
			name:          "env var set includes both paths",
			envKnownHosts: "/custom/known_hosts",
			want:          []string{"/custom/known_hosts", "/config/known_hosts"},
		},
		{
			name:          "env not set returns only container path",
			envKnownHosts: "",
			want:          []string{"/config/known_hosts"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildKnownHostsPaths(tt.envKnownHosts)
			assert.Equal(t, tt.want, got)
			// Verify no implicit home-dir known_hosts sneaks in.
			// Only check paths that were NOT explicitly provided via envKnownHosts,
			// since a caller could legitimately pass a path containing ".ssh".
			for _, p := range got {
				if p == tt.envKnownHosts {
					continue
				}
				assert.NotContains(t, p, ".ssh/known_hosts",
					"implicit ~/.ssh/known_hosts must not be consulted")
			}
		})
	}
}

func TestClassifyComposePS(t *testing.T) {
	tests := []struct {
		name          string
		entries       []composePSEntry
		wantKind      composeFailureKind
		wantUnhealthy []string
		wantFailed    []string
	}{
		{
			name: "all healthy returns start failure for unclassified compose error",
			entries: []composePSEntry{
				{Name: "app", State: "running", Health: "healthy"},
				{Name: "db", State: "running", Health: ""},
			},
			wantKind:      failureStartFailure,
			wantUnhealthy: nil,
			wantFailed:    nil,
		},
		{
			name: "one unhealthy container",
			entries: []composePSEntry{
				{Name: "app", State: "running", Health: "healthy"},
				{Name: "obsidian", State: "running", Health: "unhealthy"},
			},
			wantKind:      failureUnhealthyOnly,
			wantUnhealthy: []string{"obsidian"},
			wantFailed:    nil,
		},
		{
			name: "multiple unhealthy containers",
			entries: []composePSEntry{
				{Name: "app", State: "running", Health: "unhealthy"},
				{Name: "db", State: "running", Health: "unhealthy"},
				{Name: "redis", State: "running", Health: "healthy"},
			},
			wantKind:      failureUnhealthyOnly,
			wantUnhealthy: []string{"app", "db"},
			wantFailed:    nil,
		},
		{
			name: "exited container is start failure",
			entries: []composePSEntry{
				{Name: "app", State: "running", Health: "healthy"},
				{Name: "broken", State: "exited", Health: ""},
			},
			wantKind:      failureStartFailure,
			wantUnhealthy: nil,
			wantFailed:    []string{"broken"},
		},
		{
			name: "restarting container is start failure",
			entries: []composePSEntry{
				{Name: "app", State: "running", Health: "healthy"},
				{Name: "crashloop", State: "restarting", Health: ""},
			},
			wantKind:      failureStartFailure,
			wantUnhealthy: nil,
			wantFailed:    []string{"crashloop"},
		},
		{
			name: "dead container is start failure",
			entries: []composePSEntry{
				{Name: "app", State: "running", Health: "healthy"},
				{Name: "gone", State: "dead", Health: ""},
			},
			wantKind:      failureStartFailure,
			wantUnhealthy: nil,
			wantFailed:    []string{"gone"},
		},
		{
			name: "mixed unhealthy and failed",
			entries: []composePSEntry{
				{Name: "app", State: "running", Health: "unhealthy"},
				{Name: "broken", State: "exited", Health: ""},
			},
			wantKind:      failureStartFailure,
			wantUnhealthy: []string{"app"},
			wantFailed:    []string{"broken"},
		},
		{
			name:          "empty entries returns start failure",
			entries:       []composePSEntry{},
			wantKind:      failureStartFailure,
			wantUnhealthy: nil,
			wantFailed:    nil,
		},
		{
			name: "starting health treated as unhealthy",
			entries: []composePSEntry{
				{Name: "app", State: "running", Health: "starting"},
			},
			wantKind:      failureUnhealthyOnly,
			wantUnhealthy: []string{"app"},
			wantFailed:    nil,
		},
		{
			name: "case insensitive state",
			entries: []composePSEntry{
				{Name: "app", State: "Running", Health: "Unhealthy"},
			},
			wantKind:      failureUnhealthyOnly,
			wantUnhealthy: []string{"app"},
			wantFailed:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyComposePS(tt.entries)
			assert.Equal(t, tt.wantKind, result.Kind)
			assert.Equal(t, tt.wantUnhealthy, result.Unhealthy)
			assert.Equal(t, tt.wantFailed, result.Failed)
		})
	}
}

func TestParseComposePSOutput(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		want    []composePSEntry
		wantErr bool
	}{
		{
			name:   "single container NDJSON",
			output: `{"Name":"app","State":"running","Health":"healthy"}`,
			want:   []composePSEntry{{Name: "app", State: "running", Health: "healthy"}},
		},
		{
			name: "multiple containers NDJSON",
			output: `{"Name":"app","State":"running","Health":"healthy"}
{"Name":"db","State":"running","Health":"unhealthy"}`,
			want: []composePSEntry{
				{Name: "app", State: "running", Health: "healthy"},
				{Name: "db", State: "running", Health: "unhealthy"},
			},
		},
		{
			name:   "empty output",
			output: "",
			want:   nil,
		},
		{
			name:   "whitespace only",
			output: "  \n  \n  ",
			want:   nil,
		},
		{
			name:    "invalid JSON",
			output:  `not json at all`,
			wantErr: true,
		},
		{
			name: "trailing newline",
			output: `{"Name":"app","State":"running","Health":""}
`,
			want: []composePSEntry{{Name: "app", State: "running", Health: ""}},
		},
		{
			name:   "JSON array single container",
			output: `[{"Name":"app","State":"running","Health":"healthy"}]`,
			want:   []composePSEntry{{Name: "app", State: "running", Health: "healthy"}},
		},
		{
			name:   "JSON array multiple containers",
			output: `[{"Name":"app","State":"running","Health":"healthy"},{"Name":"db","State":"running","Health":"unhealthy"}]`,
			want: []composePSEntry{
				{Name: "app", State: "running", Health: "healthy"},
				{Name: "db", State: "running", Health: "unhealthy"},
			},
		},
		{
			name:   "JSON array empty",
			output: `[]`,
			want:   nil,
		},
		{
			name:    "JSON array invalid",
			output:  `[not json]`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseComposePSOutput([]byte(tt.output))
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFilterIgnoredDrift(t *testing.T) {
	tests := []struct {
		name      string
		items     []DriftItem
		rules     []DriftIgnoreRule
		wantItems []DriftItem
	}{
		{
			name: "no rules returns all items",
			items: []DriftItem{
				{Service: "traefik", Type: DriftMissing},
			},
			rules:     nil,
			wantItems: []DriftItem{{Service: "traefik", Type: DriftMissing}},
		},
		{
			name:      "no items returns nil",
			items:     nil,
			rules:     []DriftIgnoreRule{{Service: "traefik", Type: "*"}},
			wantItems: nil,
		},
		{
			name: "exact service and type match",
			items: []DriftItem{
				{Service: "traefik", Type: DriftUnhealthy},
				{Service: "api", Type: DriftMissing},
			},
			rules:     []DriftIgnoreRule{{Service: "traefik", Type: "unhealthy"}},
			wantItems: []DriftItem{{Service: "api", Type: DriftMissing}},
		},
		{
			name: "wildcard type matches all drift types",
			items: []DriftItem{
				{Service: "traefik", Type: DriftUnhealthy},
				{Service: "traefik", Type: DriftImageMismatch},
				{Service: "api", Type: DriftMissing},
			},
			rules:     []DriftIgnoreRule{{Service: "traefik", Type: "*"}},
			wantItems: []DriftItem{{Service: "api", Type: DriftMissing}},
		},
		{
			name: "glob pattern on service name",
			items: []DriftItem{
				{Service: "monitoring-grafana", Type: DriftUnhealthy},
				{Service: "monitoring-prometheus", Type: DriftMissing},
				{Service: "api", Type: DriftMissing},
			},
			rules:     []DriftIgnoreRule{{Service: "monitoring-*", Type: "*"}},
			wantItems: []DriftItem{{Service: "api", Type: DriftMissing}},
		},
		{
			name: "multiple rules applied",
			items: []DriftItem{
				{Service: "traefik", Type: DriftUnhealthy},
				{Service: "gatus", Type: DriftImageMismatch},
				{Service: "api", Type: DriftMissing},
			},
			rules: []DriftIgnoreRule{
				{Service: "traefik", Type: "unhealthy"},
				{Service: "gatus", Type: "image_mismatch"},
			},
			wantItems: []DriftItem{{Service: "api", Type: DriftMissing}},
		},
		{
			name: "type mismatch does not filter",
			items: []DriftItem{
				{Service: "traefik", Type: DriftMissing},
			},
			rules:     []DriftIgnoreRule{{Service: "traefik", Type: "unhealthy"}},
			wantItems: []DriftItem{{Service: "traefik", Type: DriftMissing}},
		},
		{
			name: "service mismatch does not filter",
			items: []DriftItem{
				{Service: "api", Type: DriftUnhealthy},
			},
			rules:     []DriftIgnoreRule{{Service: "traefik", Type: "unhealthy"}},
			wantItems: []DriftItem{{Service: "api", Type: DriftUnhealthy}},
		},
		{
			name: "invalid glob pattern is silently skipped",
			items: []DriftItem{
				{Service: "traefik", Type: DriftUnhealthy},
			},
			rules:     []DriftIgnoreRule{{Service: "[invalid", Type: "*"}},
			wantItems: []DriftItem{{Service: "traefik", Type: DriftUnhealthy}},
		},
		{
			name: "all items filtered returns nil",
			items: []DriftItem{
				{Service: "traefik", Type: DriftUnhealthy},
			},
			rules:     []DriftIgnoreRule{{Service: "traefik", Type: "*"}},
			wantItems: nil,
		},
		{
			name: "question mark glob pattern",
			items: []DriftItem{
				{Service: "db1", Type: DriftMissing},
				{Service: "db2", Type: DriftMissing},
				{Service: "api", Type: DriftMissing},
			},
			rules:     []DriftIgnoreRule{{Service: "db?", Type: "missing"}},
			wantItems: []DriftItem{{Service: "api", Type: DriftMissing}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterIgnoredDrift(tt.items, tt.rules)
			assert.Equal(t, tt.wantItems, got)
		})
	}
}

func TestBuildSSHKeyPaths(t *testing.T) {
	tests := []struct {
		name    string
		envKey  string
		homeDir string
		want    []string
	}{
		{
			name:    "all paths populated",
			envKey:  "/custom/key",
			homeDir: "/home/user",
			want: []string{
				"/custom/key",
				"/config/deploy-key",
				"/config/ssh-key",
				"/home/user/.ssh/id_ed25519",
				"/home/user/.ssh/id_rsa",
			},
		},
		{
			name:    "env not set filters empty string",
			envKey:  "",
			homeDir: "/home/user",
			want: []string{
				"/config/deploy-key",
				"/config/ssh-key",
				"/home/user/.ssh/id_ed25519",
				"/home/user/.ssh/id_rsa",
			},
		},
		{
			name:    "empty home dir still produces paths",
			envKey:  "",
			homeDir: "",
			want: []string{
				"/config/deploy-key",
				"/config/ssh-key",
				".ssh/id_ed25519",
				".ssh/id_rsa",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildSSHKeyPaths(tt.envKey, tt.homeDir)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestClassifyComposeResults(t *testing.T) {
	tests := []struct {
		name       string
		results    []ComposeFileResult
		wantSucc   int
		wantFail   int
		wantRB     int
	}{
		{
			name: "all succeed",
			results: []ComposeFileResult{
				{File: "a.yml", Success: true},
				{File: "b.yml", Success: true},
			},
			wantSucc: 2, wantFail: 0, wantRB: 0,
		},
		{
			name: "one fails with rollback",
			results: []ComposeFileResult{
				{File: "a.yml", Success: true},
				{File: "b.yml", Success: false, RolledBack: true, Err: fmt.Errorf("bad image")},
			},
			wantSucc: 1, wantFail: 1, wantRB: 1,
		},
		{
			name: "one fails without rollback",
			results: []ComposeFileResult{
				{File: "a.yml", Success: false, Err: fmt.Errorf("bad image")},
				{File: "b.yml", Success: true},
			},
			wantSucc: 1, wantFail: 1, wantRB: 0,
		},
		{
			name: "all fail",
			results: []ComposeFileResult{
				{File: "a.yml", Success: false, Err: fmt.Errorf("err1")},
				{File: "b.yml", Success: false, RolledBack: true, Err: fmt.Errorf("err2")},
			},
			wantSucc: 0, wantFail: 2, wantRB: 1,
		},
		{
			name:     "empty results",
			results:  nil,
			wantSucc: 0, wantFail: 0, wantRB: 0,
		},
		{
			name: "mixed with unhealthy (counted as success)",
			results: []ComposeFileResult{
				{File: "a.yml", Success: true, Err: ErrComposeUnhealthy},
				{File: "b.yml", Success: false, RolledBack: true, Err: fmt.Errorf("fail")},
				{File: "c.yml", Success: true},
			},
			wantSucc: 2, wantFail: 1, wantRB: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := classifyComposeResults(tt.results)
			assert.Equal(t, tt.wantSucc, s.Succeeded)
			assert.Equal(t, tt.wantFail, s.Failed)
			assert.Equal(t, tt.wantRB, s.RolledBack)
			assert.Equal(t, tt.results, s.Results)
		})
	}
}

func TestDetectNameConflicts(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		want   []string
	}{
		{
			name:   "no conflicts returns nil",
			stderr: "some unrelated docker error",
			want:   nil,
		},
		{
			name:   "empty stderr returns nil",
			stderr: "",
			want:   nil,
		},
		{
			name:   "single conflict without leading slash",
			stderr: `Conflict. The container name "agregarr" is already in use by container "abc123". You have to remove (or rename) that container to be able to reuse that name.`,
			want:   []string{"agregarr"},
		},
		{
			name:   "single conflict with leading slash (docker format)",
			stderr: `Conflict. The container name "/agregarr" is already in use by container "abc123".`,
			want:   []string{"agregarr"},
		},
		{
			name: "multiple conflicts across lines",
			stderr: `Conflict. The container name "/web" is already in use by container "abc".
Conflict. The container name "/db" is already in use by container "def".`,
			want: []string{"web", "db"},
		},
		{
			name:   "conflict embedded in larger error block",
			stderr: "Error response from daemon: driver failed programming\nConflict. The container name \"/traefik\" is already in use by container \"xyz\".\nAdditional context here.",
			want:   []string{"traefik"},
		},
		{
			name:   "container name with hyphens and underscores",
			stderr: `Conflict. The container name "/my-app_1" is already in use`,
			want:   []string{"my-app_1"},
		},
		{
			name:   "duplicate conflict names are preserved",
			stderr: "Conflict. The container name \"/traefik\" is already in use by container \"abc\".\nConflict. The container name \"/traefik\" is already in use by container \"def\".",
			want:   []string{"traefik", "traefik"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectNameConflicts(tt.stderr)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildOrphanPassFiles(t *testing.T) {
	backupPath := "/backups/2024-01-01"

	tests := []struct {
		name    string
		results []ComposeFileResult
		want    []string
	}{
		{
			name: "all succeed use original paths",
			results: []ComposeFileResult{
				{File: "/compose/a.yml", Success: true},
				{File: "/compose/b.yml", Success: true},
			},
			want: []string{"/compose/a.yml", "/compose/b.yml"},
		},
		{
			name: "rolled back uses backup path",
			results: []ComposeFileResult{
				{File: "/compose/a.yml", Success: true},
				{File: "/compose/b.yml", Success: false, RolledBack: true},
			},
			want: []string{
				"/compose/a.yml",
				filepath.Join(backupPath, "b.yml"),
			},
		},
		{
			name: "failed without rollback uses original",
			results: []ComposeFileResult{
				{File: "/compose/a.yml", Success: false, RolledBack: false},
			},
			want: []string{"/compose/a.yml"},
		},
		{
			name:    "empty results",
			results: nil,
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildOrphanPassFiles(tt.results, backupPath)
			assert.Equal(t, tt.want, got)
		})
	}
}

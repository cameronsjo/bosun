package reconcile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompareDrift_NoDrift(t *testing.T) {
	declared := []DeclaredService{
		{Name: "web", Image: "nginx:latest"},
		{Name: "api", Image: "myapp:v1.2"},
	}
	actual := []ActualService{
		{Name: "web", Image: "nginx:latest", State: "running", Health: "healthy"},
		{Name: "api", Image: "myapp:v1.2", State: "running", Health: ""},
	}

	report := CompareDrift(declared, actual)

	assert.False(t, report.HasDrift())
	assert.False(t, report.HasCriticalDrift())
	assert.Empty(t, report.Items)
}

func TestCompareDrift_MissingService(t *testing.T) {
	declared := []DeclaredService{
		{Name: "web", Image: "nginx:latest"},
		{Name: "api", Image: "myapp:v1.2"},
	}
	actual := []ActualService{
		{Name: "web", Image: "nginx:latest", State: "running"},
	}

	report := CompareDrift(declared, actual)

	assert.True(t, report.HasDrift())
	assert.True(t, report.HasCriticalDrift())
	require.Len(t, report.Items, 1)
	assert.Equal(t, "api", report.Items[0].Service)
	assert.Equal(t, DriftMissing, report.Items[0].Type)
	assert.Equal(t, "myapp:v1.2", report.Items[0].Declared)
}

func TestCompareDrift_StoppedService(t *testing.T) {
	declared := []DeclaredService{
		{Name: "web", Image: "nginx:latest"},
	}
	actual := []ActualService{
		{Name: "web", Image: "nginx:latest", State: "exited"},
	}

	report := CompareDrift(declared, actual)

	assert.True(t, report.HasDrift())
	require.Len(t, report.Items, 1)
	assert.Equal(t, DriftMissing, report.Items[0].Type)
	assert.Equal(t, "state=exited", report.Items[0].Actual)
}

func TestCompareDrift_ImageMismatch(t *testing.T) {
	declared := []DeclaredService{
		{Name: "web", Image: "nginx:1.25"},
	}
	actual := []ActualService{
		{Name: "web", Image: "nginx:1.24", State: "running"},
	}

	report := CompareDrift(declared, actual)

	assert.True(t, report.HasDrift())
	assert.False(t, report.HasCriticalDrift()) // Image mismatch is not critical.
	require.Len(t, report.Items, 1)
	assert.Equal(t, DriftImageMismatch, report.Items[0].Type)
	assert.Equal(t, "nginx:1.25", report.Items[0].Declared)
	assert.Equal(t, "nginx:1.24", report.Items[0].Actual)
}

func TestCompareDrift_UnhealthyService(t *testing.T) {
	declared := []DeclaredService{
		{Name: "web", Image: "nginx:latest"},
	}
	actual := []ActualService{
		{Name: "web", Image: "nginx:latest", State: "running", Health: "unhealthy"},
	}

	report := CompareDrift(declared, actual)

	assert.True(t, report.HasDrift())
	assert.True(t, report.HasCriticalDrift())
	require.Len(t, report.Items, 1)
	assert.Equal(t, DriftUnhealthy, report.Items[0].Type)
}

func TestDriftSummaries(t *testing.T) {
	report := &DriftReport{
		Items: []DriftItem{
			{Service: "web", Type: DriftUnhealthy},
			{Service: "api", Type: DriftMissing},
			{Service: "redis", Type: DriftImageMismatch},
		},
	}

	summaries := report.DriftSummaries()
	require.Len(t, summaries, 3)
	assert.Equal(t, "web:unhealthy", summaries[0])
	assert.Equal(t, "api:missing", summaries[1])
	assert.Equal(t, "redis:image_mismatch", summaries[2])
}

func TestDriftSummaries_Empty(t *testing.T) {
	report := &DriftReport{}
	assert.Nil(t, report.DriftSummaries())
}

func TestFormatHealthDetail(t *testing.T) {
	tests := []struct {
		name     string
		details  *docker.ContainerDetails
		expected string
	}{
		{
			name:     "no health log",
			details:  &docker.ContainerDetails{HealthLog: nil},
			expected: "unhealthy (no health log)",
		},
		{
			name: "with exit code and output",
			details: &docker.ContainerDetails{
				HealthFailingStreak: 5,
				HealthLog: &docker.HealthCheckLog{
					ExitCode: 1,
					Output:   "curl: (7) Failed to connect to localhost port 8080",
				},
			},
			expected: "failing_streak=5, last_exit=1, output=curl: (7) Failed to connect to localhost port 8080",
		},
		{
			name: "exit code zero but failing streak",
			details: &docker.ContainerDetails{
				HealthFailingStreak: 3,
				HealthLog: &docker.HealthCheckLog{
					ExitCode: 0,
					Output:   "OK",
				},
			},
			expected: "failing_streak=3, last_exit=0, output=OK",
		},
		{
			name: "long output truncated",
			details: &docker.ContainerDetails{
				HealthFailingStreak: 1,
				HealthLog: &docker.HealthCheckLog{
					ExitCode: 1,
					Output:   strings.Repeat("x", 300),
				},
			},
			expected: "failing_streak=1, last_exit=1, output=" + strings.Repeat("x", 197) + "...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatHealthDetail(tt.details)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCompareDrift_MultipleDriftTypes(t *testing.T) {
	declared := []DeclaredService{
		{Name: "web", Image: "nginx:latest"},
		{Name: "api", Image: "myapp:v2"},
		{Name: "db", Image: "postgres:16"},
	}
	actual := []ActualService{
		{Name: "web", Image: "nginx:latest", State: "running", Health: "unhealthy"},
		{Name: "api", Image: "myapp:v1", State: "running"},
		// db is missing entirely
	}

	report := CompareDrift(declared, actual)

	assert.True(t, report.HasDrift())
	assert.True(t, report.HasCriticalDrift())
	assert.Len(t, report.Items, 3) // unhealthy + mismatch + missing
}

func TestImagesMatch(t *testing.T) {
	tests := []struct {
		declared string
		actual   string
		match    bool
	}{
		{"nginx:latest", "nginx:latest", true},
		{"nginx", "nginx:latest", true},
		{"nginx:latest", "nginx", true},
		{"nginx:1.25", "nginx:1.25", true},
		{"nginx:1.25", "nginx:1.24", false},
		{"nginx", "nginx:1.24", false},
		{"myrepo/app:v1", "myrepo/app:v1", true},
		{"myrepo/app:v1", "myrepo/app:v2", false},
		// Digest references
		{"nginx@sha256:abc123", "nginx@sha256:abc123", true},
		{"nginx@sha256:abc123", "nginx@sha256:def456", false},
	}

	for _, tt := range tests {
		t.Run(tt.declared+"_vs_"+tt.actual, func(t *testing.T) {
			assert.Equal(t, tt.match, imagesMatch(tt.declared, tt.actual))
		})
	}
}

func TestNormalizeImage(t *testing.T) {
	assert.Equal(t, "nginx:latest", normalizeImage("nginx"))
	assert.Equal(t, "nginx:1.25", normalizeImage("nginx:1.25"))
	assert.Equal(t, "nginx@sha256:abc", normalizeImage("nginx@sha256:abc"))
}

func TestExtractDeclaredState(t *testing.T) {
	dir := t.TempDir()
	composeDir := filepath.Join(dir, "compose")
	require.NoError(t, os.MkdirAll(composeDir, 0755))

	// Write a compose file.
	composeYAML := `services:
  web:
    image: nginx:latest
  api:
    image: myapp:v1.2
  db:
    image: postgres:16
`
	require.NoError(t, os.WriteFile(filepath.Join(composeDir, "core.yml"), []byte(composeYAML), 0644))

	declared, err := ExtractDeclaredState(dir)
	require.NoError(t, err)
	require.Len(t, declared, 3)

	// Should be sorted by name.
	assert.Equal(t, "api", declared[0].Name)
	assert.Equal(t, "myapp:v1.2", declared[0].Image)
	assert.Equal(t, "db", declared[1].Name)
	assert.Equal(t, "web", declared[2].Name)
}

func TestExtractDeclaredState_MultipleFiles(t *testing.T) {
	dir := t.TempDir()
	composeDir := filepath.Join(dir, "compose")
	require.NoError(t, os.MkdirAll(composeDir, 0755))

	file1 := `services:
  web:
    image: nginx:latest
`
	file2 := `services:
  api:
    image: myapp:v1
`
	require.NoError(t, os.WriteFile(filepath.Join(composeDir, "core.yml"), []byte(file1), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(composeDir, "apps.yml"), []byte(file2), 0644))

	declared, err := ExtractDeclaredState(dir)
	require.NoError(t, err)
	require.Len(t, declared, 2)
}

func TestExtractDeclaredState_NoComposeDir(t *testing.T) {
	dir := t.TempDir()

	declared, err := ExtractDeclaredState(dir)
	require.NoError(t, err)
	assert.Empty(t, declared)
}

func TestExtractDeclaredState_DeduplicatesServices(t *testing.T) {
	dir := t.TempDir()
	composeDir := filepath.Join(dir, "compose")
	require.NoError(t, os.MkdirAll(composeDir, 0755))

	// Same service in two files.
	file1 := `services:
  web:
    image: nginx:latest
`
	require.NoError(t, os.WriteFile(filepath.Join(composeDir, "a.yml"), []byte(file1), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(composeDir, "b.yml"), []byte(file1), 0644))

	declared, err := ExtractDeclaredState(dir)
	require.NoError(t, err)
	assert.Len(t, declared, 1)
}

func TestMatchContainer_WithLabels(t *testing.T) {
	tests := []struct {
		name        string
		container   docker.ContainerInfo
		projectName string
		wantService string
		wantMatched bool
	}{
		{
			name: "label match with project filter",
			container: docker.ContainerInfo{
				Name: "core-web-1",
				Labels: map[string]string{
					ComposeProjectLabel: "core",
					ComposeServiceLabel: "web",
				},
			},
			projectName: "core",
			wantService: "web",
			wantMatched: true,
		},
		{
			name: "label match without project filter",
			container: docker.ContainerInfo{
				Name: "core-web-1",
				Labels: map[string]string{
					ComposeProjectLabel: "core",
					ComposeServiceLabel: "web",
				},
			},
			projectName: "",
			wantService: "web",
			wantMatched: true,
		},
		{
			name: "label mismatch project",
			container: docker.ContainerInfo{
				Name: "other-web-1",
				Labels: map[string]string{
					ComposeProjectLabel: "other",
					ComposeServiceLabel: "web",
				},
			},
			projectName: "core",
			wantService: "",
			wantMatched: false,
		},
		{
			name: "hyphenated service name via label",
			container: docker.ContainerInfo{
				Name: "core-my-svc-2-1",
				Labels: map[string]string{
					ComposeProjectLabel: "core",
					ComposeServiceLabel: "my-svc-2",
				},
			},
			projectName: "core",
			wantService: "my-svc-2",
			wantMatched: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, matched := matchContainer(tt.container, tt.projectName)
			assert.Equal(t, tt.wantMatched, matched)
			assert.Equal(t, tt.wantService, svc)
		})
	}
}

func TestMatchContainer_NameFallback(t *testing.T) {
	tests := []struct {
		name        string
		container   docker.ContainerInfo
		projectName string
		wantService string
		wantMatched bool
	}{
		{
			name:        "name-based match",
			container:   docker.ContainerInfo{Name: "core-web-1"},
			projectName: "core",
			wantService: "web",
			wantMatched: true,
		},
		{
			name:        "name-based no project",
			container:   docker.ContainerInfo{Name: "web"},
			projectName: "",
			wantService: "web",
			wantMatched: true,
		},
		{
			name:        "name-based mismatch",
			container:   docker.ContainerInfo{Name: "other-web-1"},
			projectName: "core",
			wantService: "",
			wantMatched: false,
		},
		{
			name:        "name-based hyphenated service",
			container:   docker.ContainerInfo{Name: "core-my-service-1"},
			projectName: "core",
			wantService: "my-service",
			wantMatched: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, matched := matchContainer(tt.container, tt.projectName)
			assert.Equal(t, tt.wantMatched, matched)
			assert.Equal(t, tt.wantService, svc)
		})
	}
}

func TestDeployState_WithDriftFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deploy-state.json")

	state := &DeployState{
		LastDeployedCommit: "abc123",
		DeclaredServices: []DeclaredService{
			{Name: "web", Image: "nginx:latest"},
		},
		DriftItems: []DriftItem{
			{Service: "api", Type: DriftMissing, Declared: "myapp:v1"},
		},
	}

	require.NoError(t, SaveState(path, state))

	loaded := LoadState(path)
	assert.Equal(t, 2, loaded.SchemaVersion)
	assert.Equal(t, "abc123", loaded.LastDeployedCommit)
	require.Len(t, loaded.DeclaredServices, 1)
	assert.Equal(t, "web", loaded.DeclaredServices[0].Name)
	require.Len(t, loaded.DriftItems, 1)
	assert.Equal(t, "api", loaded.DriftItems[0].Service)
	assert.Equal(t, DriftMissing, loaded.DriftItems[0].Type)
}

func TestDeployState_BackwardsCompatibleLoad(t *testing.T) {
	// V1 state file (no drift fields) should load fine.
	data := `{"schema_version": 1, "last_deployed_commit": "old123"}`
	dir := t.TempDir()
	path := filepath.Join(dir, "deploy-state.json")
	require.NoError(t, os.WriteFile(path, []byte(data), 0644))

	loaded := LoadState(path)
	assert.Equal(t, 1, loaded.SchemaVersion)
	assert.Equal(t, "old123", loaded.LastDeployedCommit)
	assert.Empty(t, loaded.DeclaredServices)
	assert.Empty(t, loaded.DriftItems)
}

package reconcile

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeHookDiffPaths(t *testing.T) {
	tests := []struct {
		name        string
		infraSubDir string
		files       []string
		want        []string
	}{
		{
			name:        "repo root already uses staging namespace",
			infraSubDir: ".",
			files:       []string{"appdata/traefik/dynamic.yml", "compose/core.yml"},
			want:        []string{"appdata/traefik/dynamic.yml", "compose/core.yml"},
		},
		{
			name:        "nested infra prefix is stripped and unrelated files omitted",
			infraSubDir: "unraid",
			files: []string{
				"unraid/appdata/traefik/dynamic.yml",
				"docs/README.md",
				"unraid/compose/core.yml",
			},
			want: []string{"appdata/traefik/dynamic.yml", "compose/core.yml"},
		},
		{
			name:        "paths are cleaned and deduplicated",
			infraSubDir: "./unraid/",
			files:       []string{"./unraid/appdata/a.yml", "unraid/appdata/./a.yml", "../outside"},
			want:        []string{"appdata/a.yml"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeHookDiffPaths(tt.files, tt.infraSubDir))
		})
	}
}

func TestExecutePostSyncHooksNormalizesFallbackDiffPaths(t *testing.T) {
	git := &mockGitWithDiff{diffFiles: []string{
		"unraid/appdata/traefik/dynamic.yml",
		"docs/README.md",
	}}
	cfg := &Config{
		InfraSubDir: "unraid",
		PostSyncHooks: FileConfigField([]PostSyncHook{{
			Paths:     []string{"appdata/traefik/**"},
			Action:    "restart",
			Container: "traefik",
		}}),
	}
	r := NewReconciler(cfg, WithGitOperations(git))
	r.dockerClientFn = func() *docker.Client { return nil }

	matched, err := r.executePostSyncHooks(context.Background(), "commit-A", "commit-C", nil, true)

	require.Error(t, err, "the nil Docker client proves the normalized hook matched")
	assert.Equal(t, 1, matched)
	require.Len(t, git.diffCalledWith, 1)
	assert.Equal(t, [2]string{"commit-A", "commit-C"}, git.diffCalledWith[0],
		"hook fallback must use the last successful deploy as its diff base")
}

func TestFailedTemplateKeepsHookDiffBaseAndNormalizesNextFallback(t *testing.T) {
	tmpDir := t.TempDir()
	repoDir := filepath.Join(tmpDir, "repo")
	infraDir := filepath.Join(repoDir, "unraid")
	stateFile := filepath.Join(tmpDir, "state.json")
	require.NoError(t, os.MkdirAll(infraDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(infraDir, "broken.yaml.tmpl"),
		[]byte("{{ .value | missingFunction }}"),
		0o644,
	))
	require.NoError(t, SaveState(stateFile, &DeployState{
		SchemaVersion:       2,
		LastDeployedCommit:  "commit-A",
		LastAttemptedCommit: "commit-A",
		DeployCount:         1,
	}))

	git := &mockGitWithDiff{
		syncChanged: true,
		syncBefore:  "commit-A",
		syncAfter:   "commit-B",
		diffFiles:   []string{"unraid/appdata/traefik/dynamic.yml"},
	}
	cfg := &Config{
		RepoDir:     repoDir,
		InfraSubDir: "unraid",
		StagingDir:  filepath.Join(tmpDir, "staging"),
		LockFile:    filepath.Join(tmpDir, "reconcile.lock"),
		StateFile:   stateFile,
		PostSyncHooks: FileConfigField([]PostSyncHook{{
			Paths:     []string{"appdata/traefik/**"},
			Action:    "restart",
			Container: "traefik",
		}}),
	}
	r := NewReconciler(cfg, WithGitOperations(git))

	err := r.Run(context.Background())
	require.ErrorContains(t, err, "failed to render templates")
	state := LoadState(stateFile)
	assert.Equal(t, "commit-A", state.LastDeployedCommit,
		"a failed template at commit B must not advance the last successful deploy")

	r.dockerClientFn = func() *docker.Client { return nil }
	matched, err := r.executePostSyncHooks(
		context.Background(), state.LastDeployedCommit, "commit-C", nil, true,
	)
	require.Error(t, err, "the nil Docker client proves the normalized hook matched")
	assert.Equal(t, 1, matched)
	require.NotEmpty(t, git.diffCalledWith)
	assert.Equal(t, [2]string{"commit-A", "commit-C"}, git.diffCalledWith[len(git.diffCalledWith)-1],
		"the next fallback must diff from the last successful deploy, not failed commit B")
}

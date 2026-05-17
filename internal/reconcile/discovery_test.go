package reconcile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverDeployTargets(t *testing.T) {
	tests := []struct {
		name         string
		setup        func(t *testing.T, dir string) // create staging structure
		syncPaths    []string
		excludePaths []string
		want         []DeployTarget
		wantErr      bool
	}{
		{
			name:  "empty staging directory",
			setup: func(t *testing.T, dir string) {},
			want:  nil,
		},
		{
			name: "basic discovery with appdata and compose",
			setup: func(t *testing.T, dir string) {
				mkdirs(t, dir, "appdata/traefik", "appdata/authelia", "appdata/gatus", "compose")
			},
			want: []DeployTarget{
				{RelPath: "appdata/authelia", TargetPath: "authelia", IsDir: true},
				{RelPath: "appdata/gatus", TargetPath: "gatus", IsDir: true},
				{RelPath: "appdata/traefik", TargetPath: "traefik", IsDir: true},
				{RelPath: "compose", TargetPath: "compose", IsDir: true},
			},
		},
		{
			name: "appdata files are discovered as non-dir",
			setup: func(t *testing.T, dir string) {
				require.NoError(t, os.MkdirAll(filepath.Join(dir, "appdata", "myservice"), 0755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "appdata", "solo-config.yaml"), []byte("x"), 0644))
			},
			want: []DeployTarget{
				{RelPath: "appdata/myservice", TargetPath: "myservice", IsDir: true},
				{RelPath: "appdata/solo-config.yaml", TargetPath: "solo-config.yaml", IsDir: false},
			},
		},
		{
			name: "top-level files returned as-is",
			setup: func(t *testing.T, dir string) {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "extra.conf"), []byte("x"), 0644))
				mkdirs(t, dir, "compose")
			},
			want: []DeployTarget{
				{RelPath: "compose", TargetPath: "compose", IsDir: true},
				{RelPath: "extra.conf", TargetPath: "extra.conf", IsDir: false},
			},
		},
		{
			name: "allowlist filters to matching targets",
			setup: func(t *testing.T, dir string) {
				mkdirs(t, dir, "appdata/traefik", "appdata/authelia", "compose")
			},
			syncPaths: []string{"appdata/traefik", "compose"},
			want: []DeployTarget{
				{RelPath: "appdata/traefik", TargetPath: "traefik", IsDir: true},
				{RelPath: "compose", TargetPath: "compose", IsDir: true},
			},
		},
		{
			name: "allowlist with glob pattern",
			setup: func(t *testing.T, dir string) {
				mkdirs(t, dir, "appdata/traefik", "appdata/authelia", "appdata/gatus", "compose")
			},
			syncPaths: []string{"appdata/**"},
			want: []DeployTarget{
				{RelPath: "appdata/authelia", TargetPath: "authelia", IsDir: true},
				{RelPath: "appdata/gatus", TargetPath: "gatus", IsDir: true},
				{RelPath: "appdata/traefik", TargetPath: "traefik", IsDir: true},
			},
		},
		{
			name: "blocklist excludes matching targets",
			setup: func(t *testing.T, dir string) {
				mkdirs(t, dir, "appdata/traefik", "appdata/authelia", "compose")
			},
			excludePaths: []string{"appdata/authelia"},
			want: []DeployTarget{
				{RelPath: "appdata/traefik", TargetPath: "traefik", IsDir: true},
				{RelPath: "compose", TargetPath: "compose", IsDir: true},
			},
		},
		{
			name: "exclude wins over include",
			setup: func(t *testing.T, dir string) {
				mkdirs(t, dir, "appdata/traefik", "appdata/authelia", "compose")
			},
			syncPaths:    []string{"appdata/**", "compose"},
			excludePaths: []string{"appdata/authelia"},
			want: []DeployTarget{
				{RelPath: "appdata/traefik", TargetPath: "traefik", IsDir: true},
				{RelPath: "compose", TargetPath: "compose", IsDir: true},
			},
		},
		{
			name: "results are sorted by RelPath",
			setup: func(t *testing.T, dir string) {
				mkdirs(t, dir, "appdata/zebra", "appdata/alpha", "compose")
			},
			want: []DeployTarget{
				{RelPath: "appdata/alpha", TargetPath: "alpha", IsDir: true},
				{RelPath: "appdata/zebra", TargetPath: "zebra", IsDir: true},
				{RelPath: "compose", TargetPath: "compose", IsDir: true},
			},
		},
		{
			name:    "nonexistent staging directory returns error",
			setup:   func(t *testing.T, dir string) {},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := evalSymlinks(t, t.TempDir())
			var stagingDir string

			if tt.name == "nonexistent staging directory returns error" {
				stagingDir = filepath.Join(base, "nonexistent")
			} else {
				stagingDir = base
				tt.setup(t, stagingDir)
			}

			got, err := discoverDeployTargets(stagingDir, tt.syncPaths, tt.excludePaths)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestDiscoverDeployTargets_TopLevelRepoDirs_DiscoveredCorrectly locks the
// #214 reproduction shape: when InfraSubDir resolves to the staging root (a
// misconfiguration), discoverDeployTargets walks every top-level entry —
// including repo-internal dirs like .beads, .claude, unraid — and returns
// them all as deploy targets. The field report from GH#214 shows logs like
// "Syncing .beads/.claude/unraid" for exactly this reason.
//
// This test does NOT assert that the behavior is *correct* (Layer 3 will
// address the misconfiguration root cause). It locks the current discovery
// contract so that a future refactor of discoverDeployTargets — e.g., adding
// a filter for hidden dirs, or requiring an explicit allowlist — cannot
// silently change the set of returned targets without updating this test.
func TestDiscoverDeployTargets_TopLevelRepoDirs_DiscoveredCorrectly(t *testing.T) {
	base := evalSymlinks(t, t.TempDir())

	// Mimic the field-report shape: staging IS the repo root, with hidden
	// project dirs alongside the legitimate infra dir.
	mkdirs(t, base, ".beads", ".claude", ".github", "unraid", "compose", "openspec", "internal")

	got, err := discoverDeployTargets(base, nil, nil)
	require.NoError(t, err)

	want := []DeployTarget{
		{RelPath: ".beads", TargetPath: ".beads", IsDir: true},
		{RelPath: ".claude", TargetPath: ".claude", IsDir: true},
		{RelPath: ".github", TargetPath: ".github", IsDir: true},
		{RelPath: "compose", TargetPath: "compose", IsDir: true},
		{RelPath: "internal", TargetPath: "internal", IsDir: true},
		{RelPath: "openspec", TargetPath: "openspec", IsDir: true},
		{RelPath: "unraid", TargetPath: "unraid", IsDir: true},
	}
	assert.Equal(t, want, got,
		"discoverDeployTargets must return all top-level entries — locking the GH#214 'Syncing .beads/.claude/unraid' shape so refactors can't silently change discovery scope")
}

func TestHasTarget(t *testing.T) {
	targets := []DeployTarget{
		{RelPath: "appdata/traefik", TargetPath: "traefik", IsDir: true},
		{RelPath: "compose", TargetPath: "compose", IsDir: true},
	}

	assert.True(t, hasTarget(targets, "compose"))
	assert.True(t, hasTarget(targets, "appdata/traefik"))
	assert.False(t, hasTarget(targets, "appdata/authelia"))
	assert.False(t, hasTarget(targets, ""))
}

func TestBackupPathsFromTargets(t *testing.T) {
	targets := []DeployTarget{
		{RelPath: "appdata/traefik", TargetPath: "traefik", IsDir: true},
		{RelPath: "appdata/authelia", TargetPath: "authelia", IsDir: true},
		{RelPath: "compose", TargetPath: "compose", IsDir: true}, // now included for per-file rollback
	}

	paths := backupPathsFromTargets(targets, "/mnt/appdata")
	assert.Equal(t, []string{
		"/mnt/appdata/traefik",
		"/mnt/appdata/authelia",
		"/mnt/appdata/compose",
	}, paths)
}

func TestBackupPathsFromTargets_Empty(t *testing.T) {
	paths := backupPathsFromTargets(nil, "/mnt/appdata")
	assert.Nil(t, paths)
}

// mkdirs creates nested directory structures under base.
func mkdirs(t *testing.T, base string, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		require.NoError(t, os.MkdirAll(filepath.Join(base, d), 0755))
	}
}

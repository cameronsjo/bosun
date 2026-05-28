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
// current contract: every top-level entry under the staging dir becomes a
// target, hidden dirs included. Future refactors that filter dirs (e.g.,
// hidden-dir skip, allowlist-by-default) must update this test deliberately.
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

// TestBackupFilesFromTargets asserts the backup footprint is the set of regular
// files bosun renders into staging, mapped onto appdata destinations — not the
// whole target directories. Runtime data co-located in appdata is never in
// staging, so it cannot leak into the list (bosun-5qx).
func TestBackupFilesFromTargets(t *testing.T) {
	staging := t.TempDir()
	mkdirs(t, staging, "appdata/traefik/conf", "compose")
	writeFile(t, filepath.Join(staging, "appdata/traefik/traefik.yml"), "root")
	writeFile(t, filepath.Join(staging, "appdata/traefik/conf/dynamic.yml"), "nested")
	writeFile(t, filepath.Join(staging, "compose/docker-compose.yml"), "stack")

	targets := []DeployTarget{
		{RelPath: "appdata/traefik", TargetPath: "traefik", IsDir: true},
		{RelPath: "compose", TargetPath: "compose", IsDir: true},
	}

	paths, err := backupFilesFromTargets(staging, targets, "/mnt/appdata")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{
		"/mnt/appdata/traefik/traefik.yml",
		"/mnt/appdata/traefik/conf/dynamic.yml",
		"/mnt/appdata/compose/docker-compose.yml",
	}, paths)
}

// TestBackupFilesFromTargets_SkipsSymlinks confirms symlinks in staging are not
// enumerated, matching the deploy path which never copies them.
func TestBackupFilesFromTargets_SkipsSymlinks(t *testing.T) {
	staging := t.TempDir()
	mkdirs(t, staging, "appdata/svc")
	writeFile(t, filepath.Join(staging, "appdata/svc/config.yml"), "real")
	require.NoError(t, os.Symlink("config.yml", filepath.Join(staging, "appdata/svc/link.yml")))

	targets := []DeployTarget{{RelPath: "appdata/svc", TargetPath: "svc", IsDir: true}}
	paths, err := backupFilesFromTargets(staging, targets, "/mnt/appdata")
	require.NoError(t, err)
	assert.Equal(t, []string{"/mnt/appdata/svc/config.yml"}, paths)
}

// TestBackupFilesFromTargets_FileTarget maps a single-file target directly to
// its destination path.
func TestBackupFilesFromTargets_FileTarget(t *testing.T) {
	staging := t.TempDir()
	writeFile(t, filepath.Join(staging, ".env"), "K=V")

	targets := []DeployTarget{{RelPath: ".env", TargetPath: ".env", IsDir: false}}
	paths, err := backupFilesFromTargets(staging, targets, "/mnt/appdata")
	require.NoError(t, err)
	assert.Equal(t, []string{"/mnt/appdata/.env"}, paths)
}

func TestBackupFilesFromTargets_Empty(t *testing.T) {
	paths, err := backupFilesFromTargets(t.TempDir(), nil, "/mnt/appdata")
	require.NoError(t, err)
	assert.Nil(t, paths)
}

// writeFile creates a file with the given content, making parent dirs as needed.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
}

// mkdirs creates nested directory structures under base.
func mkdirs(t *testing.T, base string, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		require.NoError(t, os.MkdirAll(filepath.Join(base, d), 0755))
	}
}

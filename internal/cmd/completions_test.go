package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestCompleteTemplateNames(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		toComplete string
		want       []string
		wantDir    cobra.ShellCompDirective
	}{
		{
			name:       "empty prefix returns all templates",
			args:       nil,
			toComplete: "",
			want:       []string{"webapp", "api", "worker", "static"},
			wantDir:    cobra.ShellCompDirectiveNoFileComp,
		},
		{
			name:       "w prefix returns webapp and worker",
			args:       nil,
			toComplete: "w",
			want:       []string{"webapp", "worker"},
			wantDir:    cobra.ShellCompDirectiveNoFileComp,
		},
		{
			name:       "api prefix returns api",
			args:       nil,
			toComplete: "api",
			want:       []string{"api"},
			wantDir:    cobra.ShellCompDirectiveNoFileComp,
		},
		{
			name:       "already has arg returns nothing",
			args:       []string{"webapp"},
			toComplete: "",
			want:       nil,
			wantDir:    cobra.ShellCompDirectiveNoFileComp,
		},
		{
			name:       "no match returns empty",
			args:       nil,
			toComplete: "xyz",
			want:       nil,
			wantDir:    cobra.ShellCompDirectiveNoFileComp,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotDir := completeTemplateNames(nil, tt.args, tt.toComplete)
			assert.ElementsMatch(t, tt.want, got)
			assert.Equal(t, tt.wantDir, gotDir)
		})
	}
}

func TestCompleteStackNames(t *testing.T) {
	// Create temporary directory structure
	tmpDir := t.TempDir()

	// Create manifest directories
	stacksDir := filepath.Join(tmpDir, "manifest", "stacks")
	servicesDir := filepath.Join(tmpDir, "manifest", "services")
	require.NoError(t, os.MkdirAll(stacksDir, 0755))
	require.NoError(t, os.MkdirAll(servicesDir, 0755))

	// Create test files
	require.NoError(t, os.WriteFile(filepath.Join(stacksDir, "core.yml"), []byte("name: core"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(stacksDir, "media.yml"), []byte("name: media"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(servicesDir, "traefik.yml"), []byte("name: traefik"), 0644))

	// Create bosun.yaml config
	configContent := `root: .
manifest_dir: manifest
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(configContent), 0644))

	// Change to temp directory
	oldWd, _ := os.Getwd()
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(oldWd) }()

	tests := []struct {
		name       string
		args       []string
		toComplete string
		wantLen    int
		wantDir    cobra.ShellCompDirective
	}{
		{
			name:       "empty prefix returns all stacks and services",
			args:       nil,
			toComplete: "",
			wantLen:    3, // core, media, traefik
			wantDir:    cobra.ShellCompDirectiveNoFileComp,
		},
		{
			name:       "c prefix returns core",
			args:       nil,
			toComplete: "c",
			wantLen:    1,
			wantDir:    cobra.ShellCompDirectiveNoFileComp,
		},
		{
			name:       "already has arg returns nothing",
			args:       []string{"core"},
			toComplete: "",
			wantLen:    0,
			wantDir:    cobra.ShellCompDirectiveNoFileComp,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotDir := completeStackNames(nil, tt.args, tt.toComplete)
			assert.Len(t, got, tt.wantLen)
			assert.Equal(t, tt.wantDir, gotDir)
		})
	}
}

func TestCompleteComposeServices_NoConfig(t *testing.T) {
	// Test that completeComposeServices returns error when no config found
	// (we can't easily test the happy path without mocking config.Load)
	oldWd, _ := os.Getwd()
	tmpDir := t.TempDir()
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(oldWd) }()

	got, gotDir := completeComposeServices(nil, nil, "")
	assert.Nil(t, got)
	assert.Equal(t, cobra.ShellCompDirectiveError, gotDir)
}

func TestParseComposeServices(t *testing.T) {
	// Test the YAML parsing logic directly
	composeContent := `services:
  traefik:
    image: traefik:latest
  authelia:
    image: authelia/authelia
  whoami:
    image: traefik/whoami
`
	tmpDir := t.TempDir()
	composeFile := filepath.Join(tmpDir, "docker-compose.yml")
	require.NoError(t, os.WriteFile(composeFile, []byte(composeContent), 0644))

	// Parse the compose file directly (simulating what the completion does)
	data, err := os.ReadFile(composeFile)
	require.NoError(t, err)

	var compose composeServices
	require.NoError(t, yaml.Unmarshal(data, &compose))

	assert.Len(t, compose.Services, 3)
	assert.Contains(t, compose.Services, "traefik")
	assert.Contains(t, compose.Services, "authelia")
	assert.Contains(t, compose.Services, "whoami")
}

func TestCompleteBackupNames(t *testing.T) {
	// Create temporary backup directory
	tmpDir := t.TempDir()
	backupDir := filepath.Join(tmpDir, "backups")
	require.NoError(t, os.MkdirAll(backupDir, 0755))

	// Create backup directories
	backup1 := filepath.Join(backupDir, "backup-20240115-100000")
	backup2 := filepath.Join(backupDir, "backup-20240116-100000")
	require.NoError(t, os.MkdirAll(backup1, 0755))
	require.NoError(t, os.MkdirAll(backup2, 0755))

	// Create configs.tar.gz files
	require.NoError(t, os.WriteFile(filepath.Join(backup1, "configs.tar.gz"), []byte("fake"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(backup2, "configs.tar.gz"), []byte("fake"), 0644))

	// Set environment variable
	t.Setenv("BACKUP_DIR", backupDir)

	tests := []struct {
		name       string
		args       []string
		toComplete string
		wantLen    int
	}{
		{
			name:       "empty prefix returns all backups",
			args:       nil,
			toComplete: "",
			wantLen:    2,
		},
		{
			name:       "already has arg returns nothing",
			args:       []string{"backup-20240115-100000"},
			toComplete: "",
			wantLen:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotDir := completeBackupNames(nil, tt.args, tt.toComplete)
			assert.Len(t, got, tt.wantLen)
			assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, gotDir)
		})
	}
}

func TestRegisterCompletions(t *testing.T) {
	// Just verify it doesn't panic
	assert.NotPanics(t, func() {
		registerCompletions()
	})

	// Verify some completions are registered
	assert.NotNil(t, crewLogsCmd.ValidArgsFunction)
	assert.NotNil(t, crewInspectCmd.ValidArgsFunction)
	assert.NotNil(t, crewRestartCmd.ValidArgsFunction)
	assert.NotNil(t, provisionCmd.ValidArgsFunction)
	assert.NotNil(t, yachtUpCmd.ValidArgsFunction)
	assert.NotNil(t, yachtRestartCmd.ValidArgsFunction)
	assert.NotNil(t, overboardCmd.ValidArgsFunction)
	assert.NotNil(t, restoreCmd.ValidArgsFunction)
	assert.NotNil(t, createCmd.ValidArgsFunction)
}

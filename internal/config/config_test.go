package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// evalSymlinks resolves symlinks for path comparison (macOS /var -> /private/var).
func evalSymlinks(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return resolved
}

func TestFindRoot_WithBosunDir(t *testing.T) {
	tmpDir := evalSymlinks(t, t.TempDir())

	// Create bosun directory with docker-compose.yml
	bosunDir := filepath.Join(tmpDir, "bosun")
	require.NoError(t, os.MkdirAll(bosunDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(bosunDir, "docker-compose.yml"), []byte("version: '3'"), 0644))

	// Create subdirectory to search from
	subDir := filepath.Join(tmpDir, "sub", "deep")
	require.NoError(t, os.MkdirAll(subDir, 0755))

	// Change to subdirectory
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalWd)

	require.NoError(t, os.Chdir(subDir))

	// FindRoot should find the project root
	root, err := FindRoot()
	require.NoError(t, err)
	assert.Equal(t, tmpDir, root)
}

func TestFindRoot_WithManifestDir(t *testing.T) {
	tmpDir := evalSymlinks(t, t.TempDir())

	// Create manifest directory (without bosun)
	manifestDir := filepath.Join(tmpDir, "manifest")
	require.NoError(t, os.MkdirAll(manifestDir, 0755))

	// Create subdirectory to search from
	subDir := filepath.Join(tmpDir, "sub", "deep")
	require.NoError(t, os.MkdirAll(subDir, 0755))

	// Change to subdirectory
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalWd)

	require.NoError(t, os.Chdir(subDir))

	// FindRoot should find the project root
	root, err := FindRoot()
	require.NoError(t, err)
	assert.Equal(t, tmpDir, root)
}

func TestFindRoot_BosunDirWithoutComposeFile(t *testing.T) {
	tmpDir := evalSymlinks(t, t.TempDir())

	// Create bosun directory WITHOUT docker-compose.yml
	bosunDir := filepath.Join(tmpDir, "bosun")
	require.NoError(t, os.MkdirAll(bosunDir, 0755))

	// Also create manifest directory so we have a valid root
	manifestDir := filepath.Join(tmpDir, "manifest")
	require.NoError(t, os.MkdirAll(manifestDir, 0755))

	// Change to bosun directory
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalWd)

	require.NoError(t, os.Chdir(bosunDir))

	// FindRoot should find root via manifest directory
	root, err := FindRoot()
	require.NoError(t, err)
	assert.Equal(t, tmpDir, root)
}

func TestFindRoot_NoProjectRoot(t *testing.T) {
	// Use a temporary directory with no bosun or manifest dirs
	tmpDir := t.TempDir()

	// Change to temp directory
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalWd)

	require.NoError(t, os.Chdir(tmpDir))

	// FindRoot should return error
	_, err = FindRoot()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "project root not found")
}

func TestFindRoot_FromProjectRoot(t *testing.T) {
	tmpDir := evalSymlinks(t, t.TempDir())

	// Create manifest directory
	manifestDir := filepath.Join(tmpDir, "manifest")
	require.NoError(t, os.MkdirAll(manifestDir, 0755))

	// Change to project root itself
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalWd)

	require.NoError(t, os.Chdir(tmpDir))

	// FindRoot should find the project root
	root, err := FindRoot()
	require.NoError(t, err)
	assert.Equal(t, tmpDir, root)
}

func TestLoad(t *testing.T) {
	tmpDir := evalSymlinks(t, t.TempDir())

	// Create manifest directory
	manifestDir := filepath.Join(tmpDir, "manifest")
	require.NoError(t, os.MkdirAll(manifestDir, 0755))

	// Create bosun directory with docker-compose.yml
	bosunDir := filepath.Join(tmpDir, "bosun")
	require.NoError(t, os.MkdirAll(bosunDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(bosunDir, "docker-compose.yml"), []byte("version: '3'"), 0644))

	// Change to project root
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalWd)

	require.NoError(t, os.Chdir(tmpDir))

	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, tmpDir, cfg.Root)
	assert.Equal(t, filepath.Join(tmpDir, "manifest"), cfg.ManifestDir)
	assert.Equal(t, filepath.Join(tmpDir, "bosun", "docker-compose.yml"), cfg.ComposeFile)
	assert.Equal(t, filepath.Join(tmpDir, "manifest", ".bosun", "snapshots"), cfg.SnapshotsDir)
}

func TestLoad_NoProjectRoot(t *testing.T) {
	tmpDir := t.TempDir()

	// Change to temp directory (no project markers)
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalWd)

	require.NoError(t, os.Chdir(tmpDir))

	cfg, err := Load()
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "project root not found")
}

func TestConfig_ProvisionsDir(t *testing.T) {
	cfg := &Config{
		ManifestDir: "/path/to/manifest",
	}

	assert.Equal(t, "/path/to/manifest/provisions", cfg.ProvisionsDir())
}

func TestConfig_ServicesDir(t *testing.T) {
	cfg := &Config{
		ManifestDir: "/path/to/manifest",
	}

	assert.Equal(t, "/path/to/manifest/services", cfg.ServicesDir())
}

func TestConfig_StacksDir(t *testing.T) {
	cfg := &Config{
		ManifestDir: "/path/to/manifest",
	}

	assert.Equal(t, "/path/to/manifest/stacks", cfg.StacksDir())
}

func TestConfig_OutputDir(t *testing.T) {
	cfg := &Config{
		ManifestDir: "/path/to/manifest",
	}

	assert.Equal(t, "/path/to/manifest/output", cfg.OutputDir())
}

func TestConfig_AllPathMethods(t *testing.T) {
	cfg := &Config{
		Root:         "/project",
		ManifestDir:  "/project/manifest",
		ComposeFile:  "/project/bosun/docker-compose.yml",
		SnapshotsDir: "/project/manifest/.bosun/snapshots",
	}

	// Verify all path methods return expected paths
	assert.Equal(t, "/project/manifest/provisions", cfg.ProvisionsDir())
	assert.Equal(t, "/project/manifest/services", cfg.ServicesDir())
	assert.Equal(t, "/project/manifest/stacks", cfg.StacksDir())
	assert.Equal(t, "/project/manifest/output", cfg.OutputDir())
}

func TestFindRoot_BosunPreferredOverManifest(t *testing.T) {
	tmpDir := evalSymlinks(t, t.TempDir())

	// Create both bosun directory (with compose) and manifest directory
	bosunDir := filepath.Join(tmpDir, "bosun")
	require.NoError(t, os.MkdirAll(bosunDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(bosunDir, "docker-compose.yml"), []byte("version: '3'"), 0644))

	manifestDir := filepath.Join(tmpDir, "manifest")
	require.NoError(t, os.MkdirAll(manifestDir, 0755))

	// Change to project root
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalWd)

	require.NoError(t, os.Chdir(tmpDir))

	// FindRoot should find the project root (bosun checked first)
	root, err := FindRoot()
	require.NoError(t, err)
	assert.Equal(t, tmpDir, root)
}

func TestFindRoot_DeepNesting(t *testing.T) {
	tmpDir := evalSymlinks(t, t.TempDir())

	// Create manifest directory at root
	manifestDir := filepath.Join(tmpDir, "manifest")
	require.NoError(t, os.MkdirAll(manifestDir, 0755))

	// Create deeply nested subdirectory
	deepDir := filepath.Join(tmpDir, "a", "b", "c", "d", "e", "f")
	require.NoError(t, os.MkdirAll(deepDir, 0755))

	// Change to deep directory
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalWd)

	require.NoError(t, os.Chdir(deepDir))

	// FindRoot should still find the project root
	root, err := FindRoot()
	require.NoError(t, err)
	assert.Equal(t, tmpDir, root)
}

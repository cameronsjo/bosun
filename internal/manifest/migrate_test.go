package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrateToV1(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantKind     string
		wantMigrated bool
		wantErr      bool
	}{
		{
			name: "unversioned provision",
			input: `compose:
  services:
    test:
      image: test:latest`,
			wantKind:     KindProvision,
			wantMigrated: true,
			wantErr:      false,
		},
		{
			name: "unversioned service",
			input: `name: myapp
provisions:
  - container
config:
  image: myapp:latest`,
			wantKind:     KindService,
			wantMigrated: true,
			wantErr:      false,
		},
		{
			name: "unversioned stack",
			input: `include:
  - service1.yml
  - service2.yml`,
			wantKind:     KindStack,
			wantMigrated: true,
			wantErr:      false,
		},
		{
			name: "already versioned",
			input: `apiVersion: bosun.io/v1
kind: Provision
compose:
  services: {}`,
			wantKind:     KindProvision,
			wantMigrated: false, // Not migrated because already versioned
			wantErr:      false,
		},
		{
			name:     "invalid YAML",
			input:    `invalid: yaml: [`,
			wantKind: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			migrated, kind, err := MigrateToV1([]byte(tt.input))
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantKind, kind)

			if tt.wantMigrated {
				// Check that apiVersion and kind were added
				assert.Contains(t, string(migrated), "apiVersion: bosun.io/v1")
				assert.Contains(t, string(migrated), "kind: "+tt.wantKind)
			}
		})
	}
}

func TestDetectKind(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantKind string
	}{
		{
			name:     "provision with compose",
			input:    `compose: {}`,
			wantKind: KindProvision,
		},
		{
			name:     "provision with traefik",
			input:    `traefik: {}`,
			wantKind: KindProvision,
		},
		{
			name:     "provision with gatus",
			input:    `gatus: {}`,
			wantKind: KindProvision,
		},
		{
			name: "provision with includes",
			input: `includes:
  - base`,
			wantKind: KindProvision,
		},
		{
			name: "service with name and provisions",
			input: `name: app
provisions:
  - container`,
			wantKind: KindService,
		},
		{
			name: "service with name and needs",
			input: `name: app
needs:
  - postgres`,
			wantKind: KindService,
		},
		{
			name: "service with name and services",
			input: `name: app
services:
  postgres: {}`,
			wantKind: KindService,
		},
		{
			name: "service with name and type",
			input: `name: app
type: raw`,
			wantKind: KindService,
		},
		{
			name: "stack with include",
			input: `include:
  - service.yml`,
			wantKind: KindStack,
		},
		{
			name:     "empty content defaults to provision",
			input:    ``,
			wantKind: KindProvision,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, err := detectKind([]byte(tt.input))
			require.NoError(t, err)
			assert.Equal(t, tt.wantKind, kind)
		})
	}
}

func TestMigrateFile(t *testing.T) {
	t.Run("dry run does not write", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.yml")

		original := `compose:
  services:
    test:
      image: test:latest`
		require.NoError(t, os.WriteFile(path, []byte(original), 0644))

		result, err := MigrateFile(path, MigrateOptions{DryRun: true})
		require.NoError(t, err)
		assert.True(t, result.Migrated)
		assert.Equal(t, KindProvision, result.Kind)
		assert.False(t, result.WasVersioned)

		// Verify file was not modified
		content, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, original, string(content))
	})

	t.Run("write mode updates file", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.yml")

		original := `compose:
  services:
    test:
      image: test:latest`
		require.NoError(t, os.WriteFile(path, []byte(original), 0644))

		result, err := MigrateFile(path, MigrateOptions{DryRun: false})
		require.NoError(t, err)
		assert.True(t, result.Migrated)

		// Verify file was modified
		content, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Contains(t, string(content), "apiVersion: bosun.io/v1")
		assert.Contains(t, string(content), "kind: Provision")
	})

	t.Run("already versioned file is skipped", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.yml")

		original := `apiVersion: bosun.io/v1
kind: Provision
compose:
  services: {}`
		require.NoError(t, os.WriteFile(path, []byte(original), 0644))

		result, err := MigrateFile(path, MigrateOptions{DryRun: false})
		require.NoError(t, err)
		assert.False(t, result.Migrated)
		assert.True(t, result.WasVersioned)
		assert.Equal(t, KindProvision, result.Kind)
	})

	t.Run("nonexistent file returns error", func(t *testing.T) {
		result, err := MigrateFile("/nonexistent/path.yml", MigrateOptions{})
		require.Error(t, err)
		assert.NotNil(t, result.Error)
	})
}

func TestMigrateDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	files := map[string]string{
		"provision.yml": `compose:
  services: {}`,
		"service.yaml": `name: app
provisions: []`,
		"already-versioned.yml": `apiVersion: bosun.io/v1
kind: Stack
include: []`,
		"not-yaml.txt": `some text`,
	}

	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0644))
	}

	results, err := MigrateDirectory([]string{tmpDir}, MigrateOptions{DryRun: true})
	require.NoError(t, err)

	// Should have processed 3 YAML files (not the .txt file)
	assert.Len(t, results, 3)

	// Count migrated vs skipped
	var migrated, skipped int
	for _, r := range results {
		if r.Migrated {
			migrated++
		} else {
			skipped++
		}
	}

	assert.Equal(t, 2, migrated)  // provision.yml and service.yaml
	assert.Equal(t, 1, skipped)   // already-versioned.yml
}

func TestScanUnversioned(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	versioned := `apiVersion: bosun.io/v1
kind: Provision
compose: {}`
	unversioned := `compose:
  services: {}`

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "versioned.yml"), []byte(versioned), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "unversioned.yml"), []byte(unversioned), 0644))

	results, err := ScanUnversioned([]string{tmpDir})
	require.NoError(t, err)

	// Should only return unversioned file
	require.Len(t, results, 1)
	assert.Contains(t, results[0].Path, "unversioned.yml")
	assert.Equal(t, KindProvision, results[0].Kind)
}

func TestFormatMigrationSummary(t *testing.T) {
	results := []*MigrationResult{
		{Path: "/path/to/file1.yml", Kind: KindProvision, Migrated: true},
		{Path: "/path/to/file2.yml", Kind: KindService, Migrated: true},
		{Path: "/path/to/file3.yml", Kind: KindStack, WasVersioned: true, Migrated: false},
		{Path: "/path/to/file4.yml", Error: os.ErrNotExist},
	}

	summary := FormatMigrationSummary(results, true)

	assert.Contains(t, summary, "Would migrate: 2 files")
	assert.Contains(t, summary, "Already versioned: 1 files")
	assert.Contains(t, summary, "Errors: 1 files")
	assert.Contains(t, summary, "file1.yml")
	assert.Contains(t, summary, "file2.yml")
}

func TestMigratePreservesContent(t *testing.T) {
	original := `# PostgreSQL sidecar profile
compose:
  services:
    ${name}-db:
      image: postgres:${version}-alpine
      container_name: ${name}-db`

	migrated, kind, err := MigrateToV1([]byte(original))
	require.NoError(t, err)
	assert.Equal(t, KindProvision, kind)

	// Check that original content is preserved
	assert.Contains(t, string(migrated), "# PostgreSQL sidecar profile")
	assert.Contains(t, string(migrated), "${name}-db")
	assert.Contains(t, string(migrated), "postgres:${version}-alpine")

	// Check that header is at the top
	assert.True(t, strings.HasPrefix(string(migrated), "apiVersion:"))
}

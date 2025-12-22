package manifest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testdataDir() string {
	return filepath.Join("testdata", "provisions")
}

func TestLoadProvision(t *testing.T) {
	tests := []struct {
		name          string
		provisionName string
		variables     map[string]any
		wantErr       bool
		validate      func(t *testing.T, p *Provision)
	}{
		{
			name:          "load simple provision",
			provisionName: "container",
			variables: map[string]any{
				"name":  "myapp",
				"image": "myapp:latest",
			},
			wantErr: false,
			validate: func(t *testing.T, p *Provision) {
				require.NotNil(t, p.Compose)
				services, ok := p.Compose["services"].(map[string]any)
				require.True(t, ok)

				myapp, ok := services["myapp"].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, "myapp:latest", myapp["image"])
				assert.Equal(t, "myapp", myapp["container_name"])
			},
		},
		{
			name:          "variable interpolation in provision",
			provisionName: "healthcheck",
			variables: map[string]any{
				"name": "testservice",
				"port": "8080",
			},
			wantErr: false,
			validate: func(t *testing.T, p *Provision) {
				require.NotNil(t, p.Compose)
				services, ok := p.Compose["services"].(map[string]any)
				require.True(t, ok)

				svc, ok := services["testservice"].(map[string]any)
				require.True(t, ok)

				hc, ok := svc["healthcheck"].(map[string]any)
				require.True(t, ok)

				test, ok := hc["test"].([]any)
				require.True(t, ok)
				require.Len(t, test, 5, "healthcheck test should have 5 elements")
				// The URL is the 5th element (index 4): ["CMD", "wget", "-q", "--spider", "http://localhost:${port}/health"]
				urlStr := test[4].(string)
				assert.Contains(t, urlStr, "8080")
			},
		},
		{
			name:          "provision with includes inheritance",
			provisionName: "webapp",
			variables: map[string]any{
				"name":        "webtest",
				"image":       "webapp:v1",
				"port":        "3000",
				"subdomain":   "app",
				"domain":      "example.com",
				"group":       "Apps",
				"icon":        "mdi-web",
				"description": "Test Web App",
			},
			wantErr: false,
			validate: func(t *testing.T, p *Provision) {
				// Should have compose from container, healthcheck, reverse-proxy, homepage
				require.NotNil(t, p.Compose)
				services, ok := p.Compose["services"].(map[string]any)
				require.True(t, ok)

				svc, ok := services["webtest"].(map[string]any)
				require.True(t, ok)

				// From container
				assert.Equal(t, "webapp:v1", svc["image"])

				// From healthcheck
				_, hasHealthcheck := svc["healthcheck"]
				assert.True(t, hasHealthcheck)

				// From reverse-proxy
				networks, ok := svc["networks"].([]any)
				if ok {
					assert.Contains(t, networks, "proxynet")
				}

				// Should have traefik output from reverse-proxy
				require.NotNil(t, p.Traefik)

				// Should have gatus output from monitoring
				require.NotNil(t, p.Gatus)
			},
		},
		{
			name:          "missing provision error",
			provisionName: "nonexistent",
			variables:     map[string]any{},
			wantErr:       true,
		},
		{
			name:          "missing variable error",
			provisionName: "container",
			variables: map[string]any{
				"name": "test",
				// missing 'image' variable
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provision, err := LoadProvision(tt.provisionName, tt.variables, testdataDir())
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.validate != nil {
				tt.validate(t, provision)
			}
		})
	}
}

func TestLoadProvision_CircularIncludeProtection(t *testing.T) {
	// Create a temporary directory with circular includes
	tmpDir := t.TempDir()

	// Write circular.yml that includes itself
	circular := `includes:
  - circular
compose:
  services:
    test:
      image: test:latest
`
	require.NoError(t, writeTestFile(tmpDir, "circular.yml", circular))

	// Should not hang or error - circular includes are silently skipped
	provision, err := LoadProvision("circular", map[string]any{}, tmpDir)
	require.NoError(t, err)
	require.NotNil(t, provision)
}

func TestListProvisions(t *testing.T) {
	provisions, err := ListProvisions(testdataDir())
	require.NoError(t, err)

	// Should find the test provisions
	assert.Contains(t, provisions, "container")
	assert.Contains(t, provisions, "healthcheck")
	assert.Contains(t, provisions, "webapp")
	assert.Contains(t, provisions, "postgres")
	assert.Contains(t, provisions, "redis")
}

func TestListProvisions_MissingDir(t *testing.T) {
	_, err := ListProvisions("/nonexistent/path")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestProvisionExists(t *testing.T) {
	tests := []struct {
		name          string
		provisionName string
		want          bool
	}{
		{
			name:          "existing provision",
			provisionName: "container",
			want:          true,
		},
		{
			name:          "nonexistent provision",
			provisionName: "doesnotexist",
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ProvisionExists(tt.provisionName, testdataDir())
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLoadProvision_MultipleIncludes(t *testing.T) {
	tmpDir := t.TempDir()

	// Create base provision
	base := `compose:
  services:
    ${name}:
      image: ${image}
`
	require.NoError(t, writeTestFile(tmpDir, "base.yml", base))

	// Create extension provision
	extension := `compose:
  services:
    ${name}:
      environment:
        KEY: value
`
	require.NoError(t, writeTestFile(tmpDir, "extension.yml", extension))

	// Create combined provision
	combined := `includes:
  - base
  - extension
compose:
  services:
    ${name}:
      labels:
        test: "true"
`
	require.NoError(t, writeTestFile(tmpDir, "combined.yml", combined))

	provision, err := LoadProvision("combined", map[string]any{
		"name":  "testapp",
		"image": "test:v1",
	}, tmpDir)
	require.NoError(t, err)

	services, ok := provision.Compose["services"].(map[string]any)
	require.True(t, ok)

	svc, ok := services["testapp"].(map[string]any)
	require.True(t, ok)

	// From base
	assert.Equal(t, "test:v1", svc["image"])

	// From extension
	env, ok := svc["environment"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "value", env["KEY"])

	// From combined
	labels, ok := svc["labels"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "true", labels["test"])
}

func writeTestFile(dir, name, content string) error {
	return os.WriteFile(filepath.Join(dir, name), []byte(content), 0644)
}

func TestLoadProvision_ReadError(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a non-yml file error scenario by using a non-readable path
	_, err := LoadProvision("nonexistent", map[string]any{}, tmpDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provision not found")
}

func TestLoadProvision_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()

	invalidYAML := `invalid: yaml: [
`
	require.NoError(t, writeTestFile(tmpDir, "invalid.yml", invalidYAML))

	_, err := LoadProvision("invalid", map[string]any{}, tmpDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse provision")
}

func TestLoadProvision_IncludeError(t *testing.T) {
	tmpDir := t.TempDir()

	// Create provision that includes a missing provision
	withIncludes := `includes:
  - missing-provision
compose:
  services:
    test:
      image: test:latest
`
	require.NoError(t, writeTestFile(tmpDir, "with-includes.yml", withIncludes))

	_, err := LoadProvision("with-includes", map[string]any{}, tmpDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "include")
}

func TestLoadProvision_EmptyProvision(t *testing.T) {
	tmpDir := t.TempDir()

	// Create an empty provision file
	require.NoError(t, writeTestFile(tmpDir, "empty.yml", ""))

	provision, err := LoadProvision("empty", map[string]any{}, tmpDir)
	require.NoError(t, err)
	require.NotNil(t, provision)
	assert.Nil(t, provision.Compose)
	assert.Nil(t, provision.Traefik)
	assert.Nil(t, provision.Gatus)
}

func TestLoadProvision_StringIncludes(t *testing.T) {
	tmpDir := t.TempDir()

	// Create base provision
	base := `compose:
  services:
    base:
      image: base:latest
`
	require.NoError(t, writeTestFile(tmpDir, "base.yml", base))

	// Create provision with includes as string slice format
	withIncludes := `includes: [base]
compose:
  services:
    test:
      image: test:latest
`
	require.NoError(t, writeTestFile(tmpDir, "with-includes.yml", withIncludes))

	provision, err := LoadProvision("with-includes", map[string]any{}, tmpDir)
	require.NoError(t, err)
	require.NotNil(t, provision.Compose)
}

func TestListProvisions_ReadDirError(t *testing.T) {
	_, err := ListProvisions("/nonexistent/directory/path")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestListProvisions_EmptyDir(t *testing.T) {
	tmpDir := t.TempDir()

	provisions, err := ListProvisions(tmpDir)
	require.NoError(t, err)
	assert.Empty(t, provisions)
}

func TestListProvisions_WithYAMLExtension(t *testing.T) {
	tmpDir := t.TempDir()

	// Create both .yml and .yaml files
	require.NoError(t, writeTestFile(tmpDir, "provision1.yml", ""))
	require.NoError(t, writeTestFile(tmpDir, "provision2.yaml", ""))
	require.NoError(t, writeTestFile(tmpDir, "notprovision.txt", ""))

	// Create a subdirectory (should be ignored)
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "subdir"), 0755))

	provisions, err := ListProvisions(tmpDir)
	require.NoError(t, err)
	assert.Len(t, provisions, 2)
	assert.Contains(t, provisions, "provision1")
	assert.Contains(t, provisions, "provision2")
}

func TestLoadProvision_AllTargets(t *testing.T) {
	tmpDir := t.TempDir()

	// Create provision with all target types
	fullProvision := `compose:
  services:
    test:
      image: test:latest
traefik:
  http:
    routers:
      test: {}
gatus:
  endpoints:
    - name: test
`
	require.NoError(t, writeTestFile(tmpDir, "full.yml", fullProvision))

	provision, err := LoadProvision("full", map[string]any{}, tmpDir)
	require.NoError(t, err)
	require.NotNil(t, provision.Compose)
	require.NotNil(t, provision.Traefik)
	require.NotNil(t, provision.Gatus)
}

package manifest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gatusEndpointProvision is a provision whose only content is one gatus
// endpoint, which makes a double merge visible: endpoints extend, so merging
// it twice yields two endpoints.
const gatusEndpointProvision = `gatus:
  endpoints:
    - name: shared
`

func gatusEndpoints(t *testing.T, p *Provision) []any {
	t.Helper()
	endpoints, ok := p.Targets["gatus"]["endpoints"].([]any)
	require.True(t, ok, "gatus endpoints missing or not a list: %#v", p.Targets["gatus"])
	return endpoints
}

func TestLoadProvision_DiamondIncludesMergeOnce(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, writeTestFile(dir, "d.yml", gatusEndpointProvision))
	require.NoError(t, writeTestFile(dir, "b.yml", "includes:\n  - d\n"))
	require.NoError(t, writeTestFile(dir, "c.yml", "includes:\n  - d\n"))
	require.NoError(t, writeTestFile(dir, "a.yml", "includes:\n  - b\n  - c\n"))

	p, err := LoadProvision("a", map[string]any{}, dir)

	// The original reports d's second occurrence as a circular include. A fix
	// that only clears the visited set on the way out would load d twice and
	// return two endpoints, so the length pins "merge once".
	require.NoError(t, err)
	assert.Len(t, gatusEndpoints(t, p), 1)
}

func TestLoadProvision_IndirectCycleStillErrors(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, writeTestFile(dir, "a.yml", "includes:\n  - b\n"))
	require.NoError(t, writeTestFile(dir, "b.yml", "includes:\n  - c\n"))
	require.NoError(t, writeTestFile(dir, "c.yml", "includes:\n  - a\n"))

	_, err := LoadProvision("a", map[string]any{}, dir)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCircularInclude)
}

func TestLoadProvision_DiamondStateIsPerCall(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, writeTestFile(dir, "d.yml", gatusEndpointProvision))

	for range 2 {
		p, err := LoadProvision("d", map[string]any{}, dir)
		require.NoError(t, err)
		assert.Len(t, gatusEndpoints(t, p), 1)
	}
}

func TestLoadProvision_YamlExtension(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, writeTestFile(dir, "foo.yaml", gatusEndpointProvision))

	p, err := LoadProvision("foo", map[string]any{}, dir)

	require.NoError(t, err)
	assert.Len(t, gatusEndpoints(t, p), 1)
	assert.True(t, ProvisionExists("foo", dir))
	names, err := ListProvisions(dir)
	require.NoError(t, err)
	assert.Equal(t, []string{"foo"}, names)
}

func TestLoadProvision_YamlExtensionInclude(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, writeTestFile(dir, "base.yaml", gatusEndpointProvision))
	require.NoError(t, writeTestFile(dir, "top.yml", "includes:\n  - base\n"))

	p, err := LoadProvision("top", map[string]any{}, dir)

	require.NoError(t, err)
	assert.Len(t, gatusEndpoints(t, p), 1)
}

func TestLoadProvision_YmlWinsOverYaml(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, writeTestFile(dir, "foo.yml", "gatus:\n  endpoints:\n    - name: from-yml\n"))
	require.NoError(t, writeTestFile(dir, "foo.yaml", "gatus:\n  endpoints:\n    - name: from-yaml\n"))

	p, err := LoadProvision("foo", map[string]any{}, dir)

	require.NoError(t, err)
	endpoints := gatusEndpoints(t, p)
	require.Len(t, endpoints, 1)
	assert.Equal(t, "from-yml", endpoints[0].(map[string]any)["name"])
}

func TestListProvisions_DedupesYmlAndYaml(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, writeTestFile(dir, "foo.yml", ""))
	require.NoError(t, writeTestFile(dir, "foo.yaml", ""))
	require.NoError(t, writeTestFile(dir, "bar.yaml", ""))

	names, err := ListProvisions(dir)

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"foo", "bar"}, names)
}

func TestProvisionPath_SkipsDirectoryNamedLikeAProvision(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "foo.yml"), 0o755))
	require.NoError(t, writeTestFile(dir, "foo.yaml", gatusEndpointProvision))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "only-dir.yml"), 0o755))

	t.Run("falls through to the yaml file", func(t *testing.T) {
		p, err := LoadProvision("foo", map[string]any{}, dir)

		require.NoError(t, err)
		assert.Len(t, gatusEndpoints(t, p), 1)
		assert.True(t, ProvisionExists("foo", dir))
	})

	t.Run("a directory alone is not a provision", func(t *testing.T) {
		assert.False(t, ProvisionExists("only-dir", dir))

		_, err := LoadProvision("only-dir", map[string]any{}, dir)
		require.Error(t, err)
	})
}

// provisionTraversalFixture lays out a provisions directory with a valid
// provision file one level above it. A name that escapes the directory by
// ".." reaches that file, so an unguarded loader returns it without error.
func provisionTraversalFixture(t *testing.T) (provisionsDir, outsideAbs string) {
	t.Helper()
	base := t.TempDir()
	provisionsDir = filepath.Join(base, "provisions")
	require.NoError(t, os.MkdirAll(filepath.Join(provisionsDir, "sub"), 0o755))
	require.NoError(t, writeTestFile(base, "outside.yml", gatusEndpointProvision))
	require.NoError(t, writeTestFile(provisionsDir, "inside.yml", gatusEndpointProvision))
	require.NoError(t, writeTestFile(filepath.Join(provisionsDir, "sub"), "inner.yml", gatusEndpointProvision))
	return provisionsDir, filepath.Join(base, "outside")
}

// TestLoadProvision_PathTraversal drives each provision-name entry point into
// the containment check: a direct name, a nested name that climbs out, and a
// name listed in a provision's includes.
func TestLoadProvision_PathTraversal(t *testing.T) {
	dir, outsideAbs := provisionTraversalFixture(t)
	require.NoError(t, writeTestFile(dir, "includes-outside.yml", "includes:\n  - ../outside\n"))
	require.NoError(t, writeTestFile(dir, "includes-nested.yml", "includes:\n  - sub/../../outside\n"))

	t.Run("refused", func(t *testing.T) {
		for _, name := range []string{
			"../outside",        // direct name
			"sub/../../outside", // climbs out through a subdirectory
			"includes-outside",  // include entry escapes
			"includes-nested",   // include entry escapes via a subdirectory
		} {
			t.Run(name, func(t *testing.T) {
				_, err := LoadProvision(name, map[string]any{}, dir)

				require.Error(t, err)
				assert.ErrorIs(t, err, ErrPathTraversal)
			})
		}
	})

	t.Run("allowed", func(t *testing.T) {
		for _, name := range []string{
			"inside",        // plain name
			"sub/inner",     // subdirectory
			"sub/../inside", // dot-dot that stays inside
			"./inside",      // leading dot-slash
		} {
			t.Run(name, func(t *testing.T) {
				p, err := LoadProvision(name, map[string]any{}, dir)

				require.NoError(t, err)
				assert.Len(t, gatusEndpoints(t, p), 1)
			})
		}
	})

	t.Run("absolute name stays inside the directory", func(t *testing.T) {
		// filepath.Join places an absolute name under the provisions dir, so it
		// is not found rather than read from outside. Control: passes on the
		// original and after the fix.
		_, err := LoadProvision(outsideAbs, map[string]any{}, dir)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "provision not found")
	})
}

func TestProvisionExists_PathTraversal(t *testing.T) {
	dir, _ := provisionTraversalFixture(t)

	assert.False(t, ProvisionExists("../outside", dir))
	assert.False(t, ProvisionExists("sub/../../outside", dir))
	assert.True(t, ProvisionExists("inside", dir))
	assert.True(t, ProvisionExists("sub/inner", dir))
	assert.False(t, ProvisionExists("missing", dir))
}

// TestRenderService_ProvisionNamePathTraversal drives the manifest-level
// entry points (provisions list, sidecar services) into the same check.
func TestRenderService_ProvisionNamePathTraversal(t *testing.T) {
	dir, _ := provisionTraversalFixture(t)

	tests := []struct {
		name     string
		manifest ServiceManifest
	}{
		{
			name:     "provisions entry",
			manifest: ServiceManifest{Name: "svc", Provisions: []string{"../outside"}},
		},
		{
			name: "sidecar services key",
			manifest: ServiceManifest{
				Name:     "svc",
				Services: map[string]map[string]any{"../outside": {}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := RenderService(&tt.manifest, dir)

			require.Error(t, err)
			assert.ErrorIs(t, err, ErrPathTraversal)
		})
	}
}

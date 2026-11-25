package reconcile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// renderInclude writes a template that includes includePath, renders it with
// IncludeDir set to includeDir, and returns any error from ExecuteTemplate.
func renderInclude(t *testing.T, sourceDir, includeDir, includePath string) error {
	t.Helper()
	templateFile := filepath.Join(sourceDir, "test.tmpl")
	content := fmt.Sprintf(`{{ include "%s" }}`, includePath)
	require.NoError(t, os.WriteFile(templateFile, []byte(content), 0644))
	outputFile := filepath.Join(sourceDir, "out", "test.txt")
	tmpl := &TemplateOps{Data: map[string]any{}, IncludeDir: includeDir}
	return tmpl.ExecuteTemplate(context.Background(), templateFile, outputFile)
}

// TestIncludeSubtreeAllowlist verifies #294: include/fromJsonFile reads are
// confined to the templates/ subtree, so sibling secrets and bosun.yaml in the
// infra root are unreachable even though they were reachable under the old
// whole-root containment.
func TestIncludeSubtreeAllowlist(t *testing.T) {
	setup := func(t *testing.T) (sourceDir, templatesDir string) {
		t.Helper()
		sourceDir, _ = filepath.EvalSymlinks(t.TempDir())
		templatesDir = filepath.Join(sourceDir, "templates")
		require.NoError(t, os.MkdirAll(templatesDir, 0755))
		return sourceDir, templatesDir
	}

	t.Run("file within templates is allowed", func(t *testing.T) {
		sourceDir, templatesDir := setup(t)
		includeFile := filepath.Join(templatesDir, "data.txt")
		require.NoError(t, os.WriteFile(includeFile, []byte("safe"), 0644))

		err := renderInclude(t, sourceDir, templatesDir, includeFile)
		require.NoError(t, err)
	})

	t.Run("sibling SOPS file in infra root is rejected", func(t *testing.T) {
		sourceDir, templatesDir := setup(t)
		sopsFile := filepath.Join(sourceDir, "secrets.sops.yaml")
		require.NoError(t, os.WriteFile(sopsFile, []byte("enc: data"), 0600))

		err := renderInclude(t, sourceDir, templatesDir, sopsFile)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "escapes the allowed include directory")
		// The error must name the allowed root so operators can fix their config.
		assert.Contains(t, err.Error(), templatesDir)
	})

	t.Run("bosun.yaml in infra root is rejected", func(t *testing.T) {
		sourceDir, templatesDir := setup(t)
		cfgFile := filepath.Join(sourceDir, "bosun.yaml")
		require.NoError(t, os.WriteFile(cfgFile, []byte("project_name: x"), 0644))

		err := renderInclude(t, sourceDir, templatesDir, cfgFile)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "escapes the allowed include directory")
	})

	t.Run("dot-dot escape from templates is rejected", func(t *testing.T) {
		_, templatesDir := setup(t)
		escape := filepath.Join(templatesDir, "..", "secret.txt")
		err := validateIncludePath(escape, templatesDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "escapes the allowed include directory")
	})

	t.Run("symlink out of templates is rejected", func(t *testing.T) {
		sourceDir, templatesDir := setup(t)
		outside := filepath.Join(sourceDir, "outside.txt")
		require.NoError(t, os.WriteFile(outside, []byte("secret"), 0644))
		link := filepath.Join(templatesDir, "link.txt")
		require.NoError(t, os.Symlink(outside, link))

		err := renderInclude(t, sourceDir, templatesDir, link)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "escapes the allowed include directory")
	})

	t.Run("hardlink inside templates to an outside file is readable", func(t *testing.T) {
		// The allowlist confines by LOCATION, not identity: a hardlink whose
		// path is inside templates/ has no ".." to resolve and no symlink to
		// follow, so it reads. This is acceptable — the property is that you
		// cannot reach a path OUTSIDE templates/, and the hardlink's path is
		// inside it.
		sourceDir, templatesDir := setup(t)
		outside := filepath.Join(sourceDir, "outside.txt")
		require.NoError(t, os.WriteFile(outside, []byte("content"), 0644))
		hard := filepath.Join(templatesDir, "hard.txt")
		if err := os.Link(outside, hard); err != nil {
			t.Skipf("hardlink unsupported: %v", err)
		}

		err := renderInclude(t, sourceDir, templatesDir, hard)
		require.NoError(t, err)
	})
}

// newIncludeSource creates a render source tree whose templates/frag.txt holds
// fragment, plus a secret.txt in the source root above templates/.
func newIncludeSource(t *testing.T, fragment string) string {
	t.Helper()
	sourceDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(sourceDir, DefaultIncludeSubdir), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, DefaultIncludeSubdir, "frag.txt"), []byte(fragment), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "secret.txt"), []byte("s3cr3t"), 0600))
	return sourceDir
}

// TestRenderDirectoryDefaultsIncludeDirPerCall verifies RenderDirectory
// confines reads to <sourceDir>/templates when the caller left IncludeDir
// empty, and restores the caller's empty value on every return.
func TestRenderDirectoryDefaultsIncludeDirPerCall(t *testing.T) {
	t.Run("default confines includes to templates and is restored", func(t *testing.T) {
		sourceDir := newIncludeSource(t, "fragment-a")
		stagingDir, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)
		include := fmt.Sprintf(`{{ include "%s" }}`, filepath.Join(sourceDir, DefaultIncludeSubdir, "frag.txt"))
		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "a.tmpl"), []byte(include), 0644))

		tmpl := &TemplateOps{Data: map[string]any{}}
		require.NoError(t, tmpl.RenderDirectory(context.Background(), sourceDir, stagingDir, "."))

		got, err := os.ReadFile(filepath.Join(stagingDir, "a"))
		require.NoError(t, err)
		assert.Equal(t, "fragment-a", string(got))
		assert.Empty(t, tmpl.IncludeDir, "the per-call default must not persist on the TemplateOps")
	})

	t.Run("read above templates is refused and IncludeDir is restored", func(t *testing.T) {
		sourceDir := newIncludeSource(t, "fragment-a")
		stagingDir, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)
		include := fmt.Sprintf(`{{ include "%s" }}`, filepath.Join(sourceDir, "secret.txt"))
		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "b.tmpl"), []byte(include), 0644))

		tmpl := &TemplateOps{Data: map[string]any{}}
		err = tmpl.RenderDirectory(context.Background(), sourceDir, stagingDir, ".")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "escapes the allowed include directory")
		assert.Empty(t, tmpl.IncludeDir)
	})

	t.Run("source root open failure restores IncludeDir", func(t *testing.T) {
		missingSource := filepath.Join(t.TempDir(), "missing")
		stagingDir := t.TempDir()

		tmpl := &TemplateOps{Data: map[string]any{}}
		err := tmpl.RenderDirectory(context.Background(), missingSource, stagingDir, ".")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "open template source root")
		assert.Empty(t, tmpl.IncludeDir, "an early return must restore IncludeDir too")
	})

	t.Run("explicit IncludeDir is kept", func(t *testing.T) {
		sourceDir := newIncludeSource(t, "fragment-a")
		stagingDir := t.TempDir()
		explicit := filepath.Join(sourceDir, DefaultIncludeSubdir)

		tmpl := &TemplateOps{Data: map[string]any{}, IncludeDir: explicit}
		require.NoError(t, tmpl.RenderDirectory(context.Background(), sourceDir, stagingDir, "."))

		assert.Equal(t, explicit, tmpl.IncludeDir)
	})
}

// TestRenderDirectory_ReuseResolvesEachSourceIncludeDir verifies a reused
// TemplateOps resolves each source tree's own templates/ directory rather than
// staying confined to the first tree's.
func TestRenderDirectory_ReuseResolvesEachSourceIncludeDir(t *testing.T) {
	tmpl := &TemplateOps{Data: map[string]any{}}

	for _, fragment := range []string{"fragment-a", "fragment-b"} {
		sourceDir := newIncludeSource(t, fragment)
		stagingDir, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)
		include := fmt.Sprintf(`{{ include "%s" }}`, filepath.Join(sourceDir, DefaultIncludeSubdir, "frag.txt"))
		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "a.tmpl"), []byte(include), 0644))

		require.NoError(t, tmpl.RenderDirectory(context.Background(), sourceDir, stagingDir, "."), fragment)

		got, err := os.ReadFile(filepath.Join(stagingDir, "a"))
		require.NoError(t, err)
		assert.Equal(t, fragment, string(got))
	}
}

func TestResolveTemplateIncludeDir(t *testing.T) {
	infra := "/srv/infra"

	assert.Equal(t, filepath.Join(infra, DefaultIncludeSubdir), ResolveTemplateIncludeDir(infra, ""),
		"empty override selects the default include subtree")
	assert.Equal(t, filepath.Join(infra, "shared"), ResolveTemplateIncludeDir(infra, "shared"),
		"relative override is joined to the infra dir")
	assert.Equal(t, "/etc/bosun/includes", ResolveTemplateIncludeDir(infra, "/etc/bosun/includes"),
		"absolute override is used as-is")
}

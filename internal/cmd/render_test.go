package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cameronsjo/bosun/internal/reconcile"
	"github.com/cameronsjo/bosun/internal/ui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderTemplate_MissingKeyIncludesTemplateAndKey(t *testing.T) {
	tmpDir := t.TempDir()
	templatePath := filepath.Join(tmpDir, "database.yml.tmpl")
	require.NoError(t, os.WriteFile(templatePath, []byte(`password: {{ .db_passwrd }}`), 0644))

	err := renderTemplate(
		context.Background(),
		templatePath,
		map[string]any{"db_password": "secret"},
		filepath.Join(tmpDir, "rendered"),
		filepath.Join(tmpDir, "templates"),
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), filepath.Base(templatePath))
	assert.Contains(t, err.Error(), `map has no entry for key "db_passwrd"`)
}

func TestRenderCmd_Registration(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"render"})
	require.NoError(t, err)
	assert.Equal(t, "render", cmd.Name())
}

func TestRenderCmd_Help(t *testing.T) {
	t.Run("render --help shows description", func(t *testing.T) {
		output, err := executeCmd(t, "render", "--help")
		require.NoError(t, err)
		assert.Contains(t, output, "render")
		assert.Contains(t, output, "templates")
		assert.Contains(t, output, "SOPS")
	})

	t.Run("render --help shows examples", func(t *testing.T) {
		output, err := executeCmd(t, "render", "--help")
		require.NoError(t, err)
		assert.Contains(t, output, "bosun render")
		assert.Contains(t, output, "config.yml.tmpl")
	})

	t.Run("render --help shows template functions", func(t *testing.T) {
		output, err := executeCmd(t, "render", "--help")
		require.NoError(t, err)
		assert.Contains(t, output, "sprig")
		assert.Contains(t, output, "include")
		assert.Contains(t, output, "fromJsonFile")
	})
}

func TestRenderCmd_NoAliases(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"render"})
	require.NoError(t, err)
	assert.Empty(t, cmd.Aliases, "render command should have no aliases")
}

func TestRenderTemplateFuncs(t *testing.T) {
	tmpDir := t.TempDir()
	includeDir := filepath.Join(tmpDir, "templates")
	require.NoError(t, os.Mkdir(includeDir, 0o755))
	textFile := filepath.Join(includeDir, "test.txt")
	require.NoError(t, os.WriteFile(textFile, []byte("hello world"), 0o644))
	jsonFile := filepath.Join(includeDir, "data.json")
	require.NoError(t, os.WriteFile(jsonFile, []byte(`{"key":"value"}`), 0o644))

	funcs := reconcile.TemplateFuncs(includeDir)

	t.Run("has include function", func(t *testing.T) {
		_, ok := funcs["include"]
		assert.True(t, ok, "TemplateFuncs should contain 'include'")
	})

	t.Run("has fromJsonFile function", func(t *testing.T) {
		_, ok := funcs["fromJsonFile"]
		assert.True(t, ok, "TemplateFuncs should contain 'fromJsonFile'")
	})

	t.Run("include reads file", func(t *testing.T) {
		includeFn := funcs["include"].(func(string) (string, error))
		result, err := includeFn(textFile)
		require.NoError(t, err)
		assert.Equal(t, "hello world", result)
	})

	t.Run("include returns error for missing file", func(t *testing.T) {
		includeFn := funcs["include"].(func(string) (string, error))
		_, err := includeFn(filepath.Join(includeDir, "missing.txt"))
		assert.Error(t, err)
	})

	t.Run("fromJsonFile parses JSON", func(t *testing.T) {
		fromJsonFn := funcs["fromJsonFile"].(func(string) (any, error))
		result, err := fromJsonFn(jsonFile)
		require.NoError(t, err)

		data, ok := result.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "value", data["key"])
	})

	t.Run("fromJsonFile returns error for invalid JSON", func(t *testing.T) {
		testFile := filepath.Join(includeDir, "bad.json")
		require.NoError(t, os.WriteFile(testFile, []byte("not json"), 0644))

		fromJsonFn := funcs["fromJsonFile"].(func(string) (any, error))
		_, err := fromJsonFn(testFile)
		assert.Error(t, err)
	})

	t.Run("fromJsonFile returns error for missing file", func(t *testing.T) {
		fromJsonFn := funcs["fromJsonFile"].(func(string) (any, error))
		_, err := fromJsonFn(filepath.Join(includeDir, "missing.json"))
		assert.Error(t, err)
	})
}

func TestRenderTemplateFuncs_RejectReadsOutsideIncludeDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	includeDir := filepath.Join(tmpDir, "templates")
	require.NoError(t, os.Mkdir(includeDir, 0o755))

	outsideText := filepath.Join(tmpDir, "secrets.sops.yaml")
	require.NoError(t, os.WriteFile(outsideText, []byte("secret"), 0o600))
	outsideJSON := filepath.Join(tmpDir, "bosun.json")
	require.NoError(t, os.WriteFile(outsideJSON, []byte(`{"secret":true}`), 0o600))

	funcs := reconcile.TemplateFuncs(includeDir)
	tests := []struct {
		name string
		call func(string) error
		path string
	}{
		{"include traversal", func(path string) error { _, err := funcs["include"].(func(string) (string, error))(path); return err }, filepath.Join(includeDir, "..", filepath.Base(outsideText))},
		{"fromJsonFile traversal", func(path string) error { _, err := funcs["fromJsonFile"].(func(string) (any, error))(path); return err }, filepath.Join(includeDir, "..", filepath.Base(outsideJSON))},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call(tc.path)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.path)
			assert.Contains(t, err.Error(), includeDir)
		})
	}

	symlinkTests := []struct {
		name   string
		call   func(string) error
		target string
	}{
		{"include symlink", func(path string) error { _, err := funcs["include"].(func(string) (string, error))(path); return err }, outsideText},
		{"fromJsonFile symlink", func(path string) error { _, err := funcs["fromJsonFile"].(func(string) (any, error))(path); return err }, outsideJSON},
	}
	for _, tc := range symlinkTests {
		t.Run(tc.name, func(t *testing.T) {
			linkPath := filepath.Join(includeDir, tc.name)
			if err := os.Symlink(tc.target, linkPath); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}

			err := tc.call(linkPath)
			require.Error(t, err)
			assert.Contains(t, err.Error(), linkPath)
			assert.Contains(t, err.Error(), includeDir)
		})
	}
}

func TestRenderTemplate_UsesConfinedFileFunctions(t *testing.T) {
	tmpDir := t.TempDir()
	includeDir := filepath.Join(tmpDir, "templates")
	require.NoError(t, os.Mkdir(includeDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(includeDir, "message.txt"), []byte("hello"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(includeDir, "data.json"), []byte(`{"name":"bosun"}`), 0o644))

	templatePath := filepath.Join(tmpDir, "config.tmpl")
	templateBody := `{{ include "` + filepath.Join(includeDir, "message.txt") + `" }} {{ (fromJsonFile "` + filepath.Join(includeDir, "data.json") + `").name }}`
	require.NoError(t, os.WriteFile(templatePath, []byte(templateBody), 0o644))
	outputDir := filepath.Join(tmpDir, "rendered")

	require.NoError(t, renderTemplate(context.Background(), templatePath, map[string]any{}, outputDir, includeDir))
	output, err := os.ReadFile(filepath.Join(outputDir, strings.TrimSuffix(templatePath, ".tmpl")))
	require.NoError(t, err)
	assert.Equal(t, "hello bosun", string(output))
}

func TestRenderTemplate_RejectsFileFunctionEscapes(t *testing.T) {
	tmpDir := t.TempDir()
	includeDir := filepath.Join(tmpDir, "templates")
	require.NoError(t, os.Mkdir(includeDir, 0o755))
	outsideText := filepath.Join(tmpDir, "secrets.sops.yaml")
	require.NoError(t, os.WriteFile(outsideText, []byte("secret"), 0o600))
	outsideJSON := filepath.Join(tmpDir, "bosun.json")
	require.NoError(t, os.WriteFile(outsideJSON, []byte(`{"secret":true}`), 0o600))

	tests := []struct {
		name string
		body string
		path string
	}{
		{"include", `{{ include "` + outsideText + `" }}`, outsideText},
		{"fromJsonFile", `{{ fromJsonFile "` + outsideJSON + `" }}`, outsideJSON},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			templatePath := filepath.Join(tmpDir, tc.name+".tmpl")
			require.NoError(t, os.WriteFile(templatePath, []byte(tc.body), 0o644))

			err := renderTemplate(context.Background(), templatePath, map[string]any{}, filepath.Join(tmpDir, "rendered"), includeDir)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.path)
			assert.Contains(t, err.Error(), includeDir)
		})
	}
}

func TestRenderTemplateIncludeDir(t *testing.T) {
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.Chdir(originalDir)) })
	t.Setenv("BOSUN_INFRA_DIR", "")
	t.Setenv("BOSUN_TEMPLATE_INCLUDE_DIR", "")

	t.Run("standalone render defaults to current templates directory", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.Chdir(dir))

		got := mustRenderTemplateIncludeDir()
		assert.Equal(t, filepath.Join(evalSymlinks(t, dir), reconcile.DefaultIncludeSubdir), got)
	})

	t.Run("project config resolves relative to infra directory", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "bosun.yaml"), []byte("template_include_dir: shared\n"), 0o644))
		nested := filepath.Join(root, "nested")
		require.NoError(t, os.Mkdir(nested, 0o755))
		require.NoError(t, os.Chdir(nested))
		t.Setenv("BOSUN_INFRA_DIR", "unraid")

		got, err := renderTemplateIncludeDir()
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(evalSymlinks(t, root), "unraid", "shared"), got)
	})

	t.Run("environment overrides project config", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "bosun.yaml"), []byte("template_include_dir: shared\n"), 0o644))
		require.NoError(t, os.Chdir(root))
		t.Setenv("BOSUN_INFRA_DIR", "")
		t.Setenv("BOSUN_TEMPLATE_INCLUDE_DIR", "env-includes")

		got, err := renderTemplateIncludeDir()
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(evalSymlinks(t, root), "env-includes"), got)
	})

	t.Run("invalid project config is reported", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "bosun.yaml"), []byte("template_include_dir: [\n"), 0o644))
		require.NoError(t, os.Chdir(root))
		t.Setenv("BOSUN_TEMPLATE_INCLUDE_DIR", "")

		_, err := renderTemplateIncludeDir()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "load project config")
		assert.Contains(t, err.Error(), "bosun.yaml")

		exitCode := 0
		oldExitFn := ui.SetExitFn(func(code int) { exitCode = code })
		t.Cleanup(func() { ui.SetExitFn(oldExitFn) })

		assert.Empty(t, mustRenderTemplateIncludeDir())
		assert.Equal(t, 1, exitCode)
	})
}

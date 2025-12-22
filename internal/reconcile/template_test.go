package reconcile

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTemplateOps(t *testing.T) {
	data := map[string]any{
		"key": "value",
	}
	tmpl := NewTemplateOps(data)

	assert.NotNil(t, tmpl)
	assert.Equal(t, "value", tmpl.Data["key"])
}

func TestTemplateOps_ExecuteTemplate(t *testing.T) {
	if _, err := exec.LookPath("chezmoi"); err != nil {
		t.Skip("chezmoi not installed")
	}

	t.Run("simple template", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		// Create template file
		templateFile := filepath.Join(tmpDir, "test.tmpl")
		templateContent := `Hello, World!`
		require.NoError(t, os.WriteFile(templateFile, []byte(templateContent), 0644))

		outputFile := filepath.Join(tmpDir, "output", "test.txt")

		tmpl := NewTemplateOps(map[string]any{})
		err := tmpl.ExecuteTemplate(ctx, templateFile, outputFile)

		require.NoError(t, err)

		// Verify output
		content, err := os.ReadFile(outputFile)
		require.NoError(t, err)
		assert.Equal(t, "Hello, World!", string(content))
	})

	t.Run("template with variables", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		// Create template file with env variable
		templateFile := filepath.Join(tmpDir, "test.tmpl")
		templateContent := `Static content`
		require.NoError(t, os.WriteFile(templateFile, []byte(templateContent), 0644))

		outputFile := filepath.Join(tmpDir, "output", "test.txt")

		data := map[string]any{
			"name": "Test",
		}
		tmpl := NewTemplateOps(data)
		err := tmpl.ExecuteTemplate(ctx, templateFile, outputFile)

		require.NoError(t, err)
	})

	t.Run("non-existent template", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		tmpl := NewTemplateOps(map[string]any{})
		err := tmpl.ExecuteTemplate(ctx, "/non/existent/template.tmpl", filepath.Join(tmpDir, "output.txt"))

		assert.Error(t, err)
	})

	t.Run("creates output directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		templateFile := filepath.Join(tmpDir, "test.tmpl")
		require.NoError(t, os.WriteFile(templateFile, []byte("content"), 0644))

		// Deep nested output path
		outputFile := filepath.Join(tmpDir, "deep", "nested", "dir", "output.txt")

		tmpl := NewTemplateOps(map[string]any{})
		err := tmpl.ExecuteTemplate(ctx, templateFile, outputFile)

		require.NoError(t, err)
		assert.FileExists(t, outputFile)
	})
}

func TestTemplateOps_RenderDirectory(t *testing.T) {
	if _, err := exec.LookPath("chezmoi"); err != nil {
		t.Skip("chezmoi not installed")
	}

	t.Run("render directory with templates and static files", func(t *testing.T) {
		tmpDir := t.TempDir()
		sourceDir := filepath.Join(tmpDir, "source")
		stagingDir := filepath.Join(tmpDir, "staging")
		ctx := context.Background()

		// Create source structure
		infraDir := filepath.Join(sourceDir, "infra")
		require.NoError(t, os.MkdirAll(infraDir, 0755))

		// Create template file
		templateFile := filepath.Join(sourceDir, "config.yaml.tmpl")
		require.NoError(t, os.WriteFile(templateFile, []byte("key: value"), 0644))

		// Create static file in infra
		staticFile := filepath.Join(infraDir, "static.yml")
		require.NoError(t, os.WriteFile(staticFile, []byte("static: content"), 0644))

		tmpl := NewTemplateOps(map[string]any{})
		err := tmpl.RenderDirectory(ctx, sourceDir, stagingDir, "infra")

		require.NoError(t, err)

		// Verify template was rendered (without .tmpl extension)
		renderedTemplate := filepath.Join(stagingDir, "config.yaml")
		assert.FileExists(t, renderedTemplate)

		// Verify static file was copied
		copiedStatic := filepath.Join(stagingDir, "infra", "static.yml")
		assert.FileExists(t, copiedStatic)
	})

	t.Run("non-existent source directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		tmpl := NewTemplateOps(map[string]any{})
		err := tmpl.RenderDirectory(ctx, "/non/existent", tmpDir, "subdir")

		assert.Error(t, err)
	})

	t.Run("empty source directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		sourceDir := filepath.Join(tmpDir, "source")
		stagingDir := filepath.Join(tmpDir, "staging")
		infraDir := filepath.Join(sourceDir, "infra")
		ctx := context.Background()

		require.NoError(t, os.MkdirAll(infraDir, 0755))

		tmpl := NewTemplateOps(map[string]any{})
		err := tmpl.RenderDirectory(ctx, sourceDir, stagingDir, "infra")

		require.NoError(t, err)
	})
}

func TestCopyNonTemplateFiles(t *testing.T) {
	t.Run("copy mixed files", func(t *testing.T) {
		tmpDir := t.TempDir()
		srcDir := filepath.Join(tmpDir, "src")
		dstDir := filepath.Join(tmpDir, "dst")

		require.NoError(t, os.MkdirAll(srcDir, 0755))

		// Create various files
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "regular.yml"), []byte("content"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "template.tmpl"), []byte("template"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "config.json"), []byte("{}"), 0644))

		err := copyNonTemplateFiles(srcDir, dstDir)
		require.NoError(t, err)

		// Regular files should be copied
		assert.FileExists(t, filepath.Join(dstDir, "regular.yml"))
		assert.FileExists(t, filepath.Join(dstDir, "config.json"))

		// Template files should NOT be copied
		assert.NoFileExists(t, filepath.Join(dstDir, "template.tmpl"))
	})

	t.Run("copy with subdirectories", func(t *testing.T) {
		tmpDir := t.TempDir()
		srcDir := filepath.Join(tmpDir, "src")
		dstDir := filepath.Join(tmpDir, "dst")

		subDir := filepath.Join(srcDir, "sub")
		require.NoError(t, os.MkdirAll(subDir, 0755))

		require.NoError(t, os.WriteFile(filepath.Join(subDir, "file.txt"), []byte("content"), 0644))

		err := copyNonTemplateFiles(srcDir, dstDir)
		require.NoError(t, err)

		assert.FileExists(t, filepath.Join(dstDir, "sub", "file.txt"))
	})

	t.Run("non-existent source", func(t *testing.T) {
		tmpDir := t.TempDir()
		dstDir := filepath.Join(tmpDir, "dst")

		err := copyNonTemplateFiles("/non/existent", dstDir)
		// Should not error because of IsNotExist check
		require.NoError(t, err)
	})
}

func TestCopyFile(t *testing.T) {
	t.Run("copy file", func(t *testing.T) {
		tmpDir := t.TempDir()
		srcFile := filepath.Join(tmpDir, "src.txt")
		dstFile := filepath.Join(tmpDir, "dst.txt")

		content := "test content"
		require.NoError(t, os.WriteFile(srcFile, []byte(content), 0644))

		err := copyFile(srcFile, dstFile)
		require.NoError(t, err)

		copied, err := os.ReadFile(dstFile)
		require.NoError(t, err)
		assert.Equal(t, content, string(copied))
	})

	t.Run("copy to nested directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		srcFile := filepath.Join(tmpDir, "src.txt")
		dstFile := filepath.Join(tmpDir, "deep", "nested", "dst.txt")

		require.NoError(t, os.WriteFile(srcFile, []byte("content"), 0644))

		err := copyFile(srcFile, dstFile)
		require.NoError(t, err)

		assert.FileExists(t, dstFile)
	})

	t.Run("non-existent source", func(t *testing.T) {
		tmpDir := t.TempDir()
		dstFile := filepath.Join(tmpDir, "dst.txt")

		err := copyFile("/non/existent/file.txt", dstFile)
		assert.Error(t, err)
	})
}

package reconcile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/cameronsjo/bosun/internal/fileutil"
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

		// Create template file with variable interpolation
		templateFile := filepath.Join(tmpDir, "test.tmpl")
		templateContent := `Hello, {{ .name }}!`
		require.NoError(t, os.WriteFile(templateFile, []byte(templateContent), 0644))

		outputFile := filepath.Join(tmpDir, "output", "test.txt")

		data := map[string]any{
			"name": "Test",
		}
		tmpl := NewTemplateOps(data)
		err := tmpl.ExecuteTemplate(ctx, templateFile, outputFile)

		require.NoError(t, err)

		// Verify output contains interpolated value
		content, err := os.ReadFile(outputFile)
		require.NoError(t, err)
		assert.Equal(t, "Hello, Test!", string(content))
	})

	t.Run("template with sprig functions", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		// Create template using sprig functions (upper, lower, default)
		templateFile := filepath.Join(tmpDir, "test.tmpl")
		templateContent := `{{ .name | upper }} - {{ .missing | default "fallback" }}`
		require.NoError(t, os.WriteFile(templateFile, []byte(templateContent), 0644))

		outputFile := filepath.Join(tmpDir, "output", "test.txt")

		data := map[string]any{
			"name": "hello",
		}
		tmpl := NewTemplateOps(data)
		err := tmpl.ExecuteTemplate(ctx, templateFile, outputFile)

		require.NoError(t, err)

		content, err := os.ReadFile(outputFile)
		require.NoError(t, err)
		assert.Equal(t, "HELLO - fallback", string(content))
	})

	t.Run("template with env function", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		// Create template using env function (provided by sprig)
		templateFile := filepath.Join(tmpDir, "test.tmpl")
		templateContent := `Home: {{ env "HOME" }}`
		require.NoError(t, os.WriteFile(templateFile, []byte(templateContent), 0644))

		outputFile := filepath.Join(tmpDir, "output", "test.txt")

		tmpl := NewTemplateOps(map[string]any{})
		err := tmpl.ExecuteTemplate(ctx, templateFile, outputFile)

		require.NoError(t, err)

		content, err := os.ReadFile(outputFile)
		require.NoError(t, err)
		assert.Contains(t, string(content), "Home: ")
	})

	t.Run("template with toJson and fromJson", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		// Create template using JSON functions (provided by sprig)
		templateFile := filepath.Join(tmpDir, "test.tmpl")
		templateContent := `{{ .data | toJson }}`
		require.NoError(t, os.WriteFile(templateFile, []byte(templateContent), 0644))

		outputFile := filepath.Join(tmpDir, "output", "test.txt")

		data := map[string]any{
			"data": map[string]any{
				"key": "value",
			},
		}
		tmpl := NewTemplateOps(data)
		err := tmpl.ExecuteTemplate(ctx, templateFile, outputFile)

		require.NoError(t, err)

		content, err := os.ReadFile(outputFile)
		require.NoError(t, err)
		assert.Equal(t, `{"key":"value"}`, string(content))
	})

	t.Run("template with include function", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		// Create a file to include
		includeFile := filepath.Join(tmpDir, "include.txt")
		require.NoError(t, os.WriteFile(includeFile, []byte("included content"), 0644))

		// Create template that includes the file
		templateFile := filepath.Join(tmpDir, "test.tmpl")
		templateContent := fmt.Sprintf(`Content: {{ include "%s" }}`, includeFile)
		require.NoError(t, os.WriteFile(templateFile, []byte(templateContent), 0644))

		outputFile := filepath.Join(tmpDir, "output", "test.txt")

		tmpl := NewTemplateOps(map[string]any{})
		err := tmpl.ExecuteTemplate(ctx, templateFile, outputFile)

		require.NoError(t, err)

		content, err := os.ReadFile(outputFile)
		require.NoError(t, err)
		assert.Equal(t, "Content: included content", string(content))
	})

	t.Run("template with fromJsonFile function", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		// Create a JSON file to read
		jsonFile := filepath.Join(tmpDir, "data.json")
		require.NoError(t, os.WriteFile(jsonFile, []byte(`{"name":"test","port":8080}`), 0644))

		// Create template that reads JSON file
		templateFile := filepath.Join(tmpDir, "test.tmpl")
		templateContent := fmt.Sprintf(`{{ $data := fromJsonFile "%s" }}Name: {{ $data.name }}, Port: {{ $data.port }}`, jsonFile)
		require.NoError(t, os.WriteFile(templateFile, []byte(templateContent), 0644))

		outputFile := filepath.Join(tmpDir, "output", "test.txt")

		tmpl := NewTemplateOps(map[string]any{})
		err := tmpl.ExecuteTemplate(ctx, templateFile, outputFile)

		require.NoError(t, err)

		content, err := os.ReadFile(outputFile)
		require.NoError(t, err)
		assert.Equal(t, "Name: test, Port: 8080", string(content))
	})

	t.Run("invalid template syntax", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		// Create template with invalid syntax
		templateFile := filepath.Join(tmpDir, "test.tmpl")
		templateContent := `{{ .name | invalidFunc }}`
		require.NoError(t, os.WriteFile(templateFile, []byte(templateContent), 0644))

		outputFile := filepath.Join(tmpDir, "output", "test.txt")

		tmpl := NewTemplateOps(map[string]any{"name": "test"})
		err := tmpl.ExecuteTemplate(ctx, templateFile, outputFile)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse template")
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

func TestValidateIncludePath(t *testing.T) {
	t.Run("allows path within source directory", func(t *testing.T) {
		tmpDir, _ := filepath.EvalSymlinks(t.TempDir())
		includeFile := filepath.Join(tmpDir, "sub", "file.txt")
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "sub"), 0755))
		require.NoError(t, os.WriteFile(includeFile, []byte("ok"), 0644))

		err := validateIncludePath(includeFile, tmpDir)
		assert.NoError(t, err)
	})

	t.Run("blocks path traversal via dot-dot", func(t *testing.T) {
		tmpDir, _ := filepath.EvalSymlinks(t.TempDir())
		outsidePath := filepath.Join(tmpDir, "..", "escape.txt")

		err := validateIncludePath(outsidePath, tmpDir)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "escapes source directory")
	})

	t.Run("blocks absolute path outside source", func(t *testing.T) {
		tmpDir, _ := filepath.EvalSymlinks(t.TempDir())

		err := validateIncludePath("/etc/passwd", tmpDir)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "escapes source directory")
	})

	t.Run("blocks symlink escape", func(t *testing.T) {
		tmpDir, _ := filepath.EvalSymlinks(t.TempDir())
		outsideDir, _ := filepath.EvalSymlinks(t.TempDir())

		// Create a target file outside the source tree.
		targetFile := filepath.Join(outsideDir, "secret.txt")
		require.NoError(t, os.WriteFile(targetFile, []byte("secret"), 0644))

		// Create a symlink inside the source tree pointing outside.
		symlinkPath := filepath.Join(tmpDir, "escape-link")
		require.NoError(t, os.Symlink(targetFile, symlinkPath))

		err := validateIncludePath(symlinkPath, tmpDir)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "escapes source directory")
	})

	t.Run("skips validation when source dir is empty", func(t *testing.T) {
		err := validateIncludePath("/any/path", "")
		assert.NoError(t, err)
	})
}

func TestTemplateOps_IncludePathValidation(t *testing.T) {
	t.Run("include blocked outside source dir", func(t *testing.T) {
		tmpDir, _ := filepath.EvalSymlinks(t.TempDir())
		ctx := context.Background()

		// Create template that tries to include /etc/hosts.
		templateFile := filepath.Join(tmpDir, "test.tmpl")
		templateContent := `{{ include "/etc/hosts" }}`
		require.NoError(t, os.WriteFile(templateFile, []byte(templateContent), 0644))

		outputFile := filepath.Join(tmpDir, "output", "test.txt")

		tmpl := &TemplateOps{Data: map[string]any{}, SourceDir: tmpDir}
		err := tmpl.ExecuteTemplate(ctx, templateFile, outputFile)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "escapes source directory")
	})

	t.Run("include allowed within source dir", func(t *testing.T) {
		tmpDir, _ := filepath.EvalSymlinks(t.TempDir())
		ctx := context.Background()

		// Create a file to include within the source dir.
		includeFile := filepath.Join(tmpDir, "data.txt")
		require.NoError(t, os.WriteFile(includeFile, []byte("safe content"), 0644))

		templateFile := filepath.Join(tmpDir, "test.tmpl")
		templateContent := fmt.Sprintf(`{{ include "%s" }}`, includeFile)
		require.NoError(t, os.WriteFile(templateFile, []byte(templateContent), 0644))

		outputFile := filepath.Join(tmpDir, "output", "test.txt")

		tmpl := &TemplateOps{Data: map[string]any{}, SourceDir: tmpDir}
		err := tmpl.ExecuteTemplate(ctx, templateFile, outputFile)

		require.NoError(t, err)

		content, err := os.ReadFile(outputFile)
		require.NoError(t, err)
		assert.Equal(t, "safe content", string(content))
	})

	t.Run("fromJsonFile blocked outside source dir", func(t *testing.T) {
		tmpDir, _ := filepath.EvalSymlinks(t.TempDir())
		ctx := context.Background()

		templateFile := filepath.Join(tmpDir, "test.tmpl")
		templateContent := `{{ fromJsonFile "/etc/hosts" }}`
		require.NoError(t, os.WriteFile(templateFile, []byte(templateContent), 0644))

		outputFile := filepath.Join(tmpDir, "output", "test.txt")

		tmpl := &TemplateOps{Data: map[string]any{}, SourceDir: tmpDir}
		err := tmpl.ExecuteTemplate(ctx, templateFile, outputFile)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "escapes source directory")
	})
}

func TestCopyFile(t *testing.T) {
	t.Run("copy file", func(t *testing.T) {
		tmpDir := t.TempDir()
		srcFile := filepath.Join(tmpDir, "src.txt")
		dstFile := filepath.Join(tmpDir, "dst.txt")

		content := "test content"
		require.NoError(t, os.WriteFile(srcFile, []byte(content), 0644))

		err := fileutil.CopyFile(srcFile, dstFile)
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

		err := fileutil.CopyFile(srcFile, dstFile)
		require.NoError(t, err)

		assert.FileExists(t, dstFile)
	})

	t.Run("non-existent source", func(t *testing.T) {
		tmpDir := t.TempDir()
		dstFile := filepath.Join(tmpDir, "dst.txt")

		err := fileutil.CopyFile("/non/existent/file.txt", dstFile)
		assert.Error(t, err)
	})
}

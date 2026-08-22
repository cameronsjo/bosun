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

func TestTemplateOps_ExecuteTemplate_ExecutionError(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	// Template that calls a function which returns an error.
	templateFile := filepath.Join(tmpDir, "bad.tmpl")
	require.NoError(t, os.WriteFile(templateFile, []byte(`{{ include "/nonexistent/required/file.txt" }}`), 0644))

	outputFile := filepath.Join(tmpDir, "output.txt")

	tmpl := NewTemplateOps(map[string]any{})
	err := tmpl.ExecuteTemplate(ctx, templateFile, outputFile)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to execute template")
}

func TestBosunTemplateFuncs_FromJsonFileNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	templateFile := filepath.Join(tmpDir, "test.tmpl")
	require.NoError(t, os.WriteFile(templateFile, []byte(`{{ fromJsonFile "/nonexistent/data.json" }}`), 0644))

	outputFile := filepath.Join(tmpDir, "output.txt")

	tmpl := NewTemplateOps(map[string]any{})
	err := tmpl.ExecuteTemplate(ctx, templateFile, outputFile)
	assert.Error(t, err)
}

func TestBosunTemplateFuncs_FromJsonFileInvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	jsonFile := filepath.Join(tmpDir, "bad.json")
	require.NoError(t, os.WriteFile(jsonFile, []byte("not json{{{"), 0644))

	templateFile := filepath.Join(tmpDir, "test.tmpl")
	require.NoError(t, os.WriteFile(templateFile, []byte(fmt.Sprintf(`{{ fromJsonFile "%s" }}`, jsonFile)), 0644))

	outputFile := filepath.Join(tmpDir, "output.txt")

	tmpl := NewTemplateOps(map[string]any{})
	err := tmpl.ExecuteTemplate(ctx, templateFile, outputFile)
	assert.Error(t, err)
}

func TestTemplateOps_RenderDirectory(t *testing.T) {
	t.Run("render directory with templates and static files", func(t *testing.T) {
		tmpDir := t.TempDir()
		// sourceDir simulates RepoDir/InfraSubDir — the caller already joins them.
		sourceDir := filepath.Join(tmpDir, "repo", "infra")
		stagingDir := filepath.Join(tmpDir, "staging")
		ctx := context.Background()

		// Create source structure (everything inside sourceDir, like production).
		composeDir := filepath.Join(sourceDir, "compose")
		require.NoError(t, os.MkdirAll(composeDir, 0755))

		// Create template file inside sourceDir.
		templateFile := filepath.Join(composeDir, "stack.yml.tmpl")
		require.NoError(t, os.WriteFile(templateFile, []byte("key: value"), 0644))

		// Create static file inside sourceDir.
		staticFile := filepath.Join(sourceDir, "static.yml")
		require.NoError(t, os.WriteFile(staticFile, []byte("static: content"), 0644))

		tmpl := NewTemplateOps(map[string]any{})
		err := tmpl.RenderDirectory(ctx, sourceDir, stagingDir, "infra")

		require.NoError(t, err)

		// Verify template was rendered inside stagingDir/subDir.
		renderedTemplate := filepath.Join(stagingDir, "infra", "compose", "stack.yml")
		assert.FileExists(t, renderedTemplate)

		// Verify static file was copied to stagingDir/subDir.
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

	t.Run("non-root InfraSubDir renders to correct staging path", func(t *testing.T) {
		// Regression test for #190: when InfraSubDir != ".", rendered files
		// must land in stagingDir/subDir/ so deployLocal can find them.
		tmpDir := t.TempDir()
		// Simulate the caller: sourceDir = RepoDir/InfraSubDir
		sourceDir := filepath.Join(tmpDir, "repo", "unraid")
		stagingDir := filepath.Join(tmpDir, "staging")
		ctx := context.Background()

		// Create repo structure: unraid/compose/stack.yml.tmpl + unraid/appdata/traefik/conf.yml
		composeDir := filepath.Join(sourceDir, "compose")
		appdataDir := filepath.Join(sourceDir, "appdata", "traefik")
		require.NoError(t, os.MkdirAll(composeDir, 0755))
		require.NoError(t, os.MkdirAll(appdataDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(composeDir, "stack.yml.tmpl"), []byte("services: {}"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(appdataDir, "conf.yml"), []byte("static: true"), 0644))

		tmpl := NewTemplateOps(map[string]any{})
		err := tmpl.RenderDirectory(ctx, sourceDir, stagingDir, "unraid")
		require.NoError(t, err)

		// deployLocal reads from staging/unraid/ — verify files land there.
		assert.FileExists(t, filepath.Join(stagingDir, "unraid", "compose", "stack.yml"),
			"rendered template must be in staging/unraid/compose/, not staging/compose/")
		assert.FileExists(t, filepath.Join(stagingDir, "unraid", "appdata", "traefik", "conf.yml"),
			"static file must be in staging/unraid/appdata/, not staging/appdata/")

		// Verify files do NOT exist at the wrong (old buggy) paths.
		assert.NoFileExists(t, filepath.Join(stagingDir, "compose", "stack.yml"),
			"rendered template must not be at staging root (missing subDir prefix)")
	})
}

func TestTemplateOps_ExecuteTemplateErrors(t *testing.T) {
	t.Run("template execution error with call on wrong type", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		// Template calls a method on a type that doesn't have it, causing Execute error
		templateFile := filepath.Join(tmpDir, "test.tmpl")
		templateContent := `{{ call .config }}`
		require.NoError(t, os.WriteFile(templateFile, []byte(templateContent), 0644))

		outputFile := filepath.Join(tmpDir, "output", "test.txt")

		tmpl := NewTemplateOps(map[string]any{
			"config": "not-a-function", // string, not callable
		})
		err := tmpl.ExecuteTemplate(ctx, templateFile, outputFile)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to execute template")
	})

	t.Run("include function with missing file", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		templateFile := filepath.Join(tmpDir, "test.tmpl")
		templateContent := `{{ include "/nonexistent/file.txt" }}`
		require.NoError(t, os.WriteFile(templateFile, []byte(templateContent), 0644))

		outputFile := filepath.Join(tmpDir, "output", "test.txt")

		tmpl := NewTemplateOps(map[string]any{})
		err := tmpl.ExecuteTemplate(ctx, templateFile, outputFile)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to execute template")
	})

	t.Run("fromJsonFile with invalid JSON", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		// Create a file with invalid JSON
		jsonFile := filepath.Join(tmpDir, "bad.json")
		require.NoError(t, os.WriteFile(jsonFile, []byte("{invalid json"), 0644))

		templateFile := filepath.Join(tmpDir, "test.tmpl")
		templateContent := fmt.Sprintf(`{{ $data := fromJsonFile "%s" }}{{ $data }}`, jsonFile)
		require.NoError(t, os.WriteFile(templateFile, []byte(templateContent), 0644))

		outputFile := filepath.Join(tmpDir, "output", "test.txt")

		tmpl := NewTemplateOps(map[string]any{})
		err := tmpl.ExecuteTemplate(ctx, templateFile, outputFile)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to execute template")
	})

	t.Run("fromJsonFile with missing file", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		templateFile := filepath.Join(tmpDir, "test.tmpl")
		templateContent := `{{ $data := fromJsonFile "/nonexistent/file.json" }}{{ $data }}`
		require.NoError(t, os.WriteFile(templateFile, []byte(templateContent), 0644))

		outputFile := filepath.Join(tmpDir, "output", "test.txt")

		tmpl := NewTemplateOps(map[string]any{})
		err := tmpl.ExecuteTemplate(ctx, templateFile, outputFile)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to execute template")
	})
}

func TestTemplateOps_RenderDirectoryErrors(t *testing.T) {
	t.Run("template parse error propagates", func(t *testing.T) {
		tmpDir := t.TempDir()
		sourceDir := filepath.Join(tmpDir, "source")
		stagingDir := filepath.Join(tmpDir, "staging")
		infraDir := filepath.Join(sourceDir, "infra")
		ctx := context.Background()

		require.NoError(t, os.MkdirAll(infraDir, 0755))

		// Create a template with invalid syntax
		tmplFile := filepath.Join(sourceDir, "bad.yaml.tmpl")
		require.NoError(t, os.WriteFile(tmplFile, []byte(`{{ .name | noSuchFunc }}`), 0644))

		tmpl := NewTemplateOps(map[string]any{})
		err := tmpl.RenderDirectory(ctx, sourceDir, stagingDir, "infra")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to render templates")
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

	t.Run("symlink is skipped and later files are copied", func(t *testing.T) {
		tmpDir := t.TempDir()
		srcDir := filepath.Join(tmpDir, "src")
		dstDir := filepath.Join(tmpDir, "dst")
		outsideFile := filepath.Join(tmpDir, "outside.txt")

		require.NoError(t, os.MkdirAll(srcDir, 0755))
		require.NoError(t, os.WriteFile(outsideFile, []byte("outside"), 0644))
		if err := os.Symlink(outsideFile, filepath.Join(srcDir, "aaa-link.txt")); err != nil {
			t.Skipf("symlink creation unavailable: %v", err)
		}
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "zzz-regular.txt"), []byte("copied"), 0644))

		err := copyNonTemplateFiles(srcDir, dstDir)
		require.NoError(t, err)
		assert.FileExists(t, filepath.Join(dstDir, "zzz-regular.txt"))
		assert.NoFileExists(t, filepath.Join(dstDir, "aaa-link.txt"))
	})

	t.Run("non-existent source directory errors", func(t *testing.T) {
		tmpDir := t.TempDir()
		dstDir := filepath.Join(tmpDir, "dst")

		err := copyNonTemplateFiles("/non/existent", dstDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "source directory does not exist")
	})

	t.Run("file missing before walk does not error", func(t *testing.T) {
		tmpDir := t.TempDir()
		srcDir := filepath.Join(tmpDir, "src")
		dstDir := filepath.Join(tmpDir, "dst")

		require.NoError(t, os.MkdirAll(srcDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "keep.txt"), []byte("keep"), 0644))

		// Remove before walking starts. This validates tolerant behavior for
		// absent entries at traversal time, not a true mid-walk race.
		vanishFile := filepath.Join(srcDir, "vanish.txt")
		require.NoError(t, os.WriteFile(vanishFile, []byte("gone"), 0644))
		require.NoError(t, os.Remove(vanishFile))

		err := copyNonTemplateFiles(srcDir, dstDir)
		require.NoError(t, err)
		assert.FileExists(t, filepath.Join(dstDir, "keep.txt"))
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
		assert.Contains(t, err.Error(), "escapes the allowed include directory")
	})

	t.Run("blocks absolute path outside source", func(t *testing.T) {
		tmpDir, _ := filepath.EvalSymlinks(t.TempDir())

		err := validateIncludePath("/etc/passwd", tmpDir)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "escapes the allowed include directory")
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
		assert.Contains(t, err.Error(), "escapes the allowed include directory")
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

		tmpl := &TemplateOps{Data: map[string]any{}, IncludeDir: tmpDir}
		err := tmpl.ExecuteTemplate(ctx, templateFile, outputFile)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "escapes the allowed include directory")
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

		tmpl := &TemplateOps{Data: map[string]any{}, IncludeDir: tmpDir}
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

		tmpl := &TemplateOps{Data: map[string]any{}, IncludeDir: tmpDir}
		err := tmpl.ExecuteTemplate(ctx, templateFile, outputFile)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "escapes the allowed include directory")
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

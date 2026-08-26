package manifest

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTemplateEngine(t *testing.T) {
	templatesDir := filepath.Join("testdata", "charts", "templates")

	t.Run("creates engine with helpers", func(t *testing.T) {
		engine, err := NewTemplateEngine(templatesDir)
		require.NoError(t, err)
		require.NotNil(t, engine)
		assert.NotNil(t, engine.helpers)
	})

	t.Run("works without helpers file", func(t *testing.T) {
		// Use provisions dir which has no _helpers.tpl
		engine, err := NewTemplateEngine(filepath.Join("testdata", "provisions"))
		require.NoError(t, err)
		require.NotNil(t, engine)
		assert.Nil(t, engine.helpers)
	})

	t.Run("rejects unreadable helpers path", func(t *testing.T) {
		templatesDir := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(templatesDir, "_helpers.tpl"), 0700))

		_, err := NewTemplateEngine(templatesDir)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "read helpers")
	})

	t.Run("rejects malformed helpers", func(t *testing.T) {
		templatesDir := t.TempDir()
		require.NoError(t, os.WriteFile(
			filepath.Join(templatesDir, "_helpers.tpl"),
			[]byte(`{{ define "unfinished" }}`),
			0600,
		))

		_, err := NewTemplateEngine(templatesDir)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "parse helpers")
	})
}

func TestTemplateEngine_RenderTemplate(t *testing.T) {
	templatesDir := filepath.Join("testdata", "charts", "templates")
	engine, err := NewTemplateEngine(templatesDir)
	require.NoError(t, err)

	t.Run("renders simple template", func(t *testing.T) {
		ctx := &TemplateContext{
			Chart: ChartMeta{
				Name:    "myapp",
				Version: "1.0.0",
			},
			Values: map[string]any{
				"image": "nginx:latest",
			},
			Deps: make(map[string]DependencyInfo),
		}

		output, err := engine.RenderTemplate("container", ctx)
		require.NoError(t, err)
		require.NotNil(t, output)

		// Check compose output
		services, ok := output.Targets[TargetCompose]["services"].(map[string]any)
		require.True(t, ok)

		myapp, ok := services["myapp"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "nginx:latest", myapp["image"])
		assert.Equal(t, "myapp", myapp["container_name"])
	})

	t.Run("uses helpers from _helpers.tpl", func(t *testing.T) {
		ctx := &TemplateContext{
			Chart: ChartMeta{
				Name:    "helpertest",
				Version: "2.0.0",
			},
			Values: map[string]any{
				"image": "test:latest",
			},
			Deps: make(map[string]DependencyInfo),
		}

		output, err := engine.RenderTemplate("container", ctx)
		require.NoError(t, err)

		services, ok := output.Targets[TargetCompose]["services"].(map[string]any)
		require.True(t, ok)

		svc, ok := services["helpertest"].(map[string]any)
		require.True(t, ok)

		// Check that labels from helper were included
		labels, ok := svc["labels"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "helpertest", labels["app"])
		assert.Equal(t, "2.0.0", labels["version"])
	})

	t.Run("returns error for missing template", func(t *testing.T) {
		ctx := &TemplateContext{
			Chart:  ChartMeta{Name: "test"},
			Values: make(map[string]any),
			Deps:   make(map[string]DependencyInfo),
		}

		_, err := engine.RenderTemplate("nonexistent", ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestTemplateEngine_ConcurrentFirstLoad(t *testing.T) {
	templatesDir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(templatesDir, "service.yaml"),
		[]byte("compose:\n  services:\n    app:\n      image: nginx:latest\n"),
		0600,
	))
	engine, err := NewTemplateEngine(templatesDir)
	require.NoError(t, err)

	type loadResult struct {
		template any
		err      error
	}

	const workers = 64
	start := make(chan struct{})
	results := make(chan loadResult, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			<-start
			tmpl, loadErr := engine.loadTemplate("service")
			results <- loadResult{template: tmpl, err: loadErr}
		}()
	}

	close(start)
	wg.Wait()
	close(results)

	var first any
	for result := range results {
		require.NoError(t, result.err)
		require.NotNil(t, result.template)
		if first == nil {
			first = result.template
			continue
		}
		assert.Same(t, first, result.template)
	}
	assert.Len(t, engine.loadedTemplates, 1)
}

func TestTemplateEngine_FailedLoadDoesNotPoisonCache(t *testing.T) {
	templatesDir := t.TempDir()
	templatePath := filepath.Join(templatesDir, "service.yaml")
	require.NoError(t, os.WriteFile(templatePath, []byte("{{"), 0600))
	engine, err := NewTemplateEngine(templatesDir)
	require.NoError(t, err)

	_, err = engine.loadTemplate("service")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse template service")
	assert.Empty(t, engine.loadedTemplates)

	require.NoError(t, os.WriteFile(templatePath, []byte("compose: {}\n"), 0600))
	first, err := engine.loadTemplate("service")
	require.NoError(t, err)
	second, err := engine.loadTemplate("service")
	require.NoError(t, err)

	assert.Same(t, first, second)
	assert.Len(t, engine.loadedTemplates, 1)
}

func TestTemplateEngine_IncludeFile(t *testing.T) {
	templatesDir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(templatesDir, "fragment.yaml"),
		[]byte("name: {{ .Name }}\n"),
		0600,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(templatesDir, "required.yaml"),
		[]byte("name: {{ .Missing }}\n"),
		0600,
	))
	engine, err := NewTemplateEngine(templatesDir)
	require.NoError(t, err)

	output, err := engine.includeFunc("fragment", map[string]any{"Name": "app"})
	require.NoError(t, err)
	assert.Equal(t, "name: app\n", output)

	_, err = engine.includeFunc("missing", map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "include missing")

	_, err = engine.includeFunc("required", map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "execute template required")
}

func TestTemplateEngine_RenderTemplateRejectsInvalidYAML(t *testing.T) {
	templatesDir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(templatesDir, "invalid.yaml"),
		[]byte("compose: [unterminated\n"),
		0600,
	))
	engine, err := NewTemplateEngine(templatesDir)
	require.NoError(t, err)

	_, err = engine.RenderTemplate("invalid", &TemplateContext{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse rendered template invalid")
}

func TestTemplateEngine_RenderTemplateString(t *testing.T) {
	templatesDir := filepath.Join("testdata", "charts", "templates")
	engine, err := NewTemplateEngine(templatesDir)
	require.NoError(t, err)

	t.Run("renders inline template", func(t *testing.T) {
		ctx := &TemplateContext{
			Chart: ChartMeta{Name: "myapp"},
			Values: map[string]any{
				"port": "8080",
			},
			Deps: make(map[string]DependencyInfo),
		}

		result, err := engine.RenderTemplateString("port: {{ .Values.port }}", ctx)
		require.NoError(t, err)
		assert.Equal(t, "port: 8080", result)
	})

	t.Run("uses Sprig functions", func(t *testing.T) {
		ctx := &TemplateContext{
			Chart: ChartMeta{Name: "test"},
			Values: map[string]any{
				"name": "hello",
			},
			Deps: make(map[string]DependencyInfo),
		}

		result, err := engine.RenderTemplateString("{{ .Values.name | upper }}", ctx)
		require.NoError(t, err)
		assert.Equal(t, "HELLO", result)
	})

	t.Run("missing value fails with template and key context", func(t *testing.T) {
		ctx := &TemplateContext{
			Chart:  ChartMeta{Name: "test"},
			Values: map[string]any{"password": "secret"},
			Deps:   make(map[string]DependencyInfo),
		}

		_, err := engine.RenderTemplateString("password: {{ .Values.passwrd }}", ctx)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "inline")
		assert.Contains(t, err.Error(), `map has no entry for key "passwrd"`)
	})

	t.Run("returns error for invalid template", func(t *testing.T) {
		ctx := &TemplateContext{
			Chart:  ChartMeta{Name: "test"},
			Values: make(map[string]any),
			Deps:   make(map[string]DependencyInfo),
		}

		_, err := engine.RenderTemplateString("{{ .Invalid }", ctx)
		assert.Error(t, err)
	})
}

func TestTemplateEngine_MissingKeyWithoutHelpers(t *testing.T) {
	templatesDir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(templatesDir, "database.yaml"),
		[]byte("compose:\n  password: {{ .Values.passwrd }}\n"),
		0644,
	))
	engine, err := NewTemplateEngine(templatesDir)
	require.NoError(t, err)
	ctx := &TemplateContext{
		Chart:  ChartMeta{Name: "test"},
		Values: map[string]any{"password": "secret"},
		Deps:   make(map[string]DependencyInfo),
	}

	t.Run("named template", func(t *testing.T) {
		_, err := engine.RenderTemplate("database", ctx)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "database")
		assert.Contains(t, err.Error(), `map has no entry for key "passwrd"`)
	})

	t.Run("inline template", func(t *testing.T) {
		_, err := engine.RenderTemplateString("password: {{ .Values.passwrd }}", ctx)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "inline")
		assert.Contains(t, err.Error(), `map has no entry for key "passwrd"`)
	})
}

func TestTemplateEngine_MissingKeyInClonedHelper(t *testing.T) {
	templatesDir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(templatesDir, "_helpers.tpl"),
		[]byte(`{{ define "database.password" }}{{ .Values.passwrd }}{{ end }}`),
		0644,
	))
	engine, err := NewTemplateEngine(templatesDir)
	require.NoError(t, err)
	ctx := &TemplateContext{
		Chart:  ChartMeta{Name: "test"},
		Values: map[string]any{"password": "secret"},
		Deps:   make(map[string]DependencyInfo),
	}

	_, err = engine.RenderTemplateString(`{{ include "database.password" . }}`, ctx)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "database.password")
	assert.Contains(t, err.Error(), `map has no entry for key "passwrd"`)
}

func TestTemplateEngine_ListTemplates(t *testing.T) {
	templatesDir := filepath.Join("testdata", "charts", "templates")
	engine, err := NewTemplateEngine(templatesDir)
	require.NoError(t, err)

	templates, err := engine.ListTemplates()
	require.NoError(t, err)

	// Should include container and postgres, but not _helpers.tpl
	assert.Contains(t, templates, "container")
	assert.Contains(t, templates, "postgres")
	assert.NotContains(t, templates, "_helpers")
}

func TestTemplateEngine_ListTemplatesErrors(t *testing.T) {
	t.Run("missing directory", func(t *testing.T) {
		engine, err := NewTemplateEngine(filepath.Join(t.TempDir(), "missing"))
		require.NoError(t, err)

		_, err = engine.ListTemplates()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "templates directory not found")
	})

	t.Run("path is not a directory", func(t *testing.T) {
		templatesPath := filepath.Join(t.TempDir(), "templates")
		require.NoError(t, os.WriteFile(templatesPath, []byte("not a directory"), 0600))
		engine, err := NewTemplateEngine(templatesPath)
		require.NoError(t, err)

		_, err = engine.ListTemplates()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "read templates directory")
	})
}

func TestToYamlFunc(t *testing.T) {
	t.Run("converts map to YAML", func(t *testing.T) {
		input := map[string]any{
			"key": "value",
		}
		result, err := toYamlFunc(input)
		require.NoError(t, err)
		assert.Contains(t, result, "key: value")
	})

	t.Run("converts slice to YAML", func(t *testing.T) {
		input := []string{"a", "b", "c"}
		result, err := toYamlFunc(input)
		require.NoError(t, err)
		assert.Contains(t, result, "- a")
		assert.Contains(t, result, "- b")
		assert.Contains(t, result, "- c")
	})
}

func TestNindentFunc(t *testing.T) {
	t.Run("indents single line", func(t *testing.T) {
		result := nindentFunc(4, "hello")
		assert.Equal(t, "\n    hello", result)
	})

	t.Run("indents multiple lines", func(t *testing.T) {
		result := nindentFunc(2, "line1\nline2")
		assert.Equal(t, "\n  line1\n  line2", result)
	})

	t.Run("preserves empty lines", func(t *testing.T) {
		result := nindentFunc(2, "line1\n\nline2")
		assert.Equal(t, "\n  line1\n\n  line2", result)
	})
}

func TestNewTemplateContext(t *testing.T) {
	chart := &Chart{
		Name:        "myapp",
		Version:     "1.0.0",
		Description: "Test app",
		Dependencies: []ChartDependency{
			{Name: "postgres", Version: "17"},
			{Name: "redis", Version: "7"},
		},
	}

	values := map[string]any{
		"port": 8080,
	}

	ctx := NewTemplateContext(chart, values)

	t.Run("populates Chart metadata", func(t *testing.T) {
		assert.Equal(t, "myapp", ctx.Chart.Name)
		assert.Equal(t, "1.0.0", ctx.Chart.Version)
		assert.Equal(t, "Test app", ctx.Chart.Description)
	})

	t.Run("includes values", func(t *testing.T) {
		assert.Equal(t, 8080, ctx.Values["port"])
	})

	t.Run("populates dependency info", func(t *testing.T) {
		pg, ok := ctx.Deps["postgres"]
		require.True(t, ok)
		assert.Equal(t, "myapp-db", pg.Name)
		assert.Equal(t, "myapp-db", pg.Host)
		assert.Equal(t, 5432, pg.Port)
		assert.Equal(t, "postgres", pg.Type)

		redis, ok := ctx.Deps["redis"]
		require.True(t, ok)
		assert.Equal(t, "myapp-redis", redis.Name)
		assert.Equal(t, 6379, redis.Port)
	})
}

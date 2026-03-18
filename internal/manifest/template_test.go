package manifest

import (
	"path/filepath"
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

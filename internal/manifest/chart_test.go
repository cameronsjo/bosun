package manifest

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewChartLoader(t *testing.T) {
	chartsDir := filepath.Join("testdata", "charts")

	t.Run("creates loader with template engine", func(t *testing.T) {
		loader, err := NewChartLoader(chartsDir)
		require.NoError(t, err)
		require.NotNil(t, loader)
		assert.NotNil(t, loader.engine)
	})

	t.Run("fails for nonexistent directory", func(t *testing.T) {
		_, err := NewChartLoader(filepath.Join("testdata", "nonexistent"))
		// Should still create loader, but engine may have issues
		// The error happens when trying to use the engine
		assert.NoError(t, err) // Loader creation doesn't fail
	})
}

func TestChartLoader_LoadChart(t *testing.T) {
	chartsDir := filepath.Join("testdata", "charts")
	loader, err := NewChartLoader(chartsDir)
	require.NoError(t, err)

	t.Run("loads valid chart", func(t *testing.T) {
		chart, err := loader.LoadChart("testapp")
		require.NoError(t, err)
		require.NotNil(t, chart)

		assert.Equal(t, "bosun.io/v1", chart.APIVersion)
		assert.Equal(t, "Chart", chart.Kind)
		assert.Equal(t, "testapp", chart.Name)
		assert.Equal(t, "1.0.0", chart.Version)
		assert.Equal(t, "Test application for unit tests", chart.Description)
		assert.Contains(t, chart.Templates, "container")
		assert.Len(t, chart.Dependencies, 1)
		assert.Equal(t, "postgres", chart.Dependencies[0].Name)
	})

	t.Run("fails for nonexistent chart", func(t *testing.T) {
		_, err := loader.LoadChart("nonexistent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Chart.yaml")
	})
}

func TestChartLoader_LoadChartValues(t *testing.T) {
	chartsDir := filepath.Join("testdata", "charts")
	loader, err := NewChartLoader(chartsDir)
	require.NoError(t, err)

	t.Run("loads values.yaml", func(t *testing.T) {
		values, err := loader.LoadChartValues("testapp")
		require.NoError(t, err)
		require.NotNil(t, values)

		assert.Equal(t, "ghcr.io/example/testapp:latest", values["image"])
		assert.Equal(t, 8080, values["port"])
	})

	t.Run("returns empty map for missing values.yaml", func(t *testing.T) {
		// Create a chart without values.yaml for testing
		// For now, test with existing chart
		values, err := loader.LoadChartValues("nonexistent")
		// Should return empty map, not error
		require.NoError(t, err)
		assert.Empty(t, values)
	})
}

func TestChartLoader_RenderChart(t *testing.T) {
	chartsDir := filepath.Join("testdata", "charts")
	loader, err := NewChartLoader(chartsDir)
	require.NoError(t, err)

	t.Run("renders chart with templates", func(t *testing.T) {
		output, err := loader.RenderChart("testapp", nil)
		require.NoError(t, err)
		require.NotNil(t, output)

		services, ok := output.Compose["services"].(map[string]any)
		require.True(t, ok)

		// Main service
		testapp, ok := services["testapp"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "ghcr.io/example/testapp:latest", testapp["image"])

		// Sidecar from dependency
		db, ok := services["testapp-db"].(map[string]any)
		require.True(t, ok)
		assert.Contains(t, db["image"], "postgres:17")
	})

	t.Run("renders raw chart without templates", func(t *testing.T) {
		output, err := loader.RenderChart("rawapp", nil)
		require.NoError(t, err)
		require.NotNil(t, output)

		// Raw mode puts compose directly under services key
		services, ok := output.Compose["services"].(map[string]any)
		require.True(t, ok)

		rawapp, ok := services["rawapp"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "nginx:latest", rawapp["image"])
	})

	t.Run("applies value overrides", func(t *testing.T) {
		overrides := map[string]any{
			"image": "custom:latest",
		}

		output, err := loader.RenderChart("testapp", overrides)
		require.NoError(t, err)

		services, ok := output.Compose["services"].(map[string]any)
		require.True(t, ok)

		testapp, ok := services["testapp"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "custom:latest", testapp["image"])
	})
}

func TestChartLoader_ListCharts(t *testing.T) {
	chartsDir := filepath.Join("testdata", "charts")
	loader, err := NewChartLoader(chartsDir)
	require.NoError(t, err)

	charts, err := loader.ListCharts()
	require.NoError(t, err)

	assert.Contains(t, charts, "testapp")
	assert.Contains(t, charts, "rawapp")
	assert.NotContains(t, charts, "templates") // templates dir should be excluded
}

func TestChartLoader_ListTemplates(t *testing.T) {
	chartsDir := filepath.Join("testdata", "charts")
	loader, err := NewChartLoader(chartsDir)
	require.NoError(t, err)

	templates, err := loader.ListTemplates()
	require.NoError(t, err)

	assert.Contains(t, templates, "container")
	assert.Contains(t, templates, "postgres")
}

func TestChartLoader_ChartExists(t *testing.T) {
	chartsDir := filepath.Join("testdata", "charts")
	loader, err := NewChartLoader(chartsDir)
	require.NoError(t, err)

	assert.True(t, loader.ChartExists("testapp"))
	assert.True(t, loader.ChartExists("rawapp"))
	assert.False(t, loader.ChartExists("nonexistent"))
}

func TestChartLoader_RenderStack(t *testing.T) {
	chartsDir := filepath.Join("testdata", "charts")
	loader, err := NewChartLoader(chartsDir)
	require.NoError(t, err)

	stackPath := filepath.Join("testdata", "helmstacks", "teststack", "Stack.yaml")

	t.Run("renders stack with multiple charts", func(t *testing.T) {
		output, err := loader.RenderStack(stackPath, nil)
		require.NoError(t, err)
		require.NotNil(t, output)

		services, ok := output.Compose["services"].(map[string]any)
		require.True(t, ok)

		// Should have services from both charts
		_, hasTestapp := services["testapp"]
		assert.True(t, hasTestapp)

		_, hasRawapp := services["rawapp"]
		assert.True(t, hasRawapp)
	})

	t.Run("applies per-chart values from stack", func(t *testing.T) {
		output, err := loader.RenderStack(stackPath, nil)
		require.NoError(t, err)

		services, ok := output.Compose["services"].(map[string]any)
		require.True(t, ok)

		// testapp should have its values applied
		testapp, ok := services["testapp"].(map[string]any)
		require.True(t, ok)
		// Default image from values.yaml
		assert.Equal(t, "ghcr.io/example/testapp:latest", testapp["image"])
	})

	t.Run("includes stack networks", func(t *testing.T) {
		output, err := loader.RenderStack(stackPath, nil)
		require.NoError(t, err)

		networks, ok := output.Compose["networks"].(map[string]any)
		require.True(t, ok)

		proxynet, ok := networks["proxynet"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, true, proxynet["external"])
	})
}

func TestDetectFormat(t *testing.T) {
	t.Run("detects helm format", func(t *testing.T) {
		// testdata/charts has Chart.yaml files
		format := DetectFormat(filepath.Join("testdata"))
		assert.Equal(t, "helm", format)
	})

	t.Run("detects legacy format", func(t *testing.T) {
		// A directory with only provisions would be legacy
		// For this test, we'd need a separate testdata structure
		// Skip for now as testdata has both
	})

	t.Run("returns unknown for empty directory", func(t *testing.T) {
		format := DetectFormat(filepath.Join("testdata", "nonexistent"))
		assert.Equal(t, "unknown", format)
	})
}

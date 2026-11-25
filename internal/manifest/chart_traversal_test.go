package manifest

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// chartTraversalFixture lays out a charts directory with a valid chart one level
// above it (base/outside). A name that climbs out by ".." reaches that chart, so
// an unguarded loader returns it without error.
func chartTraversalFixture(t *testing.T) (loader *ChartLoader, stackPath string) {
	t.Helper()
	base := t.TempDir()
	chartsDir := filepath.Join(base, "charts")
	const chartYAML = "apiVersion: bosun.io/v1\nkind: Chart\nname: %s\nversion: 1.0.0\n\ncompose:\n  %s:\n    image: nginx:latest\n"

	for _, c := range []struct{ dir, name string }{
		{filepath.Join(chartsDir, "good"), "good"},
		{filepath.Join(chartsDir, "sub", "nested"), "nested"},
		{filepath.Join(base, "outside"), "outside"},
	} {
		writeTemplateFile(t, c.dir, "Chart.yaml", fmt.Sprintf(chartYAML, c.name, c.name))
		writeTemplateFile(t, c.dir, "values.yaml", "image: nginx:latest\n")
	}
	writeTemplateFile(t, chartsDir, "templates/.keep", "")

	stackPath = filepath.Join(base, "stacks", "escape", "Stack.yaml")
	writeTemplateFile(t, filepath.Dir(stackPath), "Stack.yaml",
		"apiVersion: bosun.io/v1\nkind: Stack\nname: escape\n\ncharts:\n  - name: ../outside\n")

	loader, err := NewChartLoader(chartsDir)
	require.NoError(t, err)
	return loader, stackPath
}

// TestChartLoader_PathTraversal drives each chart-name caller into the
// containment check: the three loader methods that join the name onto the
// charts directory, and the two entry points above them (RenderChart, which
// the provision command uses, and a stack file's charts list).
func TestChartLoader_PathTraversal(t *testing.T) {
	loader, stackPath := chartTraversalFixture(t)

	refused := map[string]func(name string) error{
		"LoadChart": func(name string) error {
			_, err := loader.LoadChart(name)
			return err
		},
		"LoadChartValues": func(name string) error {
			_, err := loader.LoadChartValues(name)
			return err
		},
		"GetChartInfo": func(name string) error {
			_, err := loader.GetChartInfo(name)
			return err
		},
		"RenderChart": func(name string) error {
			_, err := loader.RenderChart(name, nil)
			return err
		},
	}

	for entry, call := range refused {
		for _, name := range []string{"../outside", "sub/../../outside"} {
			t.Run(entry+"/refuses "+name, func(t *testing.T) {
				err := call(name)

				require.Error(t, err)
				assert.ErrorIs(t, err, ErrPathTraversal)
			})
		}
	}

	t.Run("ChartExists is false for a name outside the directory", func(t *testing.T) {
		assert.False(t, loader.ChartExists("../outside"))
		assert.False(t, loader.ChartExists("sub/../../outside"))
	})

	t.Run("RenderStack refuses a charts entry outside the directory", func(t *testing.T) {
		_, err := loader.RenderStack(stackPath, nil)

		require.Error(t, err)
		assert.ErrorIs(t, err, ErrPathTraversal)
	})

	t.Run("names inside the directory still load", func(t *testing.T) {
		for _, name := range []string{"good", "sub/nested", "sub/../good", "./good"} {
			chart, err := loader.LoadChart(name)
			require.NoError(t, err, name)
			assert.NotEmpty(t, chart.Name, name)

			values, err := loader.LoadChartValues(name)
			require.NoError(t, err, name)
			assert.Equal(t, "nginx:latest", values["image"], name)

			assert.True(t, loader.ChartExists(name), name)
		}
		assert.False(t, loader.ChartExists("missing"))
	})
}

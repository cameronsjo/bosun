package manifest

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ChartLoader handles loading charts from the filesystem.
type ChartLoader struct {
	// chartsDir is the root charts directory.
	chartsDir string

	// templatesDir is the path to shared templates.
	templatesDir string

	// engine is the template engine.
	engine *TemplateEngine
}

// NewChartLoader creates a new chart loader.
func NewChartLoader(chartsDir string) (*ChartLoader, error) {
	templatesDir := filepath.Join(chartsDir, "templates")

	engine, err := NewTemplateEngine(templatesDir)
	if err != nil {
		return nil, fmt.Errorf("create template engine: %w", err)
	}

	return &ChartLoader{
		chartsDir:    chartsDir,
		templatesDir: templatesDir,
		engine:       engine,
	}, nil
}

// LoadChart loads a chart from its directory.
func (l *ChartLoader) LoadChart(name string) (*Chart, error) {
	chartDir := filepath.Join(l.chartsDir, name)

	// Load Chart.yaml
	chartPath := filepath.Join(chartDir, "Chart.yaml")
	content, err := os.ReadFile(chartPath)
	if err != nil {
		return nil, fmt.Errorf("read Chart.yaml: %w", err)
	}

	// Validate manifest
	meta, err := ValidateManifest(content)
	if err != nil {
		return nil, fmt.Errorf("validate Chart.yaml: %w", err)
	}

	if meta.APIVersion == "" {
		log.Printf("Warning: chart %s is missing apiVersion field", name)
	} else if meta.Kind != "" && meta.Kind != KindChart {
		log.Printf("Warning: chart %s has kind %s, expected %s", name, meta.Kind, KindChart)
	}

	var chart Chart
	if err := yaml.Unmarshal(content, &chart); err != nil {
		return nil, fmt.Errorf("parse Chart.yaml: %w", err)
	}

	return &chart, nil
}

// LoadChartValues loads the values.yaml for a chart.
func (l *ChartLoader) LoadChartValues(name string) (map[string]any, error) {
	chartDir := filepath.Join(l.chartsDir, name)
	valuesPath := filepath.Join(chartDir, "values.yaml")

	content, err := os.ReadFile(valuesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]any), nil
		}
		return nil, fmt.Errorf("read values.yaml: %w", err)
	}

	var values map[string]any
	if err := yaml.Unmarshal(content, &values); err != nil {
		return nil, fmt.Errorf("parse values.yaml: %w", err)
	}

	return values, nil
}

// RenderChart renders a chart with optional value overrides.
func (l *ChartLoader) RenderChart(name string, valueOverrides map[string]any) (*RenderOutput, error) {
	chart, err := l.LoadChart(name)
	if err != nil {
		return nil, err
	}

	values, err := l.LoadChartValues(name)
	if err != nil {
		return nil, err
	}

	// Apply overrides
	if valueOverrides != nil {
		values = DeepMerge(values, valueOverrides)
	}

	return l.engine.RenderChart(chart, values)
}

// ListCharts returns the names of all available charts.
func (l *ChartLoader) ListCharts() ([]string, error) {
	entries, err := os.ReadDir(l.chartsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("charts directory not found: %s", l.chartsDir)
		}
		return nil, fmt.Errorf("read charts directory: %w", err)
	}

	var charts []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// Skip templates directory
		if entry.Name() == "templates" {
			continue
		}
		// Check if Chart.yaml exists
		chartPath := filepath.Join(l.chartsDir, entry.Name(), "Chart.yaml")
		if _, err := os.Stat(chartPath); err == nil {
			charts = append(charts, entry.Name())
		}
	}

	return charts, nil
}

// ListTemplates returns the names of all available templates.
func (l *ChartLoader) ListTemplates() ([]string, error) {
	return l.engine.ListTemplates()
}

// RenderStack renders a stack in Helm-aligned format.
func (l *ChartLoader) RenderStack(stackPath string, stackValuesOverride map[string]any) (*RenderOutput, error) {
	content, err := os.ReadFile(stackPath)
	if err != nil {
		return nil, fmt.Errorf("read stack file: %w", err)
	}

	// Validate manifest
	meta, err := ValidateManifest(content)
	if err != nil {
		return nil, fmt.Errorf("validate stack: %w", err)
	}

	if meta.APIVersion == "" {
		log.Printf("Warning: stack %s is missing apiVersion field", stackPath)
	}

	var stack Stack
	if err := yaml.Unmarshal(content, &stack); err != nil {
		return nil, fmt.Errorf("parse stack file: %w", err)
	}

	output := NewRenderOutput()

	// Load stack-level values if they exist
	stackDir := filepath.Dir(stackPath)
	stackValuesPath := filepath.Join(stackDir, "values.yaml")
	var stackValues map[string]any
	if content, err := os.ReadFile(stackValuesPath); err == nil {
		if err := yaml.Unmarshal(content, &stackValues); err != nil {
			return nil, fmt.Errorf("parse stack values.yaml: %w", err)
		}
	}

	// Apply stack values override
	if stackValuesOverride != nil {
		stackValues = DeepMerge(stackValues, stackValuesOverride)
	}

	// Render charts (Helm-aligned format)
	for _, chartRef := range stack.Charts {
		// Merge: stack values < chart-specific values from stack
		chartValues := DeepMerge(stackValues, chartRef.Values)

		chartOutput, err := l.RenderChart(chartRef.Name, chartValues)
		if err != nil {
			return nil, fmt.Errorf("render chart %s: %w", chartRef.Name, err)
		}

		output.Compose = DeepMerge(output.Compose, chartOutput.Compose)
		output.Traefik = DeepMerge(output.Traefik, chartOutput.Traefik)
		output.Gatus = DeepMerge(output.Gatus, chartOutput.Gatus)
	}

	// Merge network definitions from stack
	if stack.Networks != nil {
		if existing, ok := output.Compose["networks"].(map[string]any); ok {
			output.Compose["networks"] = DeepMerge(existing, stack.Networks)
		} else {
			output.Compose["networks"] = stack.Networks
		}
	}

	return output, nil
}

// ChartExists checks if a chart directory exists.
func (l *ChartLoader) ChartExists(name string) bool {
	chartPath := filepath.Join(l.chartsDir, name, "Chart.yaml")
	_, err := os.Stat(chartPath)
	return err == nil
}

// GetChartInfo returns metadata about a chart without fully loading it.
func (l *ChartLoader) GetChartInfo(name string) (*Chart, error) {
	return l.LoadChart(name)
}

// DetectFormat detects whether a directory uses legacy or Helm-aligned format.
// Returns "helm" if charts/ directory exists with Chart.yaml files.
// Returns "legacy" if manifest/provisions/ exists.
// Returns "unknown" otherwise.
func DetectFormat(rootDir string) string {
	// Check for Helm-aligned format
	chartsDir := filepath.Join(rootDir, "charts")
	if entries, err := os.ReadDir(chartsDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() && entry.Name() != "templates" {
				chartYaml := filepath.Join(chartsDir, entry.Name(), "Chart.yaml")
				if _, err := os.Stat(chartYaml); err == nil {
					return "helm"
				}
			}
		}
	}

	// Check for legacy format
	provisionsDir := filepath.Join(rootDir, "manifest", "provisions")
	if _, err := os.Stat(provisionsDir); err == nil {
		return "legacy"
	}

	// Also check for provisions at root level
	provisionsDir = filepath.Join(rootDir, "provisions")
	if _, err := os.Stat(provisionsDir); err == nil {
		return "legacy"
	}

	return "unknown"
}

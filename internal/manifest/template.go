package manifest

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	"gopkg.in/yaml.v3"
)

// TemplateEngine handles Go template rendering for Helm-aligned charts.
type TemplateEngine struct {
	// templatesDir is the path to the templates directory.
	templatesDir string

	// helpers contains parsed helper templates from _helpers.tpl.
	helpers *template.Template

	// loadedTemplates caches parsed template files.
	loadedTemplates map[string]*template.Template
}

// NewTemplateEngine creates a new template engine for the given templates directory.
func NewTemplateEngine(templatesDir string) (*TemplateEngine, error) {
	engine := &TemplateEngine{
		templatesDir:    templatesDir,
		loadedTemplates: make(map[string]*template.Template),
	}

	// Load helpers if they exist
	helpersPath := filepath.Join(templatesDir, "_helpers.tpl")
	if _, err := os.Stat(helpersPath); err == nil {
		content, err := os.ReadFile(helpersPath)
		if err != nil {
			return nil, fmt.Errorf("read helpers: %w", err)
		}

		helpers, err := template.New("helpers").
			Option("missingkey=error").
			Funcs(engine.funcMap()).
			Parse(string(content))
		if err != nil {
			return nil, fmt.Errorf("parse helpers: %w", err)
		}
		engine.helpers = helpers
	}

	return engine, nil
}

// funcMap returns the template function map including Sprig functions.
func (e *TemplateEngine) funcMap() template.FuncMap {
	funcs := sprig.TxtFuncMap()

	// Add custom bosun functions
	funcs["include"] = e.includeFunc
	funcs["nindent"] = nindentFunc
	funcs["toYaml"] = toYamlFunc

	return funcs
}

// includeFunc implements the {{ include "name" . }} function.
func (e *TemplateEngine) includeFunc(name string, data any) (string, error) {
	// First check helpers
	if e.helpers != nil {
		if t := e.helpers.Lookup(name); t != nil {
			var buf bytes.Buffer
			if err := t.Execute(&buf, data); err != nil {
				return "", fmt.Errorf("execute helper %s: %w", name, err)
			}
			return buf.String(), nil
		}
	}

	// Check if it's a template file
	tmpl, err := e.loadTemplate(name)
	if err != nil {
		return "", fmt.Errorf("include %s: %w", name, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template %s: %w", name, err)
	}

	return buf.String(), nil
}

// nindentFunc adds a newline and then indents every line.
func nindentFunc(spaces int, text string) string {
	indent := strings.Repeat(" ", spaces)
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = indent + line
		}
	}
	return "\n" + strings.Join(lines, "\n")
}

// toYamlFunc converts a value to YAML.
func toYamlFunc(v any) (string, error) {
	data, err := yaml.Marshal(v)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(string(data), "\n"), nil
}

// loadTemplate loads and parses a template file.
func (e *TemplateEngine) loadTemplate(name string) (*template.Template, error) {
	// Check cache
	if tmpl, ok := e.loadedTemplates[name]; ok {
		return tmpl, nil
	}

	// Try .yaml and .yml extensions
	var templatePath string
	var content []byte
	var err error

	for _, ext := range []string{".yaml", ".yml"} {
		templatePath = filepath.Join(e.templatesDir, name+ext)
		content, err = os.ReadFile(templatePath)
		if err == nil {
			break
		}
	}

	if err != nil {
		return nil, fmt.Errorf("template not found: %s", name)
	}

	// Create template with helpers as base (if available)
	var tmpl *template.Template
	if e.helpers != nil {
		tmpl, err = e.helpers.Clone()
		if err != nil {
			return nil, fmt.Errorf("clone helpers: %w", err)
		}
		tmpl, err = tmpl.New(name).Parse(string(content))
	} else {
		tmpl, err = template.New(name).
			Option("missingkey=error").
			Funcs(e.funcMap()).
			Parse(string(content))
	}

	if err != nil {
		return nil, fmt.Errorf("parse template %s: %w", name, err)
	}

	e.loadedTemplates[name] = tmpl
	return tmpl, nil
}

// RenderTemplate renders a template with the given context.
func (e *TemplateEngine) RenderTemplate(name string, ctx *TemplateContext) (*RenderOutput, error) {
	tmpl, err := e.loadTemplate(name)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctx); err != nil {
		return nil, fmt.Errorf("execute template %s: %w", name, err)
	}

	// Parse the rendered YAML
	var rawOutput map[string]any
	if err := yaml.Unmarshal(buf.Bytes(), &rawOutput); err != nil {
		return nil, fmt.Errorf("parse rendered template %s: %w", name, err)
	}

	// Remove metadata fields
	delete(rawOutput, "apiVersion")
	delete(rawOutput, "kind")
	delete(rawOutput, "includes")

	// Extract target outputs dynamically from rendered YAML keys
	output := NewRenderOutput()
	for key, val := range rawOutput {
		if m, ok := val.(map[string]any); ok {
			output.Targets[key] = m
		}
	}

	return output, nil
}

// RenderTemplateString renders a raw template string with the given context.
func (e *TemplateEngine) RenderTemplateString(templateStr string, ctx *TemplateContext) (string, error) {
	var tmpl *template.Template
	var err error

	if e.helpers != nil {
		tmpl, err = e.helpers.Clone()
		if err != nil {
			return "", fmt.Errorf("clone helpers: %w", err)
		}
		tmpl, err = tmpl.New("inline").Parse(templateStr)
	} else {
		tmpl, err = template.New("inline").
			Option("missingkey=error").
			Funcs(e.funcMap()).
			Parse(templateStr)
	}

	if err != nil {
		return "", fmt.Errorf("parse inline template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctx); err != nil {
		return "", fmt.Errorf("execute inline template: %w", err)
	}

	return buf.String(), nil
}

// RenderChart renders a chart with its templates and dependencies.
func (e *TemplateEngine) RenderChart(chart *Chart, values map[string]any) (*RenderOutput, error) {
	// Build template context
	ctx := NewTemplateContext(chart, values)
	output := NewRenderOutput()

	// Handle implicit raw mode: no templates but has compose block
	if len(chart.Templates) == 0 && chart.Compose != nil {
		// Render compose block as template (supports {{ .Values.* }} in raw mode too)
		composeYAML, err := yaml.Marshal(chart.Compose)
		if err != nil {
			return nil, fmt.Errorf("marshal compose: %w", err)
		}

		rendered, err := e.RenderTemplateString(string(composeYAML), ctx)
		if err != nil {
			return nil, fmt.Errorf("render compose: %w", err)
		}

		var compose map[string]any
		if err := yaml.Unmarshal([]byte(rendered), &compose); err != nil {
			return nil, fmt.Errorf("parse rendered compose: %w", err)
		}

		output.Target(TargetCompose)["services"] = compose
		return output, nil
	}

	// Render each template
	for _, tmplName := range chart.Templates {
		tmplOutput, err := e.RenderTemplate(tmplName, ctx)
		if err != nil {
			return nil, fmt.Errorf("render template %s: %w", tmplName, err)
		}
		mergeOutput(output, tmplOutput)
	}

	// Render dependencies
	for _, dep := range chart.Dependencies {
		depOutput, err := e.renderDependency(chart, &dep, ctx)
		if err != nil {
			return nil, fmt.Errorf("render dependency %s: %w", dep.Name, err)
		}
		mergeOutput(output, depOutput)
	}

	// Apply chart-level compose overrides
	if chart.Compose != nil {
		composeYAML, err := yaml.Marshal(chart.Compose)
		if err != nil {
			return nil, fmt.Errorf("marshal compose override: %w", err)
		}

		rendered, err := e.RenderTemplateString(string(composeYAML), ctx)
		if err != nil {
			return nil, fmt.Errorf("render compose override: %w", err)
		}

		var composeOverride map[string]any
		if err := yaml.Unmarshal([]byte(rendered), &composeOverride); err != nil {
			return nil, fmt.Errorf("parse compose override: %w", err)
		}

		output.Targets[TargetCompose] = DeepMerge(output.Target(TargetCompose), composeOverride)
	}

	return output, nil
}

// renderDependency renders a chart dependency (sidecar).
func (e *TemplateEngine) renderDependency(chart *Chart, dep *ChartDependency, ctx *TemplateContext) (*RenderOutput, error) {
	// Build dependency-specific context
	depValues := make(map[string]any)

	// Start with defaults
	if defaults, ok := DependencyDefaults[dep.Name]; ok {
		depValues["version"] = defaults.Version
		for k, v := range defaults.Values {
			depValues[k] = v
		}
	}

	// Apply dependency version
	if dep.Version != "" {
		depValues["version"] = dep.Version
	}

	// Apply dependency values
	for k, v := range dep.Values {
		depValues[k] = v
	}

	// Add chart context
	depValues["name"] = chart.Name
	depValues["sidecar"] = dep.Name

	// Create dependency context
	depCtx := &TemplateContext{
		Chart:  ctx.Chart,
		Values: depValues,
		Deps:   ctx.Deps,
	}

	// Check if dependency template exists
	tmplExists := false
	for _, ext := range []string{".yaml", ".yml"} {
		tmplPath := filepath.Join(e.templatesDir, dep.Name+ext)
		if _, err := os.Stat(tmplPath); err == nil {
			tmplExists = true
			break
		}
	}

	if !tmplExists {
		return NewRenderOutput(), nil
	}

	// Render dependency template
	output, err := e.RenderTemplate(dep.Name, depCtx)
	if err != nil {
		return nil, err
	}

	// Apply dependency compose overrides
	if dep.Compose != nil {
		// Get the sidecar service name
		depInfo := ctx.Deps[dep.Name]
		sidecarName := depInfo.Name

		// Wrap the override in the service name
		override := map[string]any{
			"services": map[string]any{
				sidecarName: dep.Compose,
			},
		}

		output.Targets[TargetCompose] = DeepMerge(output.Target(TargetCompose), override)
	}

	return output, nil
}

// mergeOutput merges source into dest.
func mergeOutput(dest, source *RenderOutput) {
	for name, content := range source.Targets {
		if content != nil {
			dest.Targets[name] = DeepMerge(dest.Target(name), content)
		}
	}
}

// ListTemplates returns the names of all available templates.
func (e *TemplateEngine) ListTemplates() ([]string, error) {
	entries, err := os.ReadDir(e.templatesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("templates directory not found: %s", e.templatesDir)
		}
		return nil, fmt.Errorf("read templates directory: %w", err)
	}

	var templates []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Skip helpers file
		if name == "_helpers.tpl" {
			continue
		}
		if filepath.Ext(name) == ".yml" || filepath.Ext(name) == ".yaml" {
			templates = append(templates, name[:len(name)-len(filepath.Ext(name))])
		}
	}

	return templates, nil
}

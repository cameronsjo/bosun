// Package manifest implements the Crew Manifest engine for generating
// compose, traefik, and gatus configs from service manifests.
//
// This package supports two formats:
//   - Legacy format: Provisions, ServiceManifest, ${var} interpolation
//   - Helm-aligned format: Templates, Chart, {{ .Values.var }} Go templates
//
// See ADR-0011 for the Helm alignment decision.
package manifest

import "sort"

// API version and kind constants for manifest versioning.
const (
	// APIVersionV1 is the current API version for bosun manifests.
	APIVersionV1 = "bosun.io/v1"

	// KindProvision identifies a Provision manifest (legacy).
	KindProvision = "Provision"

	// KindTemplate identifies a Template manifest (Helm-aligned).
	KindTemplate = "Template"

	// KindStack identifies a Stack manifest.
	KindStack = "Stack"

	// KindService identifies a Service manifest (legacy).
	KindService = "Service"

	// KindChart identifies a Chart manifest (Helm-aligned).
	KindChart = "Chart"
)

// Target name constants prevent string typos in map key access.
const (
	TargetCompose = "compose"
	TargetTraefik = "traefik"
	TargetGatus   = "gatus"
)

// TargetConfig defines output metadata for a provisioning target.
type TargetConfig struct {
	// Dir is the subdirectory name under the output directory (e.g., "compose").
	Dir string

	// Filename returns the output filename for this target.
	// Accepts the stack name for targets with stack-dependent filenames.
	Filename func(stackName string) string
}

// TargetRegistry maps target names to their output configuration.
// Used by WriteOutputs and showDiff to resolve filenames and directories.
var TargetRegistry = map[string]TargetConfig{
	TargetCompose: {
		Dir:      "compose",
		Filename: func(stackName string) string { return stackName + ".yml.tmpl" },
	},
	TargetTraefik: {
		Dir:      "traefik",
		Filename: func(_ string) string { return "dynamic.yml" },
	},
	TargetGatus: {
		Dir:      "gatus",
		Filename: func(_ string) string { return "endpoints.yml" },
	},
}

// SupportedAPIVersions lists all API versions that can be loaded.
var SupportedAPIVersions = []string{APIVersionV1}

// SupportedKinds lists all valid manifest kinds.
var SupportedKinds = []string{KindProvision, KindTemplate, KindStack, KindService, KindChart}

// ServiceManifest defines a service to be provisioned.
type ServiceManifest struct {
	// APIVersion identifies the schema version (e.g., "bosun.io/v1").
	APIVersion string `yaml:"apiVersion,omitempty"`

	// Kind identifies the manifest type (e.g., "Service").
	Kind string `yaml:"kind,omitempty"`

	// Name is the service name used for interpolation and output.
	Name string `yaml:"name"`

	// Type is "raw" for passthrough mode, empty for normal provisioning.
	Type string `yaml:"type,omitempty"`

	// Provisions is the list of provision templates to apply.
	Provisions []string `yaml:"provisions,omitempty"`

	// Config holds variables for interpolation into provisions.
	Config map[string]any `yaml:"config,omitempty"`

	// Services defines sidecar services (postgres, redis, etc.) with explicit config.
	Services map[string]map[string]any `yaml:"services,omitempty"`

	// Needs is shorthand for common dependencies with defaults.
	// e.g., needs: [postgres, redis] auto-provisions with sidecar defaults.
	Needs []string `yaml:"needs,omitempty"`

	// Compose provides compose configuration.
	// In raw mode: used as the complete compose output (passthrough).
	// In provision mode: merged as overrides after provisions are applied.
	// This allows app-specific customization (env vars, volumes, user, etc.)
	// while still benefiting from DRY provisions.
	Compose map[string]any `yaml:"compose,omitempty"`
}

// Provision represents a loaded provision template with outputs for each target.
type Provision struct {
	// APIVersion identifies the schema version (e.g., "bosun.io/v1").
	APIVersion string `yaml:"apiVersion,omitempty"`

	// Kind identifies the manifest type (e.g., "Provision").
	Kind string `yaml:"kind,omitempty"`

	// Targets maps target name to its content (e.g., "compose" → {...}).
	// Populated by UnmarshalYAML from flat top-level YAML keys.
	Targets map[string]map[string]any `yaml:"-"`

	// Includes lists other provisions to inherit from.
	Includes []string `yaml:"includes,omitempty"`
}

// provisionMetadataKeys are YAML keys that are not target outputs.
var provisionMetadataKeys = map[string]bool{
	"apiVersion": true,
	"kind":       true,
	"includes":   true,
}

// UnmarshalYAML reads flat top-level keys into the Targets map.
// Metadata keys (apiVersion, kind, includes) are handled separately.
// Any other top-level key with a map value is treated as a target.
func (p *Provision) UnmarshalYAML(unmarshal func(any) error) error {
	// First pass: unmarshal metadata fields via default struct handling
	type provisionAlias Provision
	var alias provisionAlias
	if err := unmarshal(&alias); err != nil {
		return err
	}
	p.APIVersion = alias.APIVersion
	p.Kind = alias.Kind
	p.Includes = alias.Includes

	// Second pass: unmarshal raw map to extract target keys
	var raw map[string]any
	if err := unmarshal(&raw); err != nil {
		return err
	}

	p.Targets = make(map[string]map[string]any)
	for key, val := range raw {
		if provisionMetadataKeys[key] {
			continue
		}
		if m, ok := val.(map[string]any); ok {
			p.Targets[key] = m
		}
	}

	return nil
}

// MarshalYAML writes Targets as flat top-level keys for round-trip symmetry.
func (p Provision) MarshalYAML() (any, error) {
	result := make(map[string]any)
	if p.APIVersion != "" {
		result["apiVersion"] = p.APIVersion
	}
	if p.Kind != "" {
		result["kind"] = p.Kind
	}
	if len(p.Includes) > 0 {
		result["includes"] = p.Includes
	}
	for name, content := range p.Targets {
		result[name] = content
	}
	return result, nil
}

// RenderOutput holds the combined output from rendering a service or stack.
// Targets maps target name (e.g., "compose", "traefik", "gatus") to its content.
type RenderOutput struct {
	Targets map[string]map[string]any
}

// NewRenderOutput creates an initialized RenderOutput with empty maps
// for each registered target.
func NewRenderOutput() *RenderOutput {
	targets := make(map[string]map[string]any, len(TargetRegistry))
	for name := range TargetRegistry {
		targets[name] = make(map[string]any)
	}
	return &RenderOutput{Targets: targets}
}

// Target returns the content map for a named target, initializing it if nil.
// This prevents nil-map panics when assigning into a target that hasn't been
// populated yet (e.g., output.Target("compose")["name"] = "foo").
func (o *RenderOutput) Target(name string) map[string]any {
	if o.Targets == nil {
		o.Targets = make(map[string]map[string]any)
	}
	if o.Targets[name] == nil {
		o.Targets[name] = make(map[string]any)
	}
	return o.Targets[name]
}

// Stack defines a collection of services to render together.
type Stack struct {
	// APIVersion identifies the schema version (e.g., "bosun.io/v1").
	APIVersion string `yaml:"apiVersion,omitempty"`

	// Kind identifies the manifest type (e.g., "Stack").
	Kind string `yaml:"kind,omitempty"`

	// Name is the stack name.
	Name string `yaml:"name,omitempty"`

	// Description provides a human-readable description.
	Description string `yaml:"description,omitempty"`

	// Include lists service manifest files to include (legacy format).
	Include []string `yaml:"include,omitempty"`

	// Charts lists charts to include (Helm-aligned format).
	Charts []StackChartRef `yaml:"charts,omitempty"`

	// Networks defines network configurations for the stack.
	Networks map[string]any `yaml:"networks,omitempty"`
}

// StackChartRef references a chart within a stack.
type StackChartRef struct {
	// Name is the chart name (directory name under charts/).
	Name string `yaml:"name"`

	// Values provides per-chart value overrides.
	Values map[string]any `yaml:"values,omitempty"`
}

// =============================================================================
// Helm-Aligned Types (ADR-0011)
// =============================================================================

// Chart represents a Helm-aligned service package.
// Charts are directories containing Chart.yaml and values.yaml.
type Chart struct {
	// APIVersion identifies the schema version (e.g., "bosun.io/v1").
	APIVersion string `yaml:"apiVersion,omitempty"`

	// Kind identifies the manifest type ("Chart").
	Kind string `yaml:"kind,omitempty"`

	// Name is the chart name, used for container naming and references.
	Name string `yaml:"name"`

	// Version is the chart version (semver recommended).
	Version string `yaml:"version,omitempty"`

	// Description provides a human-readable description.
	Description string `yaml:"description,omitempty"`

	// Homepage is the URL to the project homepage.
	Homepage string `yaml:"homepage,omitempty"`

	// Templates lists template names to apply (from charts/templates/).
	Templates []string `yaml:"templates,omitempty"`

	// Dependencies lists required sidecars/sub-charts.
	Dependencies []ChartDependency `yaml:"dependencies,omitempty"`

	// Compose provides chart-specific compose overrides.
	// In charts with no templates, this is used directly (implicit raw mode).
	Compose map[string]any `yaml:"compose,omitempty"`
}

// ChartDependency represents a chart's dependency on a sidecar or sub-chart.
type ChartDependency struct {
	// Name is the dependency name (e.g., "postgres", "redis").
	Name string `yaml:"name"`

	// Version specifies the dependency version (e.g., "17" for postgres).
	Version string `yaml:"version,omitempty"`

	// Values provides template variables for the dependency.
	Values map[string]any `yaml:"values,omitempty"`

	// Compose provides raw compose overrides for edge cases (e.g., custom networks).
	Compose map[string]any `yaml:"compose,omitempty"`
}

// ChartMeta provides metadata available in templates as .Chart.
type ChartMeta struct {
	// Name is the chart name.
	Name string

	// Version is the chart version.
	Version string

	// Description is the chart description.
	Description string
}

// TemplateContext is the context passed to Go templates.
type TemplateContext struct {
	// Chart provides chart metadata (.Chart.Name, .Chart.Version, etc.).
	Chart ChartMeta

	// Values provides configuration values (.Values.*).
	Values map[string]any

	// Deps provides dependency information (.Deps.postgres.Host, etc.).
	Deps map[string]DependencyInfo
}

// DependencyInfo provides information about a resolved dependency.
type DependencyInfo struct {
	// Name is the full service name (e.g., "myapp-db").
	Name string

	// Host is the hostname for connecting (same as Name for Docker networking).
	Host string

	// Port is the default port for the dependency type.
	Port int

	// Type is the dependency type (e.g., "postgres", "redis").
	Type string
}

// Template represents a reusable configuration fragment (Helm-aligned).
// Templates use Go template syntax: {{ .Chart.Name }}, {{ .Values.port }}, etc.
type Template struct {
	// APIVersion identifies the schema version (e.g., "bosun.io/v1").
	APIVersion string `yaml:"apiVersion,omitempty"`

	// Kind identifies the manifest type ("Template").
	Kind string `yaml:"kind,omitempty"`

	// Targets maps target name to its content (e.g., "compose" → {...}).
	// Populated by UnmarshalYAML from flat top-level YAML keys.
	Targets map[string]map[string]any `yaml:"-"`

	// Includes lists other templates to inherit from (using {{ include }}).
	Includes []string `yaml:"includes,omitempty"`
}

// templateMetadataKeys are YAML keys that are not target outputs.
var templateMetadataKeys = map[string]bool{
	"apiVersion": true,
	"kind":       true,
	"includes":   true,
}

// UnmarshalYAML reads flat top-level keys into the Targets map.
func (t *Template) UnmarshalYAML(unmarshal func(any) error) error {
	type templateAlias Template
	var alias templateAlias
	if err := unmarshal(&alias); err != nil {
		return err
	}
	t.APIVersion = alias.APIVersion
	t.Kind = alias.Kind
	t.Includes = alias.Includes

	var raw map[string]any
	if err := unmarshal(&raw); err != nil {
		return err
	}

	t.Targets = make(map[string]map[string]any)
	for key, val := range raw {
		if templateMetadataKeys[key] {
			continue
		}
		if m, ok := val.(map[string]any); ok {
			t.Targets[key] = m
		}
	}

	return nil
}

// MarshalYAML writes Targets as flat top-level keys for round-trip symmetry.
func (t Template) MarshalYAML() (any, error) {
	result := make(map[string]any)
	if t.APIVersion != "" {
		result["apiVersion"] = t.APIVersion
	}
	if t.Kind != "" {
		result["kind"] = t.Kind
	}
	if len(t.Includes) > 0 {
		result["includes"] = t.Includes
	}
	for name, content := range t.Targets {
		result[name] = content
	}
	return result, nil
}

// SidecarDefaults provides default configuration for common sidecars.
// These are used when a service uses the "needs" shorthand (legacy format).
var SidecarDefaults = map[string]map[string]any{
	"postgres": {"version": "17", "db": "${name}", "db_user": "postgres", "db_password": "${db_password}"},
	"redis":    {"version": "7"},
	"mysql":    {"version": "8", "db": "${name}", "db_password": "${db_password}"},
	"mongodb":  {"version": "7", "db": "${name}"},
	"chrome":   {},
}

// DependencyDefaults provides default configuration for dependencies (Helm-aligned).
// Used when a chart declares dependencies without full configuration.
var DependencyDefaults = map[string]struct {
	Version string
	Port    int
	Values  map[string]any
}{
	"postgres": {Version: "17", Port: 5432, Values: map[string]any{"db_user": "postgres"}},
	"redis":    {Version: "7", Port: 6379, Values: nil},
	"mysql":    {Version: "8", Port: 3306, Values: nil},
	"mongodb":  {Version: "7", Port: 27017, Values: nil},
	"chrome":   {Version: "latest", Port: 3000, Values: nil},
}

// TargetNames returns the registered target names in sorted order.
// Used by provision loading and other contexts that need deterministic iteration.
func TargetNames() []string {
	names := make([]string, 0, len(TargetRegistry))
	for name := range TargetRegistry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// NewTemplateContext creates a TemplateContext from a Chart and values.
func NewTemplateContext(chart *Chart, values map[string]any) *TemplateContext {
	ctx := &TemplateContext{
		Chart: ChartMeta{
			Name:        chart.Name,
			Version:     chart.Version,
			Description: chart.Description,
		},
		Values: values,
		Deps:   make(map[string]DependencyInfo),
	}

	// Populate dependency info
	for _, dep := range chart.Dependencies {
		defaults, hasDefaults := DependencyDefaults[dep.Name]
		port := defaults.Port
		if !hasDefaults {
			port = 0
		}

		serviceName := chart.Name + "-" + dep.Name
		if dep.Name == "postgres" || dep.Name == "mysql" || dep.Name == "mongodb" {
			serviceName = chart.Name + "-db"
		}

		ctx.Deps[dep.Name] = DependencyInfo{
			Name: serviceName,
			Host: serviceName,
			Port: port,
			Type: dep.Name,
		}
	}

	return ctx
}

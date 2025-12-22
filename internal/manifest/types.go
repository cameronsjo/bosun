// Package manifest implements the Crew Manifest engine for generating
// compose, traefik, and gatus configs from service manifests.
package manifest

// API version and kind constants for manifest versioning.
const (
	// APIVersionV1 is the current API version for bosun manifests.
	APIVersionV1 = "bosun.io/v1"

	// KindProvision identifies a Provision manifest.
	KindProvision = "Provision"

	// KindStack identifies a Stack manifest.
	KindStack = "Stack"

	// KindService identifies a Service manifest.
	KindService = "Service"
)

// SupportedAPIVersions lists all API versions that can be loaded.
var SupportedAPIVersions = []string{APIVersionV1}

// SupportedKinds lists all valid manifest kinds.
var SupportedKinds = []string{KindProvision, KindStack, KindService}

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

	// Compose is used in raw mode to pass through compose config directly.
	Compose map[string]any `yaml:"compose,omitempty"`
}

// Provision represents a loaded provision template with outputs for each target.
type Provision struct {
	// APIVersion identifies the schema version (e.g., "bosun.io/v1").
	APIVersion string `yaml:"apiVersion,omitempty"`

	// Kind identifies the manifest type (e.g., "Provision").
	Kind string `yaml:"kind,omitempty"`

	// Compose output for docker-compose.yml.
	Compose map[string]any `yaml:"compose,omitempty"`

	// Traefik output for dynamic.yml.
	Traefik map[string]any `yaml:"traefik,omitempty"`

	// Gatus output for endpoints.yml.
	Gatus map[string]any `yaml:"gatus,omitempty"`

	// Includes lists other provisions to inherit from.
	Includes []string `yaml:"includes,omitempty"`
}

// RenderOutput holds the combined output from rendering a service or stack.
type RenderOutput struct {
	// Compose output for docker-compose.yml.
	Compose map[string]any

	// Traefik output for dynamic.yml.
	Traefik map[string]any

	// Gatus output for endpoints.yml.
	Gatus map[string]any
}

// NewRenderOutput creates an initialized RenderOutput with empty maps.
func NewRenderOutput() *RenderOutput {
	return &RenderOutput{
		Compose: make(map[string]any),
		Traefik: make(map[string]any),
		Gatus:   make(map[string]any),
	}
}

// Stack defines a collection of services to render together.
type Stack struct {
	// APIVersion identifies the schema version (e.g., "bosun.io/v1").
	APIVersion string `yaml:"apiVersion,omitempty"`

	// Kind identifies the manifest type (e.g., "Stack").
	Kind string `yaml:"kind,omitempty"`

	// Include lists service manifest files to include.
	Include []string `yaml:"include,omitempty"`

	// Networks defines network configurations for the stack.
	Networks map[string]any `yaml:"networks,omitempty"`
}

// SidecarDefaults provides default configuration for common sidecars.
// These are used when a service uses the "needs" shorthand.
var SidecarDefaults = map[string]map[string]any{
	"postgres": {"version": "17", "db": "${name}", "db_password": "${db_password}"},
	"redis":    {"version": "7"},
	"mysql":    {"version": "8", "db": "${name}", "db_password": "${db_password}"},
	"mongodb":  {"version": "7", "db": "${name}"},
}

// TargetNames lists the output targets for provisioning.
var TargetNames = []string{"compose", "traefik", "gatus"}

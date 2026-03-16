## MODIFIED Requirements

### Requirement: Service Rendering

The system SHALL render a `ServiceManifest` into output targets by loading provisions, expanding sidecars, and applying compose overrides. Output targets are data-driven: a `TargetRegistry` maps target names to output file metadata. The default registry includes `compose`, `traefik`, and `gatus`.

A `ServiceManifest` MUST contain:

- `name`: Service identifier used for interpolation and output naming

A `ServiceManifest` MAY contain:

- `apiVersion`: Schema version (e.g., `bosun.io/v1`)
- `kind`: Manifest type (`Service`)
- `type`: Set to `raw` for passthrough mode
- `provisions`: List of provision template names to apply
- `config`: Variables for interpolation into provisions
- `services`: Sidecar services with explicit per-sidecar configuration
- `needs`: Shorthand for common sidecars with defaults (e.g., `postgres`, `redis`)
- `compose`: Compose overrides deep-merged after provisions are applied

#### Scenario: Simple service with provisions

- **WHEN** a `ServiceManifest` has `name: myapp`, `provisions: [container]`, and `config.image: myapp:latest`
- **THEN** the system loads the `container` provision, interpolates variables, and produces output targets with the service configured

#### Scenario: Raw passthrough mode

- **WHEN** a `ServiceManifest` has `type: raw` and a `compose` block
- **THEN** the system uses the compose block directly as the compose target output without loading any provisions
- **AND** the compose block is placed under the `services` key in the compose target

#### Scenario: Needs shorthand expands sidecars

- **WHEN** a `ServiceManifest` has `needs: [postgres]`
- **THEN** the system applies `SidecarDefaults` for postgres, interpolates default values using the service's config, and loads the postgres provision
- **AND** the sidecar service appears in the compose target (e.g., `myapp-db`)

#### Scenario: Unknown needs are silently skipped

- **WHEN** a `ServiceManifest` has a `needs` entry that is not in `SidecarDefaults` or has no matching provision file
- **THEN** the system skips the entry without error

#### Scenario: Explicit sidecar services

- **WHEN** a `ServiceManifest` has `services.postgres` with custom configuration
- **THEN** the system uses the explicit configuration merged with manifest-level config to load and render the sidecar provision

#### Scenario: Compose overrides are deep-merged

- **WHEN** a `ServiceManifest` includes a `compose` block alongside provisions
- **THEN** the compose block is interpolated with variables and deep-merged on top of provision-generated compose target
- **AND** `${var}` placeholders in the compose block are replaced with values from config

### Requirement: Provision System

The system SHALL support reusable provision templates that produce outputs for any registered target. Provisions are loaded from YAML files, interpolated with variables, and support inheritance via an `includes` key. Target outputs are stored in a `Targets` map keyed by target name.

A `Provision` MAY contain:

- `apiVersion`: Schema version
- `kind`: Manifest type (`Provision`)
- `includes`: List of other provision names to inherit from
- Any registered target name as a top-level key (e.g., `compose`, `traefik`, `gatus`) with a map value

#### Scenario: Load and interpolate a provision

- **WHEN** a provision file contains `${name}` and `${image}` placeholders
- **THEN** the system replaces them with values from the variables map before YAML parsing
- **AND** returns a `Provision` with populated targets

#### Scenario: Provision inheritance via includes

- **WHEN** a provision has `includes: [base, extension]`
- **THEN** the system loads each included provision first, deep-merges their target outputs, then deep-merges the current provision's content on top
- **AND** the current provision's values take precedence over included values

#### Scenario: Circular include detection

- **WHEN** a provision includes itself directly or indirectly
- **THEN** the system returns an `ErrCircularInclude` error instead of entering an infinite loop

#### Scenario: Missing provision returns error

- **WHEN** a provision file does not exist at the expected path
- **THEN** the system returns a "provision not found" error

#### Scenario: Missing variable returns error

- **WHEN** a provision references a `${var}` that is not in the variables map
- **THEN** the system returns a "missing variables" error listing all unresolved references

#### Scenario: List available provisions

- **WHEN** `ListProvisions` is called with a provisions directory
- **THEN** it returns the names (without file extension) of all `.yml` and `.yaml` files in the directory, excluding subdirectories

#### Scenario: Backwards-compatible YAML deserialization

- **WHEN** a provision file uses top-level `compose:`, `traefik:`, `gatus:` keys (existing format)
- **THEN** the system unmarshals these into `Targets["compose"]`, `Targets["traefik"]`, `Targets["gatus"]` respectively

#### Scenario: YAML round-trip preserves flat structure

- **WHEN** a `Provision` is marshaled back to YAML
- **THEN** the output uses flat top-level keys (e.g., `compose:`, `traefik:`) not a nested `targets:` wrapper

### Requirement: Chart Template Engine

The system SHALL render charts using a Go template engine with Sprig functions, custom helpers, and structured template context. Template output targets are extracted dynamically from the rendered YAML by matching keys against the target registry.

Templates use `{{ .Chart.Name }}`, `{{ .Values.port }}`, and `{{ .Deps.postgres.Host }}` syntax. The engine provides:

- All Sprig text functions
- `include`: Include named helpers or other templates
- `nindent`: Add newline then indent each line
- `toYaml`: Convert values to YAML strings
- Helper definitions from `_helpers.tpl`
- Template caching for loaded template files

#### Scenario: Render template with values

- **WHEN** a template references `{{ .Values.image }}`
- **THEN** the engine substitutes the value from the template context's Values map

#### Scenario: Use Sprig functions

- **WHEN** a template uses `{{ .Values.name | upper }}`
- **THEN** the Sprig `upper` function is applied to the value

#### Scenario: Include helper definitions

- **WHEN** a template calls `{{ include "helperName" . }}`
- **THEN** the engine looks up the named helper from `_helpers.tpl` and renders it with the provided context

#### Scenario: Dependency info in template context

- **WHEN** a chart has dependencies declared
- **THEN** the template context provides `.Deps.{name}.Host`, `.Deps.{name}.Port`, and `.Deps.{name}.Name` for each dependency

#### Scenario: Missing template returns error

- **WHEN** a template name does not correspond to a file in the templates directory
- **THEN** the engine returns a "template not found" error

#### Scenario: Dynamic target extraction from rendered output

- **WHEN** a template renders YAML with top-level keys matching registered target names
- **THEN** the engine extracts each matching key into the corresponding target in the `RenderOutput`
- **AND** metadata keys (`apiVersion`, `kind`, `includes`) are stripped before extraction

### Requirement: Output Writing

The system SHALL write rendered outputs to target-specific directories as YAML files. Target names, output directories, and filenames are determined by the `TargetRegistry`.

Default target configuration:

- `compose` → `compose/{stackName}.yml.tmpl`
- `traefik` → `traefik/dynamic.yml`
- `gatus` → `gatus/endpoints.yml`

The system SHALL iterate targets in sorted order (by target name) to produce deterministic output.

#### Scenario: Write all non-empty targets

- **WHEN** `WriteOutputs` is called with a `RenderOutput` containing non-empty targets
- **THEN** each non-empty target is written to its respective subdirectory under the output directory

#### Scenario: Empty targets are not written

- **WHEN** a target has no content (empty map)
- **THEN** no file or directory is created for that target

#### Scenario: Dry-run rendering

- **WHEN** `RenderToYAML` is called with a `RenderOutput`
- **THEN** it returns a combined YAML string of all non-empty targets in sorted order for display purposes

#### Scenario: Deterministic output order

- **WHEN** multiple targets have content
- **THEN** `WriteOutputs` and `RenderToYAML` process targets in alphabetical order by target name

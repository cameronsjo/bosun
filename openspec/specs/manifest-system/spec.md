# Manifest System Specification

## Purpose

The manifest system transforms service definitions into Docker Compose, Traefik, and Gatus configuration files. It supports two formats: a legacy provision-based format using `${var}` interpolation, and a Helm-aligned chart format using Go templates (`{{ .Values.var }}`).
## Requirements
### Requirement: Service Rendering

The system SHALL render a `ServiceManifest` into output targets by loading provisions, expanding sidecars, and applying compose overrides. Output targets are data-driven: a `TargetRegistry` maps target names to output file metadata. The default registry includes `compose`, `traefik`, and `gatus`.

The `TargetRegistry` is a package-level map of target name (string) to `TargetConfig`. Each `TargetConfig` contains:

- `Dir`: Output subdirectory name (defaults to the target name, e.g., `"compose"`)
- `Filename`: A function `func(stackName string) string` that returns the output filename for a given stack

The registry provides:

- Lookup by target name
- Iteration over all registered targets (sorted by name for determinism)
- Registration of new targets (code-defined at package init; not config-driven)

Default entries:

| Target | Dir | Filename |
|--------|-----|----------|
| `compose` | `compose` | `{stackName}.yml.tmpl` |
| `traefik` | `traefik` | `dynamic.yml` |
| `gatus` | `gatus` | `endpoints.yml` |

`RenderOutput` holds rendering results in `Targets map[string]map[string]any` — a map keyed by target name where each value is a `map[string]any` representing the target's YAML content.

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

The system SHALL support reusable provision templates that produce outputs for any registered target. Provisions are loaded from YAML files, interpolated with variables, and support inheritance via an `includes` key. Target outputs are stored in `Targets map[string]map[string]any` — a map keyed by target name where each value is a `map[string]any` representing the target's YAML content.

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

### Requirement: Stack Rendering (Legacy)

The system SHALL render a `Stack` definition that groups multiple service manifests into a combined output. Stacks reference service files via an `include` list.

A `Stack` MUST contain:

- `include`: List of service manifest file paths relative to the services directory

A `Stack` MAY contain:

- `apiVersion`: Schema version
- `kind`: Manifest type (`Stack`)
- `name`: Stack identifier
- `description`: Human-readable description
- `networks`: Shared network definitions merged into compose output
- `charts`: Not allowed in legacy Stack format (use Helm-aligned Stack instead; see Chart Stack Rendering)

#### Scenario: Stack renders multiple services

- **WHEN** a stack includes multiple service manifest files
- **THEN** each service is rendered independently and the outputs are deep-merged into a single combined result

#### Scenario: Stack applies values overlay

- **WHEN** `RenderStack` is called with a non-nil `valuesOverlay`
- **THEN** the overlay values are deep-merged into each service's config before rendering

#### Scenario: Stack merges network definitions

- **WHEN** a stack defines `networks`
- **THEN** the network definitions are deep-merged with any networks already in the compose output from services

#### Scenario: Path traversal prevention

- **WHEN** a stack includes a service file path that traverses outside the services directory (e.g., `../../etc/passwd`)
- **THEN** the system returns an `ErrPathTraversal` error

### Requirement: Deep Merge

The system SHALL recursively merge two maps with configurable strategies for different key types.

Merge semantics:

- **Maps**: Recursive merge (both base and overlay are traversed)
- **Union keys** (`networks`, `depends_on`): Lists are merged as set-union (no duplicates)
- **Extend keys** (`endpoints`): Lists are appended
- **Default lists**: Overlay replaces base
- **Environment/labels normalization**: List format (`KEY=value`) is converted to map format before merging
- **Depth limit**: Maximum recursion depth of 100 prevents stack overflow

#### Scenario: Overlay wins for scalar values

- **WHEN** both base and overlay have the same key with scalar values
- **THEN** the overlay value replaces the base value

#### Scenario: Nested maps merge recursively

- **WHEN** both base and overlay have the same key containing maps
- **THEN** the maps are merged recursively, preserving keys unique to each side

#### Scenario: Networks use set-union

- **WHEN** both base and overlay have a `networks` key with lists
- **THEN** the result is the union of both lists with no duplicates

#### Scenario: Depends_on uses set-union

- **WHEN** both base and overlay have a `depends_on` key with lists
- **THEN** the result is the union of both lists with no duplicates

#### Scenario: Endpoints are appended

- **WHEN** both base and overlay have an `endpoints` key with lists
- **THEN** the overlay list is appended to the base list

#### Scenario: Default list replacement

- **WHEN** both base and overlay have a list key that is not a union or extend key
- **THEN** the overlay list replaces the base list entirely

#### Scenario: Environment normalization before merge

- **WHEN** environment values are in list format (`["FOO=bar"]`)
- **THEN** the system normalizes them to map format (`{FOO: bar}`) before performing the merge

#### Scenario: No mutation of originals

- **WHEN** `DeepMerge` is called
- **THEN** neither the base nor the overlay map is mutated; a new map is returned

### Requirement: Variable Interpolation

The system SHALL replace `${var}` placeholders in raw strings with values from a variables map before YAML parsing. The pattern `$\{(\w+)\}` is used for matching.

#### Scenario: Simple variable replacement

- **WHEN** a string contains `${name}` and the variables map has `name: World`
- **THEN** the result is the string with `${name}` replaced by `World`

#### Scenario: Multiple variables in one string

- **WHEN** a string contains `${registry}/${image}:${tag}`
- **THEN** all three placeholders are replaced with their corresponding values

#### Scenario: Missing variable returns error

- **WHEN** a string references `${missing}` and the variable is not in the map
- **THEN** the system returns an error listing all missing variable names

#### Scenario: Non-string types are converted

- **WHEN** a variable value is an integer, float, or boolean
- **THEN** it is converted to its string representation for interpolation

#### Scenario: Dollar sign without braces is preserved

- **WHEN** a string contains `$name` (without braces)
- **THEN** it is left unchanged; only `${name}` syntax triggers interpolation

#### Scenario: Go template syntax is preserved

- **WHEN** a string contains `{{ .Values.key }}` (Go template syntax)
- **THEN** the interpolation engine does not modify it, preserving it for later template processing

#### Scenario: Recursive map interpolation

- **WHEN** `InterpolateMap` is called with a nested map containing `${var}` placeholders
- **THEN** all string values at any depth are interpolated, while non-string values (int, float, bool) pass through unchanged

### Requirement: Manifest Validation

The system SHALL validate manifest metadata fields (`apiVersion` and `kind`) to ensure compatibility and correctness.

Supported API versions: `bosun.io/v1`

Supported kinds: `Provision`, `Template`, `Stack`, `Service`, `Chart`

#### Scenario: Valid versioned manifest

- **WHEN** a manifest has `apiVersion: bosun.io/v1` and a valid `kind`
- **THEN** validation succeeds and returns the parsed metadata

#### Scenario: Unversioned manifest (backwards compatibility)

- **WHEN** a manifest is missing `apiVersion` and `kind` fields
- **THEN** soft validation (`ValidateManifest`) succeeds but logs a warning recommending migration

#### Scenario: Unsupported API version

- **WHEN** a manifest has an unrecognized `apiVersion`
- **THEN** validation returns `ErrUnsupportedAPIVersion`

#### Scenario: Invalid kind

- **WHEN** a manifest has a `kind` not in the supported list
- **THEN** validation returns `ErrInvalidKind`

#### Scenario: Kind mismatch

- **WHEN** a manifest's `kind` does not match the expected kind for the context
- **THEN** validation returns `ErrKindMismatch`

#### Scenario: Strict validation requires all fields

- **WHEN** `ValidateManifestStrict` is called on a manifest missing `apiVersion` or `kind`
- **THEN** it returns `ErrMissingAPIVersion` or `ErrMissingKind` respectively

### Requirement: Chart Definition Format

The system SHALL support Helm-aligned Chart definitions with structured metadata.

A Chart MUST contain:

- `name`: Chart identifier

A Chart MAY contain:

- `apiVersion`: API version (e.g., `bosun.io/v1`)
- `kind`: Resource type (`Chart`)
- `version`: Semantic version
- `description`: Human-readable description
- `homepage`: Project URL
- `templates`: List of template names to apply
- `dependencies`: List of required sidecars/sub-charts
- `compose`: Docker Compose service overrides

#### Scenario: Valid Chart with templates

- **WHEN** a `Chart.yaml` contains name, templates, and dependencies
- **THEN** the system parses it into a `Chart` struct with all fields populated

#### Scenario: Minimal Chart definition

- **WHEN** a `Chart.yaml` contains only a name field
- **THEN** the system creates a valid Chart with defaults for optional fields

#### Scenario: Chart with compose overrides

- **WHEN** a `Chart.yaml` includes a compose block alongside templates
- **THEN** the compose block is deep-merged with template-generated compose output after templates are rendered

#### Scenario: Implicit raw mode

- **WHEN** a Chart has no `templates` array but has a `compose` block
- **THEN** the compose block is rendered with Go template support and used directly as the output

#### Scenario: Chart with neither templates nor compose

- **WHEN** a Chart has neither templates nor compose
- **THEN** the system generates an empty compose output

### Requirement: Chart Dependency Model

The system SHALL support chart dependencies with version, values, and compose overrides. Dependencies are rendered using shared templates and produce sidecar services.

A dependency MUST contain:

- `name`: Name of the dependency type (e.g., `postgres`, `redis`)

A dependency MAY contain:

- `version`: Version override for the dependency image
- `values`: Map of values to pass to the dependency template
- `compose`: Docker Compose overrides for the dependency service

Default dependency configurations are provided via `DependencyDefaults` for common types: postgres (port 5432), redis (port 6379), mysql (port 3306), mongodb (port 27017), chrome (port 3000).

#### Scenario: Dependency with defaults

- **WHEN** a chart declares a postgres dependency without specifying version
- **THEN** the system uses the default version from `DependencyDefaults` and generates a sidecar service named `{chart}-db`

#### Scenario: Dependency with values

- **WHEN** a dependency specifies custom values
- **THEN** those values override the dependency defaults in the template context

#### Scenario: Dependency with compose override

- **WHEN** a dependency specifies compose overrides
- **THEN** the overrides are deep-merged with the template-rendered dependency output, scoped to the sidecar service name

#### Scenario: Database dependency naming convention

- **WHEN** a dependency is `postgres`, `mysql`, or `mongodb`
- **THEN** the sidecar service is named `{chart}-db` instead of `{chart}-{dep}`
- **AND** non-database dependencies (e.g., `redis`, `chrome`) use the `{chart}-{dep}` naming pattern

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
- **AND** top-level keys that are neither metadata nor registered target names are silently ignored

### Requirement: Values Precedence

The system SHALL apply values in priority order from lowest to highest:

1. Template/dependency defaults
2. Chart `values.yaml`
3. Stack `values.yaml` (if rendering via stack)
4. Per-chart values from stack definition
5. CLI value overrides

#### Scenario: Stack overrides chart values

- **WHEN** both chart `values.yaml` and stack-level values define the same key
- **THEN** the stack value takes precedence

#### Scenario: Per-chart values in stack override stack-level values

- **WHEN** a stack defines both global values and per-chart values for the same key
- **THEN** the per-chart values take precedence for that chart

#### Scenario: CLI overrides all values

- **WHEN** a value is provided via value overrides at render time
- **THEN** it overrides all other sources

### Requirement: Chart Stack Rendering

The system SHALL render stacks in Helm-aligned format by loading charts referenced in the `charts` list, applying value overrides, and merging all outputs.

#### Scenario: Stack with multiple charts

- **WHEN** a Stack lists multiple chart references
- **THEN** all charts are rendered with their respective values and combined into a single compose output via deep merge

#### Scenario: Stack with per-chart values

- **WHEN** a Stack specifies values for a specific chart reference
- **THEN** those values apply only to that chart, merged on top of stack-level and chart-level values

#### Scenario: Stack includes network definitions

- **WHEN** a Stack defines shared networks
- **THEN** the networks are deep-merged into the final compose output alongside chart-generated networks

### Requirement: Format Detection

The system SHALL detect whether a directory uses legacy or Helm-aligned manifest format.

#### Scenario: Detect Helm format

- **WHEN** a `charts/` directory exists containing subdirectories with `Chart.yaml` files
- **THEN** `DetectFormat` returns `"helm"`

#### Scenario: Detect legacy format

- **WHEN** a `manifest/provisions/` or `provisions/` directory exists
- **THEN** `DetectFormat` returns `"legacy"`

#### Scenario: Unknown format

- **WHEN** neither Helm nor legacy directory structure is found
- **THEN** `DetectFormat` returns `"unknown"`

#### Scenario: Detect Helm when both formats present

- **WHEN** both a `charts/` directory with `Chart.yaml` files and a `manifest/provisions/` or `provisions/` directory exist
- **THEN** `DetectFormat` returns `"helm"` (Helm takes precedence over legacy)

### Requirement: Manifest Migration

The system SHALL migrate unversioned manifest files to v1 by adding `apiVersion` and `kind` headers. The migration detects the manifest kind from content structure.

#### Scenario: Migrate unversioned provision

- **WHEN** a YAML file contains `compose:`, `traefik:`, `gatus:`, or `includes:` without `apiVersion`/`kind`
- **THEN** `MigrateToV1` prepends `apiVersion: bosun.io/v1` and `kind: Provision`

#### Scenario: Migrate unversioned service

- **WHEN** a YAML file contains `name` with `provisions`, `needs`, `services`, or `type` fields
- **THEN** `MigrateToV1` prepends `apiVersion: bosun.io/v1` and `kind: Service`

#### Scenario: Migrate unversioned stack

- **WHEN** a YAML file contains an `include` key
- **THEN** `MigrateToV1` prepends `apiVersion: bosun.io/v1` and `kind: Stack`

#### Scenario: Already versioned files are skipped

- **WHEN** a file already has `apiVersion` and `kind` fields
- **THEN** `MigrateToV1` returns the content unchanged

#### Scenario: Dry-run mode does not write

- **WHEN** `MigrateFile` is called with `DryRun: true`
- **THEN** it reports what would be migrated without modifying the file on disk

#### Scenario: Original content is preserved

- **WHEN** a file is migrated
- **THEN** the original content (including comments and variable placeholders) is preserved below the added header

### Requirement: Output Writing

The system SHALL write rendered outputs to target-specific directories as YAML files. Target names, output directories, and filenames are determined by the `TargetRegistry`.

Default target configuration:

- `compose` → `compose/{stackName}.yml.tmpl`
- `traefik` → `traefik/dynamic.yml`
- `gatus` → `gatus/endpoints.yml`

The system SHALL iterate targets in sorted order (by target name) to produce deterministic output.

#### Scenario: Write all non-empty registered targets

- **WHEN** `WriteOutputs` is called with a `RenderOutput` containing non-empty targets
- **THEN** each non-empty target that is present in the `TargetRegistry` is written to its respective subdirectory under the output directory

#### Scenario: Empty targets are not written

- **WHEN** a target has no content (empty map)
- **THEN** no file or directory is created for that target

#### Scenario: Dry-run rendering

- **WHEN** `RenderToYAML` is called with a `RenderOutput`
- **THEN** it returns a combined YAML string of all non-empty targets in sorted order for display purposes

#### Scenario: Deterministic output order

- **WHEN** multiple targets have content
- **THEN** `WriteOutputs` and `RenderToYAML` process targets in alphabetical order by target name

#### Scenario: Unregistered target in RenderOutput

- **WHEN** `RenderOutput.Targets` contains a target name that is not present in the `TargetRegistry`
- **THEN** `WriteOutputs` logs a warning identifying the unregistered target name and skips it
- **AND** `RenderToYAML` includes the unregistered target in the combined YAML output (for diagnostic visibility)
- **AND** `showDiff` skips the unregistered target


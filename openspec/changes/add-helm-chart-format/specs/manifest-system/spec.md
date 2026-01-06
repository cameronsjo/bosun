## ADDED Requirements

### Requirement: Chart Definition Format

The system SHALL support Helm-aligned Chart definitions with structured metadata.

A Chart MUST contain:
- `name`: Chart identifier (required)

A Chart MAY contain:
- `apiVersion`: API version (e.g., "bosun.io/v1")
- `kind`: Resource type ("Chart")
- `version`: Semantic version
- `description`: Human-readable description
- `homepage`: Project URL
- `templates`: List of template names to apply
- `dependencies`: List of required charts
- `compose`: Docker Compose service overrides

#### Scenario: Valid Chart with templates
- **WHEN** a Chart.yaml contains name, templates, and dependencies
- **THEN** the system parses it into a Chart struct with all fields populated

#### Scenario: Minimal Chart definition
- **WHEN** a Chart.yaml contains only a name field
- **THEN** the system creates a valid Chart with defaults for optional fields

#### Scenario: Chart with compose overrides
- **WHEN** a Chart.yaml includes a compose block
- **THEN** the compose block is deep-merged with template-generated compose output

### Requirement: Chart Dependency Model

The system SHALL support chart dependencies with version, values, and compose overrides.

A dependency MUST contain:
- `name`: Name of the dependency chart

A dependency MAY contain:
- `version`: Version constraint
- `values`: Map of values to pass to the dependency
- `compose`: Docker Compose overrides for the dependency

#### Scenario: Dependency with values
- **WHEN** a dependency specifies values
- **THEN** those values override the dependency chart's defaults

#### Scenario: Dependency with compose override
- **WHEN** a dependency specifies compose overrides
- **THEN** the overrides are deep-merged with dependency output

### Requirement: Values Precedence

The system SHALL apply values in priority order from lowest to highest:
1. Template defaults
2. Chart values.yaml
3. Stack values.yaml
4. CLI --set flags

#### Scenario: Stack overrides chart values
- **WHEN** both chart and stack define the same value
- **THEN** the stack value takes precedence

#### Scenario: CLI overrides all values
- **WHEN** a value is set via CLI --set flag
- **THEN** it overrides all other sources

### Requirement: Implicit Raw Mode

The system SHALL treat charts without templates but with compose blocks as raw passthrough.

#### Scenario: Chart with compose but no templates
- **WHEN** a Chart has no templates array but has a compose block
- **THEN** the compose block is used directly without template processing

#### Scenario: Chart with neither templates nor compose
- **WHEN** a Chart has neither templates nor compose
- **THEN** the system generates an empty compose output

### Requirement: Stack Definition Format

The system SHALL support Stack definitions for grouping multiple charts.

A Stack MUST contain:
- `name`: Stack identifier

A Stack MAY contain:
- `apiVersion`: API version
- `kind`: Resource type ("Stack")
- `description`: Human-readable description
- `charts`: List of charts to include
- `networks`: Shared network definitions

#### Scenario: Stack with multiple charts
- **WHEN** a Stack.yaml lists multiple charts
- **THEN** all charts are rendered and combined into a single compose output

#### Scenario: Stack with per-chart values
- **WHEN** a Stack specifies values for a specific chart
- **THEN** those values apply only to that chart

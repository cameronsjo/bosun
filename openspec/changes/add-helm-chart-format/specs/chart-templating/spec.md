## ADDED Requirements

### Requirement: Go Template Engine

The system SHALL render templates using Go's text/template package with Sprig function extensions.

#### Scenario: Basic value interpolation
- **WHEN** a template contains `{{ .Values.name }}`
- **THEN** the value from the values context is substituted

#### Scenario: Sprig function usage
- **WHEN** a template uses Sprig functions like `{{ .Values.name | upper }}`
- **THEN** the function is applied to the value

#### Scenario: Invalid template syntax
- **WHEN** a template contains invalid Go template syntax
- **THEN** the system returns a descriptive error message

### Requirement: Template Context

The system SHALL provide a structured context for templates containing:
- `.Chart`: Chart metadata (Name, Version, Description, Homepage)
- `.Values`: Merged values from all sources
- `.Deps`: Rendered dependency information

#### Scenario: Access Chart metadata
- **WHEN** a template uses `{{ .Chart.Name }}`
- **THEN** the chart's name is substituted

#### Scenario: Access dependency info
- **WHEN** a template uses `{{ .Deps.postgres.ServiceName }}`
- **THEN** the dependency's service name is substituted

### Requirement: Helper Templates

The system SHALL support reusable template snippets via `_helpers.tpl` files.

Helper templates MUST be defined using Go template `define` syntax:
```
{{- define "helper.name" -}}
...content...
{{- end -}}
```

#### Scenario: Define helper in _helpers.tpl
- **WHEN** a `_helpers.tpl` file contains a `define` block
- **THEN** the helper is available in all templates via `include`

#### Scenario: No _helpers.tpl file
- **WHEN** the templates directory has no `_helpers.tpl`
- **THEN** templates render without helpers (no error)

### Requirement: Include Function

The system SHALL provide an `include` function to invoke helper templates.

Usage: `{{ include "helper.name" . }}`

The include function MUST:
- Execute the named template with the provided context
- Return the rendered output as a string
- Support pipeline operations (e.g., `| nindent 4`)

#### Scenario: Include helper with nindent
- **WHEN** a template uses `{{ include "helper" . | nindent 4 }}`
- **THEN** the helper output is indented by 4 spaces on each line

#### Scenario: Include non-existent helper
- **WHEN** a template includes a helper that doesn't exist
- **THEN** the system returns a descriptive error

### Requirement: YAML Output Functions

The system SHALL provide functions for YAML output formatting:
- `toYaml`: Convert value to YAML string
- `nindent`: Add newline and indent

#### Scenario: toYaml with map
- **WHEN** a template uses `{{ .Values.labels | toYaml }}`
- **THEN** the map is rendered as valid YAML

#### Scenario: nindent preserves structure
- **WHEN** a template uses `{{ .Values.config | toYaml | nindent 2 }}`
- **THEN** each line of the YAML output is indented by 2 spaces

### Requirement: Template Directory Structure

The system SHALL load templates from `charts/templates/` directory.

Template files:
- MUST have `.yaml` or `.yml` extension
- MUST be valid Go templates
- MAY reference helpers from `_helpers.tpl`

#### Scenario: Load template by name
- **WHEN** a chart references template "container"
- **THEN** the system loads `charts/templates/container.yaml`

#### Scenario: Template file not found
- **WHEN** a chart references a non-existent template
- **THEN** the system returns a descriptive error

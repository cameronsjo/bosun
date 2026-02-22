# Chart Templating Specification

## Purpose

The chart templating system provides Helm-aligned Go template rendering for Bosun charts. It uses Go's `text/template` package with Sprig function extensions, custom YAML output functions, helper template support, and a structured context model that exposes chart metadata, configuration values, and dependency information to templates.

## Requirements

### Requirement: Go Template Engine

The system SHALL render templates using Go's `text/template` package with Sprig function extensions.

The `TemplateEngine` MUST:

- Load and cache templates from the templates directory
- Support `.yaml` and `.yml` file extensions for templates
- Load `_helpers.tpl` as a helper source regardless of the .yaml/.yml rule (it is always loaded when present)
- Include all Sprig text functions via `sprig.TxtFuncMap()`
- Provide custom bosun functions: `include`, `nindent`, `toYaml`

#### Scenario: Basic value interpolation

- **WHEN** a template contains `{{ .Values.image }}`
- **THEN** the engine substitutes the value from the template context's Values map

#### Scenario: Sprig function usage

- **WHEN** a template uses `{{ .Values.name | upper }}`
- **THEN** the Sprig `upper` function is applied to the value

#### Scenario: Invalid template syntax

- **WHEN** a template contains invalid Go template syntax
- **THEN** the system returns a descriptive parse error

#### Scenario: Template caching

- **WHEN** the same template is loaded multiple times
- **THEN** the engine returns the cached parsed template instead of re-reading and re-parsing the file

### Requirement: Template Context

The system SHALL provide a structured `TemplateContext` for templates containing:

- `.Chart`: Chart metadata (`Name`, `Version`, `Description`)
- `.Values`: Merged values from all sources
- `.Deps`: Rendered dependency information with `Name`, `Host`, `Port`, and `Type` fields

#### Scenario: Access Chart metadata

- **WHEN** a template uses `{{ .Chart.Name }}` or `{{ .Chart.Version }}`
- **THEN** the chart's name or version from `ChartMeta` is substituted

#### Scenario: Access dependency info

- **WHEN** a template uses `{{ .Deps.postgres.Host }}` or `{{ .Deps.postgres.Port }}`
- **THEN** the dependency's hostname and port are substituted from `DependencyInfo`

#### Scenario: Database dependency naming

- **WHEN** a chart has a `postgres`, `mysql`, or `mongodb` dependency
- **THEN** `.Deps.{type}.Name` resolves to `{chart}-db` instead of `{chart}-{type}`

#### Scenario: Non-database dependency naming

- **WHEN** a chart has a `redis` or `chrome` dependency
- **THEN** `.Deps.{type}.Name` resolves to `{chart}-{type}`

#### Scenario: Unknown dependency type

- **WHEN** a chart declares a dependency not in `DependencyDefaults`
- **THEN** `.Deps.{type}.Port` is `0` and the service name follows the `{chart}-{type}` pattern

### Requirement: Helper Templates

The system SHALL support reusable template snippets via `_helpers.tpl` files in the templates directory.

Helper templates MUST be defined using Go template `define` syntax:

```gotemplate
{{- define "helper.name" -}}
...content...
{{- end -}}
```

#### Scenario: Define and use helper

- **WHEN** a `_helpers.tpl` file contains a `define` block
- **THEN** the helper is available in all templates via the `include` function

#### Scenario: No _helpers.tpl file

- **WHEN** the templates directory has no `_helpers.tpl`
- **THEN** templates render without helpers and the engine's `helpers` field is nil (no error)

#### Scenario: Helpers available to all templates

- **WHEN** a helper is defined in `_helpers.tpl`
- **THEN** it is accessible from any template loaded by the engine, not just the first template

### Requirement: Include Function

The system SHALL provide an `include` function to invoke helper templates or other template files.

Usage: `{{ include "name" . }}`

The `include` function MUST:

- First search helpers defined in `_helpers.tpl`
- Fall back to loading template files from the templates directory
- Execute the named template with the provided context
- Return the rendered output as a string for pipeline operations

#### Scenario: Include helper with context

- **WHEN** a template calls `{{ include "labels" . }}`
- **THEN** the helper is executed with the current template context and the result is inserted

#### Scenario: Include template file

- **WHEN** a template calls `{{ include "postgres" . }}` and no helper named `postgres` exists
- **THEN** the engine loads `postgres.yaml` or `postgres.yml` from the templates directory

#### Scenario: Include with pipeline

- **WHEN** a template uses `{{ include "helper" . | nindent 4 }}`
- **THEN** the helper output is piped through `nindent` for indentation

#### Scenario: Include non-existent template

- **WHEN** a template includes a name that exists neither as a helper nor as a template file
- **THEN** the system returns a "template not found" error

### Requirement: YAML Output Functions

The system SHALL provide custom functions for YAML output formatting:

- `toYaml`: Convert any value to its YAML string representation (trailing newline stripped)
- `nindent`: Add a leading newline then indent every non-empty line by the specified number of spaces

#### Scenario: toYaml with map

- **WHEN** a template uses `{{ .Values.labels | toYaml }}`
- **THEN** the map is rendered as valid YAML key-value pairs

#### Scenario: toYaml with slice

- **WHEN** a template uses `{{ .Values.ports | toYaml }}`
- **THEN** the slice is rendered as a YAML list with `-` prefixes

#### Scenario: nindent single line

- **WHEN** `nindent 4 "hello"` is called
- **THEN** the result is `\n    hello` (newline + 4 spaces + text)

#### Scenario: nindent multiple lines

- **WHEN** `nindent 2 "line1\nline2"` is called
- **THEN** each non-empty line is indented by 2 spaces with a leading newline

#### Scenario: nindent preserves empty lines

- **WHEN** the input contains empty lines between content
- **THEN** empty lines are not indented (remain empty)

### Requirement: Template Directory Structure

The system SHALL load templates from a `templates/` directory within the charts root.

Template files:

- MUST have `.yaml` or `.yml` extension
- `_helpers.tpl` is exempt from this extension rule and is always loaded as a helper source when present
- MUST contain valid Go template syntax
- MAY reference helpers from `_helpers.tpl`
- `_helpers.tpl` is excluded from template listings

#### Scenario: Load template by name

- **WHEN** a chart references template `"container"`
- **THEN** the system loads `templates/container.yaml` (or `.yml`)

#### Scenario: Template file not found

- **WHEN** a chart references a non-existent template name
- **THEN** the system returns a "template not found" error

#### Scenario: List templates excludes helpers

- **WHEN** `ListTemplates` is called
- **THEN** it returns template names (without extensions) for all `.yaml`/`.yml` files except `_helpers.tpl`

#### Scenario: Directories are excluded from listings

- **WHEN** the templates directory contains subdirectories
- **THEN** they are excluded from the template listing

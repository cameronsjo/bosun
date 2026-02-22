# Chart Migration Specification

## Purpose

The chart migration system converts legacy provision-based manifests to the Helm-aligned chart format. It provides both version migration (adding `apiVersion`/`kind` headers) and format migration (converting the entire directory structure, interpolation syntax, and manifest types from legacy to Helm-aligned).

## Requirements

### Requirement: Format Auto-Detection

The system SHALL automatically detect the manifest format (helm vs legacy) based on directory structure.

Detection rules:

- If `charts/` directory exists with Chart.yaml files, the format is `helm`
- If `manifest/provisions/` or `provisions/` directory exists, the format is `legacy`
- Otherwise, the format is `unknown`

#### Scenario: Helm format detection

- **WHEN** the project contains a `charts/` directory with at least one subdirectory containing `Chart.yaml`
- **THEN** `DetectFormat` returns `"helm"`

#### Scenario: Legacy format detection

- **WHEN** the project contains a `manifest/provisions/` or `provisions/` directory
- **THEN** `DetectFormat` returns `"legacy"`

#### Scenario: Both formats present

- **WHEN** both `charts/` with Chart.yaml files and `provisions/` exist
- **THEN** `DetectFormat` returns `"helm"` (Helm takes precedence)

#### Scenario: Unknown format

- **WHEN** neither Helm nor legacy directory structure is found
- **THEN** `DetectFormat` returns `"unknown"`

### Requirement: Helm Format Migration Command

The system SHALL provide a `bosun migrate helm` command to convert legacy manifests to Helm-aligned chart format.

The command MUST:

- Convert `provisions/*.yml` to `charts/templates/*.yaml`
- Convert `services/*.yml` to `charts/{service}/Chart.yaml` + `values.yaml`

The `{service}` directory name SHALL be derived from the service manifest's `name` field if present. If no `name` field exists, the file basename (without extension) SHALL be used. When the `name` field and file basename differ, a migration warning SHALL be emitted and the original filename SHALL be recorded in the generated `Chart.yaml` metadata. If two services resolve to the same `{service}` directory name, the migration SHALL fail with a collision error listing both source files.

- Convert `stacks/*.yml` to `stacks/{stack}/Stack.yaml`
- Preserve legacy files (not delete originals)
- Default to dry-run mode (show what would be migrated without writing)

#### Scenario: Migrate provisions to templates

- **WHEN** the user runs `bosun migrate helm`
- **THEN** provision files are listed as migration candidates with source and destination paths
- **AND** no files are written in dry-run mode

#### Scenario: Migrate service to chart

- **WHEN** a legacy service YAML has `name`, `provisions`, `config`, `needs`, and `services` fields
- **THEN** a `Chart.yaml` is generated with `name`, `templates` (from provisions), and `dependencies` (from needs/services)
- **AND** a `values.yaml` is generated from the service's `config` map

#### Scenario: Migrate stack to Helm stack

- **WHEN** a legacy stack YAML has an `include` list
- **THEN** a `Stack.yaml` is generated with a `charts` list mapping included service file names to chart references
- **AND** network definitions are preserved

#### Scenario: Apply migration with force flag

- **WHEN** the user runs `bosun migrate helm --force`
- **THEN** files are written to disk, overwriting existing charts if present

#### Scenario: Skip existing charts without force

- **WHEN** a chart directory already exists and `--force` is not set
- **THEN** the system skips the chart with a `[SKIP]` message

#### Scenario: Already helm format without force

- **WHEN** the project is already detected as Helm format and `--force` is not set
- **THEN** the system returns an error indicating the project already uses Helm-aligned format

### Requirement: Interpolation Conversion

The system SHALL convert legacy `${var}` interpolation syntax to Go template syntax during Helm migration.

Conversion rules:

- `${name}` converts to `{{ .Chart.Name }}` (service name is always chart-level)
- `${sidecar}` converts to `{{ .Values.sidecar }}`
- All other `${var}` patterns convert to `{{ .Values.var }}`
- Non-interpolation `$` characters (without braces) are preserved

#### Scenario: Convert service name variable

- **WHEN** a provision contains `${name}` in service context
- **THEN** it becomes `{{ .Chart.Name }}`

#### Scenario: Convert config variable

- **WHEN** a provision contains `${port}` or `${image}`
- **THEN** they become `{{ .Values.port }}` and `{{ .Values.image }}`

#### Scenario: Preserve non-interpolation dollar signs

- **WHEN** a template contains `$PATH` or other `$` without braces
- **THEN** the characters are preserved unchanged

#### Scenario: Add manifest header during conversion

- **WHEN** a converted provision file is missing `apiVersion` and `kind`
- **THEN** `apiVersion: bosun.io/v1` and `kind: Template` are prepended

### Requirement: Dual Format Support

The system SHALL support both legacy and Helm-aligned formats during the migration period.

The provision command MUST:

- Auto-detect format using `DetectFormat` and use the appropriate renderer
- Support `--format` flag to override auto-detection
- Support legacy rendering with `${var}` interpolation
- Support Helm rendering with Go templates and chart loader

#### Scenario: Render legacy format

- **WHEN** format is detected as legacy
- **THEN** the system uses `RenderStack` with provision-based rendering and `${var}` interpolation

#### Scenario: Render helm format

- **WHEN** format is detected as helm
- **THEN** the system uses `ChartLoader.RenderStack` with Go template rendering

#### Scenario: Override format detection

- **WHEN** the user specifies `--format=helm`
- **THEN** the Helm renderer is used regardless of directory structure

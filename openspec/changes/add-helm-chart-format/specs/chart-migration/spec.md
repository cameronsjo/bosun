## ADDED Requirements

### Requirement: Format Auto-Detection

The system SHALL automatically detect manifest format (helm vs legacy) based on directory structure.

Detection rules:
- If `charts/` directory exists → helm format
- If `manifest/provisions/` exists → legacy format
- Default → legacy format

#### Scenario: Helm format detection
- **WHEN** the project contains a `charts/` directory
- **THEN** the system uses helm format processing

#### Scenario: Legacy format detection
- **WHEN** the project contains only `manifest/provisions/`
- **THEN** the system uses legacy format processing

#### Scenario: Both formats present
- **WHEN** both `charts/` and `manifest/provisions/` exist
- **THEN** the system prefers helm format

### Requirement: Migration Command

The system SHALL provide a `bosun migrate helm` command to convert legacy manifests to helm format.

The command MUST:
- Convert `manifest/provisions/` → `charts/templates/`
- Convert `manifest/services/` → `charts/{service}/`
- Convert `manifest/stacks/` → `stacks/{stack}/`
- Transform `${var}` syntax → `{{ .Values.var }}`

#### Scenario: Migrate provisions to templates
- **WHEN** user runs `bosun migrate helm`
- **THEN** files in `manifest/provisions/` are copied to `charts/templates/`

#### Scenario: Migrate service to chart
- **WHEN** a service YAML is migrated
- **THEN** a Chart.yaml and values.yaml are generated

#### Scenario: Dry run mode
- **WHEN** user runs `bosun migrate helm --dry-run`
- **THEN** changes are displayed but not written

### Requirement: Interpolation Conversion

The system SHALL convert legacy `${var}` interpolation to Go template syntax.

Conversion rules:
- `${name}` → `{{ .Chart.Name }}` (for service name)
- `${var}` → `{{ .Values.var }}` (for other variables)
- Preserve non-interpolation `$` characters

#### Scenario: Convert simple variable
- **WHEN** a template contains `${port}`
- **THEN** it becomes `{{ .Values.port }}`

#### Scenario: Convert service name
- **WHEN** a template contains `${name}` in service context
- **THEN** it becomes `{{ .Chart.Name }}`

#### Scenario: Preserve shell variables
- **WHEN** a template contains `$PATH` or `$$`
- **THEN** the characters are preserved unchanged

### Requirement: Dual Format Support

The system SHALL support both legacy and helm formats during the migration period.

The provision command MUST:
- Auto-detect format and use appropriate renderer
- Support `--format` flag to override detection
- Produce identical compose output regardless of source format

#### Scenario: Render legacy format
- **WHEN** format is detected as legacy
- **THEN** use legacy renderer with `${var}` interpolation

#### Scenario: Render helm format
- **WHEN** format is detected as helm
- **THEN** use Go template renderer with context

#### Scenario: Override format detection
- **WHEN** user specifies `--format=helm`
- **THEN** use helm renderer regardless of directory structure

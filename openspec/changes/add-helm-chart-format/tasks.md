## 1. Core Types and Engine

- [x] 1.1 Add Chart, ChartDependency, Template types to `internal/manifest/types.go`
- [x] 1.2 Add TemplateContext, ChartMeta, DependencyInfo types
- [x] 1.3 Create Go template engine in `internal/manifest/template.go`
- [x] 1.4 Implement Sprig function integration
- [x] 1.5 Add custom helpers (`include`, `nindent`, `toYaml`)

## 2. Chart Loading and Rendering

- [x] 2.1 Create ChartLoader in `internal/manifest/chart.go`
- [x] 2.2 Implement Chart.yaml parsing
- [x] 2.3 Implement values.yaml loading and merging
- [x] 2.4 Implement `_helpers.tpl` loading
- [x] 2.5 Add template rendering with context
- [x] 2.6 Implement dependency resolution and rendering

## 3. Configuration and Format Detection

- [x] 3.1 Add `ChartsDir()`, `TemplatesDir()`, `HelmStacksDir()` to config
- [x] 3.2 Implement `Format()` detection (helm vs legacy)
- [x] 3.3 Add format detection logic based on directory structure

## 4. CLI Commands

- [x] 4.1 Update `provision` command to support both formats
- [x] 4.2 Add `provision chart` subcommand for chart operations
- [x] 4.3 Add `provision chart list` to list available charts
- [x] 4.4 Add `provision chart show` to display chart details
- [x] 4.5 Add `provision template` subcommand for template operations
- [x] 4.6 Add `provision template list` to list available templates

## 5. Migration Tooling

- [x] 5.1 Add `migrate helm` subcommand to `internal/cmd/migrate.go`
- [x] 5.2 Implement provision → template conversion
- [x] 5.3 Implement service → chart conversion
- [x] 5.4 Implement `${var}` → `{{ .Values.var }}` interpolation conversion
- [x] 5.5 Implement stack → Stack.yaml conversion

## 6. Documentation

- [x] 6.1 Create ADR-0011 for Helm alignment decision
- [x] 6.2 Create `docs/helm-alignment.md` user documentation
- [x] 6.3 Update docs with correct project name (unops → bosun)

## 7. Testing

- [x] 7.1 Fix TestProvisionCmd_RequiresStackName for new usage text
- [ ] 7.2 Add unit tests for Go template engine
- [ ] 7.3 Add unit tests for ChartLoader
- [ ] 7.4 Add integration tests for chart rendering
- [ ] 7.5 Add tests for migration command

# Change: Add Helm-Aligned Chart Format

## Why

Bosun's custom terminology (provisions, config, `${var}` interpolation) creates friction for users familiar with Helm/Kubernetes. LLMs and developers can't apply their Helm knowledge directly, requiring extra learning for what are conceptually similar patterns.

## What Changes

- **Terminology alignment**: `provisions` → `templates`, `config` → `values`, `ServiceManifest` → `Chart`
- **Directory structure**: `manifest/provisions/` → `charts/templates/`, services → chart directories
- **Templating engine**: `${var}` interpolation → Go templates with Sprig functions (`{{ .Values.var }}`)
- **Chart metadata**: New `Chart.yaml` format with apiVersion, kind, name, version, description, homepage
- **Dependency model**: `needs:`/`services:` → `dependencies:` with version, values, and compose overrides
- **Migration tooling**: `bosun migrate helm` command to convert legacy format

## Impact

- Affected specs: manifest-system (new), chart-templating (new), chart-migration (new)
- Affected code:
  - `internal/manifest/types.go` - Chart, Template, ChartDependency types
  - `internal/manifest/template.go` - Go template engine
  - `internal/manifest/chart.go` - ChartLoader
  - `internal/config/config.go` - Format detection, directory methods
  - `internal/cmd/provision.go` - Extended with chart/template subcommands
  - `internal/cmd/migrate.go` - Helm migration subcommand
- Documentation: `docs/helm-alignment.md`, `docs/adr/0011-helm-alignment.md`

## Implementation Status

**Implemented** - Code merged in commits:
- `a652fd0` feat(manifest): add Helm-aligned chart format
- `249c034` docs: replace unops references with bosun

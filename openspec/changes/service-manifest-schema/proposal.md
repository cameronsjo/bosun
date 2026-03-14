# Change: Service Manifest Schema — Single Source of Truth for Service Definitions

## Why

Service definitions are scattered across provisions, config maps, compose overrides, and Traefik label conventions. There is no declarative schema that captures everything about a service in one place. This fragmentation means consumers (compose generation, Traefik routing, port registry, drift detection) each independently extract partial information from rendered output rather than reading from a canonical source. A unified service schema would enable structured validation, richer drift detection (port/volume/healthcheck drift, not just image drift), automated Traefik route generation, and a future port registry.

## What Changes

- **New `ServiceSpec` schema**: A structured, declarative schema within both `ServiceManifest` (legacy) and `Chart` (Helm-aligned) that captures image, ports, volumes, environment, healthcheck, labels, Traefik routing, dependencies, and update policy in a single typed definition
- **Schema normalization**: After rendering provisions/templates, the system normalizes the resulting compose output into a `ServiceSpec` for downstream consumers
- **Consumer interface**: A `ResolvedService` type that consumers (compose generation, Traefik config, drift detection) can query without parsing raw YAML maps
- **Drift detection enrichment**: Drift detection compares `ResolvedService` fields (ports, volumes, healthcheck, image) against actual Docker state, not just image strings
- **Validation**: `bosun lint` gains schema-level validation (port conflicts, missing healthchecks, invalid Traefik rules) before rendering

## Impact

- Affected specs: manifest-system
- Affected code:
  - `internal/manifest/types.go` — new `ServiceSpec`, `PortMapping`, `VolumeMount`, `HealthcheckSpec`, `TraefikRoute`, `UpdatePolicy`, `ResolvedService` types
  - `internal/manifest/render.go` — normalize rendered output into `ResolvedService` structs
  - `internal/manifest/chart.go` — normalize chart-rendered output into `ResolvedService` structs
  - `internal/manifest/validate.go` — schema-level validation for `ServiceSpec` fields
  - `internal/reconcile/drift.go` — compare `ResolvedService` fields against `ActualService`
  - `internal/cmd/provision.go` — surface `ResolvedService` info in provision output
  - `internal/cmd/lint.go` — add schema validation checks
  - `internal/cmd/drift.go` — display enriched drift (ports, volumes, healthcheck)
- All consumers:
  - `internal/manifest/render.go:RenderService()` — produces compose output from `ServiceManifest`
  - `internal/manifest/chart.go:RenderChart()` — produces compose output from `Chart`
  - `internal/manifest/template.go:RenderChart()` — template engine rendering
  - `internal/reconcile/drift.go:ExtractDeclaredState()` — parses rendered compose for drift comparison
  - `internal/reconcile/drift.go:CompareDrift()` — compares declared vs actual
  - `internal/cmd/provision.go` — CLI provision/render commands
  - `internal/cmd/lint.go` — manifest validation CLI
  - `internal/cmd/drift.go` — drift display CLI

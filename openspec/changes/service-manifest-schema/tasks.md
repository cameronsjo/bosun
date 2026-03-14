## 1. Core Schema Types

- [ ] 1.1 Add `ServiceSpec` struct to `internal/manifest/types.go` with fields: image, ports, volumes, environment, healthcheck, labels, traefik, dependencies, update policy
- [ ] 1.2 Add `PortMapping` struct (host port, container port, protocol)
- [ ] 1.3 Add `VolumeMount` struct (source, target, read-only flag)
- [ ] 1.4 Add `HealthcheckSpec` struct (test command, interval, timeout, retries, start period)
- [ ] 1.5 Add `TraefikRoute` struct (rule, entrypoints, TLS, middlewares, priority)
- [ ] 1.6 Add `UpdatePolicy` struct (restart policy, pull policy)
- [ ] 1.7 Add `ResolvedService` struct aggregating `ServiceSpec` with resolved name and stack context

## 2. Schema Normalization

- [ ] 2.1 Implement `NormalizeComposeToServiceSpec()` — extract `ServiceSpec` from rendered compose `map[string]any`
- [ ] 2.2 Implement `NormalizeTraefikLabels()` — extract `TraefikRoute` from Docker labels in compose output
- [ ] 2.3 Update `RenderService()` to return `ResolvedService` alongside `RenderOutput`
- [ ] 2.4 Update `ChartLoader.RenderChart()` to return `ResolvedService` alongside `RenderOutput`
- [ ] 2.5 Update `RenderStack()` and `ChartLoader.RenderStack()` to collect `[]ResolvedService`

## 3. Validation

- [ ] 3.1 Add `ValidateServiceSpec()` — check for port conflicts, empty image, invalid port ranges
- [ ] 3.2 Add `ValidateTraefikRoute()` — check rule syntax, entrypoint existence
- [ ] 3.3 Integrate schema validation into `bosun lint` command
- [ ] 3.4 Add cross-service port conflict detection within a stack

## 4. Drift Detection Enrichment

- [ ] 4.1 Update `DeclaredService` to include `ServiceSpec` fields (ports, volumes, healthcheck)
- [ ] 4.2 Update `ExtractDeclaredState()` to populate enriched fields from compose output
- [ ] 4.3 Add `DriftPortMismatch`, `DriftVolumeMismatch`, `DriftHealthcheckMissing` drift types
- [ ] 4.4 Update `CompareDrift()` to compare ports, volumes, healthcheck against Docker inspect data
- [ ] 4.5 Update drift CLI output to display enriched drift details
- [ ] 4.6 Update `skills/onboard/resources/gitops.md` to document enriched drift types (port, volume, healthcheck) and Docker inspect requirement

## 5. CLI Integration

- [ ] 5.1 Update `bosun provision` output to show resolved service summary (ports, routes, dependencies)
- [ ] 5.2 Update `bosun drift` output format for new drift types

## 6. Testing

- [ ] 6.1 Unit tests for `NormalizeComposeToServiceSpec()` with various compose shapes
- [ ] 6.2 Unit tests for `NormalizeTraefikLabels()` extraction
- [ ] 6.3 Unit tests for `ValidateServiceSpec()` (port conflicts, empty image, etc.)
- [ ] 6.4 Unit tests for enriched drift comparison (port, volume, healthcheck drift)
- [ ] 6.5 Integration tests for full render → normalize → validate pipeline

## ADDED Requirements

### Requirement: Service Specification Schema

The system SHALL provide a typed `ServiceSpec` schema that captures the canonical definition of a service's runtime characteristics. `ServiceSpec` is a normalization target — it is extracted from rendered compose output, not authored directly by users.

A `ServiceSpec` MAY contain:

- `image`: Container image reference (registry/name:tag); the `image` field MAY be empty for build-only services that define a `build` context instead of a pre-built image

- `ports`: List of `PortMapping` entries (host port, container port, protocol)
- `volumes`: List of `VolumeMount` entries (source, target, read-only)
- `environment`: Map of environment variable names to values
- `healthcheck`: `HealthcheckSpec` (test command, interval, timeout, retries, start period)
- `labels`: Map of Docker labels
- `traefik`: List of `TraefikRoute` entries extracted from Docker labels
- `dependencies`: List of service names this service depends on
- `updatePolicy`: `UpdatePolicy` (restart policy, pull policy)
- `networks`: List of network names the service connects to

#### Scenario: Normalize compose output to ServiceSpec (legacy path)

- **WHEN** `RenderService()` produces a compose `map[string]any` for a service with image, ports, volumes, and healthcheck
- **THEN** `NormalizeComposeToServiceSpec()` extracts a `ServiceSpec` with all fields populated from the compose service definition

#### Scenario: Normalize compose output to ServiceSpec (Helm path)

- **WHEN** `ChartLoader.RenderChart()` produces a compose `map[string]any` for a chart with templates
- **THEN** `NormalizeComposeToServiceSpec()` extracts a `ServiceSpec` for each service in the compose output

#### Scenario: Extract Traefik routes from labels

- **WHEN** a compose service has `traefik.http.routers.*` labels
- **THEN** `NormalizeTraefikLabels()` extracts `TraefikRoute` entries with rule, entrypoints, TLS, and middleware fields

#### Scenario: Missing image returns empty ServiceSpec

- **WHEN** a compose service definition has no `image` field (e.g., build-only service)
- **THEN** `NormalizeComposeToServiceSpec()` returns a `ServiceSpec` with an empty `image` field and all other extractable fields populated

### Requirement: Resolved Service

The system SHALL produce `ResolvedService` structs that aggregate `ServiceSpec` with context (service name, stack name, format origin). Consumers use `ResolvedService` instead of parsing raw `map[string]any` output.

A `ResolvedService` MUST contain:

- `name`: Service name as it appears in Docker Compose
- `spec`: The `ServiceSpec` for this service

A `ResolvedService` MAY contain:

- `stack`: Stack name if rendered as part of a stack
- `format`: Origin format (`"legacy"` or `"helm"`)

#### Scenario: RenderService returns ResolvedService (legacy)

- **WHEN** `RenderService()` completes successfully
- **THEN** the returned result includes both the `RenderOutput` and a slice of `ResolvedService` for each service in the compose output

#### Scenario: RenderChart returns ResolvedService (Helm)

- **WHEN** `ChartLoader.RenderChart()` completes successfully
- **THEN** the returned result includes both the `RenderOutput` and a slice of `ResolvedService` for each service in the compose output

#### Scenario: RenderStack collects all ResolvedServices

- **WHEN** `RenderStack()` renders a stack with multiple services
- **THEN** the result includes a `[]ResolvedService` combining all services across all included service manifests or charts

### Requirement: Service Spec Validation

The system SHALL validate `ServiceSpec` fields for correctness and consistency. Validation is invoked by `bosun lint` and optionally during rendering.

#### Scenario: Port conflict within a service

- **WHEN** a `ServiceSpec` has two `PortMapping` entries with the same host port
- **THEN** validation returns an error identifying the conflicting port

#### Scenario: Port conflict across a stack

- **WHEN** two `ResolvedService` entries in a stack have `PortMapping` entries binding the same host port
- **THEN** validation returns an error identifying the conflicting services and port

#### Scenario: Invalid port range

- **WHEN** a `PortMapping` has a port number outside 1-65535
- **THEN** validation returns an error for the invalid port

#### Scenario: Empty image warning

- **WHEN** a `ServiceSpec` has an empty `image` field
- **THEN** validation emits a warning (not an error, since build-only services are valid)

#### Scenario: Traefik rule syntax validation

- **WHEN** a `TraefikRoute` has a `rule` field
- **THEN** validation checks for common syntax issues (unmatched backticks, at least one recognized Traefik matcher: Host, HostRegexp, Path, PathPrefix, PathRegexp, Method, Header/Headers, HeaderRegexp/HeadersRegexp, Query, QueryRegexp, ClientIP)

### Requirement: Enriched Drift Detection

The system SHALL compare `ResolvedService` fields against actual Docker state for richer drift detection beyond image-only comparison. Enriched drift requires Docker inspect data.

New drift types:

- `DriftPortMismatch`: Declared port binding differs from actual published ports
- `DriftVolumeMismatch`: Declared volume mount differs from actual container mounts
- `DriftHealthcheckMissing`: Declared healthcheck exists but container has no healthcheck configured

#### Scenario: Port drift detected

- **WHEN** a `ResolvedService` declares port `8080:80` but the running container publishes `9090:80`
- **THEN** drift detection reports a `DriftPortMismatch` item with expected and actual port details

#### Scenario: Volume drift detected

- **WHEN** a `ResolvedService` declares a volume mount `/data:/app/data` but the running container has no such mount
- **THEN** drift detection reports a `DriftVolumeMismatch` item

#### Scenario: Healthcheck drift detected

- **WHEN** a `ResolvedService` declares a healthcheck but the running container reports no healthcheck
- **THEN** drift detection reports a `DriftHealthcheckMissing` item

#### Scenario: No enriched drift when fields match

- **WHEN** all declared ports, volumes, and healthcheck match the running container state
- **THEN** no enriched drift items are reported (only standard image/state drift if applicable)

#### Scenario: Enriched drift requires Docker inspect

- **WHEN** drift detection runs with enriched mode enabled
- **THEN** the system calls Docker inspect for each service to obtain port, volume, and healthcheck details
- **AND** this is separate from the container list call used for basic drift

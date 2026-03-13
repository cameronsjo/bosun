## Context

Bosun currently defines services through two paths: legacy `ServiceManifest` (provisions + config + compose overrides) and Helm-aligned `Chart` (templates + values + dependencies). Both produce untyped `map[string]any` compose output. Consumers like drift detection, Traefik config generation, and the lint command each independently parse this untyped output, leading to duplicated extraction logic, incomplete validation, and drift detection limited to image-only comparison.

This proposal introduces a typed `ServiceSpec` as a normalization layer between rendering and consumption. The rendering pipeline continues to produce `map[string]any` compose output (preserving full Docker Compose compatibility), but a normalization step extracts structured `ResolvedService` data for consumers that need typed access.

## Goals / Non-Goals

- Goals:
  - Single typed schema capturing service identity, networking, storage, health, routing, and update policy
  - Normalization from rendered compose output to typed structs (works with both legacy and Helm formats)
  - Richer drift detection beyond image-only comparison
  - Schema-level validation in `bosun lint`
  - Foundation for future port registry and service catalog features

- Non-Goals:
  - Replacing the existing `ServiceManifest` or `Chart` authoring formats (users keep writing what they write today)
  - Runtime service discovery or registration
  - Changing the rendered compose output format
  - Multi-host service mesh or networking

## Decisions

- **Normalization layer, not authoring format**: `ServiceSpec` is extracted from rendered output, not written by users. This avoids a third authoring format and preserves backwards compatibility with both legacy and Helm paths.
  - *Alternative*: Require users to write `ServiceSpec` directly in manifests. Rejected because it duplicates compose fields and forces migration.

- **Post-render extraction**: Normalization happens after provisions/templates produce compose output, not before. This means the typed schema always reflects what Docker Compose will actually see.
  - *Alternative*: Pre-render typed schema that generates compose. Rejected because it limits Docker Compose features to what the schema models.

- **Traefik labels as structured data**: Traefik routing is extracted from Docker labels (the `traefik.http.*` label convention) into a `TraefikRoute` struct. This enables validation (rule syntax, duplicate routes) without changing how Traefik configuration works.

- **Incremental drift enrichment**: New drift types (port mismatch, volume mismatch, healthcheck missing) are added alongside existing types. The comparison requires Docker inspect data for port/volume details, which is already available via the Docker SDK.

## Risks / Trade-offs

- **Schema lag**: The typed schema may not cover every Docker Compose field. Mitigation: `ServiceSpec` captures the most commonly drifted/validated fields; raw compose output remains the source of truth for Docker Compose execution.
- **Performance**: Normalization adds a parsing step after rendering. Mitigation: normalization is lightweight string/map extraction; no additional I/O.
- **Docker inspect overhead**: Enriched drift comparison needs `docker inspect` for port/volume data, not just `docker ps`. Mitigation: inspect is only called during drift checks (periodic, not on every reconcile), and containers are already listed.

## Open Questions

- Should `ServiceSpec` include resource limits (CPU, memory) for future use, or defer until there is a concrete consumer?
- Should Traefik route validation warn on missing middleware definitions, or only validate syntax?

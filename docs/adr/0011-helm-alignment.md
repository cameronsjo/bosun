# ADR-0011: Helm Terminology and Pattern Alignment

## Status

Accepted

## Context

Bosun is a GitOps tool for Docker Compose that uses custom terminology rooted in a nautical metaphor:

- **Provisions** - Reusable YAML fragments
- **Config** - Variables for interpolation
- **ServiceManifest** - Service definition
- **Stack** - Collection of services
- **`${var}` syntax** - Variable interpolation

While the nautical theme is memorable, it creates friction for users and LLMs familiar with the Kubernetes/Helm ecosystem. Helm's terminology is industry-standard and widely understood:

- **Templates** - Reusable configuration fragments
- **Values** - Configuration variables
- **Chart** - Packaged application definition
- **Dependencies** - Required sub-charts

The current `${var}` interpolation syntax, while simple, differs from Helm's Go template syntax (`{{ .Values.var }}`). Go templates offer more power (conditionals, loops, functions) and are familiar to anyone who has worked with Helm, Hugo, or Go's standard library.

Additionally, services lack structured metadata (version, description, homepage) that would enable better tooling and documentation.

### Goals

1. Reduce cognitive load for Helm/K8s users
2. Enable LLMs to apply Helm knowledge to Bosun
3. Add structured metadata for charts
4. Provide more powerful templating capabilities
5. Maintain Docker Compose as the deployment target (not K8s)

## Decision

Align Bosun's terminology, structure, and templating with Helm conventions where applicable, while preserving Docker Compose as the deployment target.

### Terminology Changes

| Before | After | Rationale |
|--------|-------|-----------|
| Provision | Template | Helm-standard term for reusable fragments |
| `provisions:` field | `templates:` field | Consistency with above |
| `config:` block | `values.yaml` file | Helm-standard, enables layered overrides |
| ServiceManifest | Chart | A chart is a packaged application |
| `needs:`/`services:` | `dependencies:` | Helm-standard term |
| `manifest/` directory | `charts/` directory | Helm-standard structure |

**Preserved terminology:**
- **Stack** - Kept as-is. Represents a collection of charts (similar to Helm's "umbrella chart" but Stack is clearer for Docker Compose context)

### Directory Structure

```
charts/
├── templates/                    # Shared templates (was provisions/)
│   ├── _helpers.tpl              # Common template functions
│   ├── container.yaml
│   ├── healthcheck.yaml
│   ├── reverse-proxy.yaml
│   └── webapp.yaml               # Composite template
│
├── norish/                       # Each service is a chart
│   ├── Chart.yaml                # Metadata + templates + dependencies
│   └── values.yaml               # Default values
│
└── immich/
    ├── Chart.yaml
    └── values.yaml

stacks/
└── apps/
    ├── Stack.yaml                # Stack definition
    └── values.yaml               # Stack-level value overrides
```

### Chart.yaml Format

```yaml
apiVersion: bosun.io/v1
kind: Chart
name: norish
version: 1.2.0
description: Recipe manager and meal planner
homepage: https://github.com/norishapp/norish

templates:
  - container
  - healthcheck
  - reverse-proxy
  - homepage
  - monitoring

dependencies:
  - name: postgres
    version: "17"
    values:
      db: norish
      db_user: norish

# Optional: chart-specific compose overrides
compose:
  services:
    {{ .Chart.Name }}:
      volumes:
        - {{ .Chart.Name }}-data:/app/data
```

### Go Template Syntax

Replace `${var}` interpolation with Go templates:

**Before:**
```yaml
compose:
  services:
    ${name}:
      image: ${image}
      ports:
        - "${port}:${port}"
```

**After:**
```yaml
compose:
  services:
    {{ .Chart.Name }}:
      image: {{ .Values.image }}
      ports:
        - "{{ .Values.port }}:{{ .Values.port }}"
```

**Template context available:**
```go
type TemplateContext struct {
    Chart  ChartMeta              // .Chart.Name, .Chart.Version, etc.
    Values map[string]any         // .Values.* from values.yaml
    Deps   map[string]Dependency  // .Deps.postgres.Host, etc.
}
```

**Helper functions** via `_helpers.tpl`:
```yaml
{{- define "bosun.labels" -}}
labels:
  bosun.io/managed: "true"
  bosun.io/chart: {{ .Chart.Name }}
{{- end -}}
```

**Usage:**
```yaml
{{ include "bosun.labels" . | nindent 6 }}
```

### Dependency Values Model

Dependencies support three layers of configuration:

1. **Version** - Most common, selects dependency version
2. **Values** - Template variables passed to dependency
3. **Compose** - Raw overrides for edge cases (network customization)

```yaml
dependencies:
  # Simple: just version
  - name: redis
    version: "7"

  # With values: custom database config
  - name: postgres
    version: "17"
    values:
      db: myapp
      db_user: myapp

  # With compose override: custom networking
  - name: postgres
    version: "17"
    values:
      db: glean
    compose:
      networks:
        - glean-net
```

### Implicit Raw Mode

Services without `templates:` that define `compose:` directly are treated as "raw" - no template processing, compose block used as-is. This handles complex services like immich that need custom images (pgvecto.rs) or multi-component architectures.

```yaml
# charts/immich/Chart.yaml
apiVersion: bosun.io/v1
kind: Chart
name: immich
version: 1.0.0
description: Photo and video management

# No templates: [] - compose block used directly
compose:
  services:
    immich:
      image: ghcr.io/immich-app/immich-server:{{ .Values.version }}
    immich-db:
      image: tensorchord/pgvecto-rs:pg17  # Custom image, can't use postgres template
    immich-ml:
      image: ghcr.io/immich-app/immich-machine-learning:{{ .Values.version }}
```

### Values Precedence

From lowest to highest priority:
1. Template defaults (in template files)
2. Chart `values.yaml`
3. Stack `values.yaml`
4. CLI `--set` flags

### Stack Format

```yaml
# stacks/apps/Stack.yaml
apiVersion: bosun.io/v1
kind: Stack
name: apps
description: User-facing applications

charts:
  - name: norish
  - name: stirling-pdf
  - name: paperless
    values:
      subdomain: docs  # Per-chart override

networks:
  proxynet:
    external: true
  internal:
```

## Consequences

### Pros

- **Reduced friction** - Helm users immediately understand the model
- **LLM compatibility** - Claude, GPT, etc. can apply Helm knowledge directly
- **Powerful templating** - Conditionals, loops, Sprig functions
- **Structured metadata** - Version, description, homepage in Chart.yaml
- **Layered values** - Clear precedence for configuration overrides
- **Industry alignment** - Follows established patterns

### Cons

- **Breaking change** - Existing manifests must migrate
- **Template complexity** - Go templates more verbose than `${var}`
- **Learning curve** - Users unfamiliar with Go templates need to learn syntax
- **Migration effort** - Tooling needed to convert existing services

### Migration Path

1. **Phase 1**: Support both old and new formats (detect by directory structure)
2. **Phase 2**: Provide `bosun migrate` command to convert old → new
3. **Phase 3**: Deprecation warnings for old format
4. **Phase 4**: Remove old format support in next major version

## Alternatives Considered

| Alternative | Why Not |
|-------------|---------|
| Keep `${var}` syntax | Simpler but non-standard, no conditionals/loops |
| Support both syntaxes | Complexity, inconsistent codebases |
| Jinja2 templates | Python dependency, not Go-native |
| YAML anchors only | Too limited for real-world needs |
| Full Helm compatibility | Overkill - we're targeting Docker Compose, not K8s |

## References

- [Helm Chart Structure](https://helm.sh/docs/topics/charts/)
- [Go text/template](https://pkg.go.dev/text/template)
- [Sprig Template Functions](https://masterminds.github.io/sprig/)
- [ADR-0010: Go Rewrite](0010-go-rewrite.md) - Already uses text/template + Sprig

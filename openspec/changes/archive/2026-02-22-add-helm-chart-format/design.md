## Context

Bosun is "Helm for home" but uses custom terminology that doesn't match Helm conventions. Users and LLMs familiar with Helm need to learn Bosun-specific terms, creating unnecessary cognitive overhead. The goal is to align with Helm where it makes sense while preserving Docker Compose as the deployment target.

**Stakeholders:**
- Homelab users familiar with Helm
- LLMs (Claude, GPT) with Helm training data
- Existing Bosun users with legacy manifests

## Goals / Non-Goals

**Goals:**
- Reduce cognitive load for Helm/K8s users
- Enable LLMs to apply Helm knowledge directly
- Add structured metadata (version, description) for charts
- Provide powerful templating (conditionals, loops, functions)
- Maintain backwards compatibility during transition

**Non-Goals:**
- Full Helm compatibility (we deploy to Docker Compose, not K8s)
- Kubernetes resource types or concepts
- Tiller or Helm 2.x patterns
- Complex release management (releases, rollbacks)

## Decisions

### 1. Terminology Mapping

| Before | After | Rationale |
|--------|-------|-----------|
| Provision | Template | Helm-standard term |
| `config:` | `values.yaml` | Helm-standard, enables layering |
| ServiceManifest | Chart | A chart is a packaged application |
| `needs:`/`services:` | `dependencies:` | Helm-standard term |

**Preserved:** Stack (clearer than "umbrella chart" for Docker Compose)

### 2. Go Templates with Sprig

Replace `${var}` interpolation with Go templates. Provides:
- Conditionals: `{{ if .Values.enabled }}`
- Loops: `{{ range .Values.ports }}`
- Functions: Sprig library (80+ functions)
- Helpers: `_helpers.tpl` for reusable snippets

Template context:
```go
type TemplateContext struct {
    Chart  ChartMeta              // .Chart.Name, .Chart.Version
    Values map[string]any         // .Values.* from values.yaml
    Deps   map[string]Dependency  // .Deps.postgres.Host
}
```

### 3. Chart.yaml Format

```yaml
apiVersion: bosun.io/v1
kind: Chart
name: service-name
version: 1.0.0
description: Service description
homepage: https://example.com

templates:
  - container
  - healthcheck

dependencies:
  - name: postgres
    version: "17"
    values:
      db: myapp
```

### 4. Implicit Raw Mode

Charts without `templates:` but with `compose:` are treated as raw - no template processing. Handles complex services like immich that can't use standard templates.

### 5. Values Precedence

From lowest to highest priority:
1. Template defaults
2. Chart `values.yaml`
3. Stack `values.yaml`
4. CLI `--set` flags

**Alternatives Considered:**
- Keep `${var}` syntax: Simpler but no conditionals/loops
- Jinja2 templates: Python dependency, not Go-native
- Support both syntaxes: Complexity, inconsistent codebases

## Risks / Trade-offs

| Risk | Mitigation |
|------|------------|
| Breaking change for legacy users | Support both formats with auto-detection |
| Go templates more verbose | Provide `_helpers.tpl` with common patterns |
| Learning curve for new syntax | Migration tool, documentation, examples |

## Migration Plan

1. **Phase 1 (current)**: Support both formats via auto-detection
2. **Phase 2**: Provide `bosun migrate helm` command
3. **Phase 3**: Deprecation warnings for legacy format
4. **Phase 4**: Remove legacy support in next major version

**Rollback:** Legacy format remains fully functional; users can delay migration

## Open Questions

- None currently - design approved and implemented

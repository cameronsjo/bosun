# Helm-Aligned Chart Format

Bosun supports a Helm-aligned chart format that uses familiar terminology and patterns from the Kubernetes Helm ecosystem while targeting Docker Compose deployments.

> **See Also:** [ADR-0011: Helm Alignment](adr/0011-helm-alignment.md) for the design decision rationale.

## Overview

The Helm-aligned format provides:

- **Familiar terminology**: Charts, Templates, Values, Dependencies
- **Go template syntax**: `{{ .Values.port }}` instead of `${port}`
- **Structured metadata**: Chart.yaml with version, description, homepage
- **Layered values**: Chart values < Stack values < CLI overrides
- **Helper functions**: Sprig functions + custom helpers in `_helpers.tpl`

## Directory Structure

```
charts/
├── templates/                    # Shared templates (reusable fragments)
│   ├── _helpers.tpl              # Common template functions
│   ├── container.yaml
│   ├── healthcheck.yaml
│   ├── reverse-proxy.yaml
│   ├── homepage.yaml
│   ├── monitoring.yaml
│   ├── postgres.yaml
│   ├── redis.yaml
│   └── webapp.yaml               # Composite template
│
├── norish/                       # Chart directory
│   ├── Chart.yaml                # Metadata + templates + dependencies
│   └── values.yaml               # Default values
│
├── stirling-pdf/
│   ├── Chart.yaml
│   └── values.yaml

stacks/
└── apps/
    ├── Stack.yaml                # Stack definition
    └── values.yaml               # Stack-level value overrides
```

## Chart.yaml

The Chart.yaml file defines a service package:

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

### Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `apiVersion` | string | Yes | Schema version (`bosun.io/v1`) |
| `kind` | string | Yes | Must be `Chart` |
| `name` | string | Yes | Chart name |
| `version` | string | No | Semantic version |
| `description` | string | No | Human-readable description |
| `homepage` | string | No | Project URL |
| `templates` | list | No | Template names to apply |
| `dependencies` | list | No | Required sidecars |
| `compose` | map | No | Compose overrides |

## values.yaml

Default configuration values for the chart:

```yaml
image: ghcr.io/norishapp/norish:latest
port: 3000
subdomain: recipes
domain: example.com

# Homepage integration
group: Apps
icon: mdi-food
description: Recipe manager

# Secrets (templated at deploy time)
db_password: "{{ .secrets.apps.norish.db_password }}"
```

## Templates

Templates are reusable configuration fragments using Go template syntax.

### Template Syntax

```yaml
# charts/templates/container.yaml
apiVersion: bosun.io/v1
kind: Template

compose:
  services:
    {{ .Chart.Name }}:
      image: {{ .Values.image }}
      container_name: {{ .Chart.Name }}
      restart: unless-stopped
      environment:
        TZ: {{ .Values.timezone | default "America/Chicago" }}
```

### Template Context

Templates receive a context with these fields:

| Path | Type | Description |
|------|------|-------------|
| `.Chart.Name` | string | Chart name |
| `.Chart.Version` | string | Chart version |
| `.Chart.Description` | string | Chart description |
| `.Values.*` | any | Values from values.yaml |
| `.Deps.<name>.Name` | string | Dependency service name |
| `.Deps.<name>.Host` | string | Dependency hostname |
| `.Deps.<name>.Port` | int | Dependency port |

### Helper Functions

Define reusable snippets in `_helpers.tpl`:

```yaml
# charts/templates/_helpers.tpl

{{- define "bosun.labels" -}}
labels:
  bosun.io/managed: "true"
  bosun.io/chart: {{ .Chart.Name }}
  bosun.io/version: {{ .Chart.Version }}
{{- end -}}

{{- define "bosun.traefik.router" -}}
traefik.http.routers.{{ .Chart.Name }}.rule: Host(`{{ .Values.subdomain }}.{{ .Values.domain }}`)
traefik.http.routers.{{ .Chart.Name }}.entrypoints: websecure
traefik.http.routers.{{ .Chart.Name }}.tls.certresolver: letsencrypt
{{- end -}}
```

Use helpers with `include`:

```yaml
compose:
  services:
    {{ .Chart.Name }}:
      {{- include "bosun.labels" . | nindent 6 }}
```

### Available Functions

All [Sprig functions](https://masterminds.github.io/sprig/) are available, plus:

| Function | Description |
|----------|-------------|
| `include "name" .` | Include a named template |
| `nindent N` | Add newline + indent |
| `toYaml` | Convert value to YAML |

## Dependencies

Declare service dependencies (sidecars):

```yaml
dependencies:
  # Simple: just name and version
  - name: redis
    version: "7"

  # With custom values
  - name: postgres
    version: "17"
    values:
      db: myapp
      db_user: myapp

  # With compose override for custom networking
  - name: postgres
    version: "17"
    values:
      db: glean
    compose:
      networks:
        - glean-net
```

### Dependency Defaults

| Name | Default Version | Default Port |
|------|----------------|--------------|
| postgres | 17 | 5432 |
| redis | 7 | 6379 |
| mysql | 8 | 3306 |
| mongodb | 7 | 27017 |
| chrome | latest | 3000 |

## Stacks

Stacks combine multiple charts:

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
      subdomain: docs

networks:
  proxynet:
    external: true
  internal:
```

### Stack values.yaml

Stack-level values are merged into all charts:

```yaml
# stacks/apps/values.yaml
domain: home.example.com
timezone: America/Chicago
```

### Values Precedence

From lowest to highest priority:

1. Template defaults
2. Chart `values.yaml`
3. Stack `values.yaml`
4. Stack per-chart `values:`
5. CLI `--values` flag

## Implicit Raw Mode

Charts without `templates:` that define `compose:` directly are rendered without template processing:

```yaml
# charts/immich/Chart.yaml
apiVersion: bosun.io/v1
kind: Chart
name: immich
version: 1.0.0
description: Photo and video management

# No templates - compose used directly
compose:
  services:
    immich:
      image: ghcr.io/immich-app/immich-server:{{ .Values.version }}
    immich-db:
      image: tensorchord/pgvecto-rs:pg17
    immich-ml:
      image: ghcr.io/immich-app/immich-machine-learning:{{ .Values.version }}
```

## CLI Commands

```bash
# Render a chart
bosun provision norish

# Render a stack
bosun provision apps

# Dry run
bosun provision -n apps

# With values override
bosun provision -f prod-values.yaml apps

# List charts
bosun chart list

# Show chart details
bosun chart show norish

# List templates
bosun template list
```

## Migration from Legacy Format

Convert existing provisions/services to charts:

```bash
# Preview migration
bosun migrate helm

# Apply migration
bosun migrate helm --force
```

This will:

1. Convert `provisions/*.yml` → `charts/templates/*.yaml`
2. Convert `services/*.yml` → `charts/<name>/Chart.yaml + values.yaml`
3. Convert `stacks/*.yml` → `stacks/<name>/Stack.yaml`
4. Update `${var}` → `{{ .Values.var }}` syntax

After migration, review the generated files and test with `bosun provision -n`.

## Comparison: Legacy vs Helm-Aligned

| Aspect | Legacy | Helm-Aligned |
|--------|--------|--------------|
| Directory | `manifest/provisions/` | `charts/templates/` |
| Service def | Single YAML file | Chart directory |
| Variables | `${var}` | `{{ .Values.var }}` |
| Metadata | In manifest | Separate Chart.yaml |
| Values | Inline `config:` | Separate values.yaml |
| Dependencies | `needs:`/`services:` | `dependencies:` |
| Inheritance | `includes:` | `{{ include }}` |

Both formats are fully supported. Use `bosun migrate helm` to convert.

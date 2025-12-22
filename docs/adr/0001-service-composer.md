# ADR-0001: Service Composer

**Status:** Proposed
**Date:** 2025-12-21
**Author:** Cameron + Claude

## Context

Managing 44+ Docker services across compose, Traefik, and Gatus configs leads to:
- Repetitive boilerplate (healthchecks, labels, networks)
- Inconsistency across services
- Forgotten monitoring endpoints
- Painful bulk updates (change healthcheck pattern → 44 edits)

No existing tool handles compose + reverse proxy + monitoring as a unified service definition.

## Decision

Build a lightweight **Service Composer** that:
1. Reads simple service manifests (~10 lines)
2. Merges reusable profiles (healthcheck, reverse-proxy, monitoring, etc.)
3. Outputs complete configs for compose, Traefik, and Gatus

## Architecture Diagrams

### High-Level Flow

```mermaid
flowchart LR
    subgraph Input
        M[Service Manifest<br/>~10 lines]
        P[Profiles<br/>reusable fragments]
    end

    subgraph Composer
        R[compose.py render]
    end

    subgraph Output
        C[compose/*.yml]
        T[traefik/*.yml]
        G[gatus/*.yml]
    end

    M --> R
    P --> R
    R --> C
    R --> T
    R --> G
```

### Three-Target Generation

```mermaid
flowchart TB
    subgraph "Service Manifest"
        SM["norish.yml<br/>profiles: [container, reverse-proxy, monitoring]<br/>config: {port: 3000, subdomain: recipes}"]
    end

    subgraph "Profile: reverse-proxy.yml"
        RP_C["compose:<br/>  networks: [proxynet]<br/>  labels: {traefik.enable: true}"]
        RP_T["traefik:<br/>  routers: {rule: Host(...)}"]
    end

    subgraph "Profile: monitoring.yml"
        MON_G["gatus:<br/>  endpoints: [{url: https://...}]"]
    end

    subgraph "Output Files"
        OUT_C["compose/norish.yml"]
        OUT_T["traefik/dynamic.yml"]
        OUT_G["gatus/endpoints.yml"]
    end

    SM --> RP_C
    SM --> RP_T
    SM --> MON_G

    RP_C --> OUT_C
    RP_T --> OUT_T
    MON_G --> OUT_G
```

### Render Pipeline

```mermaid
flowchart TD
    A[1. Load Manifest] --> B[2. Load Profiles<br/>raw YAML strings]
    B --> C[3. Interpolate Variables<br/>before parsing]
    C --> D[4. Parse YAML<br/>normalize env/labels to dicts]
    D --> E[5. Deep Merge<br/>into 3 accumulators]
    E --> F{Sidecars<br/>defined?}
    F -->|Yes| G[6. Inject env vars<br/>+ depends_on]
    F -->|No| H[7. Apply Overrides]
    G --> H
    H --> I[8. Render Output Files]

    style C fill:#ffd,stroke:#333
    style G fill:#dfd,stroke:#333
```

### Merge Semantics

```mermaid
flowchart LR
    subgraph "Profile A"
        A_L["labels:<br/>  foo: bar"]
        A_N["networks:<br/>  - internal"]
        A_P["ports:<br/>  - 3000:3000"]
    end

    subgraph "Profile B"
        B_L["labels:<br/>  baz: qux"]
        B_N["networks:<br/>  - proxynet"]
        B_P["ports:<br/>  - 8080:8080"]
    end

    subgraph "Result"
        R_L["labels:<br/>  foo: bar<br/>  baz: qux<br/><i>dict merge</i>"]
        R_N["networks:<br/>  - internal<br/>  - proxynet<br/><i>set union</i>"]
        R_P["ports:<br/>  - 8080:8080<br/><i>list replace</i>"]
    end

    A_L --> R_L
    B_L --> R_L
    A_N --> R_N
    B_N --> R_N
    A_P --> R_P
    B_P --> R_P

    style R_L fill:#dfd
    style R_N fill:#ddf
    style R_P fill:#fdd
```

### Sidecar Injection

```mermaid
flowchart LR
    subgraph "Manifest"
        M["services:<br/>  postgres:<br/>    version: 17<br/>    db: norish"]
    end

    subgraph "Auto-Generated"
        DB["norish-db container<br/>image: postgres:17-alpine<br/>POSTGRES_DB: norish"]
        INJ["Injected into main:<br/>depends_on: [norish-db]<br/>POSTGRES_HOST: norish-db<br/>POSTGRES_USER: postgres<br/>POSTGRES_PASSWORD: ..."]
    end

    M --> DB
    M --> INJ
```

### GitOps Integration

```mermaid
flowchart LR
    subgraph "Local Dev"
        EDIT[Edit manifest<br/>or profile]
        RENDER[compose.py render]
        COMMIT[git commit + push]
    end

    subgraph "GitHub"
        HOOK[Webhook]
    end

    subgraph "Unraid"
        GR[gitops-runner]
        SOPS[SOPS decrypt]
        CHEM[chezmoi render]
        DEPLOY[docker compose up]
    end

    EDIT --> RENDER
    RENDER --> COMMIT
    COMMIT --> HOOK
    HOOK --> GR
    GR --> SOPS
    SOPS --> CHEM
    CHEM --> DEPLOY

    style RENDER fill:#ffd
    style GR fill:#dfd
```

### Profile Composition (Mixin Pattern)

```mermaid
classDiagram
    class ServiceManifest {
        name: string
        profiles: list
        config: dict
        services: dict
    }

    class ContainerProfile {
        compose.services.*
        image, restart, TZ
    }

    class HealthcheckProfile {
        compose.services.*.healthcheck
        test, interval, timeout
    }

    class ReverseProxyProfile {
        compose.services.*.networks
        compose.services.*.labels
        traefik.http.routers.*
        traefik.http.services.*
    }

    class MonitoringProfile {
        gatus.endpoints[]
        url, conditions, alerts
    }

    class PostgresProfile {
        compose.services.${name}-db
        auto-inject env vars
    }

    ServiceManifest --> ContainerProfile : has-a
    ServiceManifest --> HealthcheckProfile : has-a
    ServiceManifest --> ReverseProxyProfile : has-a
    ServiceManifest --> MonitoringProfile : has-a
    ServiceManifest --> PostgresProfile : has-a
```

## Specification

### Service Manifest Format

```yaml
# services/norish.yml
name: norish

profiles:
  - container           # base docker service
  - healthcheck         # wget /health pattern
  - homepage            # dashboard labels
  - reverse-proxy       # exposed via proxy
  - auth                # requires authentication
  - monitoring          # gatus endpoint

config:
  image: norishapp/norish:latest
  port: 3000
  subdomain: recipes
  group: Apps
  icon: mdi-food
  description: Recipe manager
  networks:
    - proxynet
  env:
    OIDC_CLIENT_ID: norish
    OIDC_CLIENT_SECRET: "{{ $secrets.oidc.norish.client_secret }}"

services:
  postgres:
    version: 17
    db: norish
  redis:
    version: 8
```

### Raw Passthrough Mode

For infrastructure or complex services that don't fit the profile model:

```yaml
# services/traefik.yml
name: traefik
type: raw

compose:
  traefik:
    image: traefik:v3.2
    container_name: traefik
    restart: unless-stopped
    # ... full compose spec, copied verbatim
```

### Master Include File

```yaml
# stacks/core.yml
include:
  # Profile-based
  - stirling-pdf.yml
  - llm-council.yml
  - norish.yml

  # Raw passthrough
  - traefik.yml
  - authelia.yml
  - gitops-runner.yml

networks:
  proxynet:
    external: true
  mcp-net:
    external: true
```

### Profiles

Profiles are YAML fragments merged into the final output. No logic, just templates.

| Profile | What it adds |
|---------|--------------|
| `container` | Base service: image, container_name, restart, TZ, volumes |
| `healthcheck` | Healthcheck block with configurable endpoint |
| `homepage` | Homepage dashboard labels |
| `reverse-proxy` | Traefik router + service (no auth) |
| `auth` | Traefik authelia middleware |
| `monitoring` | Gatus endpoint definition |
| `postgres` | PostgreSQL sidecar service |
| `redis` | Redis sidecar service |

### Outputs

Running `compose.py render stacks/core.yml` generates:

| Output | Description |
|--------|-------------|
| `output/compose/core.yml` | Complete Docker Compose file |
| `output/traefik/dynamic.yml` | Traefik routers and services |
| `output/gatus/endpoints.yml` | Gatus monitoring endpoints |

### CLI Interface

```bash
# Render a stack
compose.py render stacks/core.yml

# Render with dry-run (show diff)
compose.py render stacks/core.yml --dry-run

# Validate manifests without rendering
compose.py validate services/*.yml

# List available profiles
compose.py profiles

# Show what a service expands to
compose.py expand services/norish.yml
```

## Directory Structure

```
infrastructure/
├── composer/
│   ├── compose.py          # Main CLI (~100-150 lines)
│   ├── profiles/
│   │   ├── container.yml
│   │   ├── healthcheck.yml
│   │   ├── homepage.yml
│   │   ├── reverse-proxy.yml
│   │   ├── auth.yml
│   │   ├── monitoring.yml
│   │   ├── postgres.yml
│   │   └── redis.yml
│   ├── services/
│   │   ├── stirling-pdf.yml
│   │   ├── llm-council.yml
│   │   ├── norish.yml
│   │   ├── traefik.yml      # raw mode
│   │   └── ...
│   ├── stacks/
│   │   ├── core.yml         # infrastructure
│   │   ├── apps.yml         # user apps
│   │   └── mcp.yml          # MCP servers
│   └── output/              # generated files (gitignored)
│       ├── compose/
│       ├── traefik/
│       └── gatus/
```

## Implementation

### Language: Python

- Maintainable, readable
- Good YAML libraries (ruamel.yaml preserves formatting)
- Easy to extend
- No external dependencies beyond stdlib + ruamel.yaml

### Core Logic Flow

The renderer executes in strict order for safety and predictability:

1. **Load Manifest** - Read service manifest, extract config block
2. **Load & Interpolate Profiles** - Load raw YAML strings, apply `${var}` substitution
   - **Constraint:** Missing variables = fatal error
3. **Parse & Normalize** - Parse interpolated strings, normalize `environment` and `labels` to Dicts
4. **Deep Merge** - Merge profile objects into three target accumulators (compose, traefik, gatus)
5. **Sidecar Injection** - If `services.<sidecar>` present, inject env vars and `depends_on`
6. **Apply Overrides** - Apply manifest's `compose` block overrides
7. **Render** - Output files to `output/`

### Merge Semantics

| Type | Strategy | Example |
|------|----------|---------|
| Dictionaries | Recursive deep merge | labels, environment |
| Lists (default) | **Replace** | ports, volumes |
| Lists (exception) | **Set Union** | networks, depends_on |

**Why replace for lists:** Prevents "garbage collection" issues. Merging port lists could result in unwanted exposures. Later profiles overwrite earlier lists entirely.

### Profile Schema (Multi-Target)

Profiles organize fragments by target output file:

```yaml
# profiles/reverse-proxy.yml
compose:
  services:
    ${name}:
      networks: ["proxynet"]  # Merged via Union
      labels:                  # Merged via Dict
        traefik.enable: "true"

traefik:
  http:
    routers:
      ${name}:
        rule: "Host(`${subdomain}.${domain}`)"
        service: ${name}
```

```yaml
# profiles/monitoring.yml
gatus:
  endpoints:
    - name: ${name}
      group: ${group}
      url: "https://${subdomain}.${domain}"
      interval: 60s
      conditions:
        - "[STATUS] == 200"
```

### Variable Interpolation

Variables are interpolated on **raw strings before YAML parsing** to preserve data types:

```yaml
# profiles/container.yml (raw)
compose:
  services:
    ${name}:
      ports:
        - "${port}:${port}"  # Stays as string "3000:3000"
```

### Sidecar Contract

Sidecars are opinionated to maximize productivity:

**Postgres Profile:**
- Creates service: `${name}-db`
- Volume: `${name}_db_data:/var/lib/postgresql/data`
- **Auto-injects into main service:**
  - `depends_on: ["${name}-db"]`
  - `POSTGRES_HOST: ${name}-db`
  - `POSTGRES_USER`, `POSTGRES_DB`, `POSTGRES_PASSWORD` from config

**Escape hatch:** Don't use sidecar profile if non-standard setup needed. Use `type: raw` or manual env vars.

### SOPS Integration

Secrets stay as SOPS template syntax. Composer passes through unchanged:

```yaml
# In service manifest
env:
  API_KEY: "{{ $secrets.myapp.api_key }}"

# Output unchanged, SOPS/chezmoi handles at deploy time
```

## Guardrails

1. **Composer stays under 250 lines** - if bigger, we're over-engineering
2. **Max 10 profiles** - more means abstraction is wrong
3. **Profiles have no logic** - just YAML fragments with variable substitution
4. **Raw mode is the escape hatch** - don't handle every edge case
5. **No nested profiles** - profiles don't include other profiles
6. **Missing variables = hard error** - no silent failures
7. **Normalize environment/labels to Dicts** - simplifies merge logic

## Success Criteria

- New service: **< 5 minutes** from manifest to deployed
- Profile change: **< 1 minute** to update all services
- Learning curve: **< 15 minutes** to understand the system
- Debugging: Generated YAML is readable, not minified/mangled

## Alternatives Considered

| Alternative | Why not |
|-------------|---------|
| Helm | K8s only, heavyweight |
| Kustomize | K8s only |
| docker-compose `include` | Compose only, no traefik/gatus |
| ytt | Another tool to learn, overkill |
| Jsonnet/Dhall/CUE | Real programming languages, overkill |
| VS Code snippets | Doesn't solve bulk updates |

## Risks

1. **Scope creep** - Mitigate: strict guardrails, raw escape hatch
2. **Edge cases pile up** - Mitigate: use raw mode, don't add flags
3. **Debugging complexity** - Mitigate: keep generated YAML clean, add `expand` command
4. **Adoption friction** - Mitigate: gradual migration, both modes work

## Phases

### Phase 1: Compose Only
- Core renderer with multi-target profile schema
- 5 basic profiles (container, healthcheck, homepage, postgres, redis)
- Raw passthrough mode
- Merge semantics (dict merge, list replace, network union)
- Validate with 3 real services

### Phase 2: Traefik Integration
- reverse-proxy profile
- auth profile
- Generate traefik/dynamic.yml
- `compose.py import` command to scaffold manifests from existing compose files

### Phase 3: Gatus Integration
- monitoring profile
- Generate gatus/endpoints.yml

### Phase 4: Polish
- Dry-run diff output
- Validation command
- Error messages

## Unresolved Dissents

Minor implementation details deferred to developer discretion:

1. **Import Command Priority** - Some argue `import` is critical for Phase 1 adoption (44 services to migrate). Resolution: Build core renderer first; if manual migration too painful, prioritize import.

2. **Sidecar Opt-Out** - Disagreement on explicit `auto_inject: false` flag vs implicit "just don't use the profile". Resolution: Start implicit (convention over configuration); add flags only if edge cases abound.

3. **Networks Definition** - Should be in stack files for validation. Profiles reference by name; renderer validates referenced networks exist.

## Open Questions

1. **Stack organization** - One big core.yml or split by category (infra, apps, mcp)?
2. **Networks** - Defined in stack file or inferred from profiles?
3. **Volumes** - Standard pattern or per-service config?
4. **Secrets path convention** - `$secrets.app.key` or `$secrets.apps.app.key`?

## References

- [GitOps Workflow Guide](obsidian://open?vault=The%20Compendium&file=50%20-%20Home%20Info%2FNetwork%2FGitOps%20Workflow%20Guide)
- [Docker Compose Specification](https://docs.docker.com/compose/compose-file/)
- [Traefik Dynamic Configuration](https://doc.traefik.io/traefik/providers/file/)

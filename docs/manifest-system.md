# Bosun Manifest System

The Bosun manifest system generates Docker Compose, Traefik, and Gatus configurations from declarative service manifests. It uses a provision-based approach where reusable templates are composed together and customized through variable interpolation.

## Overview

The manifest system solves the problem of maintaining consistent infrastructure configurations across multiple services. Instead of duplicating compose files, Traefik routes, and monitoring endpoints, you define a service manifest that references provisions (templates) and provides configuration values.

```
┌─────────────────┐     ┌──────────────┐     ┌─────────────────┐
│ Service Manifest│────▶│   Renderer   │────▶│ Output Files    │
│   (myapp.yml)   │     │              │     │                 │
└─────────────────┘     │  ┌────────┐  │     │ - compose/      │
                        │  │Interp. │  │     │ - traefik/      │
┌─────────────────┐     │  └────────┘  │     │ - gatus/        │
│   Provisions    │────▶│  ┌────────┐  │     └─────────────────┘
│   (templates)   │     │  │ Merge  │  │
└─────────────────┘     │  └────────┘  │
                        └──────────────┘
```

## Manifest Structure

A service manifest defines what to deploy and how to configure it.

### Complete Schema

```yaml
# Service name - used for interpolation and output naming
name: string  # REQUIRED

# Type: "raw" for passthrough mode, omit for normal provisioning
type: string  # OPTIONAL

# Provisions to apply (in order)
provisions:  # OPTIONAL
  - provision-name
  - another-provision

# Variables for interpolation into provisions
config:  # OPTIONAL
  key: value
  port: "8080"
  domain: example.com

# Shorthand for common dependencies with defaults
needs:  # OPTIONAL
  - postgres
  - redis

# Explicit sidecar configuration (overrides defaults)
services:  # OPTIONAL
  postgres:
    version: "17"
    db: mydb
  redis:
    version: "7"

# Raw compose passthrough (only used with type: raw)
compose:  # OPTIONAL
  service-name:
    image: ...
```

### Field Reference

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Service name, used in `${name}` interpolation |
| `type` | string | No | Set to `"raw"` for compose passthrough mode |
| `provisions` | list | No | Provision templates to apply in order |
| `config` | map | No | Variables for interpolation |
| `needs` | list | No | Shorthand for sidecars with defaults |
| `services` | map | No | Explicit sidecar configuration |
| `compose` | map | No | Raw compose config (only with `type: raw`) |

## Variable Interpolation

Variables use the `${varname}` syntax and are replaced before YAML parsing.

### Syntax

```yaml
# In provisions:
compose:
  services:
    ${name}:
      image: ${image}
      container_name: ${name}
      environment:
        PORT: "${port}"
```

### Available Variables

Variables come from multiple sources, applied in order:

1. **Built-in variables:**
   - `${name}` - Service name from manifest

2. **Config variables:**
   - Any key-value pair from the `config` section

3. **Sidecar variables (for sidecar provisions):**
   - `${sidecar}` - Sidecar type (postgres, redis, etc.)
   - Sidecar-specific defaults (see [Sidecars](#sidecars))

### Type Conversion

All variable values are converted to strings:

| Type | Example | Result |
|------|---------|--------|
| string | `"hello"` | `hello` |
| int | `8080` | `8080` |
| bool | `true` | `true` |
| float | `3.14` | `3.14` |

### Error Handling

Missing variables cause an error:

```
error: interpolate provision webapp: missing variables: ${port}, ${domain}
```

## Merge Semantics

When multiple provisions are applied, their outputs are merged using specific strategies.

### Merge Strategies by Key

| Strategy | Keys | Behavior |
|----------|------|----------|
| **Union** | `networks`, `depends_on` | Set union (no duplicates) |
| **Extend** | `endpoints` | Append to list |
| **Replace** | All other lists | Later value replaces earlier |
| **Recursive** | All maps | Deep merge |

### Union Keys

Lists are merged as sets - duplicates are removed:

```yaml
# Provision A:
networks:
  - internal

# Provision B:
networks:
  - internal
  - proxynet

# Result:
networks:
  - internal
  - proxynet
```

### Extend Keys

Lists are appended:

```yaml
# Provision A:
endpoints:
  - name: health

# Provision B:
endpoints:
  - name: metrics

# Result:
endpoints:
  - name: health
  - name: metrics
```

### Environment/Labels Normalization

The `environment` and `labels` keys are normalized from list to map before merging:

```yaml
# Input (list format):
environment:
  - FOO=bar
  - BAZ=qux

# Normalized (map format):
environment:
  FOO: bar
  BAZ: qux
```

This allows provisions to safely merge environment variables:

```yaml
# Provision A:
environment:
  TZ: America/Chicago

# Provision B:
environment:
  DEBUG: "true"

# Result:
environment:
  TZ: America/Chicago
  DEBUG: "true"
```

### Merge Order

1. Provisions are applied in the order listed
2. `needs` sidecars are applied after provisions
3. `services` sidecars are applied last
4. Later values override earlier values (except for union/extend keys)

## Provisions

Provisions are reusable templates that generate output for one or more targets.

### Provision File Structure

```yaml
# Optional: inherit from other provisions
includes:
  - base-provision
  - another-provision

# Output for docker-compose.yml
compose:
  services:
    ${name}:
      image: ${image}
      # ...
  volumes:
    ${name}_data:
  networks:
    internal:

# Output for Traefik dynamic.yml
traefik:
  http:
    routers:
      ${name}:
        rule: Host(`${subdomain}.${domain}`)
        # ...
    services:
      ${name}:
        loadBalancer:
          # ...

# Output for Gatus endpoints.yml
gatus:
  endpoints:
    - name: ${name}
      url: https://${subdomain}.${domain}
      # ...
```

### Inheritance with Includes

Provisions can inherit from other provisions:

```yaml
# webapp.yml
includes:
  - container
  - healthcheck
  - reverse-proxy
  - homepage
  - monitoring
```

**Inheritance chain:**

1. Load all included provisions first
2. Merge them together in order
3. Merge this provision on top

**Cycle detection:**

The system tracks loaded provisions and skips already-loaded ones to prevent infinite loops.

### Built-in Provisions

| Provision | Description | Required Variables |
|-----------|-------------|-------------------|
| `container` | Base container configuration | `image` |
| `healthcheck` | HTTP health check | `port` |
| `reverse-proxy` | Traefik integration | `subdomain`, `domain`, `port` |
| `homepage` | Homepage dashboard labels | `group`, `icon`, `description` |
| `monitoring` | Gatus endpoint | `subdomain`, `domain`, `group` |
| `webapp` | All of the above combined | All of the above |
| `postgres` | PostgreSQL sidecar | `version`, `db`, `db_password` |
| `redis` | Redis sidecar | `version` |

### Creating Custom Provisions

Place `.yml` files in your provisions directory:

```yaml
# provisions/my-custom.yml
compose:
  services:
    ${name}:
      labels:
        my.custom.label: "${custom_value}"
```

## Sidecars

Sidecars are auxiliary services (databases, caches) that run alongside your main service.

### Using `needs` Shorthand

The simplest way to add a sidecar:

```yaml
name: myapp
provisions:
  - container
needs:
  - postgres
  - redis
config:
  image: myapp:latest
  db_password: secret123
```

### Sidecar Defaults

When using `needs`, these defaults are applied:

| Sidecar | Defaults |
|---------|----------|
| `postgres` | `version: "17"`, `db: ${name}`, `db_password: ${db_password}` |
| `redis` | `version: "7"` |
| `mysql` | `version: "8"`, `db: ${name}`, `db_password: ${db_password}` |
| `mongodb` | `version: "7"`, `db: ${name}` |

### Explicit Sidecar Configuration

Override defaults using `services`:

```yaml
name: myapp
provisions:
  - container
services:
  postgres:
    version: "16"        # Override version
    db: custom_db_name   # Override database name
config:
  image: myapp:latest
  db_password: secret123
```

### What Sidecars Inject

A sidecar provision typically adds:

1. **Sidecar service** (`${name}-db`, `${name}-redis`)
2. **Environment variables** on the main service
3. **depends_on** relationship
4. **Shared network** (internal)
5. **Persistent volume**

Example postgres sidecar output:

```yaml
services:
  myapp-db:
    image: postgres:17-alpine
    environment:
      POSTGRES_DB: myapp
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: secret123
    volumes:
      - myapp_db_data:/var/lib/postgresql/data
    networks:
      - internal
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U postgres"]

  myapp:
    environment:
      POSTGRES_HOST: myapp-db
      POSTGRES_PORT: "5432"
      POSTGRES_DB: myapp
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: secret123
    depends_on:
      - myapp-db
    networks:
      - internal

volumes:
  myapp_db_data:

networks:
  internal:
```

## Output Targets

The manifest system generates three output types.

### Compose Output

Docker Compose configuration in `compose/<stack>.yml`:

```yaml
services:
  myapp:
    image: ghcr.io/example/myapp:latest
    container_name: myapp
    restart: unless-stopped
    environment:
      TZ: America/Chicago
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:8080/health"]
      interval: 30s
      timeout: 5s
      retries: 3
    labels:
      traefik.enable: "true"
      traefik.http.routers.myapp.rule: Host(`myapp.example.com`)
    networks:
      - proxynet
```

### Traefik Output

Traefik dynamic configuration in `traefik/dynamic.yml`:

```yaml
http:
  routers:
    myapp:
      rule: Host(`myapp.example.com`)
      service: myapp
      entrypoints:
        - websecure
      tls:
        certResolver: letsencrypt
  services:
    myapp:
      loadBalancer:
        servers:
          - url: http://myapp:8080
```

### Gatus Output

Gatus monitoring endpoints in `gatus/endpoints.yml`:

```yaml
endpoints:
  - name: myapp
    group: Applications
    url: https://myapp.example.com
    interval: 60s
    conditions:
      - "[STATUS] == 200"
```

## Stacks

Stacks combine multiple service manifests into a single deployment.

### Stack File Structure

```yaml
# Include service manifest files
include:
  - service-a.yml
  - service-b.yml
  - service-c.yml

# Network definitions for the stack
networks:
  default:
    driver: bridge
  proxynet:
    external: true
```

### Values Overlay

Apply configuration overrides to all services in a stack:

```yaml
# values.yml
domain: production.example.com
db_password: production_secret
```

## Examples

### Simple Web App

A minimal web application with health checks and reverse proxy:

```yaml
# services/stirling-pdf.yml
name: stirling-pdf
provisions:
  - container
  - healthcheck
  - homepage
  - reverse-proxy
  - monitoring
config:
  image: frooodle/s-pdf:latest
  port: "8080"
  subdomain: pdf
  domain: example.com
  group: Tools
  icon: mdi-file-pdf-box
  description: PDF toolkit
```

Or using the `webapp` bundle:

```yaml
name: stirling-pdf
provisions:
  - webapp
config:
  image: frooodle/s-pdf:latest
  port: "8080"
  subdomain: pdf
  domain: example.com
  group: Tools
  icon: mdi-file-pdf-box
  description: PDF toolkit
```

### App with Database Sidecar

Using `needs` shorthand:

```yaml
name: myapp
provisions:
  - webapp
needs:
  - postgres
config:
  image: ghcr.io/example/myapp:latest
  port: "3000"
  subdomain: myapp
  domain: example.com
  group: Apps
  icon: mdi-application
  description: My Application
  db_password: secret123
```

With explicit sidecar configuration:

```yaml
name: norish
provisions:
  - container
  - healthcheck
  - homepage
  - reverse-proxy
  - monitoring
services:
  postgres:
    version: "17"
    db: norish
    db_password: production_secret
config:
  image: ghcr.io/norishapp/norish:latest
  port: "3000"
  subdomain: recipes
  domain: example.com
  group: Apps
  icon: mdi-food
  description: Recipe manager
```

### App with Custom Traefik Config

Override or extend Traefik configuration:

```yaml
name: api-gateway
provisions:
  - container
  - reverse-proxy
config:
  image: ghcr.io/example/api:latest
  port: "8080"
  subdomain: api
  domain: example.com
```

Then create a custom provision for additional middleware:

```yaml
# provisions/api-middleware.yml
traefik:
  http:
    middlewares:
      ${name}-ratelimit:
        rateLimit:
          average: 100
          burst: 50
      ${name}-cors:
        headers:
          accessControlAllowMethods:
            - GET
            - POST
          accessControlAllowOriginList:
            - https://${domain}
    routers:
      ${name}:
        middlewares:
          - ${name}-ratelimit
          - ${name}-cors
```

### Multi-Service Stack

```yaml
# stacks/production.yml
include:
  - frontend.yml
  - api.yml
  - worker.yml
networks:
  proxynet:
    external: true
  internal:
    driver: bridge
```

```yaml
# services/frontend.yml
name: frontend
provisions:
  - webapp
config:
  image: ghcr.io/example/frontend:latest
  port: "3000"
  subdomain: app
  domain: example.com
  group: Production
  icon: mdi-web
  description: Frontend application
```

```yaml
# services/api.yml
name: api
provisions:
  - webapp
needs:
  - postgres
  - redis
config:
  image: ghcr.io/example/api:latest
  port: "8080"
  subdomain: api
  domain: example.com
  group: Production
  icon: mdi-api
  description: API server
  db_password: ${API_DB_PASSWORD}
```

```yaml
# services/worker.yml
name: worker
provisions:
  - container
needs:
  - redis
config:
  image: ghcr.io/example/worker:latest
```

### Raw Passthrough Mode

For services that need full compose control:

```yaml
name: legacy-app
type: raw
compose:
  legacy-app:
    image: legacy/app:v1
    container_name: legacy-app
    restart: unless-stopped
    volumes:
      - /custom/path:/data
    ports:
      - "9000:9000"
    environment:
      CUSTOM_VAR: value
```

## Directory Structure

Recommended project layout:

```
manifest/
├── provisions/           # Provision templates
│   ├── container.yml
│   ├── healthcheck.yml
│   ├── reverse-proxy.yml
│   ├── homepage.yml
│   ├── monitoring.yml
│   ├── webapp.yml
│   ├── postgres.yml
│   └── redis.yml
├── services/             # Service manifests
│   ├── myapp.yml
│   └── another-app.yml
├── stacks/               # Stack definitions
│   └── production.yml
└── output/               # Generated files
    ├── compose/
    │   └── production.yml
    ├── traefik/
    │   └── dynamic.yml
    └── gatus/
        └── endpoints.yml
```

## CLI Usage

```bash
# Render a single service
bosun manifest render services/myapp.yml

# Render a stack
bosun manifest render-stack stacks/production.yml

# Render with values overlay
bosun manifest render-stack stacks/production.yml --values values.yml

# Dry run (show output without writing files)
bosun manifest render services/myapp.yml --dry-run

# List available provisions
bosun manifest list-provisions
```

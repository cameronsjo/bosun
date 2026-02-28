# Bosun Manifest System

The manifest system generates Docker Compose, Traefik, and Gatus configurations from short declarative service files. Instead of writing hundreds of lines of compose YAML, you write a 10-line manifest and reference reusable provisions (templates).

## How It Works

```
Service Manifest (what to deploy)
        +
Provisions (how to deploy)
        |
        v
   Renderer (interpolate variables, merge provisions)
        |
        +------+-------+
        |      |       |
        v      v       v
   compose/ traefik/  gatus/
```

1. You write a **service manifest** that names your service, lists provisions to use, and sets config values
2. Each **provision** is a YAML template with `${variable}` placeholders
3. The renderer **interpolates** your config values into the provisions
4. Multiple provisions are **merged** together (compose sections combined, networks unified, etc.)
5. Output files are written to `compose/`, `traefik/`, and `gatus/` directories

## Manifest Schema

```yaml
# REQUIRED -- service name, used in ${name} interpolation and output naming
name: string

# OPTIONAL -- set to "raw" for compose passthrough mode (skips provisions)
type: string

# OPTIONAL -- provisions to apply, in order
provisions:
  - provision-name
  - another-provision

# OPTIONAL -- variables for interpolation into provisions
config:
  key: value
  port: "8080"
  domain: example.com

# OPTIONAL -- shorthand for common sidecar dependencies with defaults
needs:
  - postgres
  - redis

# OPTIONAL -- explicit sidecar configuration (overrides defaults from needs)
services:
  postgres:
    version: "17"
    db: mydb
  redis:
    version: "7"

# OPTIONAL -- raw compose passthrough (only with type: raw)
compose:
  service-name:
    image: ...
```

### Field Reference

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Service name. Used in `${name}` interpolation |
| `type` | string | No | Set to `"raw"` for compose passthrough (no provisions) |
| `provisions` | list | No | Provision templates to apply, in order |
| `config` | map | No | Key-value pairs for variable interpolation |
| `needs` | list | No | Shorthand for sidecar services with defaults |
| `services` | map | No | Explicit sidecar configuration (overrides `needs` defaults) |
| `compose` | map | No | Raw compose config (only with `type: raw`) |

## Variable Interpolation

Variables use `${varname}` syntax and are replaced before YAML parsing.

### Variable Sources (in order of precedence)

1. **Built-in:** `${name}` -- the service name from the manifest
2. **Config values:** any key under `config:` in the manifest
3. **Sidecar variables:** `${sidecar}` type and sidecar-specific defaults (when rendering sidecar provisions)

### Example

Given this manifest:

```yaml
name: myapp
provisions: [container]
config:
  image: ghcr.io/org/myapp:latest
  port: 3000
```

And this provision template:

```yaml
compose:
  services:
    ${name}:
      image: ${image}
      container_name: ${name}
      ports:
        - "${port}:${port}"
```

The renderer produces:

```yaml
compose:
  services:
    myapp:
      image: ghcr.io/org/myapp:latest
      container_name: myapp
      ports:
        - "3000:3000"
```

### Type Conversion

All values are converted to strings: `8080` becomes `"8080"`, `true` becomes `"true"`, etc.

### Missing Variables

If a provision references a variable not defined in the manifest's `config`, bosun returns an error:

```
error: interpolate provision webapp: missing variables: ${port}, ${domain}
```

## Provisions (Templates)

Provisions live in `manifest/provisions/` and are YAML files with `${variable}` placeholders. Each provision contributes one or more output sections (compose, traefik, gatus).

### Available Provisions

| Provision | What It Adds |
|-----------|-------------|
| `container` | Base Docker Compose service definition (image, name, restart, networks) |
| `healthcheck` | Docker health check configuration |
| `homepage` | Homepage dashboard entry (labels for homepage integration) |
| `reverse-proxy` | Traefik routing labels and dynamic config (includes `secure-defaults@file,default-compress@file` middleware chain) |
| `protected` | Extends `reverse-proxy` with auth middleware (`${auth_middleware}`, default: `authelia@file`). Use for services behind SSO |
| `monitoring` | Gatus monitoring endpoint |
| `postgres` | PostgreSQL sidecar container with volume, network, and env |
| `redis` | Redis sidecar container |

Run `bosun provisions` to see all available provisions in your project.

### Protected Provision

The `protected` provision extends `reverse-proxy` by prepending an auth middleware to the router chain. Use it for services that should sit behind SSO (e.g., Authelia, Authentik):

```yaml
name: myapp
provisions:
  - container
  - healthcheck
  - protected       # instead of reverse-proxy
config:
  image: myimage:latest
  port: 8080
  subdomain: myapp
  domain: example.com
  auth_middleware: authelia@file   # optional, defaults to authelia@file
```

This produces router middlewares: `authelia@file,secure-defaults@file,default-compress@file`. The `${auth_middleware}` variable lets you swap auth providers without changing the provision.

### Provision Structure

A provision YAML file can contain any of these top-level keys:

```yaml
compose:         # Docker Compose service definitions
  services: ...
  networks: ...
  volumes: ...

traefik:         # Traefik dynamic configuration
  http:
    routers: ...
    services: ...

gatus:           # Gatus monitoring endpoints
  endpoints: ...
```

### Writing Custom Provisions

Create a YAML file in `manifest/provisions/`:

```yaml
# manifest/provisions/my-custom.yml
compose:
  services:
    ${name}:
      labels:
        - "my.custom.label=true"
      environment:
        CUSTOM_VAR: "${custom_value}"
```

Then reference it in a service manifest:

```yaml
name: myapp
provisions: [container, my-custom]
config:
  image: myimage:latest
  custom_value: hello
```

## Merge Behavior

When a manifest uses multiple provisions, their outputs are merged. The merge strategy depends on the key:

| Strategy | Keys | Behavior |
|----------|------|----------|
| **Union** | `networks`, `depends_on` | Set union (no duplicates) |
| **Extend** | `endpoints` | Append to list |
| **Replace** | All other lists | Later provision wins |
| **Recursive** | All maps | Deep merge |

### Union Merge (networks, depends_on)

Duplicate entries are removed:

```yaml
# Provision A: networks: [internal]
# Provision B: networks: [internal, proxynet]
# Result:      networks: [internal, proxynet]
```

### Extend Merge (endpoints)

Lists are concatenated:

```yaml
# Provision A: endpoints: [{name: health}]
# Provision B: endpoints: [{name: metrics}]
# Result:      endpoints: [{name: health}, {name: metrics}]
```

### Environment/Labels Normalization

`environment` and `labels` are normalized from list to map format before merging, so provisions can safely merge environment variables:

```yaml
# Input (list format):     environment: ["FOO=bar", "BAZ=qux"]
# Normalized (map format): environment: {FOO: bar, BAZ: qux}
```

## Sidecars (needs/services)

Sidecars are companion containers (databases, caches) that run alongside your service.

### Using `needs` (Shorthand)

```yaml
name: myapp
needs: [postgres, redis]
```

This applies default configurations for each sidecar type. Defaults include standard image versions, container names, network configuration, and volume mounts.

### Using `services` (Explicit)

Override defaults with explicit sidecar configuration:

```yaml
name: myapp
services:
  postgres:
    version: 17
    db: myapp_db
    db_password: "{{ $secrets.apps.myapp.db_password }}"
  redis:
    version: 7
```

`services` entries are merged on top of the sidecar defaults, so you only need to specify what you want to override.

## Stacks

A stack groups multiple services for bulk rendering.

### Stack Manifest

```yaml
# manifest/stacks/core.yml
name: core
services:
  - traefik
  - authelia
  - gatus
```

### Rendering a Stack

```bash
bosun provision core           # Render all services in the stack
bosun provision core -n        # Dry run
bosun provision core -d        # Show diff
bosun provision core -f prod.yaml  # Apply values overlay
```

This renders each service in the stack and merges the output into combined files.

## Values Overlays

Override config values per-environment using the `-f` flag:

```bash
bosun provision mystack -f prod.yaml
```

The values file contains config overrides:

```yaml
# prod.yaml
myapp:
  domain: prod.example.com
  image: ghcr.io/org/myapp:v1.2.3
```

## Raw Mode (Compose Passthrough)

For services that don't fit the provision model, use `type: raw` to pass compose config directly:

```yaml
name: custom-service
type: raw
compose:
  custom-service:
    image: myimage:latest
    ports:
      - "8080:8080"
    volumes:
      - ./data:/data
```

No provisions are applied. The compose section is used as-is.

## Complete Example

A typical homelab service with all the bells and whistles:

```yaml
name: norish
provisions:
  - container
  - healthcheck
  - homepage
  - reverse-proxy
  - monitoring
config:
  image: ghcr.io/norishapp/norish:latest
  port: 3000
  subdomain: recipes
  domain: example.com
  group: Apps
  icon: mdi-food
  description: Recipe manager
services:
  postgres:
    version: 17
    db: norish
    db_password: "{{ $secrets.apps.norish.db_password }}"
```

This generates:
- **compose/**: Service definition + Postgres sidecar, networks, volumes, health check, homepage labels
- **traefik/**: HTTP router for `recipes.example.com` -> port 3000
- **gatus/**: Monitoring endpoint for the service

## CLI Workflow

```bash
# List available provisions
bosun provisions

# Scaffold a new service
bosun create webapp myapp

# Edit the generated manifest
$EDITOR manifest/services/myapp.yml

# Preview rendered output
bosun provision myapp -n

# Show diff against current files
bosun provision myapp -d

# Render and write output files
bosun provision myapp

# Validate all manifests
bosun lint

# Start the service
bosun yacht up myapp
```

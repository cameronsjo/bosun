## ADDED Requirements

### Requirement: Traefik Secure Defaults Middleware

The system SHALL provide a `secure-defaults` middleware definition in the default Traefik dynamic configuration that combines security headers and response compression.

The `secure-defaults` middleware chain MUST include:

- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`
- `Strict-Transport-Security` with `max-age=31536000`, `includeSubDomains`, and `preload`
- `Referrer-Policy: strict-origin-when-cross-origin`
- `X-XSS-Protection: 1; mode=block`

The system SHALL also provide a `default-compress` middleware that enables response compression with a minimum body size of 1024 bytes.

#### Scenario: New project gets secure-defaults middleware

- **WHEN** `bosun init` generates a Traefik dynamic configuration
- **THEN** the dynamic config includes a `secure-defaults` middleware with all listed security headers
- **AND** includes a `default-compress` middleware with compression enabled

#### Scenario: Middleware definitions are overridable

- **WHEN** a user defines their own `secure-defaults` middleware in a custom Traefik config file
- **THEN** the Traefik file provider merges or overrides the bosun-generated definition per standard Traefik behavior

### Requirement: Traefik Static Config Defaults

The system SHALL generate a Traefik static configuration with the following defaults when bootstrapping a new project:

- `providers.docker.exposedByDefault: false` — containers require explicit `traefik.enable=true`
- `providers.docker.defaultRule` — auto-generates `Host()` rules from container names using the configured domain (e.g., `Host({{ trimPrefix "/" .Name }}.example.com)`)
- `providers.docker.network` — defaults to `proxynet`
- HTTP-to-HTTPS redirect on the `web` entrypoint
- Let's Encrypt ACME certificate resolver on the `websecure` entrypoint

#### Scenario: New project gets defaultRule

- **WHEN** `bosun init` generates a Traefik static configuration
- **AND** the user provides a domain (e.g., `example.com`)
- **THEN** the static config includes `defaultRule: "Host({{ trimPrefix \"/\" .Name }}.example.com)"`
- **AND** `exposedByDefault` is `false`

#### Scenario: HTTP-to-HTTPS redirect is configured by default

- **WHEN** `bosun init` generates a Traefik static configuration
- **THEN** the `web` entrypoint (port 80) redirects all traffic to `websecure` (port 443)

#### Scenario: Let's Encrypt resolver is pre-configured

- **WHEN** `bosun init` generates a Traefik static configuration
- **THEN** a `letsencrypt` certificate resolver is configured with httpChallenge on the `web` entrypoint

### Requirement: Protected Service Provision

The system SHALL provide a `protected` provision that chains ForwardAuth middleware for services requiring authentication.

The `protected` provision MUST add a middleware reference to the service's Traefik router pointing to the configured auth provider (default: `authelia@file`).

The provision SHALL accept an optional `auth_middleware` variable to allow swapping the auth provider (e.g., `authentik@file`, `keycloak@file`).

#### Scenario: Service with protected provision

- **WHEN** a service manifest includes `provisions: [reverse-proxy, protected]`
- **THEN** the rendered Traefik router includes `middlewares: [authelia@file]`
- **AND** the compose labels include the middleware reference

#### Scenario: Custom auth provider

- **WHEN** a service manifest includes `provisions: [protected]` and `config.auth_middleware: authentik@file`
- **THEN** the rendered Traefik router references `authentik@file` instead of `authelia@file`

### Requirement: Default Domain Configuration

The project configuration (`bosun.yaml`) SHALL support a `domain` field that provides the default domain for Traefik routing.

The `domain` field SHALL be used by:

- Traefik `defaultRule` generation (static config)
- The `reverse-proxy` provision as a fallback when `domain` is not set per-service
- The `bosun init` wizard to prompt the user for their domain

#### Scenario: Domain from bosun.yaml

- **WHEN** `bosun.yaml` contains `domain: example.com`
- **AND** a service uses the `reverse-proxy` provision without specifying `domain` in its config
- **THEN** the provision uses `example.com` as the domain value

#### Scenario: Per-service domain overrides project domain

- **WHEN** `bosun.yaml` contains `domain: example.com`
- **AND** a service specifies `config.domain: other.com`
- **THEN** the service uses `other.com` for its routing rule

## MODIFIED Requirements

### Requirement: Provision System

The system SHALL support reusable provision templates that produce compose, traefik, and gatus output sections. Provisions are loaded from YAML files, interpolated with variables, and support inheritance via an `includes` key.

A `Provision` MAY contain:

- `apiVersion`: Schema version
- `kind`: Manifest type (`Provision`)
- `compose`: Docker Compose output section
- `traefik`: Traefik dynamic configuration output section
- `gatus`: Gatus endpoint monitoring output section
- `includes`: List of other provision names to inherit from

#### Scenario: Load and interpolate a provision

- **WHEN** a provision file contains `${name}` and `${image}` placeholders
- **THEN** the system replaces them with values from the variables map before YAML parsing
- **AND** returns a `Provision` struct with populated compose, traefik, and gatus fields

#### Scenario: Provision inheritance via includes

- **WHEN** a provision has `includes: [base, extension]`
- **THEN** the system loads each included provision first, deep-merges their outputs, then deep-merges the current provision's content on top
- **AND** the current provision's values take precedence over included values

#### Scenario: Circular include detection

- **WHEN** a provision includes itself directly or indirectly
- **THEN** the system returns an `ErrCircularInclude` error instead of entering an infinite loop

#### Scenario: Missing provision returns error

- **WHEN** a provision file does not exist at the expected path
- **THEN** the system returns a "provision not found" error

#### Scenario: Missing variable returns error

- **WHEN** a provision references a `${var}` that is not in the variables map
- **THEN** the system returns a "missing variables" error listing all unresolved references

#### Scenario: List available provisions

- **WHEN** `ListProvisions` is called with a provisions directory
- **THEN** it returns the names (without file extension) of all `.yml` and `.yaml` files in the directory, excluding subdirectories

#### Scenario: Reverse-proxy provision chains secure-defaults middleware

- **WHEN** a service uses the `reverse-proxy` provision
- **THEN** the generated Traefik router includes `secure-defaults@file` and `default-compress@file` in its middleware chain
- **AND** the compose labels include the middleware references

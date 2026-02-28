## ADDED Requirements

### Requirement: Traefik Configuration Diagnostics

The `bosun doctor` command SHALL validate the Traefik configuration and warn about missing security best practices.

The following checks SHALL be performed when Traefik is detected as an infrastructure container:

- **No HTTPS redirect**: Warn if the `web` entrypoint does not redirect to `websecure`
- **No security headers**: Warn if no `secure-defaults` or equivalent headers middleware is defined in the dynamic config
- **Exposed by default**: Warn if `exposedByDefault` is `true` or not explicitly set to `false`
- **Direct socket mount**: Info-level note suggesting docker-socket-proxy for improved security
- **No defaultRule**: Info-level note that `defaultRule` is not configured (zero-config routing unavailable)
- **No ACME resolver**: Warn if no certificate resolver is configured (services will use self-signed or no TLS)

Each check SHALL produce a labeled diagnostic (pass/warn/info) in the doctor output.

#### Scenario: Doctor detects missing HTTPS redirect

- **WHEN** `bosun doctor` runs and the Traefik static config does not include an HTTP-to-HTTPS redirect on the `web` entrypoint
- **THEN** the doctor output includes a warning: "Traefik: HTTP-to-HTTPS redirect not configured on :80"

#### Scenario: Doctor detects missing security headers

- **WHEN** `bosun doctor` runs and no `secure-defaults` middleware is defined in the Traefik dynamic config directory
- **THEN** the doctor output includes a warning: "Traefik: No security headers middleware found"

#### Scenario: Doctor detects exposedByDefault not disabled

- **WHEN** `bosun doctor` runs and the Traefik static config has `exposedByDefault: true` or does not set it
- **THEN** the doctor output includes a warning: "Traefik: exposedByDefault is not set to false — all containers are exposed"

#### Scenario: Doctor passes when all defaults are present

- **WHEN** `bosun doctor` runs and Traefik has HTTPS redirect, security headers, `exposedByDefault=false`, and a certificate resolver
- **THEN** all Traefik checks show as passing

### Requirement: Traefik Upgrade Command

The system SHALL provide a `bosun upgrade traefik` command that detects the current Traefik configuration, compares it against recommended defaults, and offers to apply missing improvements.

The upgrade command SHALL:

1. Locate the Traefik static config (from the compose file or a known path)
2. Locate the Traefik dynamic config directory
3. Compare against the recommended baseline (HTTPS redirect, defaultRule, security headers, compression, exposedByDefault)
4. Display a summary of what is missing and what would be added
5. In interactive mode, prompt the user before applying each change
6. Support `--dry-run` to show what would change without modifying files
7. Support `--yes` to apply all changes non-interactively

The upgrade command SHALL NOT overwrite user customizations. It SHALL only add missing configuration blocks and warn if existing configuration conflicts with recommendations.

#### Scenario: Upgrade detects missing HTTPS redirect

- **WHEN** `bosun upgrade traefik` runs and the static config lacks an HTTP-to-HTTPS redirect
- **THEN** the command shows the recommended entrypoint redirect configuration
- **AND** prompts the user to apply it (in interactive mode)

#### Scenario: Upgrade adds security headers middleware

- **WHEN** `bosun upgrade traefik` runs and the dynamic config has no `secure-defaults` middleware
- **THEN** the command offers to add the `secure-defaults` middleware definition to the dynamic config

#### Scenario: Upgrade skips already-configured features

- **WHEN** `bosun upgrade traefik` runs and HTTPS redirect is already configured
- **THEN** the HTTPS redirect check shows as "already configured" and no changes are proposed for it

#### Scenario: Dry-run shows diff without modifying files

- **WHEN** `bosun upgrade traefik --dry-run` runs
- **THEN** the command displays all proposed changes but does not modify any files on disk

#### Scenario: Non-interactive mode applies all changes

- **WHEN** `bosun upgrade traefik --yes` runs
- **THEN** all missing defaults are applied without prompting

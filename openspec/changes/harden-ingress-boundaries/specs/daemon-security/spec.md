## ADDED Requirements

### Requirement: Socket configuration credential authorization
The Unix socket GET /config SHALL apply the same peer authorization as POST /trigger. Missing credentials SHALL fail closed unless the existing explicit opt-out is enabled. The shared response builder SHALL include the webhook secret only when explicitly requested; status and public health SHALL expose no credential.

#### Scenario: Unauthorized or missing peer identity
- **WHEN** an unapproved UID or a connection without peer credentials requests /config
- **THEN** the response is 403 with no webhook secret unless the explicit opt-out is enabled

#### Scenario: Authorized secret consumer
- **WHEN** the daemon UID or allowed UID requests /config
- **THEN** the configured webhook secret is returned without changing URL credential redaction

#### Scenario: Shared response consumer omits credential
- **WHEN** a caller builds a response without opting into credentials
- **THEN** the webhook secret is absent

### Requirement: Standalone receiver fails closed
Every standalone receiver trigger endpoint SHALL reject requests with 403 when no secret resolves unless BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK is exactly true. A configured secret SHALL always require its valid signature. Public health/readiness SHALL remain unaffected. Startup and each accepted unsigned request SHALL disclose the opt-out posture.

#### Scenario: Every provider has no secret
- **WHEN** a generic, GitHub, GitLab, Gitea or Bitbucket trigger arrives without a resolved secret
- **THEN** it is rejected before forwarding to the daemon

#### Scenario: Explicit opt-out and configured secret
- **WHEN** unsigned acceptance is explicitly enabled
- **THEN** secretless triggers may forward with a warning but a configured secret still requires validation

#### Scenario: Secret fetch fails
- **WHEN** explicit --fetch-secret cannot authorize with the daemon
- **THEN** startup fails instead of silently accepting unsigned triggers

### Requirement: Untrusted operator attribution is sanitized
Provider attribution and refs, socket, TCP and API trigger sources, and container health output SHALL strip control, format and separator characters before applying length limits. Health byte caps SHALL preserve UTF-8 validity.

#### Scenario: Provider values include controls
- **WHEN** any standalone provider or daemon GitHub webhook receives control-bearing attribution or refs
- **THEN** operator logs and reconcile attribution contain sanitized bounded values

#### Scenario: Socket source contains controls
- **WHEN** a socket trigger supplies control-bearing source text
- **THEN** reconciliation receives sanitized bounded text

#### Scenario: TCP source contains controls
- **WHEN** an authorized TCP trigger supplies control-bearing source text
- **THEN** reconciliation receives sanitized bounded text

#### Scenario: Health output contains controls and multibyte text
- **WHEN** unhealthy container output includes controls or exceeds its cap
- **THEN** drift and health-gate diagnostics are sanitized, bounded and valid UTF-8

#### Scenario: API source contains controls
- **WHEN** an authorized /api/trigger supplies control-bearing source text
- **THEN** reconciliation receives sanitized bounded text with the existing webui fallback

#### Scenario: Sanitized ref resembles the tracked branch
- **WHEN** an original nontracked ref becomes the tracked ref only after stripping controls
- **THEN** the event remains ignored and branch fields and reasons are sanitized

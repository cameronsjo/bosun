## ADDED Requirements

### Requirement: Webhook authentication fail-closed

The daemon SHALL NOT accept unsigned webhook requests as valid GitOps triggers
in its default configuration. When the HTTP server is enabled (`EnableHTTP`) and
no webhook secret is configured, the daemon SHALL refuse to start. An operator
MAY opt out by setting `BOSUN_ALLOW_UNSIGNED_WEBHOOKS=true`, in which case the
daemon SHALL log a security warning naming the risk on startup and on every
webhook receipt. When a webhook secret is configured, every webhook entry point
(`/webhook`, `/webhook/github`, `/webhook/manual`) SHALL reject requests with a
missing or invalid signature with HTTP 401 and SHALL NOT trigger a reconcile.

#### Scenario: HTTP enabled with no secret refuses to start
- **WHEN** the daemon starts with `EnableHTTP=true`, an empty `WebhookSecret`, and `BOSUN_ALLOW_UNSIGNED_WEBHOOKS` unset
- **THEN** startup fails with an error explaining that a webhook secret is required
- **AND** the HTTP listener never binds

#### Scenario: Explicit opt-in permits unsigned webhooks with a warning
- **WHEN** the daemon starts with `EnableHTTP=true`, an empty `WebhookSecret`, and `BOSUN_ALLOW_UNSIGNED_WEBHOOKS=true`
- **THEN** the daemon starts and logs a security warning at startup
- **AND** each subsequent webhook receipt logs a security warning identifying the request as unauthenticated

#### Scenario: Configured secret rejects unsigned request
- **WHEN** a webhook secret is configured and a POST arrives at `/webhook`, `/webhook/github`, or `/webhook/manual` with no signature or an invalid signature
- **THEN** the daemon responds 401
- **AND** no reconcile is triggered

#### Scenario: Configured secret accepts valid signature
- **WHEN** a webhook secret is configured and a POST arrives with a valid HMAC-SHA256 signature over the request body
- **THEN** the daemon responds 202 and triggers a reconcile

### Requirement: Unix socket peer-credential enforcement

The Unix socket server SHALL authorize trigger and control requests by peer
credentials, not solely by socket file mode. A request SHALL be authorized only
when the connecting peer's UID is the daemon's own UID or is present in a
configured allowlist (`BOSUN_SOCKET_ALLOWED_UIDS`). When peer credentials cannot
be determined for a connection — including platforms without `SO_PEERCRED`
support — the daemon SHALL deny mutating requests (fail-closed) unless the
operator has explicitly disabled peer-credential enforcement.

#### Scenario: Non-allowlisted UID is denied
- **WHEN** a local process whose UID is neither the daemon UID nor in the allowlist sends `POST /trigger`
- **THEN** the daemon responds 403
- **AND** no reconcile is triggered

#### Scenario: Daemon-owner UID is allowed
- **WHEN** a process running as the daemon's own UID sends `POST /trigger`
- **THEN** the daemon responds 202 and triggers a reconcile

#### Scenario: Peer credentials unavailable fails closed
- **WHEN** a `POST /trigger` arrives on a connection for which peer credentials cannot be determined
- **THEN** the daemon responds 403 unless peer-credential enforcement has been explicitly disabled

### Requirement: Socket permission integrity

The Unix socket SHALL be created with its configured file mode
(`SocketConfig.SocketMode`) and SHALL NOT pass through a more-permissive mode at
any point between creation and first accept. The configured mode SHALL be
honored rather than hard-coded.

#### Scenario: Socket honors configured mode with no permissive window
- **WHEN** the daemon creates its Unix socket with a configured mode of `0600`
- **THEN** the socket's mode is `0600` from the moment it is observable
- **AND** at no point is the socket world- or group-connectable before the mode is applied

### Requirement: HTTP request hardening

Every daemon HTTP server SHALL set a finite `ReadHeaderTimeout` and a bounded
`MaxHeaderBytes`, and every trigger entry point SHALL bound its request body with
`http.MaxBytesReader`. This applies to the webhook server, the Unix socket HTTP
server, the TCP API server, and the HTTP API trigger handler, sized to the
trigger schema.

#### Scenario: Webhook server bounds headers and bodies
- **WHEN** the webhook HTTP server is constructed
- **THEN** it sets a finite `ReadHeaderTimeout` and a finite `MaxHeaderBytes`
- **AND** a slow-header client is disconnected after the timeout
- **AND** a `/webhook/manual` body exceeding the trigger-body limit is rejected

#### Scenario: Unix socket server bounds headers and bodies
- **WHEN** the Unix socket HTTP server is constructed
- **THEN** it sets a finite `ReadHeaderTimeout` and `MaxHeaderBytes`
- **AND** a `POST /trigger` body exceeding the trigger-body limit is rejected before full decode

#### Scenario: TCP API server bounds headers and bodies
- **WHEN** the TCP API server is constructed
- **THEN** it sets a finite `ReadHeaderTimeout` and `MaxHeaderBytes`
- **AND** a `POST /trigger` body exceeding the trigger-body limit is rejected before full decode

#### Scenario: HTTP API trigger bounds body
- **WHEN** `handleAPITrigger` decodes a request body
- **THEN** the body is wrapped in `http.MaxBytesReader` and an oversized payload is rejected

### Requirement: Anonymous endpoint exposure control

The `/metrics` and `/api/widget` endpoints SHALL NOT be reachable by
unauthenticated remote callers in the daemon's default configuration. These
endpoints SHALL be served only to localhost, gated behind authentication, or
enabled by an explicit operator opt-in. The deployed git SHA and reconcile
cadence SHALL NOT be disclosed to anonymous remote callers by default.

#### Scenario: Widget not remotely reachable by default
- **WHEN** a remote, unauthenticated caller requests `/api/widget` under the default configuration
- **THEN** the request is refused (not served the deploy SHA and cadence)

#### Scenario: Metrics not remotely reachable by default
- **WHEN** a remote, unauthenticated caller requests `/metrics` under the default configuration
- **THEN** the request is refused

#### Scenario: Explicit opt-in exposes endpoints
- **WHEN** the operator sets the metrics-exposure opt-in
- **THEN** `/metrics` and `/api/widget` are served to remote callers as configured

### Requirement: Webhook payload sanitization

Attacker-controlled webhook fields SHALL be length-capped and stripped of
control characters before being written to logs, recorded in trace span
attributes, or forwarded to telemetry sinks. This applies to the GitHub pusher
name and to the reconcile source string derived from it (Sentry and OTEL
included).

#### Scenario: Oversized pusher name is capped before logging
- **WHEN** a GitHub push payload carries a pusher name longer than the cap
- **THEN** the value written to logs, spans, and the reconcile source is truncated to the cap

#### Scenario: Control characters are stripped
- **WHEN** a pusher name contains newlines, ANSI escapes, or other control characters
- **THEN** those characters are removed or escaped before the value is logged or sent to telemetry

# Change: Daemon HTTP and Unix-socket security hardening

## Why

The April 2026 reconcile-path bug hunt found that the daemon's network and IPC
surfaces are unauthenticated or fail-open in their default configuration. The
HTTP webhook server defaults to enabled and binds `:8080` on all interfaces; when
no webhook secret is configured it accepts **any** unsigned POST as a valid
GitOps trigger (GHSA-q67h-gp7c-rx88, Critical). The Unix socket extracts peer
credentials but never enforces them, so any local UID in the daemon's group can
force a reconcile (#253). The socket hard-codes `0660` and races between
`net.Listen` and `Chmod` (#254). No HTTP server sets `ReadHeaderTimeout` or
`MaxHeaderBytes`, exposing a pre-auth Slowloris vector (#255). The trigger JSON
handlers decode bodies without a size cap (#295). `/metrics` and `/api/widget`
disclose the deployed commit and reconcile cadence to anonymous callers (#296).
GitHub pusher names from unsigned payloads flow unbounded into logs, OTEL spans,
and Sentry (#297).

There is no spec describing the daemon's security posture, so these are
implementation gaps with no authoritative requirement to regress against. This
proposal establishes a `daemon-security` capability that makes the daemon
**fail-closed by default** across every entry point.

## What Changes

- **Webhook fail-closed request gate** *(shipped, #345)* — when HTTP is enabled
  and no webhook secret is set, every trigger endpoint SHALL reject requests
  with 403 unless the operator explicitly opts out via
  `BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK=true`, which logs a loud security
  warning on startup and on every webhook receipt. The daemon still starts (a
  homelab daemon that refuses to boot on upgrade bricks polling and the socket
  API too — per-request rejection closes the hole without that blast radius).
  A `BOSUN_LISTEN_ADDR` knob narrows the HTTP bind; the default stays
  all-interfaces because container-side callers reach the daemon over the
  docker bridge. **BREAKING**: existing secret-less HTTP deployments must set
  a secret or opt out.
- **Unix socket peer-credential enforcement** — the socket SHALL reject trigger
  requests whose peer UID is not the daemon owner or in a configured allowlist,
  and SHALL fail-closed when peer credentials are unavailable (including
  non-Linux platforms unless explicitly overridden).
- **Socket permission integrity** — the socket SHALL be created with its
  configured mode with no world-connectable window (honor `SocketMode`, set umask
  before listen).
- **HTTP request hardening** — every daemon HTTP server (webhook, socket, TCP)
  SHALL set `ReadHeaderTimeout` and `MaxHeaderBytes`, and SHALL bound trigger
  request bodies with `http.MaxBytesReader`.
- **Anonymous-endpoint exposure control** — `/metrics` and `/api/widget` SHALL
  NOT be reachable by unauthenticated remote callers in the default
  configuration (localhost-bound, auth-gated, or explicitly flag-enabled).
- **Public health response minimization** — `/health` SHALL remain
  unauthenticated for load-balancer and container liveness probes, but its JSON
  body SHALL be limited to top-level health, readiness, and uptime. Reconcile
  errors, last-reconcile timestamps, repository/path-bearing messages,
  subsystem state, and circuit-breaker counts SHALL remain off this public
  response. Operators retain diagnostics through the local or bearer-protected
  `/status` surface and daemon logs.
- **Webhook payload sanitization** — attacker-controlled fields (pusher name)
  SHALL be length-capped and control-character-sanitized before being logged,
  recorded in spans, or sent to telemetry sinks.

## Impact

- Affected specs: NEW capability `daemon-security`. Touches behavior also
  described in `reconcile` (trigger entry points) and `observability` (metrics
  exposure), but adds no requirements there.
- Affected code:
  - `internal/daemon/server.go` — webhook handlers (`:191`, `:259`, `:360`),
    `handleWidget` (`:457`), `/metrics` registration (`:68`), `http.Server`
    config (`:70`), pusher-name logging (`:327`, `:342`)
  - `internal/daemon/socket.go` — `net.Listen`/`Chmod` (`:82`), `handleTrigger`
    body decode (`:156`), `http.Server` config (`:60`)
  - `internal/daemon/tcp.go` — `http.Server` config (`:45`), `handleTrigger`
    (`:154`), existing `authMiddleware`/`auditMiddleware`
  - `internal/daemon/api.go` — `handleAPITrigger` (`:289`)
  - `internal/daemon/daemon.go` — `Config` (`SocketMode` `:189`, `EnableHTTP`
    `:89`, `WebhookSecret` `:1350`), `ValidateConfig` (`:1733`),
    `ConfigFromEnv`, detailed `HealthStatus`, and the bounded public health
    response projection
  - `internal/daemon/server.go`, `socket.go`, `tcp.go` — public `/health`
    handlers on all three transports; authenticated/operator `/status` remains
    the diagnostic surface
  - `internal/daemon/peercred_linux.go` / `peercred_other.go` —
    `getPeerCredentials`, `WrapServerForPeerCred`, `InjectPeerCred`
- All consumers of the trigger surface (each needs its own scenario + task):
  - HTTP webhook server (`server.go`) — `/webhook`, `/webhook/github`,
    `/webhook/manual`
  - Unix socket server (`socket.go`) — `POST /trigger`
  - TCP API server (`tcp.go`) — `POST /trigger`, bearer-token gated
  - HTTP API (`api.go`) — `handleAPITrigger`
  - Public liveness clients — HTTP/TCP/Unix-socket `/health`, including
    `daemon.Client.Health`, `bosun daemon-status`, `bosun validate`, and the
    standalone webhook receiver's health/ready proxy. Each named consumer has
    an explicit migration scenario and implementation task.
- New config / env vars:
  - `BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK` (bool, default false; strict `== "true"`)
  - `BOSUN_LISTEN_ADDR` (string, default empty = all interfaces)
  - `BOSUN_SOCKET_ALLOWED_UIDS` / allowlist config (existing `SocketMode` becomes
    load-bearing)
  - `BOSUN_METRICS_EXPOSE` (or equivalent) to opt metrics/widget into remote
    exposure
- Docs: `docs/security.md`, `docs/commands.md`, `docs/gitops.md`,
  `docs/cli-reference.md`, `skills/onboard/resources/gitops.md` (daemon
  security and health contracts), `CLAUDE.md` env-var table.

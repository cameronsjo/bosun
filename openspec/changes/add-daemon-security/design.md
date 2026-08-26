## Context

The daemon exposes three network/IPC surfaces (HTTP webhook server, Unix socket,
optional TCP API) plus an HTTP API handler. Each evolved independently, so their
security postures diverge: the TCP server has bearer-token `authMiddleware` and
`auditMiddleware`; the socket extracts peer credentials but discards them; the
webhook server gates signature checks on a non-empty secret and otherwise
fails open. The default config enables HTTP and binds `:8080` on all interfaces.

This change unifies the posture around **fail-closed by default** without
breaking the single-binary, zero-dependency homelab deployment model. Stakeholders
are homelab operators who frequently run with minimal config; the design must make
the safe path the default while leaving a clearly-labeled escape hatch.

## Goals / Non-Goals

- Goals:
  - No unauthenticated mutating trigger reachable by default on any surface.
  - No unauthenticated diagnostic response exposes repository/path-bearing
    errors, reconcile timing, subsystem messages, or circuit-breaker state.
  - Reuse existing primitives (`authMiddleware`, `auditMiddleware`,
    `InjectPeerCred`) rather than introduce a new auth framework.
  - Backwards-compatibility via explicit, loud opt-in flags — never silent.
- Non-Goals:
  - TLS termination / mTLS for the HTTP servers (out of scope; reverse-proxy
    territory).
  - Per-route RBAC beyond owner/allowlist UID and bearer token.
  - Rate limiting beyond header/body bounds (separate concern, Cluster I).

## Decisions

- **Decision: Startup gate over per-request rejection for the empty-secret case.**
  Failing at startup (rather than 401-ing every webhook) surfaces the
  misconfiguration immediately and prevents a daemon that silently accepts
  nothing useful. The `BOSUN_ALLOW_UNSIGNED_WEBHOOKS` escape hatch preserves the
  current behavior for operators who knowingly run on a trusted LAN segment.
  - Alternatives considered: default-deny at request time (less discoverable);
    auto-generate a secret (breaks the GitHub-side shared-secret model).

- **Decision: Peer UID allowlist with fail-closed default.**
  `SO_PEERCRED` is already wired on Linux; the gap is enforcement. Owner-UID is
  always allowed; `BOSUN_SOCKET_ALLOWED_UIDS` extends it. On platforms without
  peer creds the mutating handlers deny by default — the socket is then only
  safe behind file-mode, so denying mutation is the conservative choice.
  - Alternatives considered: group-based allowlist (coarser; the 0660 group is
    exactly the over-broad grant we're closing).

- **Decision: Reuse `MaxBytesReader` + small trigger limit (e.g. 64KB).**
  The trigger schema is `{source, force}`; 64KB is generous. The webhook body
  limit (1MB) stays for signed payloads which legitimately carry full push JSON.

- **Decision: Metrics/widget default to localhost-or-opt-in.**
  Homepage-dashboard users who relied on remote `/api/widget` set the opt-in;
  this is the documented breaking change with a one-line migration.

- **Decision: `/health` is always a bounded liveness projection.**
  Health probes stay unauthenticated on the webhook HTTP server, TCP API, and
  Unix socket, and keep their existing 200-versus-503 semantics. Their JSON is
  a transport-independent projection containing only `status`, `ready`, and
  `uptime`; it never expands based on an Authorization header. Operators use
  `/status` (bearer-protected over TCP or protected by Unix-socket access) and
  daemon logs for last-error and reconcile diagnostics. This avoids a
  credential-varying health response that caches or proxies could mix up.

## Risks / Trade-offs

- **Breaking change for secret-less HTTP deployments** → mitigated by the opt-in
  flag and a clear startup error message naming the exact env var to set.
- **Peer-cred enforcement could lock out legitimate tooling running as a
  different UID** → mitigated by the allowlist and by allowing the daemon owner
  unconditionally.
- **Metrics gating could break existing dashboards** → mitigated by documenting
  the opt-in and keeping localhost access unchanged.
- **Consumers may have parsed detailed `/health` fields** → retain the existing
  top-level status, readiness, uptime, and status codes; direct operator clients
  to `/status` for diagnostics. Removing unauthenticated detail is the intended
  security boundary.

## Migration Plan

1. Operators running HTTP without a secret: set `WEBHOOK_SECRET`, or set
   `BOSUN_ALLOW_UNSIGNED_WEBHOOKS=true` to retain current behavior.
2. Operators consuming `/api/widget` or `/metrics` remotely: set the
   metrics-exposure opt-in.
3. Consumers using `/health` for diagnostics: use `/status` over the Unix socket
   or authenticated TCP API; liveness probes require no change.
4. Rollback: unset the new env vars; the startup gate is the only hard behavior
   change and is itself gated by the opt-in.

## Open Questions

- Should `BOSUN_SOCKET_ALLOWED_UIDS` accept usernames in addition to numeric
  UIDs? (Lean: numeric only for v1, document the mapping.)
- Exact name for the metrics-exposure opt-in (`BOSUN_METRICS_EXPOSE` vs
  `BOSUN_EXPOSE_METRICS`) — settle during implementation for env-var-table
  consistency.

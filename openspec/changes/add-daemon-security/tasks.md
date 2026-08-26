## 1. Webhook authentication fail-closed (shipped, #345)

- [x] 1.1 Add `AllowUnauthenticatedWebhook` to `Config`, parsed from `BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK` in `ConfigFromEnv` (strict `== "true"`)
- [x] 1.2 Route all three handlers through a shared `authorizeTrigger` helper that rejects secret-less requests with 403 (per-request fail-closed; the daemon still starts)
- [x] 1.3 Log a startup security warning for both postures (fail-closed active, and opt-out enabled)
- [x] 1.4 Log a per-receipt security warning when serving unauthenticated under the opt-out
- [x] 1.5 Remove `handleManualTrigger`'s secret-less accept branch (folded into `authorizeTrigger`)
- [x] 1.6 Tests: 403 + no reconcile on all three endpoints, opt-out path, 401 on bad signature, 202 on good signature, single-response write
- [x] 1.7 Add `BOSUN_LISTEN_ADDR` bind knob (default unchanged: all interfaces)

## 2. Unix socket peer-credential enforcement

- [x] 2.1 Add `AllowedUIDs` (and enforcement-disable flag) to `SocketConfig`, parsed from `BOSUN_SOCKET_ALLOWED_UIDS`
- [x] 2.2 Enforce peer UID in `SocketServer.handleTrigger` (and other mutating handlers) using `InjectPeerCred` context
- [x] 2.3 Fail-closed when peer credentials are unavailable (incl. `peercred_other.go` no-op path) unless enforcement is explicitly disabled
- [x] 2.4 Tests: denied non-allowlisted UID (403), allowed owner UID (202), unavailable-creds fail-closed

## 3. Socket permission integrity

- [x] 3.1 Honor `SocketConfig.SocketMode` instead of hard-coded `0660` in `socket.go` Start
- [x] 3.2 Avoid process-global umask by binding inside a private `0700` staging directory, applying the configured mode, and atomically renaming the socket into place
- [x] 3.3 Tests: socket initial mode equals configured mode; no permissive window; concurrent modes, unsafe stale entries, replacement-safe cleanup, and publish-error cleanup

## 4. HTTP request hardening

- [x] 4.1 Add `ReadHeaderTimeout` and `MaxHeaderBytes` to the webhook `http.Server` (server.go)
- [x] 4.2 Add `ReadHeaderTimeout` and `MaxHeaderBytes` to the socket `http.Server` (socket.go)
- [x] 4.3 Add `ReadHeaderTimeout` and `MaxHeaderBytes` to the TCP `http.Server` (tcp.go)
- [ ] 4.4 Wrap `SocketServer.handleTrigger` body in `http.MaxBytesReader`
- [ ] 4.5 Wrap `TCPServer.handleTrigger` body in `http.MaxBytesReader`
- [ ] 4.6 Wrap `handleAPITrigger` body in `http.MaxBytesReader`
- [ ] 4.7 Tests: slow-header disconnect; oversized body rejected on each of socket/TCP/API trigger

## 5. Anonymous endpoint exposure control

- [ ] 5.1 Add metrics/widget exposure opt-in to `Config`, parsed from env
- [ ] 5.2 Gate `/metrics` and `/api/widget` to localhost / auth / opt-in in `NewServer`
- [ ] 5.3 Tests: default refuses remote `/metrics` and `/api/widget`; opt-in serves them
- [x] 5.4 Add one bounded public-health projection (`status`, `ready`, `uptime`) and use it for HTTP, TCP, and Unix-socket `/health`
- [x] 5.5 Keep `/status` as the operator diagnostic contract (bearer-protected on TCP, Unix-socket access locally); do not vary `/health` by Authorization header
- [x] 5.6 Tests: exact public schema and 200/503/405 status semantics on all three transports, including sensitive reconcile errors and circuit-breaker/subsystem state
- [x] 5.7 Change `daemon.Client.Health` to decode a dedicated bounded response type; test 200 and 503 decoding plus malformed/transport errors
- [x] 5.8 Keep `daemon-status` diagnostics sourced from `/status`; migrate `validate` to request `/status` for sanitized last-error detail after a non-healthy result; test human/JSON and status-failure behavior
- [x] 5.9 Keep the standalone webhook receiver's `/health` proxy bounded, retain its `/ready` contract, and reject non-GET requests to both endpoints before contacting the daemon; test proxy status, schema, and method handling

## 6. Webhook payload sanitization

- [x] 6.1 Add a sanitize helper (length cap + control-char strip) for pusher name
- [x] 6.2 Apply it before logging and the downstream `github:%s` reconcile source in both direct and standalone `handleGitHubWebhook` paths
- [x] 6.3 Tests: oversized name truncated; control characters stripped

## 7. Documentation

- [x] 7.1 Update `docs/security.md` with the daemon security posture
- [x] 7.2 Update `skills/onboard/resources/gitops.md`
- [x] 7.3 Add new env vars to the `CLAUDE.md` env-var table
- [x] 7.4 Document the bounded `/health` schema and `/status` diagnostic route in `docs/commands.md`, `docs/security.md`, `docs/gitops.md`, `docs/cli-reference.md`, and the onboard GitOps resource

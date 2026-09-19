## ADDED Requirements

### Requirement: Socket config route credential scope

The Unix socket `GET /config` route SHALL require the same peer-credential authorization as the mutating routes, despite being a read.

The response it builds includes the webhook secret, and that secret is exactly what authorizes a forced reconcile on the HTTP listener. A read that hands out the credential is therefore a mutation by proxy, and the documented connect-versus-mutate split is bypassable without this rule: a connector reads the secret, signs a trigger, and reaches the reconcile path it was not authorized to reach.

The response builder SHALL emit the webhook secret only when a caller explicitly asks for it, so a future consumer of the same builder cannot inherit the credential by default. Routes that return no credential — `/status`, `/health` — SHALL continue to rest on socket file permissions alone.

#### Scenario: Unauthorized peer cannot read the webhook secret

- **WHEN** a local process whose UID is neither the daemon UID nor in `BOSUN_SOCKET_ALLOWED_UIDS` sends `GET /config` on the Unix socket
- **THEN** the daemon responds 403
- **AND** the webhook secret does not appear in the response

#### Scenario: Peer credentials unavailable fails closed on /config

- **WHEN** `GET /config` arrives on a connection for which peer credentials cannot be determined
- **THEN** the daemon responds 403 unless `BOSUN_ALLOW_UNAUTHENTICATED_SOCKET=true` is set

#### Scenario: Authorized peer still receives the configuration

- **WHEN** a process running as the daemon's own UID sends `GET /config`
- **THEN** the daemon responds 200 with the configuration including the webhook secret

#### Scenario: Credential-free routes are unchanged

- **WHEN** any socket peer sends `GET /status` or `GET /health`
- **THEN** the response is unchanged by this requirement
- **AND** no credential is included in it

### Requirement: Standalone webhook receiver fail-closed

The standalone `bosun webhook` receiver SHALL apply its own fail-closed authentication gate on its own HTTP port, rather than relying on the daemon's gate.

The receiver forwards to the daemon over the peer-authorized Unix socket, and the daemon never re-applies its webhook gate to a socket trigger. Without its own gate, the receiver is an unauthenticated path to a reconcile that the daemon believes it has already refused. When no secret is resolved, the receiver SHALL reject every provider endpoint with HTTP 403 and SHALL NOT forward.

The receiver SHALL read the same `BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK` opt-out the daemon uses, with the same strict `true` match, and SHALL log a security warning at startup and on every accepted unauthenticated receipt.

`--fetch-secret` retrieves the secret over the socket `/config` route and therefore SHALL require the receiver to run as an authorized socket peer; a failure to fetch SHALL leave the receiver in its fail-closed state rather than starting without a secret.

#### Scenario: No secret rejects every provider endpoint

- **WHEN** the receiver starts with no resolved webhook secret and `BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK` unset
- **THEN** a POST to the GitHub, GitLab, Gitea, or Bitbucket endpoint is rejected with 403
- **AND** nothing is forwarded to the daemon socket

#### Scenario: Explicit opt-out permits unauthenticated receipts with a warning

- **WHEN** the receiver starts with no secret and `BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK=true`
- **THEN** requests are accepted and forwarded
- **AND** a security warning is logged at startup and on each accepted receipt

#### Scenario: Secret fetch failure leaves the receiver fail-closed

- **WHEN** `--fetch-secret` cannot retrieve the secret because the receiver is not an authorized socket peer
- **THEN** the receiver does not start with an empty secret silently
- **AND** its endpoints remain fail-closed

### Requirement: Untrusted attribution and ref sanitization

Every operator-facing text sink in the daemon and the standalone receiver SHALL neutralize attacker-chosen text before it reaches a log line, an error string, a metric label, a trace attribute, or an alert body.

Webhook attribution (pusher, sender, actor) and pushed ref names are supplied by the request body and are chosen by whoever can reach the endpoint. They SHALL be stripped of control, formatting, and separator characters and capped in length for all four providers — GitHub, GitLab, Gitea, Bitbucket — not GitHub alone, and on both the daemon's own handler and the standalone receiver's normalizer. The socket `/trigger` source string SHALL be treated the same way.

Stripping SHALL occur before the length cap, so a control character cannot survive by sitting beyond the truncation point.

#### Scenario: Forged log line in attribution is neutralized

- **WHEN** a webhook body from any supported provider carries newlines or terminal escape sequences in its attribution field
- **THEN** those characters are removed before the value reaches a log line, an error, a metric, a trace attribute, or an alert
- **AND** the operator-visible line count is unchanged

#### Scenario: Ref names are sanitized on every provider path

- **WHEN** a pushed ref name contains control or separator characters
- **THEN** the sanitized ref is what appears in operator-facing output on both the daemon handler and the standalone receiver

#### Scenario: Socket trigger source is sanitized

- **WHEN** a socket `POST /trigger` supplies a source string containing control characters
- **THEN** the sanitized source is what appears in the reconcile attribution

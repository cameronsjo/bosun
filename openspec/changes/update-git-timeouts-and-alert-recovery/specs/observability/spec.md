## ADDED Requirements

### Requirement: HTTP Request Client Attribution

Every HTTP request log entry emitted by the daemon's request middleware SHALL
carry the observed peer address in a `remote_addr` field, sourced from the
connection itself. This field is the observed fact and SHALL always be present.

When — and only when — the observed peer is a configured trusted proxy, the
entry SHALL additionally carry a `forwarded_for` field holding the IP address
parsed from the **first** comma-separated element of the request's
`X-Forwarded-For` header. This field is a claim made by the sender, not an
observation.

When that first element does not parse as an IP address, `forwarded_for` SHALL be
omitted and later elements SHALL NOT be scanned. Scanning onward lets a sender
prepend a garbage element to shift which entry gets recorded, turning a malformed
header into a choice of attributed origin.

The two fields SHALL be recorded separately and SHALL NOT be collapsed into a
single "client address" field, and `forwarded_for` SHALL NOT be preferred over
`remote_addr` when both are present. The daemon's listener binds all interfaces
by design, so any host that can reach it may send a well-formed
`X-Forwarded-For`; parsing a value as an IP address does not make it true.
Recording a forged header as the sender would make the next investigation
confidently wrong.

When the observed peer is not a trusted proxy, `forwarded_for` SHALL be omitted
entirely, even if an `X-Forwarded-For` header is present on the request.

The set of trusted proxies SHALL be a list of CIDR prefixes, defaulting to
empty, so `forwarded_for` is never emitted until an operator names a proxy. An
empty set SHALL NOT be interpreted as "trust everything". A bare IP address SHALL
be accepted and treated as a single-host prefix.

Membership SHALL be tested in canonical IP terms, never as string comparison:
split the host from `r.RemoteAddr` (which is `host:port`), parse that host as an
IP, and test it against the parsed configured prefixes. Comparing the raw
`RemoteAddr` string can never match a configured value, and it fails in the safe
direction — silently never emitting `forwarded_for` — so a test that only asserts
the field is absent passes over the bug. Canonical comparison SHALL cover IPv4
and IPv6 alike, including bracketed IPv6 hosts and IPv4-mapped forms.

Configured entries SHALL be IP addresses or CIDR prefixes only. A hostname, an
empty string, or any other unparseable value SHALL be **rejected at configuration
load** rather than silently ignored, so a typo cannot quietly disable
attribution. Hostname matching is disallowed outright: it would make the trust
decision depend on a resolver.

#### Scenario: No trusted proxies configured

- **GIVEN** no trusted proxies are configured
- **WHEN** any request completes, with or without an `X-Forwarded-For` header
- **THEN** the log entry includes `remote_addr`
- **AND** the log entry has no `forwarded_for` field

#### Scenario: Direct request records the peer only

- **GIVEN** a request arriving from a host that is not a configured trusted proxy
- **WHEN** the request completes
- **THEN** the log entry includes `remote_addr` with the connection's peer address
- **AND** the log entry has no `forwarded_for` field

#### Scenario: Untrusted peer sending a forwarded header is not believed

- **GIVEN** a request from a host that is not a configured trusted proxy, carrying `X-Forwarded-For: 203.0.113.9`
- **WHEN** the request completes
- **THEN** the log entry's `remote_addr` is the actual peer address
- **AND** the log entry has no `forwarded_for` field

#### Scenario: Port is stripped before the membership test

- **GIVEN** a trusted-proxy list containing the proxy's exact IP address
- **WHEN** a request arrives from that proxy, so `r.RemoteAddr` is that IP followed by a colon and an ephemeral port
- **THEN** the proxy is recognised as trusted
- **AND** `forwarded_for` is emitted

#### Scenario: IPv6 peer is matched canonically

- **GIVEN** a trusted-proxy list containing an IPv6 prefix
- **WHEN** a request arrives from an address in that prefix, so `r.RemoteAddr` is a bracketed IPv6 host with a port
- **THEN** the proxy is recognised as trusted
- **AND** `forwarded_for` is emitted

#### Scenario: Source port does not affect the decision

- **GIVEN** a trusted proxy and two requests from it on different ephemeral source ports
- **WHEN** both complete
- **THEN** both are recognised as trusted

#### Scenario: Unparseable trusted-proxy entry is rejected

- **GIVEN** a trusted-proxy list containing an entry that is neither an IP nor a CIDR — a hostname or an empty string
- **WHEN** the configuration is loaded
- **THEN** loading fails with an error naming the offending entry

#### Scenario: Malformed first forwarded element omits the field

- **GIVEN** a request from a trusted proxy carrying `X-Forwarded-For: not-an-ip, 203.0.113.9`
- **WHEN** the request completes
- **THEN** `forwarded_for` is omitted
- **AND** `203.0.113.9` is NOT recorded, because later elements are not scanned

#### Scenario: Trusted proxy contributes a forwarded address

- **GIVEN** a request arriving from a configured trusted proxy, carrying `X-Forwarded-For: 203.0.113.9, 198.51.100.4`
- **WHEN** the request completes
- **THEN** `remote_addr` is the proxy's address
- **AND** `forwarded_for` is `203.0.113.9`

#### Scenario: Malformed forwarded header is dropped, not logged raw

- **GIVEN** a request from a configured trusted proxy carrying an `X-Forwarded-For` value that does not parse as an IP address
- **WHEN** the request completes
- **THEN** `remote_addr` is still recorded
- **AND** `forwarded_for` is omitted

#### Scenario: Unmatched path is attributable

- **GIVEN** a request to a path the daemon does not register, producing a 404
- **WHEN** the request completes
- **THEN** the log entry includes the request path, the 404 status, and `remote_addr`

## ADDED Requirements

### Requirement: Git Network Timeout Enforcement

Each declared git network timeout SHALL bound the wall-clock duration of the operation it names. When the bound elapses, the call SHALL return a timeout error and its resources SHALL be released before the caller resumes.

The bound SHALL NOT be satisfied by handing the work to a detached goroutine and returning. A goroutine that outlives its bound keeps writing to the working tree, keeps the auth handle open, and cannot be cancelled — an in-flight guard built over one is fail-*stuck*, clearing only on process restart, which is worse than the unbounded stall it replaces.

A `GitSSHDialTimeout` (default 30 seconds) SHALL bound TCP connection establishment for git operations over SSH.

The SSH handshake SHALL be bounded separately, by a deadline on the connection itself. Setting `ssh.ClientConfig.Timeout` is **not** sufficient: the transport applies that value only to the dial context (`plumbing/transport/ssh/common.go:192`) and then hands the raw connection to `ssh.NewClientConn` (`:198`), which honors no deadline. A remote that completes the TCP accept and then never speaks therefore stalls indefinitely with `Timeout` set, so any implementation satisfied only by setting that field leaves the handshake unbounded.

`GitCloneTimeout` and `GitFetchTimeout` SHALL bound their respective whole operations, including the reference-negotiation and packfile-transfer phases. Because the transport consults the operation context only *between* protocol steps, satisfying this requires a deadline on the connection rather than on the context alone.

That connection deadline SHALL be derived from the **fixed operation deadline**, not refreshed freely on each read. A deadline reset to "now plus an idle interval" on every successful read enforces only an idle timeout: a peer that emits one byte before each interval elapses keeps the transfer — and the reconcile lock — alive indefinitely while every individual read succeeds. Each read deadline SHALL therefore be the earlier of any idle bound and the operation's absolute deadline, and the operation SHALL terminate at that absolute deadline regardless of how recently data arrived.

Applying the dial timeout SHALL preserve every field of the resolved authentication method — at minimum `User`, `Auth`, `HostKeyCallback`, and `HostKeyAlgorithms`. An implementation that supplies a client configuration carrying only a timeout SHALL be treated as a defect: `overrideConfig` (`ssh/common.go:290-307`) assigns every field unconditionally via reflection, zero values included, at `:142` — *after* the nil-`HostKeyCallback` repair at `:128-134`. The same obligation applies to any replacement transport that builds the client itself.

A git operation's own timeout SHALL be applied whether or not the caller's context already carries a deadline. `Clone` currently applies `GitCloneTimeout` only when `ctx.Deadline()` reports none (`git.go:362-367`), while the daemon sets a `ReconcileTimeout` deadline on every reconcile cycle (`daemon.go:1008`) — so on the daemon path, the only path that runs unattended, `GitCloneTimeout` is never applied and the error text names it regardless. The effective bound SHALL be the **earlier** of the operation's own timeout and any caller deadline.

A timeout error SHALL report the **actual elapsed time**, not the configured bound, and SHALL name the bound that actually expired rather than a constant that may never have been applied. The reconciler SHALL log the timeout at the point it is raised, at error level, with the operation name, the sanitized repository URL, the branch, the elapsed milliseconds, and the effective bound in milliseconds. This applies to clone and fetch alike; the existing clone timeout log (`git.go:401-407`) carries only `operation` and `duration_ms` and SHALL gain the remaining fields.

The URL written to that log SHALL have credentials removed from **both** userinfo and query parameters. `SanitizeGitURL` (`git_auth.go:145-154`) currently clears `parsed.User` only and returns the remaining parsed URL intact, so a repository URL carrying a credential as a query parameter — `?token=`, `?access_token=`, `?password=` and equivalents — would write that secret into a log line this change newly adds. Sanitization SHALL redact credential-bearing query parameter *values* while leaving the rest of the URL legible enough to identify the repository.

The reconcile lock SHALL NOT be held past a git operation's configured bound on account of that operation.

#### Scenario: Unreachable remote is bounded at the dial

- **GIVEN** a remote address that accepts no TCP connection and sends no reset — a blackholed address
- **WHEN** the reconciler fetches from that remote with `GitSSHDialTimeout` configured to a short test value
- **THEN** the fetch fails within that value plus a small margin
- **AND** the returned error identifies the failure as a timeout

#### Scenario: Stalled SSH handshake is bounded

- **GIVEN** a listener that accepts the TCP connection and then never speaks
- **WHEN** the reconciler fetches from that remote
- **THEN** the fetch fails within the connection read deadline plus a small margin
- **AND** it does NOT wait for the whole `GitFetchTimeout`
- **AND** setting `ssh.ClientConfig.Timeout` alone SHALL NOT be accepted as satisfying this scenario, because the dial has already succeeded when the stall begins

#### Scenario: Slow-drip transfer is bounded at the operation deadline

- **GIVEN** a remote that completes the handshake and then emits a single byte just before each idle interval elapses, indefinitely
- **WHEN** the reconciler fetches from that remote
- **THEN** the fetch terminates at the configured operation bound
- **AND** it does NOT continue because individual reads kept succeeding
- **AND** the reconcile lock is released at that bound

#### Scenario: Stalled transfer is bounded

- **GIVEN** a remote that completes the SSH handshake and then stalls mid-transfer
- **WHEN** the reconciler fetches from that remote
- **THEN** the fetch fails within the configured fetch bound plus a small margin
- **AND** no goroutine continues the abandoned fetch after the call returns

#### Scenario: Authentication survives the dial timeout

- **GIVEN** a resolved SSH authentication method carrying a `User`, an `Auth`, a `HostKeyCallback`, and `HostKeyAlgorithms`
- **WHEN** the dial timeout is applied to that authentication method
- **THEN** the resulting client configuration retains all four fields unchanged
- **AND** the configuration additionally carries the configured timeout

#### Scenario: Credentialed URL is redacted in the timeout log

- **GIVEN** a repository URL carrying a credential in a query parameter
- **WHEN** a timeout is logged for an operation against that URL
- **THEN** the logged URL contains neither the credential value nor userinfo
- **AND** the repository remains identifiable from what survives

#### Scenario: Timeout error reports elapsed time

- **GIVEN** a configured fetch timeout of a known value
- **WHEN** a fetch is abandoned after measurably longer than that value
- **THEN** the error text names the measured elapsed duration, not the configured value
- **AND** an error-level log entry is emitted at the throw site carrying `operation`, the sanitized URL, `branch`, `elapsed_ms`, and `timeout_ms`

#### Scenario: Clone is bounded with no caller deadline

- **GIVEN** no local repository exists and the remote stalls during clone
- **WHEN** the reconciler clones with a caller context carrying no deadline
- **THEN** the clone fails within `GitCloneTimeout` plus a small margin
- **AND** the failure log carries `operation`, the sanitized URL, `branch`, `elapsed_ms`, and `timeout_ms`

#### Scenario: Clone timeout applies under a longer caller deadline

- **GIVEN** a caller context carrying a deadline longer than `GitCloneTimeout` — as the daemon supplies on every reconcile cycle
- **WHEN** the reconciler clones and the remote stalls
- **THEN** the clone fails at `GitCloneTimeout`, not at the caller's later deadline
- **AND** the error names the bound that expired

#### Scenario: Caller deadline shorter than the operation timeout still wins

- **GIVEN** a caller context whose deadline is shorter than `GitFetchTimeout`
- **WHEN** the reconciler fetches and the remote stalls
- **THEN** the fetch fails at the caller's deadline
- **AND** the error reports the elapsed time rather than `GitFetchTimeout`

### Requirement: Git Timeout Configuration

The git operation timeouts SHALL be fields on the git operations value rather than package-level constants, so that they can be shortened by a test and set by the caller that constructs them. Each field SHALL default to its existing constant value when unset: `GitCloneTimeout` 5 minutes, `GitFetchTimeout` 2 minutes, `GitSSHDialTimeout` 30 seconds.

A zero or negative configured timeout SHALL be treated as unset and SHALL fall back to the default, so a partially-populated value cannot silently produce an unbounded or instantly-expiring operation.

Operator-facing configuration of these values is **out of scope for this change**: no config key and no environment variable is added. The fields exist to make the bounds testable and caller-settable. A later change that exposes them to operators owes the `BOSUN_*` environment variable, its row in the environment-variable table, and its own scenarios.

#### Scenario: Unset timeouts use defaults

- **WHEN** git operations are constructed without explicit timeout fields
- **THEN** the clone timeout is 5 minutes, the fetch timeout is 2 minutes, and the SSH dial timeout is 30 seconds

#### Scenario: Explicit timeout is honoured

- **GIVEN** git operations constructed with a fetch timeout of 200 milliseconds
- **WHEN** a fetch stalls
- **THEN** it is abandoned after approximately 200 milliseconds

#### Scenario: Non-positive timeout falls back to the default

- **GIVEN** git operations constructed with a fetch timeout of zero
- **WHEN** a fetch runs
- **THEN** the default 2-minute bound applies rather than an unbounded or immediately-expired operation

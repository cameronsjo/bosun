## ADDED Requirements

### Requirement: Git Network Timeout Enforcement

Each declared git network timeout SHALL bound the wall-clock duration of the
operation it names. An operation that exceeds its configured timeout SHALL be
abandoned and SHALL return a timeout error; it SHALL NOT continue running past
the bound while a caller's deadline elapses.

A `GitSSHDialTimeout` (default 30 seconds) SHALL bound TCP connection
establishment and the SSH handshake for git operations over SSH. `GitCloneTimeout`
and `GitFetchTimeout` SHALL bound their respective whole operations, including
the reference-negotiation and packfile-transfer phases.

Applying the dial timeout SHALL preserve every field of the resolved
authentication method — at minimum `User`, `Auth`, `HostKeyCallback`, and
`HostKeyAlgorithms`. An implementation that supplies a client configuration
carrying only a timeout SHALL be treated as a defect, because the underlying
transport assigns configuration fields unconditionally including zero values,
and a fetch that authenticates before the change MUST still authenticate after
it.

A timeout error SHALL report the **actual elapsed time**, not the configured
bound. The reconciler SHALL log the timeout at the point it is raised, at error
level, with the operation name, the sanitized repository URL, the branch, the
elapsed milliseconds, and the configured bound in milliseconds.

Timeouts SHALL apply equally to clone and to fetch. The reconcile lock SHALL NOT
be held past a git operation's configured bound on account of that operation.

#### Scenario: Stalled SSH handshake is bounded

- **GIVEN** a remote that accepts the TCP connection and then never speaks
- **WHEN** the reconciler fetches from that remote
- **THEN** the fetch fails within `GitSSHDialTimeout` plus a small margin
- **AND** the returned error identifies the failure as a timeout

#### Scenario: Stalled transfer is bounded

- **GIVEN** a remote that completes the SSH handshake and then stalls mid-transfer
- **WHEN** the reconciler fetches from that remote
- **THEN** the fetch fails within `GitFetchTimeout` plus a small margin
- **AND** the reconciler does not block indefinitely waiting on the read

#### Scenario: Authentication survives the dial timeout

- **GIVEN** a resolved SSH authentication method with a host key callback and a configured user
- **WHEN** the dial timeout is applied to that authentication method
- **THEN** the resulting client configuration retains the original `User`, `Auth`, `HostKeyCallback`, and `HostKeyAlgorithms`
- **AND** a fetch against a reachable remote authenticates successfully

#### Scenario: Timeout error reports elapsed time

- **GIVEN** a configured `GitFetchTimeout` of 2 minutes
- **WHEN** a fetch is abandoned after 2 minutes and 3 seconds of wall-clock time
- **THEN** the error text names the elapsed duration, not the configured 2 minutes
- **AND** an error-level log entry is emitted at the throw site carrying `operation`, the sanitized URL, `branch`, `elapsed_ms`, and `timeout_ms`

#### Scenario: Clone is bounded on the same terms

- **GIVEN** no local repository exists and the remote stalls during clone
- **WHEN** the reconciler clones
- **THEN** the clone fails within `GitCloneTimeout` plus a small margin
- **AND** the failure is logged with the same fields as a fetch timeout

### Requirement: Git Timeout Configuration

The git operation timeouts SHALL be fields on the git operations value rather
than package-level constants, so that they can be tuned by an operator and
shortened by a test. Each field SHALL default to its existing constant value
when unset: `GitCloneTimeout` 5 minutes, `GitFetchTimeout` 2 minutes,
`GitSSHDialTimeout` 30 seconds.

A zero or negative configured timeout SHALL be treated as unset and SHALL fall
back to the default, so a partially-populated value cannot silently produce an
unbounded or instantly-expiring operation.

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

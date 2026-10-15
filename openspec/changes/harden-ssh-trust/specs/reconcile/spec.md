## MODIFIED Requirements

### Requirement: Git Repository Sync

The reconciler SHALL clone the repository on first run and pull on subsequent
runs. SSH authentication SHALL prefer a usable SSH agent and then configured
private-key files. SSH host-key verification SHALL use
`BOSUN_SSH_KNOWN_HOSTS` first and `/config/known_hosts` second. The
user-profile `~/.ssh/known_hosts` path SHALL NOT be consulted by the Git
transport. If no configured candidate exists or a candidate is malformed,
Git authentication SHALL fail before network access. The only bypass SHALL be
`BOSUN_SSH_INSECURE_HOST_KEY=true`, which MUST be an explicit,
case-insensitive exact value.

Standalone reconcile and daemon startup SHALL use the same validation path as
clone and fetch, so a missing or malformed Git trust policy prevents the daemon
from binding listeners. A valid candidate SHALL install a verifying callback on
both agent and key-file authentication methods.

#### Scenario: Git SSH trust file is absent

- **WHEN** an SSH repository is configured with no `BOSUN_SSH_KNOWN_HOSTS`, no
  `/config/known_hosts`, and no insecure opt-out
- **THEN** Git authentication returns an actionable `known_hosts` error before
  network access
- **AND** daemon startup validation fails before listeners bind

#### Scenario: Git SSH trust file is malformed

- **WHEN** the first configured `known_hosts` candidate exists but cannot be
  parsed
- **THEN** Git authentication fails with the candidate path in the error
- **AND** it does not silently fall through to another candidate or accept an
  unverified host

#### Scenario: Valid Git trust file verifies host keys

- **WHEN** a configured candidate is parseable
- **THEN** agent and key-file SSH authentication use a verifying host-key
  callback
- **AND** an unknown host key is rejected

#### Scenario: Explicit Git trust opt-out

- **WHEN** `BOSUN_SSH_INSECURE_HOST_KEY=true`
- **THEN** Git authentication may use an accepting callback without consulting
  known-hosts files

### Requirement: File Deployment

Remote SSH and SCP operations SHALL use strict host-key verification. When the
first usable configured candidate from `BOSUN_SSH_KNOWN_HOSTS` or
`/config/known_hosts` exists, the command SHALL pass
`StrictHostKeyChecking=yes` and `UserKnownHostsFile=<candidate>`. When no
configured candidate exists, the command SHALL pass only
`StrictHostKeyChecking=yes`, leaving OpenSSH's normal trust stores available
for already-pinned hosts while refusing unknown hosts. The command SHALL NOT
use `accept-new` as an implicit trust decision. The explicit
`BOSUN_SSH_INSECURE_HOST_KEY=true` opt-out SHALL disable these checks for the
operation.

#### Scenario: Remote deploy has a configured trust file

- **WHEN** a remote deploy runs and a configured `known_hosts` candidate exists
- **THEN** every SSH/SCP leg carries strict checking and that candidate path
- **AND** an unknown host key is refused

#### Scenario: Remote deploy has no configured candidate

- **WHEN** a remote deploy runs without a configured `known_hosts` candidate
  and without the insecure opt-out
- **THEN** every SSH/SCP leg carries `StrictHostKeyChecking=yes`
- **AND** it does not carry `StrictHostKeyChecking=accept-new`
- **AND** an unpinned host is refused before the secret-bearing transfer

#### Scenario: Explicit deploy trust opt-out

- **WHEN** `BOSUN_SSH_INSECURE_HOST_KEY=true`
- **THEN** remote SSH and SCP legs omit strict host-key enforcement
- **AND** the opt-out remains visible in operator documentation


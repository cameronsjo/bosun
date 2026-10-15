## ADDED Requirements

### Requirement: Trusted Filesystem Namespaces

Bosun SHALL resolve local deploy destination mutations, content-hash skip
comparisons, managed stale-file removals, template source/include reads, and
rollback extraction writes through a trusted root namespace whenever the
platform provides one. An escaping, untrusted, or unopenable path SHALL fail
closed and SHALL NOT be silently skipped. Standard atomic-swap deployment MAY
use a trusted sibling temporary directory and trusted parent rename sequence,
and documentation SHALL state that narrower guarantee precisely.

#### Scenario: Swapped destination ancestor

- **WHEN** a container replaces a destination ancestor with a symlink to a
  directory outside the configured application-data root after validation
- **THEN** the local deploy fails before writing through that symlink
- **AND** no file or directory is created outside the trusted root

#### Scenario: Hash skip reads the pinned destination

- **WHEN** a content-hash deploy compares an existing destination while a
  container races an ancestor replacement
- **THEN** comparison and the eventual copy use the same trusted destination
  namespace
- **AND** an unsafe comparison fails closed instead of reporting a silent skip

#### Scenario: Managed stale removal

- **WHEN** a previously managed file is absent from the rendered source tree
- **THEN** Bosun removes only the corresponding relative entry beneath the
  trusted destination root
- **AND** an ancestor replacement cannot redirect removal outside that root

#### Scenario: Template source and include race

- **WHEN** a template source or an allowlisted `include`/`fromJsonFile` path is
  replaced between validation and reading
- **THEN** Bosun reads the already validated descriptor in the pinned source
  namespace
- **AND** a symlink, special file, ancestor escape, or unopenable path fails
  rendering without reading outside the allowlist

#### Scenario: Rollback archive link chain

- **WHEN** a rollback archive uses a sequence of links whose lexical names stay
  under the extraction directory but whose realized path escapes it
- **THEN** extraction fails before an outside file is created
- **AND** the temporary extraction tree is cleaned up

### Requirement: Reconcile Lock Ownership

On Unix, Bosun SHALL create or migrate the reconcile lock directory with
owner-only permissions, reject symlinked or untrusted lock paths, tighten an
existing lock file to `0600`, and fail closed when these guarantees cannot be
established. The error SHALL name the affected path and an actionable ownership
or permission fix. On Windows, Bosun SHALL retain the existing `LockFileEx`
behavior and consumer documentation SHALL identify the remaining reparse/ACL
residual.

#### Scenario: New owner-only lock

- **WHEN** the configured Unix lock path and parent do not exist
- **THEN** Bosun creates the parent with mode `0700` and the lock file with mode
  `0600`
- **AND** another local user cannot open the lock by ordinary filesystem
  permissions

#### Scenario: Insecure lock path fails closed

- **WHEN** a Unix lock parent or lock file is symlinked, untrusted, or cannot be
  tightened to owner-only permissions
- **THEN** lock acquisition fails before reconciliation proceeds
- **AND** the error names the path and tells the operator to fix owner and
  owner-only permissions

## ADDED Requirements

### Requirement: Infra Directory Misconfiguration Hint

The reconciler SHALL diagnose a likely `BOSUN_INFRA_DIR` misconfiguration when the configured infra/staging directory has no `compose/` child (the condition that produces `ErrComposeDirMissing`), before surfacing the failure.

To do so, the reconciler SHALL scan the immediate child directories of the
infra/staging directory and identify any whose own contents include a
`compose/` subdirectory. Dot-prefixed children (e.g. `.beads`, `.git`) SHALL be excluded,
and a `compose` entry that is a file rather than a directory SHALL NOT count as
a candidate.

- When one or more candidates are found, the surfaced `ErrComposeDirMissing`
  failure SHALL name them and SHALL include a suggested `BOSUN_INFRA_DIR` value
  formed by joining the current `InfraSubDir` with the candidate name.
- When no candidate is found, the failure SHALL retain its existing bare message
  naming the missing compose directory path, with no suggestion.

The hint SHALL be diagnostic only. It SHALL NOT auto-correct `InfraSubDir`,
SHALL NOT change which paths are deployed, and SHALL NOT alter the unconditional
failure semantics of `ErrComposeDirMissing` defined by the Deploy Sync
Invariants requirement. The scan SHALL run only on the failing path (compose
directory absent), never on a successful reconcile.

#### Scenario: Single candidate names the infra dir to set

- **WHEN** `ExtractDeclaredState` finds no `compose/` under the configured infra dir
- **AND** exactly one child directory (e.g. `unraid`) contains a `compose/` subdirectory
- **THEN** the reconcile fails with `ErrComposeDirMissing`
- **AND** the error names `unraid` as the candidate infra directory
- **AND** the surfaced error includes a suggested `BOSUN_INFRA_DIR` value formed from the current `InfraSubDir` joined with `unraid`

#### Scenario: Multiple candidates are all listed

- **WHEN** `ExtractDeclaredState` finds no `compose/` under the configured infra dir
- **AND** more than one child directory contains a `compose/` subdirectory
- **THEN** the error lists every candidate directory
- **AND** directs the operator to set `BOSUN_INFRA_DIR` to one of them

#### Scenario: No candidate keeps the bare error

- **WHEN** `ExtractDeclaredState` finds no `compose/` under the configured infra dir
- **AND** no child directory contains a `compose/` subdirectory
- **THEN** the reconcile fails with `ErrComposeDirMissing` naming the missing path
- **AND** no `BOSUN_INFRA_DIR` suggestion is appended

#### Scenario: Dot-directories and compose files are not candidates

- **WHEN** scanning for candidate infra directories
- **AND** a child is dot-prefixed (e.g. `.beads`) or its `compose` entry is a file
- **THEN** that child is not offered as a candidate

#### Scenario: Hint does not change failure semantics

- **WHEN** a candidate infra directory is identified
- **THEN** the reconcile still fails unconditionally on `ErrComposeDirMissing`
- **AND** `BOSUN_ALLOW_EMPTY_DECLARED_STATE` does not suppress the failure
- **AND** no deploy or `InfraSubDir` value is changed automatically

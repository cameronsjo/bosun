## ADDED Requirements

### Requirement: Directory-Aware Deploy Change Tracking

Content-hash sync SHALL include each descendant directory it actually creates
in the canonical staging-relative deploy change set, including empty
directories. It SHALL exclude an ordinarily created deploy target root and
directories that already existed, so plumbing and no-op reconciles do not
trigger post-sync hooks. A managed file-to-directory transition SHALL include
the transitioned path, including a top-level target path, and every descendant
directory created from its source subtree.

Each recorded directory SHALL be subject to the post-deploy existence, type,
and fresh-mtime invariant. A change set containing directories but no regular
file writes SHALL NOT suppress the existing content invariant: every regular
source file must already be present and byte-identical at the destination.

#### Scenario: Newly created empty directory triggers a matching hook

- **WHEN** content-hash sync creates a previously absent descendant directory containing no files
- **THEN** its staging-relative path is included in the deploy change set
- **AND** a matching post-sync hook is eligible to run

#### Scenario: Existing directory remains a no-op

- **WHEN** a source directory already exists at the destination
- **THEN** that directory is not included in the deploy change set
- **AND** an otherwise unchanged reconcile does not trigger a hook

#### Scenario: Deploy target root is plumbing

- **WHEN** content-hash sync creates the deploy target root or a missing ancestor needed to reach it
- **THEN** neither the root marker nor its ancestors are included in the deploy change set

#### Scenario: Descendant directory keeps its canonical target prefix

- **WHEN** content-hash sync creates descendant directory `cache` for deploy target `appdata/service`
- **THEN** local deploy aggregation records `appdata/service/cache` in the canonical staging-relative change set
- **AND** post-sync hook matching evaluates that prefixed path rather than the producer-local `cache` path

#### Scenario: Directory-only change set does not mask a missing file

- **WHEN** the deploy change set contains newly created directories but no regular file writes
- **AND** a regular source file is missing or byte-different at the destination
- **THEN** the post-deploy invariant fails before compose-up

#### Scenario: Recorded directory must be fresh and remain a directory

- **WHEN** a recorded directory is missing, has an mtime older than the reconcile start, or is a non-directory destination entry (including a symlink)
- **THEN** the post-deploy invariant fails before compose-up

#### Scenario: Nested managed file becomes an empty directory tree

- **WHEN** a previously managed regular file below a deploy target is replaced by a source directory containing empty descendants
- **THEN** the transitioned path and each created descendant directory are included in the deploy change set
- **AND** matching hooks can observe the type transition

#### Scenario: Top-level managed file becomes a directory

- **WHEN** a deploy target previously recorded as a managed regular file is replaced by a source directory
- **THEN** the top-level target path is included in the deploy change set despite ordinary target-root creation being plumbing
- **AND** each created descendant directory is included under that target path

#### Scenario: Deploy modes without authoritative path tracking retain fallback behavior

- **WHEN** a standard-copy local deploy or remote deploy does not produce authoritative per-path change evidence
- **THEN** an empty `WrittenFiles` result is not interpreted as an authoritative no-change result
- **AND** the existing git-diff or remote all-hooks fallback remains in effect

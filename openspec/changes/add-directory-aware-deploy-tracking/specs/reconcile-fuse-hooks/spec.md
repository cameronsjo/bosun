## ADDED Requirements

### Requirement: Directory-Aware Deploy Change Tracking

Content-hash sync SHALL include each descendant directory it actually creates
in the canonical staging-relative deploy change set, including empty
directories. It SHALL exclude an ordinarily created deploy target root and
directories that already existed, so plumbing and no-op reconciles do not
trigger post-sync hooks. A managed file-to-directory transition SHALL include
the transitioned path, including a top-level target path, and every descendant
directory created in its private replacement tree.

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

#### Scenario: Directory-only change set does not mask a missing file

- **WHEN** the deploy change set contains newly created directories but no regular file writes
- **AND** a regular source file is missing or byte-different at the destination
- **THEN** the post-deploy invariant fails before compose-up

#### Scenario: Recorded directory must be fresh and remain a directory

- **WHEN** a recorded directory is missing, has an mtime older than the reconcile start, or resolves to a non-directory destination entry
- **THEN** the post-deploy invariant fails before compose-up

#### Scenario: Managed file becomes an empty directory tree

- **WHEN** a previously managed regular file is replaced by a source directory containing empty descendants
- **THEN** the transitioned path and each created descendant directory are included in the deploy change set
- **AND** matching hooks can observe the type transition

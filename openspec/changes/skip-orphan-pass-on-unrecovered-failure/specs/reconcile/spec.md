## ADDED Requirements

### Requirement: Orphan Pass Safety After Unrecovered Failure

The orphan-reconciliation pass (`up --remove-orphans`) SHALL NOT run when any
compose file in the same isolated compose up failed and was not rolled back
from a verified backup. A file marked rolled back without an extracted backup
tree SHALL count as not rolled back. Such a file is left out of the pass, so
its running containers would look like orphans and be removed.

When the pass is skipped for this reason, the reconciler SHALL log a warning
naming the unrecovered files, and the deploy error (partial or all-failed)
SHALL state that the orphan pass was skipped and name those files, so the
deploy-failure alert carries the reason. Orphans are cleaned by the next full
run.

When every failed file was rolled back from a verified backup, the pass SHALL
run as before, with the rolled-back files' backup copies.

#### Scenario: Failed file without rollback

- **WHEN** a full run's compose up fails for one file and that file's rollback fails or has no backup
- **THEN** no command in that deploy passes `--remove-orphans`
- **AND** the deploy error states that the orphan pass was skipped and names the file

#### Scenario: Failed file with verified rollback

- **WHEN** a full run's compose up fails for one file and its rollback from the verified backup succeeds
- **THEN** the orphan pass runs over the succeeded files and the rolled-back file's backup copy

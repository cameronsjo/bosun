## ADDED Requirements

### Requirement: Service State Selection

Wherever Bosun reads a service's container from actual state (drift, skip check, post-deploy health poll), it SHALL read one selected container per service, superseding any per-container reading in Declared vs Actual State Comparison and State-Based Skip Logic. That container is a running or restarting one when any exists, else a stopped one; among those, the oldest by creation time, then the smallest container ID. Health and restarting-or-running changes SHALL NOT alter it.

#### Scenario: Stopped container beside a running replica

- **WHEN** a declared service has an exited container and a running container, in either list order
- **THEN** the running container is the service's observed container
- **AND** the drift report has no `missing` item for the service
- **AND** the commit-already-deployed skip check counts the service as present

#### Scenario: Health poll ignores a stopped leftover

- **WHEN** a declared service has an exited container and a running container with no healthcheck, in either list order
- **AND** the post-deploy health poll runs
- **THEN** the poll counts the service as healthy
- **AND** the poll does not wait out its timeout on the exited container

#### Scenario: Every container stopped

- **WHEN** every container of a declared service is exited
- **THEN** the oldest container is observed, with ties broken by the smallest container ID
- **AND** the drift report includes a `missing` item noting that container's state

#### Scenario: Paused container counts as stopped

- **WHEN** a declared service has only a paused container
- **THEN** the paused container is observed as a stopped one
- **AND** the drift report includes a `missing` item noting `state=paused`
- **AND** the commit-already-deployed skip check does not count the service as present

#### Scenario: Oldest wins among running replicas

- **WHEN** a declared service has two running replicas created at different times
- **THEN** the older replica is the observed container, in either list order

#### Scenario: Selection ignores health

- **WHEN** a declared service has an older healthy replica and a newer unhealthy replica
- **THEN** the older replica is observed
- **AND** the drift report has no `unhealthy` item for the service

#### Scenario: Crash loop keeps one selection

- **WHEN** the oldest replica of a service alternates between restarting and running across drift checks
- **AND** a newer replica of the service is running
- **THEN** the same container is observed on every check

#### Scenario: Image drift reads the selected replica

- **WHEN** a service's replicas run different images
- **THEN** image drift compares the declared image with the selected replica's image only

### Requirement: Restart Breaker Replica Tracking

The restart circuit breaker SHALL evaluate every running or restarting container of a service it owns, each with its own restart baseline keyed by container ID, and SHALL stop each one that reaches the threshold. Service State Selection SHALL NOT affect breaker tracking.

#### Scenario: Every replica crash-loops

- **WHEN** two replicas of a service both restart past the threshold
- **THEN** both are tripped and each is stopped by its own full container ID

#### Scenario: Newer replica crash-loops beside a healthy one

- **WHEN** the older replica of a service is healthy and a newer replica restarts past the threshold
- **THEN** the newer replica is tripped and stopped
- **AND** the older replica's baseline is unaffected

#### Scenario: Stopped replica stays tripped while a sibling runs

- **WHEN** the breaker has stopped one replica of a service and a sibling replica is running
- **THEN** the stopped replica's entry stays tripped
- **AND** no resolved alert is sent for it
- **AND** the sibling is tracked with its own baseline

#### Scenario: Single-container service is unchanged

- **WHEN** a service has one container
- **THEN** the breaker keeps one baseline for it and trips it at the threshold

### Requirement: Restart Breaker Tripped Entry Lifecycle

A tripped entry SHALL stay tied to the container that tripped. A new container takes it over only when the tripped container no longer exists and the new one has the same Compose service and container number; a sibling with another number never inherits it. A gone container with no replacement SHALL clear its entry without a resolved alert. A service is resolved only when none of its containers is still tripped.

#### Scenario: Restarted replica resolves after a stable run

- **WHEN** a tripped replica is running again
- **AND** its restart count is unchanged across two consecutive checks
- **THEN** its entry resolves
- **AND** a resolved alert is sent when no other container of the service is still tripped

#### Scenario: Replacement container resolves after a grace sample

- **WHEN** a deploy replaces a tripped container with a new container of the same service and container number
- **THEN** the entry stays tripped for the first sample on the new container
- **AND** it resolves on the next sample whose restart count is unchanged

#### Scenario: Gone container with no replacement clears quietly

- **WHEN** a tripped container no longer exists and no container with its container number replaces it
- **THEN** its entry is cleared without a resolved alert
- **AND** the clearing is logged at info level
- **AND** the next deploy starts the service from a fresh baseline

#### Scenario: Sibling with another number does not inherit

- **WHEN** a tripped container no longer exists and a new container of the service has a different container number
- **THEN** the new container is tracked with its own entry
- **AND** the tripped entry is cleared, not taken over

#### Scenario: Service resolves when the last tripped container recovers

- **WHEN** two containers of a service are tripped and one recovers
- **THEN** no resolved alert is sent
- **AND** a resolved alert is sent when the other recovers

#### Scenario: Restarted replica that keeps looping is not stopped again

- **WHEN** a tripped replica is started again and its restart count keeps rising
- **THEN** its entry stays tripped and no second stop is attempted

#### Scenario: Skip check counts a tripped container as present

- **WHEN** every container of a service is stopped and one of them has a tripped entry
- **THEN** the commit-already-deployed skip check counts the service as present

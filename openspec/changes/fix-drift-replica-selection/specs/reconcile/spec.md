## ADDED Requirements

### Requirement: Service State Selection

Wherever Bosun reads a service's container from actual state (drift, skip check, post-deploy health poll, restart breaker), it SHALL read one selected container per service, superseding any per-container reading in Declared vs Actual State Comparison and State-Based Skip Logic. That container is a running or restarting one when any exists, else a stopped one; among those, the oldest by creation time, then the smallest container ID. Health and restarting-or-running changes SHALL NOT alter it.

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
- **AND** the restart circuit breaker keeps one baseline and trips once the threshold is reached

#### Scenario: Image drift reads the selected replica

- **WHEN** a service's replicas run different images
- **THEN** image drift compares the declared image with the selected replica's image only

#### Scenario: Breaker-stopped replica hands over to a live sibling

- **WHEN** the restart circuit breaker has stopped the selected replica of a service
- **AND** a sibling replica is running
- **THEN** the sibling becomes the observed container
- **AND** the service's tripped entry stays tripped for the first sample on the sibling
- **AND** it resolves on the next sample whose restart count is unchanged, with a resolved alert, while the stopped replica stays stopped

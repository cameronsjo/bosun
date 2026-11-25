## ADDED Requirements

### Requirement: Service State Selection

Where Declared vs Actual State Comparison and State-Based Skip Logic read a service's container, they SHALL read one container per service. A running or restarting container is live. A live container SHALL be selected when one exists, else a stopped one. Among those, the oldest by creation time wins, then the smallest container ID. Health and state changes between restarting and running SHALL NOT change the selection.

#### Scenario: Stopped container beside a running replica

- **WHEN** a declared service has an exited container and a running container, in either list order
- **THEN** the running container is the service's observed container
- **AND** the drift report has no `missing` item for the service
- **AND** the commit-already-deployed skip check counts the service as present

#### Scenario: Every container stopped

- **WHEN** every container of a declared service is exited
- **THEN** the oldest container is observed, with ties broken by the smallest container ID
- **AND** the drift report includes a `missing` item noting that container's state

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
- **AND** it resolves on the next unchanged sample, with a resolved alert, while the stopped replica stays stopped

## ADDED Requirements

### Requirement: Declared State Extraction

After rendering manifests (provisions or charts), the reconciler SHALL extract a
declared state snapshot listing every expected service with its name and image.

The declared state SHALL be derived from the rendered compose output's `services`
map, extracting `image` per service name. This avoids re-parsing manifests and
uses the same artifact that `docker compose up` consumes.

The declared state snapshot SHALL be persisted in the deploy state file alongside
the deployed commit, so drift checks can run without re-rendering.

#### Scenario: Declared state extracted from rendered compose

- **WHEN** the reconciler renders templates and produces compose output
- **THEN** it extracts a list of declared services with name and image
- **AND** stores the list in the deploy state file after successful deployment

#### Scenario: Raw passthrough services included

- **WHEN** a service manifest uses `type: raw` with explicit compose definitions
- **THEN** its services are still included in the declared state snapshot

### Requirement: Actual State Collection

The reconciler SHALL collect actual state from Docker by querying running
containers and extracting their name, image, state, and health status.

Actual state collection SHALL use the existing `docker.Client.ListContainers()`
API to avoid introducing new Docker dependencies.

#### Scenario: Actual state collected from Docker

- **WHEN** a drift check runs
- **THEN** the system queries Docker for all containers (running and stopped)
- **AND** maps each container to its name, image, running state, and health

#### Scenario: Docker unreachable during drift check

- **WHEN** the Docker daemon is unreachable during a drift check
- **THEN** the drift check fails gracefully with a logged warning
- **AND** the previous drift status is retained (not cleared)

### Requirement: Drift Detection

The reconciler SHALL compare declared state against actual state and produce a
drift report containing: missing services (declared but not running), extra
services (running but not declared), image mismatches (declared image differs
from running image), and unhealthy services (running but unhealthy).

Drift detection SHALL only compare services that belong to the bosun-managed
compose project (filtered by project name label) to avoid false positives from
unrelated containers.

#### Scenario: Missing service detected

- **WHEN** a service is declared in the manifest but no matching container exists
- **THEN** the drift report includes it as a "missing" drift item

#### Scenario: Image mismatch detected

- **WHEN** a service is declared with image `foo:latest` but the running container uses `foo:v1.2`
- **THEN** the drift report includes it as an "image_mismatch" drift item
- **AND** the report includes both the declared and actual image

#### Scenario: Unhealthy service detected

- **WHEN** a declared service is running but Docker reports it as "unhealthy"
- **THEN** the drift report includes it as an "unhealthy" drift item

#### Scenario: No drift detected

- **WHEN** all declared services are running, healthy, and match their declared images
- **THEN** the drift report is empty
- **AND** the system logs a confirmation at INFO level

### Requirement: Post-Deploy Verification

After `docker compose up` succeeds, the reconciler SHALL perform a drift check
to verify that all declared services are running.

If drift is detected immediately after deployment, the reconciler SHALL log a
warning but SHALL NOT fail the reconciliation (compose up succeeded, the
containers may still be starting).

The post-deploy verification SHALL respect a configurable startup grace period
(default: 30 seconds) before reporting unhealthy containers as drift, to allow
time for health checks to pass.

#### Scenario: Post-deploy verification passes

- **WHEN** `docker compose up` completes successfully
- **AND** all declared services are running within the grace period
- **THEN** the reconciler logs success and records zero drift in the state file

#### Scenario: Post-deploy verification finds missing service

- **WHEN** `docker compose up` completes but a declared service fails to start
- **THEN** the reconciler logs a warning with the missing service name
- **AND** records the drift in the state file
- **AND** the reconciliation still reports success (compose up itself succeeded)

### Requirement: Drift Status in Deploy State

The deploy state file SHALL be extended with a `drift` field recording the
result of the most recent drift check: timestamp, number of drifted services,
and a summary list of drift items (service name, drift type, detail).

The drift field SHALL be updated after every drift check (post-deploy and
periodic), not only after deployments.

#### Scenario: Drift recorded in state file

- **WHEN** a drift check completes
- **THEN** the state file is updated with drift timestamp and items
- **AND** the state file write uses the existing atomic write pattern

#### Scenario: Clean state after no drift

- **WHEN** a drift check finds no issues
- **THEN** the state file records an empty drift items list with the check timestamp

### Requirement: Drift CLI Command

A `bosun drift` command SHALL display the current drift status by reading the
deploy state file and optionally performing a live drift check.

The command SHALL support `--live` flag to perform a fresh drift check against
Docker instead of reading cached state.

The command SHALL support `--json` flag for machine-readable output.

#### Scenario: Cached drift status

- **WHEN** `bosun drift` is run without flags
- **THEN** it reads the deploy state file and displays the last drift check result
- **AND** shows the timestamp of the last check

#### Scenario: Live drift check

- **WHEN** `bosun drift --live` is run
- **THEN** it reads the declared state from the deploy state file
- **AND** queries Docker for actual state
- **AND** displays the comparison result

#### Scenario: No previous state

- **WHEN** `bosun drift` is run but no deploy state file exists
- **THEN** it prints a message indicating no deployments have been recorded

### Requirement: Drift API Endpoint

The daemon SHALL expose a `/api/drift` GET endpoint returning the current drift
status from the deploy state file.

The endpoint SHALL include: last check timestamp, drift item count, drift items
(service, type, detail), and overall drift status ("clean" or "drifted").

#### Scenario: Drift endpoint with no drift

- **WHEN** GET `/api/drift` is called and no drift exists
- **THEN** the response includes `{"status": "clean", "items": [], "checked_at": "..."}`

#### Scenario: Drift endpoint with drift detected

- **WHEN** GET `/api/drift` is called and drift items exist
- **THEN** the response includes `{"status": "drifted", "items": [...], "checked_at": "..."}`

### Requirement: Periodic Drift Checks

The daemon SHALL perform periodic drift checks on a configurable interval
(default: 300 seconds / 5 minutes), independent of the reconciliation poll
interval.

Periodic drift checks SHALL NOT trigger reconciliation. They only update the
drift status in the state file and logs.

The drift check interval SHALL be configurable via `BOSUN_DRIFT_INTERVAL`
environment variable. Setting it to 0 disables periodic drift checks.

#### Scenario: Periodic drift check detects crash

- **WHEN** a container crashes between reconciliations
- **AND** the next periodic drift check runs
- **THEN** the drift is detected and logged at WARN level
- **AND** the deploy state file is updated with the drift

#### Scenario: Periodic drift checks disabled

- **WHEN** `BOSUN_DRIFT_INTERVAL` is set to 0
- **THEN** no periodic drift checks run
- **AND** drift is only checked after deployments

### Requirement: Drift Alerts

When drift is detected during a periodic check (not post-deploy), the system
SHALL send an alert through the configured alert providers if drift items include
missing or unhealthy services.

Drift alerts SHALL be rate-limited to one alert per drift check interval to
prevent notification storms from flapping services.

#### Scenario: Alert on missing service

- **WHEN** a periodic drift check finds a missing service
- **THEN** an alert is sent via configured providers
- **AND** the alert includes the service name and drift type

#### Scenario: No alert on image mismatch only

- **WHEN** a periodic drift check finds only image mismatches (no missing/unhealthy)
- **THEN** no alert is sent (image mismatches are informational)
- **AND** the drift is still logged and recorded in the state file

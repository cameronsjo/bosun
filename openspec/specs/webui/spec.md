# WebUI Specification

## Purpose

The WebUI provides a browser-based dashboard for monitoring daemon status, viewing containers, triggering reconciliation, and viewing container logs. It uses bearer token authentication and supports dark mode.

## Requirements

### Requirement: Dashboard Status Display

The system SHALL provide a web dashboard displaying daemon status.

#### Scenario: User views dashboard

- **WHEN** user navigates to the dashboard
- **THEN** the UI displays daemon health (healthy/degraded)
- **AND** displays uptime
- **AND** displays last reconcile time and status
- **AND** displays time until next scheduled poll

#### Scenario: Dashboard polls for updates

- **WHEN** dashboard is open
- **THEN** status refreshes every 5 seconds
- **AND** updates without full page reload

#### Scenario: Daemon is unreachable

- **WHEN** API calls fail
- **THEN** the UI displays "Daemon offline" banner
- **AND** shows last known data with staleness timestamp
- **AND** disables action buttons

### Requirement: Manual Reconciliation Trigger

The system SHALL allow triggering reconciliation from the dashboard.

#### Scenario: User triggers reconciliation

- **WHEN** user clicks "Trigger Reconcile" button
- **AND** confirms the action
- **THEN** the system calls POST /api/trigger
- **AND** displays success notification
- **AND** status updates to show reconciling state

#### Scenario: Trigger disabled during reconciliation

- **WHEN** daemon is already reconciling
- **THEN** trigger button is disabled
- **AND** shows "Reconciling..." status

### Requirement: Container List

The system SHALL display all Docker containers.

#### Scenario: User views container list

- **WHEN** user navigates to containers page
- **THEN** the UI displays a table of all containers
- **AND** each row shows: name, image, state, health status

#### Scenario: Container health indicators

- **WHEN** container is running and healthy
- **THEN** row displays green health badge
- **WHEN** container is running but unhealthy
- **THEN** row displays yellow health badge
- **WHEN** container is stopped
- **THEN** row displays red status badge

### Requirement: Container Restart

The system SHALL allow restarting containers.

#### Scenario: User restarts container

- **WHEN** user clicks restart button on a container
- **AND** confirms the action
- **THEN** the system calls POST /api/containers/:id/restart
- **AND** displays success notification

### Requirement: Container Log Viewer

The system SHALL display container logs.

#### Scenario: User views logs

- **WHEN** user selects a container and views logs
- **THEN** the UI displays the last 100 lines of logs
- **AND** user can select different line counts (100, 500, 1000)

#### Scenario: User refreshes logs

- **WHEN** user clicks refresh button
- **THEN** logs are fetched again from the API

### Requirement: Dark Mode

The system SHALL support dark mode.

#### Scenario: System preference respected

- **WHEN** user's system prefers dark mode
- **THEN** the UI defaults to dark theme

#### Scenario: Manual toggle

- **WHEN** user toggles dark mode switch
- **THEN** theme changes immediately
- **AND** preference is persisted in localStorage

### Requirement: Bearer Token Authentication

The system SHALL authenticate API requests with bearer token.

#### Scenario: Valid token

- **WHEN** request includes valid Authorization header
- **THEN** request is processed

#### Scenario: Invalid or missing token

- **WHEN** request has invalid or missing token
- **THEN** API returns 401 Unauthorized
- **AND** UI displays authentication error

### Requirement: WebUI Health Endpoint

The WebUI container SHALL expose a health endpoint.

#### Scenario: Health check

- **WHEN** orchestrator calls GET /health on WebUI container
- **THEN** nginx returns 200 OK

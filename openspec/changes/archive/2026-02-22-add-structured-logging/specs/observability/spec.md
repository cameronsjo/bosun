# Observability Specification

## ADDED Requirements

### Requirement: Structured Logging

The system SHALL provide structured logging with JSON output support for machine parsing and console output for human readability.

#### Scenario: JSON output in daemon mode

- **WHEN** bosun runs as a daemon
- **THEN** logs SHALL be output in JSON format to stdout
- **AND** each log entry SHALL include timestamp, level, message, and component fields

#### Scenario: Console output in CLI mode

- **WHEN** bosun runs as a CLI command with a TTY attached
- **THEN** logs SHALL be output in human-readable format with colors
- **AND** success messages SHALL display a green checkmark
- **AND** error messages SHALL display a red X

#### Scenario: Explicit format override

- **WHEN** BOSUN_LOG_FORMAT is set to "json" or "console"
- **THEN** the system SHALL use the specified format regardless of TTY detection

### Requirement: Log Levels

The system SHALL support configurable log levels for filtering log output.

#### Scenario: Log level filtering

- **WHEN** BOSUN_LOG_LEVEL is set to "warn"
- **THEN** only WARN and ERROR level messages SHALL be output
- **AND** DEBUG and INFO messages SHALL be suppressed

#### Scenario: Default log level

- **WHEN** no log level is configured
- **THEN** the default log level SHALL be INFO

#### Scenario: CLI flag override

- **WHEN** --log-level flag is provided
- **THEN** it SHALL override the BOSUN_LOG_LEVEL environment variable

### Requirement: Correlation IDs

The system SHALL propagate correlation IDs through operations for request tracing.

#### Scenario: HTTP request correlation

- **WHEN** the daemon receives an HTTP request
- **THEN** a unique request_id SHALL be generated
- **AND** all log entries for that request SHALL include the request_id field

#### Scenario: Reconcile run correlation

- **WHEN** a reconciliation run starts
- **THEN** a unique reconcile_id SHALL be generated
- **AND** all log entries for that run SHALL include the reconcile_id field
- **AND** the reconcile_id SHALL be included in success/failure alerts

### Requirement: Structured Fields

The system SHALL include consistent structured fields in log entries.

#### Scenario: Component identification

- **WHEN** a log entry is created
- **THEN** it SHALL include a component field identifying the subsystem (daemon, reconcile, git, deploy)

#### Scenario: Duration tracking

- **WHEN** an operation completes
- **THEN** the log entry MAY include a duration_ms field with the elapsed time in milliseconds

#### Scenario: Error context

- **WHEN** logging an error
- **THEN** the log entry SHALL include an error field with the error message
- **AND** the log entry SHALL include relevant context fields (operation, target, etc.)

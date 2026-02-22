# Observability Specification

## Purpose

The observability system provides structured logging, log level filtering, correlation ID propagation, structured field conventions, and console output for CLI interactions.

## Requirements

### Requirement: Structured Logging

The system SHALL provide structured logging with JSON output support for machine parsing and console output for human readability.

#### Scenario: JSON output in daemon mode

- **WHEN** bosun runs as a daemon (BOSUN_DAEMON_MODE=true)
- **THEN** logs SHALL be output in JSON format to stdout
- **AND** each log entry SHALL include timestamp, level, message, and component fields

#### Scenario: Console output in CLI mode

- **WHEN** bosun runs as a CLI command with a TTY attached
- **THEN** logs SHALL be output in human-readable format with colors
- **AND** log levels SHALL be color-coded: INFO (blue), WARN (yellow), ERROR (red), DEBUG (gray)
- **AND** timestamps SHALL use Kitchen format (e.g. 3:04PM)

#### Scenario: Explicit format override

- **WHEN** BOSUN_LOG_FORMAT is set to "json" or "console"
- **THEN** the system SHALL use the specified format regardless of TTY detection

#### Scenario: Non-terminal default

- **WHEN** stdout is not a terminal and BOSUN_DAEMON_MODE is not set
- **THEN** the system SHALL default to JSON format

#### Scenario: Additional log writers

- **WHEN** additional writers are configured via Options.AdditionalWriters
- **THEN** log output SHALL be fanned out to all writers using a multi-level writer
- **AND** the primary stdout writer SHALL continue to function alongside additional writers

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

#### Scenario: Case-insensitive level parsing

- **WHEN** a log level string is provided in any case (e.g. "DEBUG", "debug", "Debug")
- **THEN** the system SHALL parse it correctly
- **AND** "warning" SHALL be accepted as an alias for "warn"
- **AND** unrecognized values SHALL default to INFO

### Requirement: Correlation IDs

The system SHALL propagate correlation IDs through operations for request tracing.

#### Scenario: HTTP request correlation

- **WHEN** the daemon receives an HTTP request
- **THEN** a unique request_id SHALL be generated as a UUID
- **AND** all log entries for that request SHALL include the request_id field

#### Scenario: Reconcile run correlation

- **WHEN** a reconciliation run starts
- **THEN** a unique reconcile_id SHALL be generated as a UUID
- **AND** all log entries for that run SHALL include the reconcile_id field
- **AND** the reconcile_id SHALL be included in success/failure alerts

#### Scenario: Context-derived logger

- **WHEN** a logger is created from a context containing correlation IDs
- **THEN** the logger SHALL automatically include all present correlation ID fields (request_id, reconcile_id)

#### Scenario: Explicit correlation ID

- **WHEN** a correlation ID is provided explicitly (non-empty string)
- **THEN** the system SHALL use the provided ID instead of generating a new UUID

### Requirement: Structured Fields

The system SHALL include consistent structured fields in log entries.

#### Scenario: Component identification

- **WHEN** a log entry is created
- **THEN** it SHALL include a component field identifying the subsystem
- **AND** valid component values SHALL include: daemon, reconcile, git, deploy, sops, template, docker, manifest, webhook, http

#### Scenario: Source identification

- **WHEN** a log entry is created for a triggered operation
- **THEN** it MAY include a source field identifying the trigger
- **AND** valid source values SHALL include: webhook, poll, manual, startup, github, gitlab, gitea

#### Scenario: Duration tracking

- **WHEN** an operation completes
- **THEN** the log entry MAY include a duration_ms field with the elapsed time in milliseconds
- **AND** the duration SHALL be calculated from the operation start time

#### Scenario: Error context

- **WHEN** logging an error
- **THEN** the log entry SHALL include an error field with the error message
- **AND** the log entry SHALL include relevant context fields (operation, target, path, commit, branch, container, method, url, status)
- **AND** nil errors SHALL be silently ignored (no error field added)

### Requirement: Console Output

The ui package SHALL provide colored output helpers for CLI interactions that adapt to the current log format.

#### Scenario: Console mode output

- **WHEN** the log format is console
- **THEN** Success messages SHALL display a green checkmark prefix
- **AND** Error messages SHALL display a red X prefix
- **AND** Warning messages SHALL display a yellow warning symbol prefix
- **AND** Info messages SHALL display in blue
- **AND** Debug messages SHALL display in gray
- **AND** Step messages SHALL display a cyan numbered prefix (e.g. "[1]")
- **AND** Header messages SHALL display in bold

#### Scenario: JSON mode fallback

- **WHEN** the log format is JSON
- **THEN** ui output functions SHALL delegate to the structured logging system
- **AND** Success messages SHALL log at INFO level with a success=true field
- **AND** Error messages SHALL log at ERROR level
- **AND** Warning messages SHALL log at WARN level
- **AND** Info messages SHALL log at INFO level
- **AND** Step messages SHALL log at INFO level with a step field
- **AND** Header messages SHALL log at INFO level with type=header

#### Scenario: Fatal output

- **WHEN** Fatal is called
- **THEN** the message SHALL be written to stderr in console mode
- **AND** the process SHALL exit with code 1

### Requirement: Nautical Themed Output

The ui package SHALL provide nautical-themed output functions consistent with Bosun's nautical metaphor.

#### Scenario: Themed message functions

- **WHEN** a themed output function is called in console mode
- **THEN** Anchor messages SHALL display with an anchor icon in blue
- **AND** Ship messages SHALL display with a ship icon in green
- **AND** Compass messages SHALL display with a compass icon in cyan
- **AND** Mayday messages SHALL display with a mayday icon in red
- **AND** Snapshot messages SHALL display with a camera icon in blue
- **AND** Package messages SHALL display with a package icon in green

#### Scenario: Themed JSON fallback

- **WHEN** a themed output function is called in JSON mode
- **THEN** the message SHALL be logged with an icon field identifying the theme (anchor, ship, compass, mayday, snapshot, package)
- **AND** Mayday messages SHALL log at ERROR level
- **AND** all other themed messages SHALL log at INFO level

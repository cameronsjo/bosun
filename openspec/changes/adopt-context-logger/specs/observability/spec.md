<!-- markdownlint-disable MD041 -->
## MODIFIED Requirements

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

#### Scenario: Context-enriched logger stashed at pipeline entry

- **WHEN** a pipeline entry point creates or receives correlation IDs (daemon.TriggerReconcile, HTTP middleware, reconcile.Run)
- **THEN** it SHALL build a zerolog.Logger with all available correlation fields
- **AND** stash the enriched logger on context via log.WithContext
- **AND** all downstream sub-operations SHALL inherit the enriched logger via log.Ctx or log.ComponentCtx

#### Scenario: Sub-operation logger inherits correlation

- **WHEN** a reconcile sub-operation (git, deploy, sops, template) creates a logger via log.ComponentCtx(ctx, component)
- **THEN** the returned logger SHALL include the component field AND all correlation IDs from context
- **AND** the sub-operation SHALL NOT need to explicitly extract or attach correlation IDs

#### Scenario: Reconcile sub-operations carry reconcile_id

- **WHEN** reconcile.Run() delegates to git.Clone, git.Pull, deploy.Deploy, sops.DecryptSecrets, or template.RenderTemplates
- **THEN** every log entry from those sub-operations SHALL include the reconcile_id field
- **AND** every log entry SHALL include a component field identifying the sub-operation (git, deploy, sops, template)

#### Scenario: Daemon API handler carries request_id

- **WHEN** a daemon API handler processes a request routed through loggingMiddleware
- **THEN** every log entry from the handler SHALL include the request_id field
- **AND** every log entry SHALL include the component field

### Requirement: Structured Fields

The system SHALL include consistent structured fields in log entries.

#### Scenario: Component identification

- **WHEN** a log entry is created
- **THEN** it SHALL include a component field identifying the subsystem
- **AND** valid component values SHALL include: daemon, reconcile, git, deploy, sops, template, docker, manifest, webhook, http, tunnel

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

## ADDED Requirements

### Requirement: Context-Aware Logger Construction

The log package SHALL provide a function to create a logger that combines component identity with context-derived correlation IDs.

#### Scenario: ComponentCtx with full context

- **WHEN** log.ComponentCtx(ctx, "git") is called
- **AND** context contains reconcile_id="abc-123" and request_id="def-456"
- **THEN** the returned logger SHALL include component="git", reconcile_id="abc-123", and request_id="def-456"

#### Scenario: ComponentCtx with empty context

- **WHEN** log.ComponentCtx(ctx, "git") is called
- **AND** context contains no correlation IDs
- **THEN** the returned logger SHALL include component="git" with no correlation fields
- **AND** behavior SHALL be equivalent to log.Component("git")

#### Scenario: ComponentCtx logger is chainable

- **WHEN** a caller needs additional fields beyond component and correlation IDs
- **THEN** the returned zerolog.Logger supports standard .With().Str(...).Logger() chaining

### Requirement: Pipeline Story Logging

Operations that form a logical unit of work SHALL log their lifecycle: start, success with duration, and failure with error context.

#### Scenario: Reconcile Run lifecycle

- **WHEN** reconcile.Run() begins
- **THEN** it SHALL log an info-level message indicating the reconcile run is starting
- **AND** on success, it SHALL log an info-level message with duration_ms
- **AND** on failure, it SHALL log an error-level message with the error and duration_ms

#### Scenario: Daemon API handler lifecycle

- **WHEN** a daemon API handler begins processing a request
- **THEN** it SHALL log a debug-level message with the handler name and relevant parameters
- **AND** on success, it SHALL log at info level with the response outcome
- **AND** on failure, it SHALL log at error level with the error

#### Scenario: Tunnel provider subprocess lifecycle

- **WHEN** a tunnel provider executes a subprocess (cloudflared, tailscale)
- **THEN** it SHALL log an info-level message with operation name and command
- **AND** on success, it SHALL log an info-level message with duration_ms
- **AND** on failure, it SHALL log an error-level message with exit code and stderr content
- **AND** on success, subprocess stderr SHALL be logged at debug level (if non-empty)

#### Scenario: Retry logging

- **WHEN** retryWithBackoff retries an operation
- **THEN** each retry attempt SHALL be logged at warn level with the attempt number, max attempts, backoff duration, and error from the previous attempt

# Alerting Specification

## Purpose

The alerting system is a native, multi-provider notification framework. It dispatches alerts for deployment lifecycle events (success, failure, recovery, rollback) and operational events (drift detection, health check results, unhealthy containers). Providers are pluggable via a common interface, and the system supports sending to all configured providers simultaneously with graceful partial-failure handling.

## Requirements

### Requirement: Alert Dispatching

The alert manager SHALL dispatch alerts to all configured providers sequentially. Each alert SHALL carry a title, message body, severity level, source identifier, and optional metadata key-value pairs.

The severity levels SHALL be `info`, `warning`, `error`, and `critical`.

When no providers are configured, the manager SHALL return nil without error.

#### Scenario: Alert sent to all configured providers

- **WHEN** an alert is dispatched with two configured providers
- **THEN** both providers receive the alert
- **AND** the manager returns nil on success

#### Scenario: No providers configured

- **WHEN** an alert is dispatched with no configured providers
- **THEN** the manager returns nil without error
- **AND** no network calls are made

#### Scenario: Unconfigured providers are excluded

- **WHEN** a provider is added to the manager but its `IsConfigured()` returns false
- **THEN** the provider is not registered
- **AND** it does not receive alerts

### Requirement: Multi-Provider Error Handling

The alert manager SHALL continue sending to remaining providers when one provider fails. The manager SHALL return an aggregated error containing all provider failures, prefixed with the provider name.

The manager SHALL log per-provider timing and outcome at debug level, and log a summary warning when partial failures occur.

#### Scenario: One provider fails, others succeed

- **WHEN** an alert is dispatched to three providers and the first provider fails
- **THEN** the remaining two providers still receive the alert
- **AND** the returned error contains the failing provider's name

#### Scenario: All providers fail

- **WHEN** an alert is dispatched to two providers and both fail
- **THEN** the returned error contains both provider names
- **AND** the error is a joined aggregation of individual errors

### Requirement: Discord Provider

The Discord provider SHALL send alerts as rich embed messages via Discord webhook. The provider SHALL be configured with a webhook URL, supplied directly or via the `DISCORD_WEBHOOK_URL` environment variable.

The embed SHALL include: title, description (message body), color mapped to severity, footer with source identifier (`bosun/{source}`), UTC timestamp in RFC 3339 format, and metadata as inline fields.

Severity-to-color mapping SHALL be: info = green (`0x2ecc71`), warning = orange (`0xf39c12`), error = red (`0xe74c3c`), critical = purple (`0x9b59b6`).

Empty metadata values SHALL be excluded from embed fields. Field values longer than 1024 characters SHALL be truncated with an ellipsis.

The provider SHALL accept HTTP 200 or 204 as success. The HTTP client SHALL use a 10-second timeout.

#### Scenario: Successful Discord alert

- **WHEN** a warning-severity alert is sent with metadata
- **THEN** a POST request is sent to the webhook URL with `Content-Type: application/json`
- **AND** the embed color is orange (`0xf39c12`)
- **AND** the footer reads `bosun/{source}`
- **AND** non-empty metadata values appear as inline fields

#### Scenario: Discord returns error status

- **WHEN** the Discord webhook returns HTTP 400
- **THEN** the provider returns an error containing "unexpected status: 400"

#### Scenario: Unconfigured Discord provider silently succeeds

- **WHEN** `Send` is called on a Discord provider with no webhook URL
- **THEN** the provider returns nil without making any HTTP request

### Requirement: SendGrid Provider

The SendGrid provider SHALL send alerts as emails via the SendGrid v3 API (`https://api.sendgrid.com/v3/mail/send`). Configuration requires an API key, sender email address, and at least one recipient email. An optional sender name MAY be provided.

Each email SHALL include both `text/plain` and `text/html` content. The subject line SHALL be prefixed with the severity in brackets (e.g., `[ERROR] Deployment Failed`). Unknown severity levels SHALL have no prefix.

The HTML body SHALL use severity-specific colors, escape all user-provided content with `html.EscapeString`, and render metadata as a details table. The plain text body SHALL include title, severity, message, metadata, and source.

The provider SHALL authenticate with a Bearer token. The HTTP client SHALL use a 30-second timeout. HTTP 202 Accepted is the expected success status. Error responses SHALL be parsed for structured error messages when available.

#### Scenario: Successful email alert

- **WHEN** an error-severity alert is sent with two recipients
- **THEN** a POST is sent to the SendGrid API with Bearer authorization
- **AND** the subject line is `[ERROR] {title}`
- **AND** both text/plain and text/html content are included
- **AND** both recipients appear in a single personalization

#### Scenario: HTML content is XSS-safe

- **WHEN** an alert message contains `<script>` tags
- **THEN** the HTML body contains the escaped form `&lt;script&gt;`

#### Scenario: Unconfigured SendGrid returns error

- **WHEN** `Send` is called on a SendGrid provider missing required configuration
- **THEN** the provider returns an error indicating it is not configured

### Requirement: Twilio Provider

The Twilio provider SHALL send SMS alerts via the Twilio REST API using HTTP Basic authentication (account SID and auth token). Configuration requires an account SID, auth token, sender phone number, and at least one recipient phone number.

The provider SHALL only send SMS for `error` and `critical` severity alerts. Info and warning severity alerts SHALL be silently skipped (return nil) to minimize SMS costs.

Messages SHALL be formatted as `[SEVERITY] Title: Message` and truncated to 1600 characters with an ellipsis if exceeded. Phone numbers SHALL be normalized to E.164 format (prepend `+` if missing). Phone numbers SHALL be masked in log output (show only last 4 digits).

The provider SHALL send to each recipient sequentially. If some recipients fail and others succeed, the provider SHALL log a partial delivery warning and return the last error encountered.

#### Scenario: Error-severity alert sends SMS

- **WHEN** an error-severity alert is sent to two recipients
- **THEN** SMS messages are sent to both recipients via the Twilio API
- **AND** the message format is `[ERROR] {title}: {message}`

#### Scenario: Info-severity alert is silently skipped

- **WHEN** an info-severity alert is sent
- **THEN** no SMS is sent
- **AND** the provider returns nil

#### Scenario: Warning-severity alert is silently skipped

- **WHEN** a warning-severity alert is sent
- **THEN** no SMS is sent
- **AND** the provider returns nil

#### Scenario: Phone number normalization

- **WHEN** a phone number `15551234567` is used (no `+` prefix)
- **THEN** the number is normalized to `+15551234567` before sending

### Requirement: Alert Configuration

Alert providers SHALL be configurable via the Bosun configuration file (`bosun.yml` or `.bosun/config.yml`) under the `alerts` key, with environment variable fallbacks for secrets.

The configuration SHALL support:
- `discord_webhook_url` (env: `DISCORD_WEBHOOK_URL`)
- `sendgrid_api_key` (env: `SENDGRID_API_KEY`), `sendgrid_from_email` (env: `SENDGRID_FROM_EMAIL`), `sendgrid_from_name` (env: `SENDGRID_FROM_NAME`), `sendgrid_to_emails`
- `twilio_account_sid` (env: `TWILIO_ACCOUNT_SID`), `twilio_auth_token` (env: `TWILIO_AUTH_TOKEN`), `twilio_from_number` (env: `TWILIO_FROM_NUMBER`), `twilio_to_numbers`
- `on_success` (bool) and `on_failure` (bool, default: true)

When neither `on_success` nor `on_failure` is explicitly set, `on_failure` SHALL default to true.

#### Scenario: Default failure alerting

- **GIVEN** a configuration file with no `on_success` or `on_failure` settings
- **WHEN** the configuration is loaded
- **THEN** `on_failure` is true
- **AND** `on_success` is false

#### Scenario: Environment variable fallback for secrets

- **GIVEN** no `discord_webhook_url` in the config file
- **WHEN** the `DISCORD_WEBHOOK_URL` environment variable is set
- **THEN** the Discord provider uses the environment variable value

### Requirement: Reconciliation Lifecycle Alerts

The alert system SHALL provide convenience methods for reconciliation lifecycle events with pre-formatted messages:

- **Deploy Success**: title "Deployment Successful", severity info, source "reconcile", includes short commit (first 8 chars) and target
- **Deploy Failure**: title "Deployment Failed", severity error, source "reconcile", includes short commit, target, and error reason
- **Deploy Recovery**: title "Deployment Recovered", severity info, source "reconcile", includes short commit, target, and count of prior failures
- **Rollback Success**: title "Rollback Successful", severity warning, source "reconcile", includes target and backup name
- **Rollback Failure**: title "CRITICAL: Rollback Failed", severity critical, source "reconcile", includes target, error reason, and "Manual intervention required!" message
- **Unhealthy Containers**: title "Unhealthy Containers Detected", severity warning, source "reconcile", includes target and comma-separated container names
- **Drift Detected**: title "Drift Detected", severity warning, source "drift", includes target and comma-separated drift items
- **Doctor Alert**: severity-dependent title (critical: "CRITICAL: Health Check Failed", error: "Health Check Errors", warning: "Health Check Warnings", info: "Health Check Complete"), source "doctor", message is newline-joined issues

#### Scenario: Deploy success alert formatting

- **WHEN** a deploy success alert is sent for commit `abc123def456` to target `unraid`
- **THEN** the alert title is "Deployment Successful"
- **AND** the message contains the short commit `abc123de`
- **AND** metadata includes the full commit hash and target

#### Scenario: Rollback failure triggers critical severity

- **WHEN** a rollback failure alert is sent
- **THEN** the alert severity is critical
- **AND** the message includes "Manual intervention required!"

#### Scenario: Short commits are not truncated

- **WHEN** a deploy success alert is sent for a 3-character commit `abc`
- **THEN** the message contains `abc` without truncation

### Requirement: Alert Throttling for Repeated Failures

The alert system SHALL throttle repeated failure notifications to prevent notification spam. Alerts SHALL be sent on attempt 1, 3, 10, 30, then every 30th attempt thereafter. Circuit breaker activation (attempt count equals max attempts) SHALL always trigger an alert regardless of throttle state.

The system SHALL track `LastAlertedAttempt` in the deploy state file to persist throttle state across daemon restarts. On successful deployment, both `AttemptCount` and `LastAlertedAttempt` SHALL be reset to zero.

When an alert is sent, `LastAlertedAttempt` SHALL be updated to the current attempt count and persisted to the state file.

#### Scenario: First failure always alerts

- **GIVEN** no previous failures (attempt count 0, last alerted attempt 0)
- **WHEN** the first reconciliation attempt fails (attempt count becomes 1)
- **THEN** a failure alert is sent

#### Scenario: Intermediate failures are throttled

- **GIVEN** 5 consecutive failures and last alerted attempt was 3
- **WHEN** the 6th attempt fails
- **THEN** no alert is sent (next alert at attempt 10)

#### Scenario: Circuit breaker activation always alerts

- **GIVEN** the circuit breaker activates at the max attempt threshold
- **WHEN** the max attempt is reached
- **THEN** a failure alert is sent regardless of throttle state

#### Scenario: Successful deploy resets throttle state

- **WHEN** a deployment succeeds after failures
- **THEN** `AttemptCount` is reset to 0
- **AND** `LastAlertedAttempt` is reset to 0

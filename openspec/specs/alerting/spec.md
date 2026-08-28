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

Empty metadata keys and values SHALL be excluded from embed fields. The provider SHALL sort metadata keys before constructing fields and include at most 25 fields. It SHALL truncate titles to 256 units, descriptions to 4096 units, footer text to 2048 units, field names to 256 units, and field values to 1024 units. The combined title, description, footer, field-name, and field-value text SHALL NOT exceed 6000 units. Discord units SHALL be counted as UTF-16 code units so supplementary Unicode code points count as two units. Truncation SHALL preserve valid UTF-8, SHALL NOT split a Unicode code point, SHALL retain the leading context, and SHALL include the three-unit ASCII ellipsis `...` within the bound when content is omitted.

After applying individual limits, the provider SHALL consume the aggregate budget in this order: title, description, footer, and metadata fields in sorted-key order. It SHALL evaluate each metadata name/value pair against the remaining budget without partially committing the pair, evaluating the bounded name before the bounded value. It SHALL omit a pair when both non-empty components cannot fit and continue considering later sorted fields. Content at an individual or aggregate boundary SHALL remain unchanged.

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

#### Scenario: Oversized Discord content is bounded

- **WHEN** an alert contains a title, message, source, or metadata that exceeds an individual Discord embed limit
- **THEN** each component is truncated to its provider limit with an ellipsis
- **AND** the POSTed embed text totals no more than 6000 units

#### Scenario: Discord aggregate budget is deterministic

- **WHEN** metadata would push an otherwise valid embed beyond 6000 units or 25 fields
- **THEN** fields are considered in sorted-key order and bounded or omitted to keep the embed valid
- **AND** repeated sends of the same alert produce the same bounded embed content

#### Scenario: Discord aggregate budget preserves component priority

- **WHEN** individually valid title, description, footer, and metadata text together exceed 6000 units
- **THEN** aggregate budget is assigned to title, description, footer, and sorted metadata fields in that order
- **AND** an omitted metadata pair does not consume budget or prevent a later smaller pair from being considered

#### Scenario: Discord truncation preserves Unicode

- **WHEN** a provider boundary falls within multibyte Unicode content
- **THEN** the POSTed embed remains valid UTF-8
- **AND** a supplementary Unicode code point counts as two units and is never split
- **AND** content at the exact boundary is not truncated

#### Scenario: In-bound Discord content remains compatible

- **WHEN** every embed component and their aggregate fit within the Discord bounds
- **THEN** title, description, footer, field names, and field values are unchanged
- **AND** metadata fields appear in sorted-key order

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

Messages SHALL be formatted as `[SEVERITY] Title: Message` and bounded to one SMS segment. A formatted message containing only characters from the complete GSM 03.38 default and extension alphabets SHALL be limited to 160 septets. Default-alphabet characters SHALL count as one septet. The extension characters form feed, `^`, `{`, `}`, `\`, `[`, `~`, `]`, `|`, and `€` SHALL count as two septets. A formatted message containing any other character SHALL be limited to 70 UTF-16 code units, with supplementary Unicode code points counting as two units. The provider SHALL choose the encoding budget from the complete untruncated formatted message. Truncation SHALL preserve valid Unicode, SHALL NOT split a Unicode code point, SHALL retain the leading context, and SHALL include the three-unit ASCII ellipsis `...` within the bound when content is omitted. Phone numbers SHALL be normalized to E.164 format (prepend `+` if missing). Phone numbers SHALL be masked in log output (show only last 4 digits).

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

#### Scenario: GSM-7 SMS stays within one segment

- **WHEN** a formatted SMS contains only GSM-7 characters and exceeds 160 septets
- **THEN** the POSTed body is truncated with an ellipsis to no more than 160 septets
- **AND** GSM-7 extension-table characters count as two septets
- **AND** non-ASCII GSM-7 default-alphabet characters count as one septet

#### Scenario: Unicode SMS stays within one segment

- **WHEN** a formatted SMS contains a non-GSM-7 character and exceeds 70 UTF-16 code units
- **THEN** the POSTed body is truncated with an ellipsis to no more than 70 UTF-16 code units
- **AND** no Unicode code point is split

#### Scenario: Encoding selection uses the complete SMS

- **WHEN** a formatted SMS contains a non-GSM-7 character beyond the retained prefix
- **THEN** the complete untruncated message selects the 70-UTF-16-unit budget
- **AND** truncation does not reclassify the retained prefix as GSM-7

#### Scenario: SMS at the exact segment boundary is unchanged

- **WHEN** a formatted GSM-7 or Unicode SMS exactly fits its one-segment budget
- **THEN** the POSTed body is unchanged and has no ellipsis

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
- **Drift Resolved**: title "Drift Resolved", severity info, source "drift", includes target and comma-separated resolved item keys
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

### Requirement: Drift Alert Deduplication

The alert system SHALL deduplicate periodic drift alerts using per-item cooldowns to prevent notification spam from persistent drift (e.g., ~288 alerts/day from a 5-minute check interval).

Each drift item SHALL be keyed by `"service:type"` (e.g., `traefik:unhealthy`). Two different drift types on the same service SHALL be treated as distinct alert items.

An alert SHALL be sent for a drift item when it is newly detected (not in the alerted items map) or when its cooldown has expired. Items within their cooldown period SHALL be suppressed.

The cooldown duration SHALL default to 1 hour and SHALL be configurable via the `BOSUN_DRIFT_ALERT_COOLDOWN` environment variable (accepts Go duration strings or bare seconds).

Per-item alert timestamps SHALL be persisted in the deploy state file under `drift_alerted_items` (a map of key to timestamp) and `drift_alerted_at` (timestamp of the last alert sent). These fields SHALL use `omitempty` JSON tags for backwards compatibility with older bosun versions.

#### Scenario: First drift detection alerts immediately

- **WHEN** a drift item is detected for the first time
- **AND** it is not present in the alerted items map
- **THEN** an alert is sent for the item
- **AND** the item's timestamp is recorded in `drift_alerted_items`

#### Scenario: Repeated drift within cooldown is suppressed

- **WHEN** the same drift item is detected on the next check cycle
- **AND** its cooldown has not expired
- **THEN** no alert is sent for the item

#### Scenario: Drift re-alerts after cooldown expires

- **WHEN** the same drift item persists past the cooldown period
- **THEN** a new alert is sent for the item
- **AND** the item's timestamp is updated in `drift_alerted_items`

#### Scenario: New drift item added to existing set

- **WHEN** a new drift item appears alongside an existing suppressed item
- **THEN** an alert is sent only for the new item
- **AND** the suppressed item remains silent

#### Scenario: Fresh state treats all items as new

- **WHEN** `drift_alerted_items` is nil or empty (daemon restart, first run)
- **THEN** all current drift items trigger alerts

### Requirement: Drift Resolution Alerts

The alert system SHALL send resolution alerts when previously detected drift clears. A drift item SHALL be considered resolved when it is present in `drift_alerted_items` but absent from the current drift check results.

Resolution alerts SHALL be opt-in, defaulting to enabled. The `BOSUN_DRIFT_RESOLVE_ALERTS` environment variable SHALL control this behavior (`false` or `0` disables, any other value enables).

When all drift clears (no drift items remaining), the system SHALL send a single resolution alert listing all resolved keys and clear `drift_alerted_items`.

#### Scenario: Single item resolves

- **WHEN** a drift item was previously alerted and is no longer detected
- **AND** `BOSUN_DRIFT_RESOLVE_ALERTS` is not `false`
- **THEN** a resolution alert is sent listing the resolved item key
- **AND** the resolved key is removed from `drift_alerted_items`

#### Scenario: All drift resolves

- **WHEN** all previously alerted drift items clear in a single check
- **THEN** a single resolution alert is sent listing all resolved keys
- **AND** `drift_alerted_items` is cleared

#### Scenario: Resolution alerts disabled

- **WHEN** `BOSUN_DRIFT_RESOLVE_ALERTS` is set to `false`
- **AND** a drift item resolves
- **THEN** no resolution alert is sent
- **AND** the resolved key is still removed from `drift_alerted_items`

### Requirement: Cancellation-Resilient Reconcile Alert Delivery

When a reconcile result is classified as interrupted, the alert system SHALL
attempt exactly one deploy-failure lifecycle alert when reconciliation failure
alerts are enabled. The alert reason SHALL identify the result as interrupted.

Interruption alert delivery SHALL preserve values from the caller context while
detaching caller cancellation and enforcing a maximum delivery budget of 30
seconds. It SHALL bypass repeated-failure attempt throttling and SHALL NOT update
`last_alerted_attempt`, because an interruption does not consume the deploy
circuit-breaker failure budget.

The run-boundary interruption finalizer SHALL exclusively own this alert. A
stage-specific alert path that receives an error wrapping `context.Canceled`
while the caller context reports `context.Canceled` SHALL return the causal error
without calling its ordinary `sendThrottledFailureAlert` or
`sendGateFailureAlerts` path. For a declared-scope health gate, suppression SHALL
include the companion rollback notification; rollback results SHALL remain
logged. A stage error that does not satisfy both classifier conditions SHALL
retain its existing stage-owned alert behavior and SHALL NOT receive a finalizer
interruption alert.

Provider delivery failure SHALL be logged and SHALL NOT erase the persisted
interruption outcome or extend shutdown beyond the delivery budget.

#### Scenario: Cancelled deploy still attempts an alert

- **GIVEN** reconciliation failure alerts are enabled
- **WHEN** a mid-deploy shutdown is classified as interrupted
- **THEN** exactly one deploy-failure lifecycle alert is attempted
- **AND** its reason identifies the reconcile as interrupted
- **AND** the alert context is not cancelled with the caller context
- **AND** the alert context retains the reconcile correlation value
- **AND** the alert context has a deadline no later than 30 seconds after
  interruption handling begins

#### Scenario: Interruption does not consume failure alert throttle

- **GIVEN** a commit has existing failure-attempt and alert-throttle state
- **WHEN** a later run of that commit is classified as interrupted
- **THEN** an interruption alert is attempted regardless of the next scheduled
  repeated-failure alert
- **AND** `last_alerted_attempt` remains unchanged

#### Scenario: Stage alert is suppressed in favor of finalizer ownership

- **GIVEN** a pipeline stage normally sends a throttled deploy-failure alert
- **WHEN** its causal error wraps `context.Canceled`
- **AND** the caller context reports `context.Canceled`
- **THEN** the stage-specific path attempts no alert
- **AND** the run-boundary finalizer attempts exactly one interruption alert

#### Scenario: Cancelled declared health gate sends no companion alerts

- **GIVEN** the declared health gate normally sends deploy-failure and rollback
  companion notifications
- **WHEN** its gate error and caller context satisfy interruption classification
- **THEN** `sendGateFailureAlerts` is not called
- **AND** no rollback companion notification is attempted
- **AND** the run-boundary finalizer attempts exactly one interruption alert

#### Scenario: Real stage error racing with shutdown keeps stage ownership

- **WHEN** the caller context reports `context.Canceled`
- **AND** a stage returns an error that does not wrap `context.Canceled`
- **THEN** the existing stage-specific alert behavior is retained
- **AND** the run-boundary finalizer attempts no interruption alert

#### Scenario: Disabled failure alerts suppress interruption delivery

- **GIVEN** reconciliation failure alerts are disabled
- **WHEN** a run is classified as interrupted
- **THEN** no interruption alert is attempted
- **AND** the interrupted outcome is still persisted

#### Scenario: Provider failure preserves interruption state

- **WHEN** every configured provider fails to deliver an interruption alert
- **THEN** the provider failure is logged
- **AND** the interrupted outcome remains persisted
- **AND** delivery returns no later than the 30-second budget

#### Scenario: Multi-target shutdown has one aggregate alert budget

- **GIVEN** a reconciliation cycle has multiple targets
- **WHEN** shutdown interrupts the in-flight target
- **THEN** exactly one interruption alert is attempted for that target
- **AND** no later target attempts an interruption alert
- **AND** aggregate interruption-alert delivery for the cycle is bounded by 30
  seconds

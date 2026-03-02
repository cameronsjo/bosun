# Alerting Spec Changes

## MODIFIED Requirements

### Requirement: Alert Configuration

Alert providers SHALL be configurable via the Bosun configuration file (`bosun.yml` or `.bosun/config.yml`) under the `alerts` key, with environment variable fallbacks for secrets.

The configuration SHALL support:
- `discord_webhook_url` (env: `DISCORD_WEBHOOK_URL`)
- `sendgrid_api_key` (env: `SENDGRID_API_KEY`), `sendgrid_from_email` (env: `SENDGRID_FROM_EMAIL`), `sendgrid_from_name` (env: `SENDGRID_FROM_NAME`), `sendgrid_to_emails`
- `twilio_account_sid` (env: `TWILIO_ACCOUNT_SID`), `twilio_auth_token` (env: `TWILIO_AUTH_TOKEN`), `twilio_from_number` (env: `TWILIO_FROM_NUMBER`), `twilio_to_numbers`
- `on_success` (bool) and `on_failure` (bool, default: true)

When neither `on_success` nor `on_failure` is explicitly set, `on_failure` SHALL default to true.

The `on_failure` flag SHALL gate all failure-related alert dispatch in the reconciler. When `on_failure` is false, the reconciler SHALL NOT send deploy failure alerts, circuit breaker alerts, or recovery alerts.

The `on_success` flag SHALL gate success alert dispatch in the reconciler. When `on_success` is false, the reconciler SHALL NOT send deploy success alerts.

These flags SHALL be propagated from the alert configuration to the reconciler configuration at daemon startup.

#### Scenario: Default failure alerting

- **GIVEN** a configuration file with no `on_success` or `on_failure` settings
- **WHEN** the configuration is loaded
- **THEN** `on_failure` is true
- **AND** `on_success` is false

#### Scenario: Environment variable fallback for secrets

- **GIVEN** no `discord_webhook_url` in the config file
- **WHEN** the `DISCORD_WEBHOOK_URL` environment variable is set
- **THEN** the Discord provider uses the environment variable value

#### Scenario: on_failure gates failure alerts in the reconciler

- **GIVEN** `on_failure` is set to false in the configuration
- **WHEN** a reconciliation fails at any stage
- **THEN** no failure alert is sent
- **AND** the error is still logged and returned

#### Scenario: on_success gates success alerts in the reconciler

- **GIVEN** `on_success` is set to false in the configuration
- **WHEN** a reconciliation succeeds
- **THEN** no success alert is sent

#### Scenario: on_failure true sends alerts for alert-eligible failure stages

- **GIVEN** `on_failure` is true (default)
- **WHEN** a reconciliation fails at an alert-eligible pipeline stage (stages 2-14, including git sync)
- **THEN** a throttled failure alert is sent

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

The reconciler SHALL send failure alerts for pipeline stage failures (stages 2-14), including git sync failures (stage 2) that occur before the deploy state file is loaded. Lock acquisition failures (stage 1) SHALL NOT trigger alerts because they are transient and no state context is available. For git sync failures, the reconciler SHALL load the state file before sending the alert to ensure throttle state is available.

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

#### Scenario: Git sync failure sends deploy failure alert

- **WHEN** a git sync operation fails with a network error
- **AND** `on_failure` is enabled
- **THEN** a "Deployment Failed" alert is sent with severity error
- **AND** the reason includes the git sync error message
- **AND** the alert respects the throttle schedule

## MODIFIED Requirements

### Requirement: Alert Configuration

Alert providers SHALL be configurable via the Bosun configuration file (`bosun.yml` or `.bosun/config.yml`) under the `alerts` key, with environment variable fallbacks for secrets.

The configuration SHALL support:
- `discord_webhook_url` (env: `DISCORD_WEBHOOK_URL`)
- `sendgrid_api_key` (env: `SENDGRID_API_KEY`), `sendgrid_from_email` (env: `SENDGRID_FROM_EMAIL`), `sendgrid_from_name` (env: `SENDGRID_FROM_NAME`), `sendgrid_to_emails`
- `twilio_account_sid` (env: `TWILIO_ACCOUNT_SID`), `twilio_auth_token` (env: `TWILIO_AUTH_TOKEN`), `twilio_from_number` (env: `TWILIO_FROM_NUMBER`), `twilio_to_numbers`
- `on_success` (bool), `on_failure` (bool, default: true), and `on_recovery` (bool, default: true)

When a gate is not explicitly set, `on_failure` and `on_recovery` SHALL default to true and `on_success` SHALL default to false. `on_recovery` defaults to true because a system that alerts on failure and cannot retract that alert leaves the operator worse informed than one that never alerted.

The default SHALL be applied everywhere an alert configuration is constructed — file extraction, environment-variable construction, and the built-in default configuration — so that a configuration reaching the daemon by any path carries the same gate values. The daemon SHALL propagate all three gates into the reconciler configuration at startup.

#### Scenario: Default failure alerting

- **GIVEN** a configuration file with no `on_success`, `on_failure`, or `on_recovery` settings
- **WHEN** the configuration is loaded
- **THEN** `on_failure` is true
- **AND** `on_recovery` is true
- **AND** `on_success` is false

#### Scenario: Recovery gate is independent of the success gate

- **GIVEN** a configuration with `on_success: false` and no `on_recovery` setting
- **WHEN** the configuration is loaded
- **THEN** `on_recovery` is true
- **AND** a recovery alert is not suppressed by `on_success` being false

#### Scenario: Recovery alerting can be disabled explicitly

- **GIVEN** a configuration with `on_recovery: false`
- **WHEN** a reconcile run recovers from a previously-alerted failure
- **THEN** no recovery alert is dispatched

#### Scenario: Gates reach the daemon reconciler

- **GIVEN** a configuration with `on_recovery` at its default
- **WHEN** the daemon builds its reconciler configuration at startup
- **THEN** the reconciler configuration carries `on_recovery` true

#### Scenario: Environment variable fallback for secrets

- **GIVEN** no `discord_webhook_url` in the config file
- **WHEN** the `DISCORD_WEBHOOK_URL` environment variable is set
- **THEN** the Discord provider uses the environment variable value

## ADDED Requirements

### Requirement: Deploy Recovery Dispatch

A Deploy Recovery alert SHALL be dispatched when a reconcile run for a target ends without failure and a failure alert has previously been sent for that target and not yet retracted. Dispatch SHALL be gated on `on_recovery` alone.

Recovery SHALL be evaluated at the **run** boundary, not the deploy boundary. A run that completes successfully without deploying — because no deploy-relevant files changed, or because the resolved commit is already deployed — SHALL dispatch the recovery alert before clearing failure state. These skip paths currently reset the failure counters and return early, which is why a failure followed by a documentation-only recovery produces no retraction today.

A single failure that produced an alert SHALL earn a retraction. Dispatch SHALL NOT require more than one recorded failure attempt, because failure alerts are emitted at the first attempt.

Failure tracking state (the attempt count and the last-alerted attempt) SHALL be cleared only after the recovery alert has been dispatched, so a run cannot silently discard the evidence that a retraction is owed.

#### Scenario: Docs-only recovery still retracts

- **GIVEN** a target whose previous reconcile run failed and sent a failure alert at attempt 1
- **WHEN** the next run succeeds and changes only files outside the configured deploy paths, so deployment is skipped
- **THEN** a Deploy Recovery alert is dispatched
- **AND** the failure tracking state is cleared afterwards

#### Scenario: Already-deployed recovery still retracts

- **GIVEN** a target whose previous reconcile run failed and sent a failure alert
- **WHEN** the next run succeeds and skips deployment because the resolved commit is already deployed
- **THEN** a Deploy Recovery alert is dispatched

#### Scenario: One failure earns one retraction

- **GIVEN** a target with exactly one failed attempt that produced a failure alert
- **WHEN** the next run ends clean
- **THEN** a Deploy Recovery alert is dispatched

#### Scenario: Recovery is not gated on the success gate

- **GIVEN** `on_success` is false and `on_recovery` is true
- **WHEN** a target recovers from an alerted failure
- **THEN** a Deploy Recovery alert is dispatched
- **AND** no Deploy Success alert is dispatched

#### Scenario: No failure alert means no retraction

- **GIVEN** a target with no previously-sent failure alert
- **WHEN** a reconcile run ends clean
- **THEN** no Deploy Recovery alert is dispatched

#### Scenario: Recovery is dispatched once

- **GIVEN** a target that has just been sent a Deploy Recovery alert
- **WHEN** the following reconcile run also ends clean
- **THEN** no further Deploy Recovery alert is dispatched

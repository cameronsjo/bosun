## MODIFIED Requirements

### Requirement: Alert Configuration

Alert providers SHALL be configurable via the Bosun configuration file (`bosun.yml` or `.bosun/config.yml`) under the `alerts` key, with environment variable fallbacks for secrets.

The configuration SHALL support:
- `discord_webhook_url` (env: `DISCORD_WEBHOOK_URL`)
- `sendgrid_api_key` (env: `SENDGRID_API_KEY`), `sendgrid_from_email` (env: `SENDGRID_FROM_EMAIL`), `sendgrid_from_name` (env: `SENDGRID_FROM_NAME`), `sendgrid_to_emails`
- `twilio_account_sid` (env: `TWILIO_ACCOUNT_SID`), `twilio_auth_token` (env: `TWILIO_AUTH_TOKEN`), `twilio_from_number` (env: `TWILIO_FROM_NUMBER`), `twilio_to_numbers`
- `on_success` (bool), `on_failure` (bool, default: true), and `on_recovery` (bool, default: true)

When neither `on_success` nor `on_failure` is explicitly set, `on_failure` SHALL default to true. This existing coupling is unchanged: setting `on_success` explicitly while leaving `on_failure` unset leaves `on_failure` false.

`on_recovery` SHALL default to true whenever it is not explicitly set, unconditionally — it is **not** coupled to `on_success` or `on_failure`.

The principle the default serves is that **the retract gate must never be more restrictive than the alert gate**: a system that alerts on failure and cannot retract leaves the operator worse informed than one that never alerted. Default-true satisfies that principle without tying `on_recovery` to a gate whose own effective value is currently in question, and it costs nothing when `on_failure` is false, since Deploy Recovery Dispatch already requires a prior failure alert.

`on_success` SHALL default to false.

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

#### Scenario: Explicit on_success does not suppress the recovery default

- **GIVEN** a configuration with `on_success: true` and neither `on_failure` nor `on_recovery` set
- **WHEN** the configuration is loaded
- **THEN** `on_recovery` is true
- **AND** `on_failure` is false, preserving the existing coupling unchanged

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

Recovery SHALL be evaluated at the **run** boundary, not the deploy boundary. A run that completes successfully without deploying — because no deploy-relevant files changed, or because the resolved commit is already deployed — SHALL dispatch the recovery alert before clearing failure state.

The two skip paths fail differently today and SHALL NOT be treated as one case:

- The deploy-path skip zeroes `AttemptCount`, `LastAttemptedCommit`, **and** `LastAlertedAttempt`, then returns. It destroys the evidence that a retraction is owed.
- The already-deployed skip zeroes only `AttemptCount` and `LastAttemptedCommit`, and does so conditionally. `LastAlertedAttempt` survives it. Its defect is the early return before dispatch, not counter destruction.

**The predicate for "a retraction is owed" SHALL be `LastAlertedAttempt > 0`.** `AttemptCount > 0` is wrong — a failure below the alert threshold never produced an alert and owes no retraction. Naming the predicate is part of the requirement because the three candidate state fields clear differently per branch, so an implementation that picks a different one satisfies some scenarios and not others.

A single failure that produced an alert SHALL earn a retraction. Dispatch SHALL NOT require more than one recorded failure attempt, because failure alerts are emitted at the first attempt.

The prior-failure count carried in the alert SHALL be the number of failed attempts, which is `AttemptCount` — not `AttemptCount - 1`. The existing call site subtracts one because it ran only when `AttemptCount > 1`; with that condition removed, the subtraction reports **0 prior failures** in exactly the single-failure case this change exists to serve, contradicting the retained requirement that the Deploy Recovery alert include a count of prior failures.

Failure tracking state SHALL be cleared when a run ends clean — after dispatch when a retraction was owed and `on_recovery` is enabled, and unconditionally otherwise. State SHALL NOT be left uncleared because dispatch was disabled: an operator who later enables `on_recovery` must not receive a retraction for a months-old failure. Clearing SHALL include `LastAlertedAttempt` on **every** clean-run path, including the already-deployed skip, which does not clear it today.

`on_recovery` SHALL be hot-reloadable on the same terms as `on_success` and `on_failure`. Those gates are propagated both at daemon startup and per run through the config-reload path; a gate wired only at startup would be the sole gate that cannot be changed without a restart, and the reload log would report two of three.

#### Scenario: Docs-only recovery still retracts

- **GIVEN** a target whose previous reconcile run failed and sent a failure alert at attempt 1
- **WHEN** the next run succeeds and changes only files outside the configured deploy paths, so deployment is skipped
- **THEN** a Deploy Recovery alert is dispatched
- **AND** the failure tracking state is cleared afterwards

#### Scenario: Already-deployed recovery still retracts

- **GIVEN** a target whose previous reconcile run failed and sent a failure alert
- **WHEN** the next run succeeds and skips deployment because the resolved commit is already deployed
- **THEN** a Deploy Recovery alert is dispatched

#### Scenario: One failure earns one retraction, counted as one

- **GIVEN** a target with exactly one failed attempt that produced a failure alert
- **WHEN** the next run ends clean
- **THEN** a Deploy Recovery alert is dispatched
- **AND** the alert reports 1 prior failure, not 0

#### Scenario: A failure below the alert threshold owes no retraction

- **GIVEN** a target with a recorded failed attempt that did not reach an alert threshold, so `LastAlertedAttempt` is 0
- **WHEN** the next run ends clean
- **THEN** no Deploy Recovery alert is dispatched

#### Scenario: Recovery is not gated on the success gate

- **GIVEN** `on_success` is false and `on_recovery` is true
- **WHEN** a target recovers from an alerted failure
- **THEN** a Deploy Recovery alert is dispatched
- **AND** no Deploy Success alert is dispatched

#### Scenario: No failure alert means no retraction

- **GIVEN** a target with no previously-sent failure alert
- **WHEN** a reconcile run ends clean
- **THEN** no Deploy Recovery alert is dispatched

#### Scenario: Recovery is dispatched once, via the deploy path

- **GIVEN** a target that has just been sent a Deploy Recovery alert on a run that deployed
- **WHEN** the following reconcile run also ends clean and deploys
- **THEN** no further Deploy Recovery alert is dispatched

#### Scenario: Recovery is dispatched once, via the already-deployed skip

- **GIVEN** a target that has just been sent a Deploy Recovery alert
- **WHEN** the following reconcile run ends clean and takes the already-deployed skip — the branch that does not clear `LastAlertedAttempt` today
- **THEN** no further Deploy Recovery alert is dispatched
- **AND** a run that repeats that skip indefinitely does not re-alert on each pass

## MODIFIED Requirements

### Requirement: Alert Configuration

Alert providers SHALL be configurable via the Bosun configuration file (`bosun.yml` or `.bosun/config.yml`) under the `alerts` key, with environment variable fallbacks for secrets.

The configuration SHALL support:
- `discord_webhook_url` (env: `DISCORD_WEBHOOK_URL`)
- `sendgrid_api_key` (env: `SENDGRID_API_KEY`), `sendgrid_from_email` (env: `SENDGRID_FROM_EMAIL`), `sendgrid_from_name` (env: `SENDGRID_FROM_NAME`), `sendgrid_to_emails`
- `twilio_account_sid` (env: `TWILIO_ACCOUNT_SID`), `twilio_auth_token` (env: `TWILIO_AUTH_TOKEN`), `twilio_from_number` (env: `TWILIO_FROM_NUMBER`), `twilio_to_numbers`
- `on_success` (bool, default: false), `on_failure` (bool, default: true) and `on_recovery` (bool, default: true)

Each gate SHALL resolve from its own key only. A key that is unset takes that gate's default whatever the other gates are set to, and an explicit `false` SHALL be honored.

#### Scenario: Default failure alerting

- **GIVEN** a configuration file with no `on_success` or `on_failure` settings
- **WHEN** the configuration is loaded
- **THEN** `on_failure` is true
- **AND** `on_success` is false

#### Scenario: Setting on_success alone keeps failure alerts on

- **GIVEN** a configuration file with `on_success: true` and no `on_failure` or `on_recovery` setting
- **WHEN** the configuration is loaded
- **THEN** `on_success` is true
- **AND** `on_failure` is true
- **AND** `on_recovery` is true

#### Scenario: Explicit false is honored

- **GIVEN** a configuration file with `on_success: true` and `on_failure: false`
- **WHEN** the configuration is loaded
- **THEN** `on_failure` is false
- **AND** `on_success` is true

#### Scenario: Environment variable fallback for secrets

- **GIVEN** no `discord_webhook_url` in the config file
- **WHEN** the `DISCORD_WEBHOOK_URL` environment variable is set
- **THEN** the Discord provider uses the environment variable value

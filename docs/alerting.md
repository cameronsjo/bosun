# Alerting Configuration

Bosun supports multiple alert providers for deployment notifications. Alerts are sent on:

- **Deployment success** (optional, disabled by default)
- **Deployment failure** (enabled by default)
- **Rollback success**
- **Rollback failure** (critical severity)

## Providers

### Discord

Send alerts to a Discord channel via webhook.

**Configuration:**

```yaml
# bosun.yaml
alerts:
  discord_webhook_url: "https://discord.com/api/webhooks/..."
  on_success: false  # Optional: alert on successful deploys
  on_failure: true   # Default: alert on failures
```

**Environment variable:**

```bash
DISCORD_WEBHOOK_URL=https://discord.com/api/webhooks/...
```

Alerts appear as rich embeds with:
- Color-coded severity (green=success, orange=warning, red=error, purple=critical)
- Deployment metadata (commit, target host)
- Timestamp and source

### SendGrid (Email)

Send alert emails via SendGrid API.

**Configuration:**

```yaml
# bosun.yaml
alerts:
  sendgrid_api_key: "SG.xxx..."
  sendgrid_from_email: "alerts@example.com"
  sendgrid_from_name: "Bosun Alerts"
  sendgrid_to_emails:
    - "admin@example.com"
    - "oncall@example.com"
```

**Environment variables:**

```bash
SENDGRID_API_KEY=SG.xxx...
SENDGRID_FROM_EMAIL=alerts@example.com
SENDGRID_FROM_NAME="Bosun Alerts"
```

Emails include:
- HTML-formatted body with severity-colored headers
- Plain text fallback
- Deployment metadata table

### Twilio (SMS)

Send SMS alerts for critical issues via Twilio.

**Configuration:**

```yaml
# bosun.yaml
alerts:
  twilio_account_sid: "ACxxx..."
  twilio_auth_token: "xxx..."
  twilio_from_number: "+15551234567"
  twilio_to_numbers:
    - "+15559876543"
    - "+15551112222"
```

**Environment variables:**

```bash
TWILIO_ACCOUNT_SID=ACxxx...
TWILIO_AUTH_TOKEN=xxx...
TWILIO_FROM_NUMBER=+15551234567
```

**Note:** SMS alerts are only sent for **error** and **critical** severity to minimize costs.

## Alert Settings

```yaml
# bosun.yaml
alerts:
  on_success: false  # Alert on successful deployments
  on_failure: true   # Alert on failed deployments (default: true)
```

## Severity Levels

| Severity | When Used | Providers |
|----------|-----------|-----------|
| `info` | Successful deployment | Discord, SendGrid |
| `warning` | Rollback completed | All |
| `error` | Deployment failed | All |
| `critical` | Rollback failed | All (SMS included) |

## Environment Variable Priority

Environment variables override config file values. This allows sensitive values to be injected at runtime:

1. Environment variable (highest priority)
2. Config file value
3. No configuration (provider disabled)

## Example: Full Configuration

```yaml
# bosun.yaml
alerts:
  # Discord (primary)
  discord_webhook_url: "${DISCORD_WEBHOOK_URL}"

  # SendGrid (email backup)
  sendgrid_api_key: "${SENDGRID_API_KEY}"
  sendgrid_from_email: "bosun@example.com"
  sendgrid_from_name: "Bosun GitOps"
  sendgrid_to_emails:
    - "admin@example.com"

  # Twilio (critical only)
  twilio_account_sid: "${TWILIO_ACCOUNT_SID}"
  twilio_auth_token: "${TWILIO_AUTH_TOKEN}"
  twilio_from_number: "+15551234567"
  twilio_to_numbers:
    - "+15559876543"

  # Settings
  on_success: true
  on_failure: true
```

## Testing Alerts

Use `bosun reconcile --dry-run` to test the reconciliation workflow without making changes. Alerts will still be sent for dry-run results if configured.

To test individual providers:

```bash
# Check if providers are configured
bosun validate

# Trigger a reconcile (will send alerts on completion)
bosun trigger -s "test-alert"
```

## Troubleshooting

### Alerts not sending

1. Check provider is configured: `bosun validate`
2. Verify environment variables are set
3. Check daemon logs for provider errors
4. Ensure network connectivity to provider APIs

### Discord rate limiting

Discord has rate limits on webhook calls. If deploying frequently, consider:
- Using `on_success: false` to reduce alert volume
- Adding delays between rapid deployments

### SMS not received

Twilio SMS is only sent for `error` and `critical` severity. For testing, you can temporarily change a provider's configuration to send test alerts.

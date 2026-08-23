# Change: Bound provider alert content

## Why

Alert titles, messages, sources, and metadata can contain deploy errors and
service lists whose size is controlled by repository content and external
command output. Discord rejects an embed when any component or its aggregate
text exceeds the API limits, silently losing the operational alert, while the
current 1600-character Twilio cap can fan one alert out across many billed SMS
segments.

## What Changes

- Bound Discord title, description, footer, field, field-count, and aggregate
  embed content before sending while retaining the alert's leading context.
- Make Discord truncation Unicode-safe and deterministic for metadata fields.
- Bound Twilio alerts to one SMS segment, accounting for GSM-7 extension
  characters and UTF-16 code units for non-GSM content.
- Document the provider-specific delivery and cost bounds.

## Impact

- Affected specs: `alerting`
- Affected code: `internal/alert/discord.go`, `internal/alert/twilio.go`, and
  provider tests
- All consumers: every `alert.Alert` sent through `DiscordProvider.Send` or
  `Twilio.Send`, including reconcile lifecycle, drift, doctor, daemon, and
  `bosun alert test` alerts

## MODIFIED Requirements

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
- **AND** the POSTed embed text totals no more than 6000 characters

#### Scenario: Discord aggregate budget is deterministic

- **WHEN** metadata would push an otherwise valid embed beyond 6000 characters or 25 fields
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

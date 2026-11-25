## ADDED Requirements

### Requirement: Webhook secret resolves one way

The daemon, the standalone `bosun webhook` receiver, and `bosun validate` SHALL resolve the webhook secret in one order: the receiver's `--secret` flag (receiver only), then `WEBHOOK_SECRET`, then `GITHUB_WEBHOOK_SECRET`. A placeholder SHALL count as no secret: after trimming whitespace, `change-me` or the `bosun init` template's example value, in any case. A whitespace-only value is no secret too.

#### Scenario: Only the GitHub variable set

- **WHEN** only `GITHUB_WEBHOOK_SECRET` is set
- **THEN** the daemon and the receiver both validate signatures with it

#### Scenario: Both variables set to the same value

- **WHEN** `WEBHOOK_SECRET` and `GITHUB_WEBHOOK_SECRET` are set to the same value
- **THEN** the daemon and the receiver both validate signatures with that value

#### Scenario: The change-me placeholder is no secret

- **WHEN** the resolved secret is the literal `change-me` and `BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK` is not `true`
- **THEN** every trigger endpoint on the daemon and the receiver rejects requests with 403, including requests signed with `change-me`
- **AND** no reconcile is triggered and nothing is forwarded

#### Scenario: Placeholder variants are no secret

- **WHEN** the resolved secret is `Change-Me` padded with whitespace, or the `bosun init` template's example value uncommented as written
- **THEN** both servers treat it as no secret and reject a request signed with it with 403

#### Scenario: A placeholder variable defers to the other

- **WHEN** `WEBHOOK_SECRET` is `change-me` and `GITHUB_WEBHOOK_SECRET` holds another value
- **THEN** both servers validate signatures with the `GITHUB_WEBHOOK_SECRET` value

### Requirement: Conflicting webhook secrets fail closed

When `WEBHOOK_SECRET` and `GITHUB_WEBHOOK_SECRET` hold different non-placeholder values and no receiver `--secret` settles it, every trigger endpoint on both servers SHALL reject requests with 403, even with the opt-out, and log an error naming both variables, never their values. The daemon's socket config response SHALL report the conflict, and a receiver adopting its secret SHALL fail closed the same way.

#### Scenario: Both variables set to different values

- **WHEN** `WEBHOOK_SECRET` and `GITHUB_WEBHOOK_SECRET` are set to different values
- **THEN** a request signed with either value is rejected with 403 on every trigger endpoint of the daemon and the receiver
- **AND** no reconcile is triggered and nothing is forwarded
- **AND** each server logs an error naming both variables and containing neither value

#### Scenario: The opt-out does not reopen a conflict

- **WHEN** the secrets conflict and `BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK=true`
- **THEN** unsigned and signed requests are still rejected with 403

#### Scenario: A fetching receiver inherits the daemon's conflict

- **WHEN** the daemon's secrets conflict and a receiver with no secret of its own and `BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK=true` fetches its secret from the daemon (`--fetch-secret`, or no local secret)
- **THEN** the daemon's config response reports the conflict
- **AND** every receiver trigger endpoint rejects requests with 403 and forwards nothing

#### Scenario: The receiver flag settles a conflict

- **WHEN** the secrets conflict and the receiver is started with `--secret`
- **THEN** the receiver validates signatures with the flag's value

#### Scenario: Validate reports the conflict

- **WHEN** `bosun validate` runs with the secrets conflicting
- **THEN** it reports an error naming both variables and containing neither value

### Requirement: Generic webhook signature header order

The generic `/webhook` endpoint on the daemon and on the receiver SHALL read its HMAC signature from the first non-empty header of `X-Signature`, `X-Hub-Signature-256`, `X-Signature-256`; a valid later header SHALL NOT rescue an invalid earlier one. `/webhook/github` SHALL read only `X-Hub-Signature-256`, and the daemon's `/webhook/manual` only `X-Signature`.

#### Scenario: Any of the three generic headers

- **WHEN** a generic `/webhook` request carries a valid HMAC under `X-Signature`, `X-Hub-Signature-256`, or `X-Signature-256`
- **THEN** the daemon and the receiver both accept it

#### Scenario: First present header decides

- **WHEN** a generic request carries an invalid `X-Signature` and a valid `X-Hub-Signature-256`
- **THEN** the daemon and the receiver both reject it with 401

#### Scenario: Scheme-specific endpoints stay narrow

- **WHEN** `/webhook/manual` receives a valid HMAC only under `X-Hub-Signature-256` or `X-Signature-256`, or `/webhook/github` receives one only under `X-Signature` or `X-Signature-256`
- **THEN** the request is rejected with 401

# Change: Resolve the webhook secret and generic signature header one way

## Why

The daemon and the standalone `bosun webhook` receiver disagreed on webhook auth (cameronsjo/bosun#794):

- The daemon preferred `GITHUB_WEBHOOK_SECRET` over `WEBHOOK_SECRET`. The receiver and `bosun validate` preferred `WEBHOOK_SECRET`. With both set to different values, the two servers checked signatures against different secrets, and the receiver's `--fetch-secret` could hand it the daemon's winner.
- The daemon's generic `/webhook` read `X-Signature`, then `X-Hub-Signature-256`. The receiver's read `X-Hub-Signature-256`, then `X-Signature-256`. A sender configured for one server could fail on the other.
- Older starter compose files shipped `WEBHOOK_SECRET: ${WEBHOOK_SECRET:-change-me}`. That value is public, yet both servers accepted it as a configured secret.

No spec stated any of this. The open changes cover the fail-closed gate for a missing secret, but not how a secret resolves.

## What Changes

- The daemon, the receiver (after its `--secret` flag), and `bosun validate` SHALL resolve the secret in one order: `WEBHOOK_SECRET`, then `GITHUB_WEBHOOK_SECRET`.
- A placeholder SHALL count as no secret on both servers, so the existing fail-closed `403` applies to it. After trimming whitespace and ignoring case, that means `change-me`, or the example value the `bosun init` env template writes. A whitespace-only value counts as none too.
- When `WEBHOOK_SECRET` and `GITHUB_WEBHOOK_SECRET` are set to different values, every trigger endpoint on both servers SHALL answer `403`, even with `BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK=true`. Each server SHALL log an error naming both variables, never their values. No sender can know which secret to sign with, so neither is picked. A receiver `--secret` flag settles the conflict for the receiver. `bosun validate` reports the conflict as an error.
- The daemon's socket `GET /config` SHALL report a conflict alongside the secret (`webhook_secret_conflict`). A receiver that would adopt the daemon's secret, either through `--fetch-secret` or because it has no local secret, SHALL then fail closed the same way, whatever its own opt-out says (cameronsjo/bosun#837). Without this, the empty secret a conflicting daemon sends would read as "no secret configured".
- Both generic `/webhook` endpoints SHALL read the signature from the first present of `X-Signature`, `X-Hub-Signature-256`, `X-Signature-256`. `/webhook/github` and `/webhook/manual` keep their single header.

## Impact

- Affected specs: `daemon-security`. ADDED: Webhook secret resolves one way; Conflicting webhook secrets fail closed; Generic webhook signature header order.
- Affected code: `internal/daemon/webhook_guard.go`, `internal/daemon/daemon.go`, `internal/daemon/server.go`, `internal/daemon/socket.go` and `pure.go` (the config response), `internal/cmd/webhook.go`, `internal/cmd/validate.go`, `internal/cmd/init.go`, the Unraid template, and their tests. The implementation is cameronsjo/bosun#833.
- **Breaking:**
  - A deployment whose secret is a placeholder (`change-me` in any case, padded or not, or the init example value) gets `403` on every trigger unless the opt-out is set.
  - A deployment with two differing secret variables gets `403` on every trigger.
  - On the receiver, a generic request with an invalid `X-Signature` is rejected even when its `X-Hub-Signature-256` is valid.

## Relation to open changes

These changes are unarchived, and each one ADDs to `daemon-security`. No base `daemon-security` spec exists, so there is nothing to MODIFY, and this change ADDs as well. None of the three new requirement names collides with an existing one.

- `add-daemon-security`, "Webhook authentication fail-closed": it sets the 403-or-opt-out rule when no secret is configured. This change defines when a secret counts as configured (`change-me` does not), and adds a second fail-closed case, conflicting secrets, that the opt-out does not reach. Its scenarios stay true.
- `harden-ingress-boundaries`, "Standalone receiver fails closed": the same relation, for the receiver's "no secret resolves" condition.
- `harden-webhook-delete-and-replay`, "Manual trigger does not accept webhook bodies": it assumes `/webhook/manual` reads only `X-Signature`. The header requirement here states that explicitly.

## Limits

- A daemon from before this change omits `webhook_secret_conflict`. A receiver reads the missing field as no conflict, which is the old behavior.
- Two variables that differ only by whitespace still conflict: the values are compared exactly, because HMAC uses the exact bytes. The error message names whitespace as a likely cause.

## Scope

This PR carries the spec delta only. The implementation follows in cameronsjo/bosun#833 once this change is labelled `ready-to-build`.

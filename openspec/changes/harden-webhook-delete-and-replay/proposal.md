# Change: Ignore branch-delete pushes and drop replayed webhook deliveries

## Why

Two webhook gaps remained after `filter-generic-webhook-branch` (#738) and
`filter-receiver-provider-webhook-branch` (#743), tracked as #747:

1. A push that deletes the tracked branch carries `ref:
   refs/heads/<branch>`, so it passes the branch filter and starts a
   reconcile of a branch that no longer exists. GitHub marks it with
   `deleted: true` and an all-zero `after`; GitLab and Gitea send an
   all-zero `after`. Bitbucket already falls out, because a deletion has no
   `new` target.
2. Nothing records which deliveries were already handled, so a captured,
   validly signed request can be sent again to start another reconcile.

## What Changes

- Every trigger endpoint that reads a push payload (the daemon's `/webhook`
  and `/webhook/github`; the receiver's `/webhook`, `/webhook/github`,
  `/webhook/gitlab`, `/webhook/gitea`) SHALL ignore a push whose JSON body has
  `deleted: true` or an `after` made only of `0` characters. The response is
  `200` `ignored`, nothing is forwarded or triggered, and the push is logged
  at warn level, because deleting the deploy branch is unusual.
- Each server (the daemon's HTTP listener and the receiver) SHALL keep a
  bounded in-memory record of recent delivery IDs, read from the first
  non-empty header of `X-GitHub-Delivery`, `X-Gitea-Delivery`, `webhook-id`,
  `Idempotency-Key`, `X-Gitlab-Event-UUID`, `X-Request-UUID`. The record is
  checked only after the request's signature or token check passes. A repeat
  within the window gets `200` `ignored`, is logged at warn level, and is not
  forwarded or triggered.
- The record holds at most 1024 entries for 1 hour. It stores a SHA-256
  digest of each ID, so an entry's size does not depend on the header's
  length. When it is full, the oldest entry is dropped.
- A delivery whose trigger fails (the receiver cannot reach the daemon, or the
  daemon is shutting down) SHALL be removed from the record, so the
  provider's own retry still goes through.
- A request without any delivery header is processed as before.

## Impact

- Affected specs: `daemon-security`. ADDED: Webhook branch-delete pushes are
  ignored; Webhook delivery replay is dropped.
- Affected code: `internal/daemon/webhook_guard.go` (new:
  `PushDeletesBranch`, `WebhookDeliveryID`, `DeliveryCache`),
  `internal/daemon/server.go`, `internal/cmd/webhook.go`, with tests.
- Docs: `docs/gitops.md`, `docs/commands.md`, and the onboard skill
  resources.
- Behavior change: a delivery repeated within an hour is no longer acted on.
  No new flags or env vars.

## Known limit

The delivery headers are not covered by any provider's signature. This record
stops a request sent again unchanged. It does not stop a sender who also
changes the delivery header; doing that needs a signed timestamp or nonce,
which GitHub, Gitea, and Bitbucket do not provide.

GitLab's `X-Gitlab-Webhook-UUID` identifies the webhook configuration, not one
delivery, so it is deliberately not read: every push from one hook would share
it.

## Scope

Spec delta and implementation ship together in one PR.

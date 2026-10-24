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
  bounded in-memory record of two replay keys per authorized request:
  - the delivery ID, read from the first non-empty header of
    `X-GitHub-Delivery`, `X-Gitea-Delivery`, `webhook-id`, `Idempotency-Key`,
    `X-Gitlab-Event-UUID`, `X-Request-UUID`;
  - the SHA-256 of the raw request body, in a separate key namespace.
- The record is checked only after the request's signature or token check
  passes, on every trigger endpoint. A repeat of either key within the window
  gets `200` `ignored`, is logged at warn level, and is not forwarded or
  triggered.
- The body key closes the gap the delivery ID leaves: no provider signs the
  delivery headers, so a captured signed body re-sent with a new delivery ID
  still matches. GitLab authenticates with a token rather than a body
  signature; the body key applies there too.
- An empty body has no body key, so the generic endpoint's plain signed
  trigger (an empty body, sent repeatedly) keeps working. A non-empty body
  sent twice within an hour to the generic endpoint is now ignored the second
  time; a caller that wants repeated triggers sends an empty body or varies
  the body.
- The record holds at most 1024 entries for 1 hour. It stores a SHA-256
  digest of each key, so an entry's size does not depend on the input. When
  it is full, the oldest entry is dropped.
- A delivery whose trigger fails (the receiver cannot reach the daemon, or the
  daemon is shutting down) SHALL have both keys removed from the record, so
  the provider's own retry still goes through.
- A request without any delivery header is still checked by its body key.

## Impact

- Affected specs: `daemon-security`. ADDED: Webhook branch-delete pushes are
  ignored; Webhook delivery replay is dropped.
- Affected code: `internal/daemon/webhook_guard.go` (new:
  `PushDeletesBranch`, `WebhookDeliveryID`, `WebhookBodyKey`,
  `DeliveryCache`),
  `internal/daemon/server.go`, `internal/cmd/webhook.go`, with tests.
- Docs: `docs/gitops.md`, `docs/commands.md`, and the onboard skill
  resources.
- Behavior change: a delivery ID or a non-empty body repeated within an hour
  is no longer acted on. No new flags or env vars.

## Limits

The record lives in memory, so a restart clears it, and a body replayed more
than an hour after it was first seen is processed again. Closing that needs a
signed timestamp, which GitHub, Gitea, and Bitbucket do not send.

GitLab's `X-Gitlab-Webhook-UUID` identifies the webhook configuration, not one
delivery, so it is deliberately not read: every push from one hook would share
it.

## Scope

Spec delta and implementation ship together in one PR.

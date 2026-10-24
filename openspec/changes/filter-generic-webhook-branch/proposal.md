# Change: Filter generic webhook pushes on the tracked branch

## Why

The generic `/webhook` handler starts a reconcile for every authorized
request. `/webhook/github` already ignores pushes to any branch other than
`BOSUN_REPO_BRANCH`, but the generic handler has no such check (#728). A
repository hook that sends push events for every branch, or every GitHub event
type, to the generic endpoint therefore starts a reconcile for each push to
each branch, each tag, and each issue or pull-request event. One deployment
saw 100 generic webhooks and 89 webhook-sourced reconciles against 9 commits
to the deploy branch.

## What Changes

- After authorization, the generic handler SHALL read an optional top-level
  `ref` from a JSON object body. When present, a reconcile SHALL start only
  when `ref` equals `refs/heads/<tracked branch>`, the same comparison
  `/webhook/github` uses. Other branches, tags, an empty `ref`, and a
  non-string `ref` SHALL be ignored.
- When the request carries an `X-GitHub-Event` header, the generic handler
  SHALL apply the GitHub endpoint's event filter: only `push` can start a
  reconcile; `ping` and every other event type are ignored. A GitHub push
  without a readable JSON `ref` (a form-encoded hook) SHALL be ignored, since
  its branch is unknown.
- An ignored request SHALL get `200` with `{"status":"ignored","message":…}`
  so senders do not retry, and SHALL log the sanitized ref or event and the
  reason. Ignored requests SHALL NOT start a reconcile or count as a webhook
  trigger in metrics.
- A body with no `ref` (empty, non-JSON, or a JSON object without the key)
  SHALL keep starting a reconcile as before. This applies only without an
  `X-GitHub-Event` header. The generic endpoint stays a
  plain signed trigger for senders that carry no branch.
- Authorization SHALL stay the first check. No payload is parsed before the
  signature or the fail-closed gate passes.

## Impact

- Affected specs: `daemon-security` — ADDED: Generic webhook branch filter.
- Affected code: `internal/daemon/pure.go` (pure decision function),
  `internal/daemon/server.go` (`handleWebhook`), tests in
  `internal/daemon/pure_test.go` and `internal/daemon/server_test.go`.
- Docs: `docs/gitops.md`, `skills/onboard/resources/gitops.md`.
- Behavior change: a sender that posts a `ref` naming another branch, a tag,
  or a bare branch name without `refs/heads/` no longer triggers. No new env
  vars or config.

## Scope

Spec delta and implementation ship together in one PR.

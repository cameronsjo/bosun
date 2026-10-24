# Tasks

- [x] 1. Spec delta: `specs/daemon-security/spec.md`, ADDED Webhook
  branch-delete pushes are ignored and Webhook delivery replay is dropped.
- [x] 2. Pure `daemon.PushDeletesBranch`, `daemon.WebhookDeliveryID`, and
  `daemon.WebhookBodyKey`, with table tests.
- [x] 3. `daemon.DeliveryCache`: bounded (1024), 1-hour TTL, digest keys,
  oldest-first eviction, multi-key `Claim`/`Release`, usable as a zero value,
  with unit tests for repeat, expiry, eviction, and release.
- [x] 4. Daemon `/webhook` and `/webhook/github`: claim after authorization,
  ignore deletes after the branch filter, release when the reconcile cannot
  start.
- [x] 5. Receiver `/webhook`, `/webhook/github`, `/webhook/gitlab`,
  `/webhook/gitea`, `/webhook/bitbucket`: the same, releasing when the forward
  fails.
- [x] 6. Handler tests per provider for deletes, repeated IDs, the same body
  under a changed delivery header, different bodies to one branch, a bad
  signature on a repeated ID, a failed-forward retry, and the empty-body
  trigger. Staged break of each guard turns its tests red.
- [x] 7. Docs: `docs/gitops.md`, `docs/commands.md`, and the onboard skill
  resources.

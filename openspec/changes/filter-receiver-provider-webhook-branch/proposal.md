# Change: Filter receiver provider webhook pushes on the tracked branch

## Why

The `bosun webhook` receiver's provider endpoints (`/webhook/github`,
`/webhook/gitlab`, `/webhook/gitea`, `/webhook/bitbucket`) forward every
signed push event to the daemon, whatever branch or tag it names (#740).
`filter-generic-webhook-branch` (#728, #738) added a branch filter to the
generic `/webhook` on the daemon and the receiver, and the daemon's own
`/webhook/github` already filters, but the receiver's provider endpoints did
not. Each push to another branch or tag costs a reconcile of the tracked
branch, because reconcile ignores the payload's ref.

## What Changes

- After its signature or token check and its event-type check, each receiver
  provider endpoint SHALL compare the push's branch with the tracked branch
  and forward only a match.
- GitHub, GitLab, and Gitea carry a full `ref`. It SHALL equal
  `refs/heads/<tracked branch>`, the comparison the daemon's
  `/webhook/github` uses. Other branches, tags, bare branch names, and a push
  with no readable `ref` SHALL be ignored.
- Bitbucket lists one `push.changes[]` entry per updated ref, with a bare
  name. The push SHALL be forwarded when any entry's `new` has `type`
  `branch` and `name` equal to the tracked branch. Tags, deletions (`new` is
  null), other branches, and a push with no changes SHALL be ignored.
- An ignored push SHALL get `200` `{"status":"ignored","message":…}` so the
  provider does not retry, SHALL be logged with the sanitized ref and reason,
  and SHALL NOT be forwarded.
- The tracked branch comes from the source `filter-generic-webhook-branch`
  already defined (`--branch`, then `BOSUN_REPO_BRANCH`, then the daemon's
  `GET /config`). With no branch known, the ref check SHALL be off on every
  endpoint and the existing startup warning SHALL say so.

## Impact

- Affected specs: `daemon-security` — ADDED: Receiver provider webhook branch
  filter.
- Affected code: `internal/daemon/pure.go` (exported `ShouldProcessPushRef`),
  `internal/cmd/webhook.go` (four provider handlers,
  `shouldProcessBitbucketPush`, startup wording), tests in
  `internal/daemon/pure_test.go` and `internal/cmd/webhook_test.go`.
- Docs: `docs/commands.md`, `docs/gitops.md`,
  `skills/onboard/resources/commands.md`, `skills/onboard/resources/gitops.md`.
- Behavior change: a receiver with a known branch no longer forwards provider
  pushes to other branches or tags. No new flags or env vars.

## Out of scope

Replay protection (recording `X-GitHub-Delivery` IDs) is a separate hardening
step; it predates this change and is not addressed here.

## Scope

Spec delta and implementation ship together in one PR.

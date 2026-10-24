# Tasks

- [x] 1. Spec delta: `specs/daemon-security/spec.md`, ADDED Receiver provider
  webhook branch filter.
- [x] 2. Exported `daemon.ShouldProcessPushRef` reusing the GitHub push-ref
  comparison, with table tests: matching, other branch, tag, bare name,
  missing ref, unknown branch.
- [x] 3. Pure `shouldProcessBitbucketPush` over `push.changes[]`.
- [x] 4. Apply both in the receiver's four provider handlers after the
  signature and event checks; ignored pushes return `200` `ignored`, log the
  sanitized ref, and are not forwarded.
- [x] 5. Receiver tests per provider: matching, other branch, tag, bare name or
  deletion, missing ref or no changes, unknown branch, and a bad signature
  with a matching ref. A staged break of both deciders turns the 16 ignore
  cases red.
- [x] 6. Docs: `docs/commands.md`, `docs/gitops.md`, and the onboard skill
  resources.

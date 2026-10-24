# Tasks

- [x] 1. Spec delta: `specs/daemon-security/spec.md`, ADDED Generic webhook
  branch filter.
- [x] 2. Pure decision function `shouldProcessGenericWebhook` reusing the
  GitHub event and push-ref checks, with table tests: matching ref, other
  branch, tag ref, empty ref, bare branch name, non-string ref, missing ref,
  empty and non-JSON bodies, GitHub event header cases including a
  form-encoded GitHub push.
- [x] 3. Apply it in `handleWebhook` after authorization; ignored requests
  return `200` `ignored`, log the sanitized ref or event, and skip the
  trigger metric and reconcile.
- [x] 4. Handler tests for matching, non-matching, missing, and tag refs, a
  non-push GitHub event, and an invalid signature with a matching ref.
- [x] 5. Docs: `docs/gitops.md` and `skills/onboard/resources/gitops.md`.

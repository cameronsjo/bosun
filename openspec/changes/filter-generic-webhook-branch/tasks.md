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
- [x] 6. Receiver: export the helper, apply it in the standalone receiver's
  generic `/webhook` after the signature check, resolve the branch from
  `--branch`, `BOSUN_REPO_BRANCH`, or the daemon config, and warn at startup
  when it is unknown. Receiver tests for matching, other-branch, tag, bare,
  missing ref, non-push event, unknown branch, and bad signature; a staged
  break of the receiver filter turns the four ignore cases red.
- [x] 7. Docs: `docs/commands.md` and `skills/onboard/resources/commands.md`
  for `--branch` and the receiver filter.

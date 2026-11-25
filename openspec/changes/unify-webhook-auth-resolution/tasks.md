# Tasks

- [x] 1. Spec delta: `specs/daemon-security/spec.md`, ADDED Webhook secret resolves one way, Conflicting webhook secrets fail closed, and Generic webhook signature header order.
- [ ] 2. Pure helpers in `internal/daemon/webhook_guard.go`: `ResolveWebhookSecret`, `WebhookSecretConfigured` with the `change-me` placeholder, and `GenericWebhookSignature`, with table tests.
- [ ] 3. Daemon: `ConfigFromEnv` resolves through the helper and records a conflict. The trigger gate answers 403 on a conflict before the opt-out, and treats the placeholder as no secret. The startup posture logs the conflict as an error. The generic scheme uses the shared header order.
- [ ] 4. Receiver: `--secret`, then the shared helper. A conflict gate runs on every trigger endpoint. Provider checks use `WebhookSecretConfigured`. The generic endpoint uses the shared header order, and a fetched daemon secret does not settle a local conflict.
- [ ] 5. `bosun validate`: resolves through the helper and reports a conflict as an error.
- [ ] 6. Tests on both servers: conflict rows signed with either secret, with and without the opt-out; `change-me` rows; generic header parity (one table in each package); scheme-isolation rows; posture rows; the validate conflict row; and the existing `ConfigFromEnv` precedence subtest inverted in place.
- [ ] 7. The `init` starter compose requires `WEBHOOK_SECRET` (`${WEBHOOK_SECRET:?set WEBHOOK_SECRET}`). Update the `init` env template, the Unraid template description and README, `docs/security.md`, `docs/gitops.md`, `docs/commands.md`, the `AGENTS.md` env table, the onboard skill resources, and `llms.txt`.

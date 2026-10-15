## 1. Specification and implementation

- [x] 1.1 Record the approved per-target scope and exclusions in the plan,
  proposal, and design.
- [ ] 1.2 Add the explicit project scope parameter and empty-scope guard to
  `reconcile.RunRestartBreaker`.
- [ ] 1.3 Forward the existing target-specific scope from the daemon without
  changing target resolution, state paths, or named-target fallback behavior.

## 2. Verification and consumer coverage

- [ ] 2.1 Keep the foreign-project container regression and verify the breaker
  does not inspect or stop it.
- [ ] 2.2 Keep the two-project regression and verify each target retains its
  own restart state and only its own project can be stopped.
- [ ] 2.3 Add unresolved implicit-target coverage proving no Docker inspection,
  stop, or restart-tracking advancement occurs when scope is empty.
- [ ] 2.4 Update `docs/gitops.md` and
  `skills/onboard/resources/gitops.md` with the exact bounded semantics.
- [ ] 2.5 Run `openspec validate harden-restart-breaker-scope --strict`,
  formatting, and diff checks.
- [ ] 2.6 Prepare a reversible staged-break recipe and execute the permitted
  Go gate attempt; record the 57 GiB resource-floor block if it recurs.

## 1. Specification and implementation

- [x] 1.1 Record the approved per-target scope and exclusions in the plan,
  proposal, and design.
- [x] 1.2 Add the explicit project scope parameter and empty-scope guard to
  `reconcile.RunRestartBreaker`.
- [x] 1.3 Forward the existing target-specific scope from the daemon without
  changing target resolution, state paths, or named-target fallback behavior.

## 2. Verification and consumer coverage

- [x] 2.1 Keep the foreign-project container regression and verify the breaker
  does not inspect or stop it.
- [x] 2.2 Keep the two-project regression and verify each target retains its
  own restart state and only its own project can be stopped.
- [x] 2.3 Add unresolved implicit-target coverage proving no Docker inspection,
  stop, or restart-tracking advancement occurs when scope is empty.
- [x] 2.4 Update `docs/gitops.md` and
  `skills/onboard/resources/gitops.md` with the exact bounded semantics.
- [x] 2.5 Run `openspec validate harden-restart-breaker-scope --strict`,
  formatting, and diff checks.
- [x] 2.6 Prepare a reversible staged-break recipe and execute the permitted
  Go gate attempt; record the 52.3 GiB resource-floor block.
- [ ] 2.7 Execute the staged-break red/green test and record final CI results.

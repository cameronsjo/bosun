## 1. Specification and implementation

- [x] 1.1 Record the approved per-target scope and exclusions in the plan,
  proposal, and design.
- [x] 1.2 Add the explicit project scope parameter and empty-scope guard to
  `reconcile.RunRestartBreaker`.
- [x] 1.3 Forward the existing target-specific scope from the daemon without
  changing target resolution, state paths, or named-target fallback behavior.
- [x] 1.4 Add diagnostic-only startup and `bosun doctor` reporting for an
  enabled implicit default target with no `project_name`; demote the recurring
  cycle message to debug without changing the runtime guard.

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
  Go gate attempt; record the mandatory free-space resource-floor block.
- [x] 2.7 Execute guard-removal proof: CI38760065810 fails direct and daemon inspection assertions.
- [x] 2.8 Add startup and doctor seam tests for the unresolved implicit-default
  warning, explicit root project name, named-target `compose` fallback, and
  disabled breaker.
- [ ] 2.9 Execute the fix2 warning/check mutation red-green proof and record
  the restored-head CI result.
- [x] 2.10 Stop the root-level `project_name` from suppressing the startup and
  doctor warning, since it never scopes the breaker before a repository
  reload. Reproduce with a daemon test that runs the startup warning and a
  drift check against the same config (bosun#719).
- [x] 2.11 Require label ownership before a stop: the breaker re-checks each
  inspected container's Compose project and service labels, so an unlabeled
  container matched only by name prefix is never stopped, and the `compose`
  fallback also requires the working_dir label to equal the target's local
  compose directory, compared literally or after resolving symlinks. A trip
  stops the inspected container ID rather than the name (bosun#717).
- Final restored-head CI results and readiness: [PR #714](https://github.com/cameronsjo/bosun/pull/714).

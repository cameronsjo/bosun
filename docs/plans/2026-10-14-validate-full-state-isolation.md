---
date: 2026-10-14
issue: 674
status: implementation
---

# Isolate `validate --full` state writes

## Goal

Keep `bosun validate --full` a real reconciliation dry run without allowing
simulated success, attempt, breaker, or recovery state to alter the daemon's
configured deploy-state file.

## Scope

- Reuse `prepareStateFileForCLIRun` for the validation command's temporary
  state copy and clean it up on both success and failure.
- Exercise a successful render and a failed run against temporary local Git
  repositories and temporary paths only.
- Document the state-isolation contract in the CLI and onboarding command
  references.

## Non-goals

- Change `Reconciler.Run` dry-run semantics globally.
- Make `validate --full` consume project configuration or change its existing
  environment/config parity.

## Checklist

- [x] Route `runFullDryRun` through a state-isolating helper.
- [x] Add regression coverage for byte-preserved configured state and scratch
  cleanup after failure.
- [x] Update command and onboarding documentation.
- [ ] Run the repository's gated Go checks from the delivery worktree.

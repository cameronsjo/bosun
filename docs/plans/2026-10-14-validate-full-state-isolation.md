---
date: 2026-10-14
issue: 674
status: implemented
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
- [x] Attempt the repository's local gated Go checks and record the resource
  refusal; verify compiler, race tests, lint, and WebUI through PR CI.

## Verification

Codex's `git diff --check` and `gofmt -d internal/cmd/validate.go
internal/cmd/validate_state_test.go` passed. The local
`scripts/agent-go-gate.sh sh -c 'make build && go test -race ./... && go vet
./... && make lint'` exited 75 before compilation with 33.3 GiB free versus
the mandatory 100 GiB floor; no compiler or standalone vet ran locally.

PR #702 CI run `38625449736` passed Build, Test (full race/coverage suite),
Lint, and WebUI against feature commit
`41a1aa0c5333e13be044f0388e3cc67803ca2bf2`. Build and Lint initially failed
on module-download stream errors and a schema-download timeout, respectively;
Codex retried only the failed jobs, and both passed without code or workflow
changes. The Pages build also passed; deployment was correctly skipped for a
PR. CodeRabbit's success was a rate-limit skip, not a substantive review.

Final-head CI after this evidence/comment update is recorded in the sweep
report. No live daemon or NAS state was used in the regression tests.

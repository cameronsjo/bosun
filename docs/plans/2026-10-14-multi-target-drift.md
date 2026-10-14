# Multi-target drift and status (#347)

## Scope

Implement the approved R4 remainder of `add-multi-target-reconcile`: resolve
the target topology once at daemon startup, use that snapshot for reconciliation
and operator views, fan periodic drift and restart-breaker checks across local
targets, preserve remote cached state without querying local Docker, and expose
per-target deploy, drift, and breaker diagnostics through detailed status
surfaces while keeping public `/health` bounded and single-default output
compatible.

## Checklist

- [x] Capture and reuse the startup-resolved target snapshot.
- [x] Fan periodic drift and restart-breaker handling across local targets.
- [x] Mark remote periodic drift unavailable without local Docker access or
      remote state mutation.
- [x] Add ordered per-target diagnostics to detailed health, `/status`, and
      `/api/status` responses.
- [x] Show per-target deploy, drift, and breaker state in `bosun status`.
- [x] Update onboard configuration, GitOps, and command resources.
- [x] Add focused mutation-sensitive tests.
- [x] Prepare every effective target state directory at startup, preserve the
      base-state-file opt-out for periodic drift, and retain hot-reloaded
      Compose project names across reconcile, drift, and status consumers.
- [x] Keep the legacy `bosun status` dashboard for the implicit or lone
      `default` target, including when that target is remote.
- [x] Attempt the required local compiler gate and record its resource refusal.
- [x] Verify build, race-enabled tests, lint, and WebUI through PR CI, and run
      strict OpenSpec validation locally.
- [x] Reconcile the implementation checklist with CI evidence; the sweep
      report records the final delivery head and all check names.

## Verification

Local compiler-gated commands are required by `AGENTS.md`; the current machine
has reported the repository's 100 GiB free-space floor as unavailable. Use the
focused gate when resources permit, then rely on CI evidence for compiler,
race, vet, and lint results. Read-only formatting, `git diff --check`, and
strict OpenSpec validation remain local checks.

Codex attempted `scripts/agent-go-gate.sh sh -c 'make build && go test -race
./... && go vet ./... && make lint'`; it exited 75 before compilation with
34.0 GiB free versus the mandatory 100 GiB floor. After the CI fixture fix,
`scripts/agent-go-gate.sh go test -race ./internal/daemon` again exited 75
with 32.1 GiB free. No compiler command or standalone vet ran locally, and
Codex did not bypass the gate.

`git diff --check`, `gofmt -d` on the changed Go files, and
`openspec validate add-multi-target-reconcile --strict` passed locally.
PR #701 CI run `38626151350` passed Build, Test (full race/coverage suite),
Lint (including workflow and diagram contracts), and WebUI against feature
commit `6e1afd145112cdcf4104bdde70413a8f11ceb106`. The first Test run exposed
incomplete synthetic Docker inspect responses; Codex filled their required
network settings and re-ran CI. Independent review verified the fixes for
target isolation, state setup, namespace reload, and cached timestamps.

Final-head checks after this evidence-only update are recorded in the sweep
report. No live NAS deployment was performed.

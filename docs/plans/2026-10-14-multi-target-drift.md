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
- [ ] Run the required compiler, race, vet, lint, and strict validation gates
      (compiler execution remains pending external CI evidence).
- [ ] Reconcile this checklist with final CI evidence before delivery.

## Verification

Local compiler-gated commands are required by `AGENTS.md`; the current machine
has reported the repository's 100 GiB free-space floor as unavailable. Use the
focused gate when resources permit, then rely on CI evidence for compiler,
race, vet, and lint results. Read-only formatting, `git diff --check`, and
strict OpenSpec validation remain local checks.

The focused gate was first blocked locally with exit 64 because the shared lock
could not be created under `.git`. After that lock issue was fixed, the same
gate exited 75 before running Go because the wrapper measured 41.2 GiB free,
below its required 100 GiB floor. No compiler command ran:
`scripts/agent-go-gate.sh go test ./internal/daemon ./internal/cmd -run
'Test(DaemonRun_PreparesNestedStateFileForNamedTarget|RunDriftCheck_DisabledBaseStateSkipsNamedLocalAndRemoteTargets|RunDriftCheck_FanoutUsesIndependentLocalNamespacesAndStateFiles|ConfigForTarget_PreservesHotReloadedProjectName|HealthStatus_SingleNamedTargetIncludesTargetSubsystem|TargetHealthStatuses|RunDriftCheck_AllRemoteTargetsSkipDockerAndPreserveState|SocketStatus_DefaultShapeDoesNotPingDocker|APIStatus_IncludesOrderedTargetState)'`
No ungated compiler command was used.

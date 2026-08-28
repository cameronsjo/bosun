## Context

Bosun already resolves target descriptors from YAML or `BOSUN_TARGETS`, derives isolated configurations, and runs the existing reconciler once per target. The active design predated that implementation and incorrectly described a shared Git worktree phase, a process-wide CLI/daemon mutex, a separate daemon-owned target slice, cleanup of failed staging, and several API shapes that never shipped.

The canonical reconcile and alerting specs have also gained interruption, failed-staging, backup, health-gate, and image behavior since this change was proposed. Multi-target deltas must compose with those requirements rather than reproduce their whole blocks.

## Goals / Non-Goals

- **Goals:**
  - Describe shipped multi-target behavior precisely and additively.
  - Preserve backwards-compatible implicit and lone-default behavior.
  - Keep per-target state, staging, secrets, alert, and CLI contracts explicit.
  - Define a bounded, testable remainder for the genuine implementation gaps.
- **Non-Goals:**
  - Change runtime behavior in this proposal PR.
  - Parallelize targets or share Git/decrypt work across target attempts.
  - Introduce target-specific daemon triggers.
  - Replace canonical pipeline, interruption, backup, image, health-gate, failed-staging, locking, or alert-delivery semantics.

## Decisions

### Decision: Overlay each target onto the existing reconciler configuration

`ResolveTargets` returns effective descriptors. For each descriptor, `ConfigForTarget` copies the base configuration, applies target fields, derives named paths, and passes the result to the unchanged reconciler constructor. This matches the shipped architecture and keeps the complete canonical pipeline authoritative.

A separate target-aware constructor and a daemon-held `[]Target` were proposed but did not ship. They are superseded by this overlay model.

### Decision: Run the full canonical pipeline once per target

Targets execute sequentially in configuration order. Each target creates its own `Reconciler` and therefore performs the complete canonical pipeline, including Git sync and secret decryption. There is no once-per-cycle Git/decrypt phase, shared commit pin, or changed-file snapshot.

An ordinary target failure is accumulated and the next target starts while the shared cycle context remains live. A canceled or expired shared context stops later targets under the canonical non-live-context requirement. Failed staging follows the canonical failed-evidence lifecycle: verified success may clean up; dry-run and failure evidence is hardened and retained or securely deleted if hardening cannot be proven.

### Decision: Distinguish daemon admission from reconciler file locking

Daemon-owned triggers use the daemon's in-process admission and dirty-trigger coalescing, so one daemon cycle runs at a time and concurrent triggers collapse into a follow-up cycle. A separately invoked CLI is another process and cannot share that mutex. Direct reconciler overlap is governed by the canonical file-lock requirement; a second holder fails immediately.

Named targets receive derived lock paths through `ConfigForTarget`. This change does not promise blocking CLI/daemon coordination or a cross-process shared-worktree mutex.

### Decision: Preserve actual default and override semantics

No configured targets resolves to one implicit `default`. A lone target named `default`, case-insensitively, is accepted and normalized so its explicit fields configure the compatibility target. A multi-target set containing `default` is rejected. An empty or unsafe target name, a case-insensitive duplicate, staging overlap, a deploy-path collision, a Docker namespace collision, or a state-path collision rejects the complete effective target set. It must never be skipped in a way that permits a sibling or synthesized default target to run.

`BOSUN_TARGETS` is a JSON array. JSON `null` leaves project targets in effect; an explicit empty array is authoritative and resolves to the implicit default. YAML and environment descriptors expose the same shipped fields.

### Decision: Preserve target isolation without replacing canonical state behavior

Named targets derive independent state, staging, and lock paths unless an explicit override is supplied. The default target keeps legacy paths. State files use the canonical deploy-state schema and update rules; multi-target behavior adds only independent path selection and sibling isolation.

Staging paths must be pairwise disjoint. A target owns only its staging slot, and its failed/dry-run evidence remains available under the canonical retention and permission rules. A later target never cleans or overwrites a sibling's evidence.

### Decision: Keep alert context additive

Alert manager methods accept the target as a positional argument. An explicit named target, including one literally named `local`, receives a bracketed title suffix and target metadata/message context. The implicit or lone-default local compatibility target uses the empty legacy sentinel; a default remote deployment retains its existing host-derived context. The design does not add a `TargetName` field to every alert API and does not replace lifecycle alert requirements.

### Decision: Record the actual CLI surface

`bosun reconcile --target` and `bosun drift --target` filter a direct command to one effective target. Cached multi-target drift emits one JSON object with a `targets` array: `{"targets":[...]}`. A daemon trigger continues to request a complete cycle, so the proposed target-specific trigger flag is removed.

Remote named targets cannot be inspected correctly through the local Docker daemon. The remaining implementation must preflight the complete live selection and reject it before any Docker access or state mutation when any selected target is remote. Cached reporting remains available. When no project target configuration is discoverable, the existing single-target drift compatibility fallback may still derive the requested cached-state path; it must not execute an unrelated target.

### Decision: Finish scalar presence and operational visibility explicitly

Slice overrides already use presence semantics: nil inherits and an explicit empty slice clears, with independent backing storage after derivation and reload. Scalar decoding will preserve presence and implement the normative field rules in the reconcile delta: `name` is required; omitted `target_host`, appdata paths, `project_name`, and `secrets_scope` inherit while their explicit-empty values respectively select local, fail validation, select the Compose-derived namespace, and disable the scoped overlay; state/staging omission or empty uses the documented default/derived path and a non-empty value is an exact confined override.

The daemon currently reads its base state path for periodic drift, operator `/status`, WebUI `/api/status`, and circuit-breaker visibility. The remaining implementation will enumerate the daemon's startup-resolved target snapshot, expose per-target state through those operator status views, keep the public `/health` projection bounded by the daemon-security contract, and make `bosun status` expose each target without regressing the one-default presentation. Structural topology changes continue to require a daemon restart. Target context must also reach target-owned `Reconciler` log records from daemon and direct CLI runs, not only the daemon wrapper.

## Risks / Trade-offs

- **Presence-aware scalar decoding adds schema machinery.** It is required to prevent an omitted field and a deliberate empty value from collapsing into an unsafe inherited path or host.
- **Sequential full pipelines can observe different repository heads.** This is current behavior; commit pinning or a shared checkout phase would be a separate architectural change.
- **Per-target state visibility expands daemon consumers.** Central target/state enumeration should be shared so status, drift, health, and breaker views cannot diverge.
- **Remote live drift remains unavailable.** Failing before local Docker access is safer than reporting the wrong host; remote Docker transport is separate scope.

## Migration Plan

1. Merge this grounding proposal and update issue #438 with the exact bounded remainder.
2. Implement the unchecked task groups with focused compatibility and mutation-sensitive tests.
3. Update onboard configuration, GitOps, and command resources with the final behavior.
4. Strict-validate and archive the change only after every remaining task is implemented and verified.

Rollback of the eventual runtime work is scoped by feature: users can remove `targets:` to return to the implicit default, and each implementation slice must preserve existing state files and canonical single-target behavior.

## Open Questions

None. Target-specific daemon triggers, parallelism, shared cycle-level Git/decrypt stages, and remote Docker transport are explicitly outside this change.

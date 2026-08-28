## Grounded implementation ledger

This ledger preserves the original task identifiers while classifying them against current `main`. Completed boxes mean the delivered behavior was verified in code, tests, documentation, or the superseding implementation described inline. Partial boxes remain open and point to the bounded remainder below.

Summary: **32 shipped, 9 partially shipped, 6 superseded, 1 genuinely missing, and 1 invalid item removed from scope.**

## 1. Foundation — target type and configuration parsing

- [x] 1.1 **SHIPPED:** Define the public target descriptor with the YAML/JSON per-target fields used by reconciliation (`internal/reconcile/target.go`; PR #158).
- [x] 1.2 **SHIPPED:** Store descriptors on `reconcile.Config.Targets`; project configuration exposes a cloned accessor and `ResolveTargets` materializes the effective list (PR #158).
- [x] 1.3 **SHIPPED:** Parse the `targets:` list from `bosun.yaml` (PR #158).
- [x] 1.4 **SHIPPED:** Convert YAML target entries to independent reconcile target descriptors (PR #158).
- [x] 1.5 **SHIPPED:** Resolve an absent or effectively empty target set to one implicit `default` target (PR #158).
- [x] 1.6 **SHIPPED:** Parse the `BOSUN_TARGETS` JSON-array override; `null` retains project targets and `[]` is authoritative (PR #158).
- [ ] 1.7 **PARTIAL:** The mixed-config warning ships for `targets:` plus flat `target_host`; finish the documented scalar presence/inheritance contract in R2 instead of claiming every flat field is ignored.
- [x] 1.8 **SHIPPED:** Cover YAML targets, implicit-default compatibility, and environment override parsing (PRs #158 and #160).

## 2. Per-target isolation — staging, state, and locking

- [x] 2.1 **SHIPPED:** Derive a named target staging slot below the configured staging root unless an explicit override is supplied (PR #158).
- [x] 2.2 **SHIPPED:** Derive `deploy-state-<name>.json` for named targets while preserving the default state path (PR #158).
- [x] 2.3 **SHIPPED:** Derive `reconcile-<name>.lock` for named targets while preserving the default lock path (PR #158).
- [x] 2.4 **SUPERSEDED:** Instead of changing `New` to accept a target, `ConfigForTarget(target)` overlays a copied config and the existing constructor remains unchanged (PR #158).
- [x] 2.5 **SHIPPED:** Cover default, named, and explicit target path derivation (PRs #158 and #160).

## 3. Sequential orchestration and admission

- [x] 3.1 **SUPERSEDED:** The daemon retains its reconcile configuration and resolves targets per cycle rather than holding a separate `[]Target` (PR #158).
- [x] 3.2 **SHIPPED:** Iterate effective targets in order, build `ConfigForTarget`, and run a complete reconciler pipeline for each (PR #158).
- [x] 3.3 **SHIPPED:** Accumulate ordinary target failures and continue while the shared context is live; stop later targets when it is canceled or expired (PRs #158 and #626).
- [ ] 3.4 **PARTIAL:** The daemon's outer target log records carry target context; propagate that context through every target-owned `Reconciler` log record in R3.
- [x] 3.5 **SUPERSEDED:** Daemon-owned entry points are single-flight and dirty-trigger coalesced; the original process-wide CLI/daemon mutex is replaced by canonical fail-fast file locking for separate processes (PRs #158 and #160).
- [x] 3.6 **SHIPPED:** Cover continuation to a sibling after an ordinary target failure while context remains live (PRs #160 and #626).
- [x] 3.7 **SHIPPED:** Cover daemon dirty-trigger coalescing during an active cycle (PR #160).

## 4. Secrets scoping

- [x] 4.1 **SHIPPED:** Merge `targets.<scope>` values over shared decrypted values for the active target (PR #158).
- [x] 4.2 **SHIPPED:** Render each target with its scoped secret map (PR #158).
- [x] 4.3 **SHIPPED:** Log scoped override keys at debug level without logging secret values (PR #158).
- [x] 4.4 **SHIPPED:** Cover shared-only, scoped override, and no-scope behavior (PRs #158 and #160).

## 5. Named-target alert context

- [x] 5.1 **SUPERSEDED:** Alert helpers accept a positional target identifier rather than adding a `TargetName` field to every signature (PR #158).
- [ ] 5.2 **PARTIAL:** Named-target context ships for deploy success, failure, recovery, and rollback alerts, but an explicit target literally named `local` is currently mistaken for the legacy local-default sentinel; finish in R3 (PR #158).
- [ ] 5.3 **PARTIAL:** Named-target context ships for drift and breaker alerts with the same `local` ambiguity; finish in R3 (PRs #158 and #503).
- [ ] 5.4 **PARTIAL:** Existing tests cover ordinary title suffixes and target context; add explicit named-`local`, local-default, and remote-default cases in R6 (PRs #158 and #160).

## 6. CLI and daemon visibility

- [x] 6.1 **SHIPPED:** Filter direct `bosun reconcile` execution with `--target` (PR #158).
- [x] 6.2 **SHIPPED:** Filter `bosun drift` with `--target` and aggregate cached results for all effective targets (PRs #158 and #503).
- [ ] 6.3 **MISSING:** Implement per-target daemon periodic drift, operator-status, and circuit-breaker visibility plus `bosun status`, while preserving the bounded public `/health` contract, in R4.

The original 6.4 target-specific `bosun trigger` task is removed as invalid scope. A daemon trigger requests a complete reconciliation cycle; direct per-target work uses the reconcile and drift filters.

- [ ] 6.5 **PARTIAL:** Reconcile/drift filtering tests ship; add configured-unknown consumer parity, mixed-selection remote live-drift preflight, no-config cached compatibility, and exact aggregate JSON coverage in R5.

## 7. Configuration reload

- [x] 7.1 **SHIPPED:** Reload target-owned fields through the effective per-target configuration rather than returning a separate target config type (PRs #158 and #506).
- [x] 7.2 **SHIPPED:** Preserve environment-owned target and override fields during file reload (PRs #158, #506, and #510).
- [x] 7.3 **SHIPPED:** Cover multiple targets, inheritance, clearing, and slice independence during reload (PRs #160, #506, and #510).

## 8. Consumer documentation

- [x] 8.1 **SHIPPED:** Document direct CLI target filters in `docs/commands.md` (PR #167).
- [x] 8.2 **SHIPPED:** Document sequential multi-target reconciliation in the onboard GitOps resource (PR #167).
- [x] 8.3 **SHIPPED:** Document the target schema and full example in the onboard configuration resource (PR #167).
- [x] 8.4 **SUPERSEDED:** The onboard configuration resource is the primary consumer document and already contains the complete example; duplicating it in README is unnecessary (PR #167).

## 9. Target validation and safety

- [ ] 9.1 **PARTIAL:** Collision checks ship, but unsafe/duplicate descriptors are skipped instead of rejecting the whole effective set; restore fail-closed descriptor validation and finish explicit state/staging and local deploy-root confinement for YAML and `BOSUN_TARGETS` in R1.
- [x] 9.2 **SUPERSEDED:** A lone case-insensitive `default` is accepted and normalized for compatibility; `default` in a multi-target set is rejected (PR #407).
- [x] 9.3 **SHIPPED:** Reject target state collisions and equal or ancestor/descendant staging paths before execution (PRs #313 and #528).
- [x] 9.4 **SHIPPED:** Reject colliding Docker namespaces and deploy destinations (PRs #313 and #528).
- [x] 9.5 **SHIPPED:** Keep YAML/environment target fields in parity and resolve an authoritative empty environment array to the implicit default (PRs #510 and #526).
- [x] 9.6 **SHIPPED:** Deep-copy target-owned and inherited slice fields during derivation (PRs #313 and #510).
- [x] 9.7 **SHIPPED:** Deep-copy caller-owned slices during hot reload (PR #506).
- [ ] 9.8 **PARTIAL:** Host and slice rules ship, but scalar path/project fields collapse omission and explicit empty values; finish presence-aware decoding and the field-by-field inheritance contract in R2.
- [ ] 9.9 **PARTIAL:** Collision, deep-copy, parity, and default tests ship; add the remaining confinement and scalar-presence mutation tests in R1, R2, and R6.

## Bounded remaining implementation

- [ ] R1. Reject the complete effective set for an empty/unsafe name or case-insensitive duplicate. Confine explicit named-target `state_file` and `staging_dir` values to their configured roots and local appdata/deploy values to their permitted local root. Apply identical checks to YAML and `BOSUN_TARGETS`; never skip an invalid descriptor or run a sibling/default before validation succeeds.
- [ ] R2. Preserve scalar presence during YAML and JSON decoding and implement the field-by-field normative rules: required `name`; omitted host/path/project/scope inheritance; explicit-empty host selects local, appdata path is invalid, project selects Compose derivation, and scope disables overlay; empty state/staging selects the documented derived or legacy path; non-empty overrides remain confined.
- [ ] R3. Propagate normalized target context into every target-owned `Reconciler` structured log from daemon and direct CLI runs. Distinguish the legacy local-default alert sentinel from an explicit target literally named `local`, while preserving remote-default host context and canonical alert behavior.
- [ ] R4. Centralize startup-resolved target-state enumeration for daemon periodic drift, operator `/status`, and circuit-breaker diagnostics; make `bosun status` show every target, keep remote periodic drift from querying local Docker, preserve one-default compatibility, require restart for structural topology changes, and leave public `/health` bounded by daemon security.
- [ ] R5. Preflight the complete live-drift selection and reject it before any Docker access or state mutation when any selected target is remote. Preserve cached target drift, configured-unknown filtering, the no-config cached compatibility fallback, and aggregate JSON as `{"targets":[...]}`.
- [ ] R6. Add mutation-sensitive tests for R1-R5, including fail-whole-set invalid descriptors/confinement, omission versus explicit empty values, named-`local` alert attribution, daemon and CLI log attribution, untouched sibling state, mixed local/remote live-drift rejection, periodic remote handling, exact JSON/status/public-health shapes, restart-bound topology, and single-default compatibility.
- [ ] R7. Update onboard configuration, GitOps, and command resources after behavior lands; run focused/full Go gates and strict OpenSpec validation, then archive this change without `--skip-specs`.

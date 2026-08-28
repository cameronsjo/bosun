# Grounded implementation ledger

This ledger preserves the original task identifiers while classifying them against current `main`. Completed boxes mean the delivered behavior was verified in code, tests, documentation, or the superseding implementation described inline. Partial boxes remain open and point to the bounded remainder below.

Summary: **35 shipped, 7 partially shipped, 5 superseded, 1 genuinely missing, and 1 invalid item removed from scope.**

## 1. Foundation — target type and configuration parsing

- [x] 1.1 **SHIPPED:** Define the public target descriptor with the YAML/JSON per-target fields used by reconciliation (`internal/reconcile/target.go`; PR #158).
- [x] 1.2 **SHIPPED:** Store target descriptors on the base reconciliation configuration through its target accessors and resolver rather than an exported mutable slice (PR #158).
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
- [ ] 3.5 **PARTIAL:** Daemon-owned entry points are single-flight and dirty-trigger coalesced; finish the spec/test boundary in R3 without claiming a cross-process CLI mutex. Direct overlap remains governed by canonical file locking.
- [x] 3.6 **SHIPPED:** Cover continuation to a sibling after an ordinary target failure while context remains live (PRs #160 and #626).
- [x] 3.7 **SHIPPED:** Cover daemon dirty-trigger coalescing during an active cycle (PR #160).

## 4. Secrets scoping

- [x] 4.1 **SHIPPED:** Merge `targets.<scope>` values over shared decrypted values for the active target (PR #158).
- [x] 4.2 **SHIPPED:** Render each target with its scoped secret map (PR #158).
- [x] 4.3 **SHIPPED:** Log scoped override keys at debug level without logging secret values (PR #158).
- [x] 4.4 **SHIPPED:** Cover shared-only, scoped override, and no-scope behavior (PRs #158 and #160).

## 5. Named-target alert context

- [x] 5.1 **SUPERSEDED:** Alert helpers accept a positional target identifier rather than adding a `TargetName` field to every signature (PR #158).
- [x] 5.2 **SHIPPED:** Include named-target context in deploy success, failure, recovery, and rollback alerts while preserving legacy empty/`local` titles (PR #158).
- [x] 5.3 **SHIPPED:** Include named-target context in drift and breaker alerts (PRs #158 and #503).
- [x] 5.4 **SHIPPED:** Cover title suffixes and target context across lifecycle alerts (PRs #158 and #160).

## 6. CLI and daemon visibility

- [x] 6.1 **SHIPPED:** Filter direct `bosun reconcile` execution with `--target` (PR #158).
- [x] 6.2 **SHIPPED:** Filter `bosun drift` with `--target` and aggregate cached results for all effective targets (PRs #158 and #503).
- [ ] 6.3 **MISSING:** Implement per-target daemon state visibility and `bosun status` output for last deploy, drift, health, and circuit-breaker state in R4.

The original 6.4 target-specific `bosun trigger` task is removed as invalid scope. A daemon trigger requests a complete reconciliation cycle; direct per-target work uses the reconcile and drift filters.

- [ ] 6.5 **PARTIAL:** Reconcile/drift filtering tests ship; add remote named-target live-drift fail-safe and exact aggregate JSON coverage in R5.

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

- [ ] 9.1 **PARTIAL:** Name, remote-path, staging-overlap, state collision, Docker namespace, and deploy collision validation ship; finish explicit state/staging and local deploy-root confinement for YAML and `BOSUN_TARGETS` in R1.
- [x] 9.2 **SUPERSEDED:** A lone case-insensitive `default` is accepted and normalized for compatibility; `default` in a multi-target set is rejected (PR #407).
- [x] 9.3 **SHIPPED:** Reject target state collisions and equal or ancestor/descendant staging paths before execution (PRs #313 and #528).
- [x] 9.4 **SHIPPED:** Reject colliding Docker namespaces and deploy destinations (PRs #313 and #528).
- [x] 9.5 **SHIPPED:** Keep YAML/environment target fields in parity and resolve an authoritative empty environment array to the implicit default (PRs #510 and #526).
- [x] 9.6 **SHIPPED:** Deep-copy target-owned and inherited slice fields during derivation (PRs #313 and #510).
- [x] 9.7 **SHIPPED:** Deep-copy caller-owned slices during hot reload (PR #506).
- [ ] 9.8 **PARTIAL:** Host and slice rules ship, but scalar path/project fields collapse omission and explicit empty values; finish presence-aware decoding and the field-by-field inheritance contract in R2.
- [ ] 9.9 **PARTIAL:** Collision, deep-copy, parity, and default tests ship; add the remaining confinement and scalar-presence mutation tests in R1, R2, and R6.

## Bounded remaining implementation

- [ ] R1. Confine explicit named-target `state_file` and `staging_dir` values to their configured roots and local appdata/deploy values to their permitted local root. Apply identical checks to YAML and `BOSUN_TARGETS`; reject the whole target set before any reconcile work.
- [ ] R2. Preserve scalar presence during YAML and JSON decoding, document each scalar's omitted and explicit-empty rule, keep `target_host: ""` local, and prevent an omitted or cleared path/project field from silently selecting an unintended target.
- [ ] R3. Propagate target context into the reconciler logger and pin the true concurrency boundary: daemon entry points coalesce in process, while separate processes use canonical fail-fast file locking.
- [ ] R4. Centralize effective target-state enumeration for daemon periodic drift, health/status, and circuit-breaker views; make `bosun status` show every target while preserving the one-default presentation and state compatibility.
- [ ] R5. Reject `bosun drift --live` for a remote named target before local Docker access. Preserve cached target drift, filtering, and aggregate JSON as `{"targets":[...]}`.
- [ ] R6. Add mutation-sensitive tests for R1-R5, including no partial execution on confinement failure, omission versus explicit empty values, sibling log attribution, untouched sibling state, remote live-drift rejection, exact JSON shape, and single-default compatibility.
- [ ] R7. Update onboard configuration, GitOps, and command resources after behavior lands; run focused/full Go gates and strict OpenSpec validation, then archive this change without `--skip-specs`.

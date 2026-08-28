# Change: Ground and finish multi-target reconciliation

## Why

Multi-target reconciliation is substantially implemented, but this active change still describes the pre-implementation architecture and leaves all original tasks open. Archiving it as written would replace newer canonical reconcile and alerting requirements with stale pipeline text, including incorrect once-per-cycle Git and secret stages, obsolete locking claims, and failed-staging cleanup behavior that no longer matches the product.

This change now records the shipped behavior as orthogonal requirements and narrows the remaining work to the gaps confirmed against current code, tests, documentation, and merged pull requests.

## Grounding Evidence

- The approved spec foundation shipped through PR #152 (`23b8c23`) and the core implementation through PR #158 (`3fdb569`).
- Follow-up tests and consumer documentation shipped through PRs #160 (`60800aa`) and #167 (`1f0ba50`).
- Target-safety requirements were approved in PR #313 (`461d7a4`).
- Later fixes established case-insensitive default handling, named-target drift state, reload slice isolation, explicit empty slice overrides, YAML/environment parity, and collision rejection through PRs #407, #503, #506, #510, #526, and #528.
- Canonical interruption and failed-staging behavior subsequently landed through PRs #626 (`5e4e517`) and #631 (`f7d4546`), so this change must compose with those blocks rather than replace them.
- The 49-item implementation ledger now classifies 32 items as shipped, 9 as partially shipped, 6 as superseded by the implementation that landed, one as genuinely missing, and one target-specific trigger item as invalid scope.

## What Changes

- Replace stale whole-block modifications with orthogonal added requirements for configuration, staging, state, sequential orchestration, admission, secrets, alerts, CLI filtering, drift, validation, configuration independence, and daemon state visibility.
- Record the actual architecture: each effective target receives an independent `ConfigForTarget` copy and runs the complete canonical reconciliation pipeline through the existing `Reconciler`.
- Record the actual multi-target JSON shape as `{"targets":[...]}`, the case-insensitive lone `default` compatibility rule, daemon-owned trigger coalescing, canonical file locking, and secure failed-staging retention.
- Preserve the bounded implementation remainder: fail-closed target-set validation, explicit state/staging and local deploy-root confinement, scalar presence/inheritance semantics, unambiguous named-target alert and log context, remote live-drift fail-safe behavior, and per-target daemon/operator state visibility.
- Remove target-specific daemon trigger scope. Daemon triggers continue to request a complete cycle; per-target execution remains available through the direct reconcile and drift CLI filters.
- Ground the campaign ledger without changing runtime code, consumer documentation, or issue #438 in this PR.

## Impact

- Affected specs: `reconcile` and `alerting`, through additive multi-target requirements only.
- Affected active change files: this proposal, design, task ledger, and the two delta specs.
- Affected campaign file: section 4's first two checklist items in `docs/plans/2026-08-28-openspec-backlog-campaign.md`.
- Future implementation consumers:
  - `internal/reconcile/target.go`, `reconcile.go`, `state.go`, `config_reload.go`, and logging call sites
  - `internal/daemon/daemon.go` and daemon status/periodic drift consumers
  - `internal/cmd/drift.go` and `internal/cmd/diagnostics.go`
  - `internal/config/config.go` and target presence-aware decoding
  - `skills/onboard/resources/configuration.md`, `gitops.md`, and `commands.md`
- No production behavior, public documentation, release metadata, or issue text changes in this grounding PR.

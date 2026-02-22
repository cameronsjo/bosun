# Baseline Specs and Architectural Ceilings — Field Report

**Date:** 2026-02-21
**Type:** architecture
**Project:** bosun

## Goal

Reverse-engineer formal OpenSpec specifications from the existing Bosun codebase, then compare Bosun's architecture against Argo CD and Flux CD to identify structural constraints ("ceilings") that limit growth.

## Architecture

### Baseline Spec Extraction

We wrote 6 specs by reading source code and incorporating 4 previously-completed change deltas:

| Spec | Requirements | Scenarios | Source |
|------|:-----------:|:---------:|--------|
| reconcile | 20 | 51 | `internal/reconcile/` + 3 change deltas |
| manifest-system | 14 | 69 | `internal/manifest/` + helm-chart-format delta |
| alerting | 8 | 22 | `internal/alert/` |
| observability | 6 | — | `internal/ui/`, logging patterns |
| chart-templating | 6 | 23 | `internal/manifest/chart*.go` |
| chart-migration | 4 | — | `internal/manifest/migrate*.go` |

Total: 58 requirements, 165+ scenarios across 6 specs.

After writing, 4 completed changes were archived:
- `add-structured-logging`
- `add-helm-chart-format`
- `add-state-feedback-loop`
- `add-reconcile-state-tracking`

### GitOps Comparison

Compared Bosun against Argo CD and Flux CD across 12 capability areas. The comparison went through three iterations:

1. **Feature matrix** — raw capability comparison across 10 areas
2. **Tool-level pros/cons** — 10-11 items per tool with decision matrix
3. **Architectural approach comparison** — the real value. Compared 6 fundamental design choices:
   - Reconciliation (daemon vs API server vs decoupled controllers)
   - State persistence (JSON file vs CRDs)
   - Drift detection (periodic inspection vs real-time watch vs re-application)
   - Secret management (pipeline vs plugin vs controller-native)
   - Notification architecture (in-process vs bundled vs dedicated controller)
   - Deploy mechanism (rsync vs K8s API apply)

### Ceiling Analysis

Three parallel research agents analyzed the codebase for structural constraints. Found 23 raw ceilings, deduplicated to 14 beads:

| Priority | Count | Examples |
|:--------:|:-----:|---------|
| P2 | 4 | Single-target reconciler, hardcoded output targets, GitHub-only webhooks, Docker-local drift |
| P3 | 6 | Single-repo daemon, no config hot-reload, frozen alert system, shallow clone breaks DiffFiles |
| P4 | 4 | Mutually exclusive format paths, filesystem-only provision lookup, grace period blocks lock |

**Key pattern**: Most P2 ceilings stem from the same root cause — Bosun was designed as a single-yacht tool. Lifting them means evolving from personal homelab tool to fleet management.

## Decisions Made

1. **Specs include chart content in both manifest-system AND dedicated chart specs** — accepted the overlap since the archived helm-chart-format change targeted all three paths. Future cleanup can deduplicate.

2. **Ceilings are beads, not OpenSpec changes** — ceilings describe structural constraints, not proposed solutions. Each one needs its own design work before becoming a change proposal.

3. **Comparison doc lives at `docs/gitops-comparison.md`** — a living reference, not an ADR. Will evolve as Bosun's architecture changes.

## Gotchas

- **OpenSpec strict validation requires `## Purpose` and `## Requirements` headers** — two specs (alerting, observability) failed validation because agents used `## Context` instead. The other 4 specs passed because their agents happened to use the right headers.

- **Race condition with parallel agents** — the manifest-spec-writer agent continued working after receiving a shutdown request, picking up Wave 2 tasks that were also assigned to dedicated agents. Both wrote to the same files. Last-write-wins resolved it, but this is a real coordination gap with multi-agent workflows.

- **PostHog telemetry noise** — every `openspec` CLI command produces verbose PostHog errors. Harmless but noisy. Filtered with `grep -v "PostHog"`.

## Key Takeaways

- The 6 baseline specs now serve as ground truth for what Bosun does today — any future change proposal diffs against these
- Bosun's architecture is sound for its design envelope (single yacht, single repo, homelab scale) — the ceilings are edges of that envelope, not bugs
- The architectural approach comparison (not the feature matrix) was the most valuable output — it shows *why* each tool made its choices, not just what it can do
- Multi-agent spec writing works but needs better task claiming/locking to prevent race conditions
- The 14 ceiling beads create a prioritized backlog for architectural evolution when/if Bosun outgrows homelab scale

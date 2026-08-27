# Handoff: bosun architecture diagrams

**Written:** 2026-08-26 · **Repo:** `cameronsjo/bosun` · **Branch to start from:** `main`

## The ask

Redraw bosun's architecture diagrams as editorial diagrams using the
`diagram-design` plugin (installed 2026-08-26 from `cathrynlavery/diagram-design`).
Four Mermaid sources exist today; `diagram-design:import-mermaid` is the entry point.

## Current state

Four `.mmd` files on `main`, rendered by `scripts/diagrams/render.mjs` (pnpm):

| File | Covers |
|---|---|
| `docs/diagrams/reconcile-pipeline.mmd` | 16-step reconcile pipeline, lock to unlock |
| `docs/diagrams/locking-singleflight.mmd` | flock + dirty-flag coalescing |
| `docs/diagrams/architecture.mmd` | package/component layout |
| `docs/diagrams/pipeline-overview.mmd` | high-level GitOps flow |

## The one real finding — resolve this first

`docs/436-refresh-diagrams` (pushed, no PR) and `main` hold **two different
versions of `reconcile-pipeline.mmd`, and neither contains the other's nodes.**

- `main`'s version has a `DeployGate` decision node, "Fail Closed if Missing",
  "Verify Writes / Transfer", and a 14-step ordering.
- The branch's version has "Deploy Sync Invariant Check", "Circuit Breaker",
  and a different 15-step ordering.

PR #563 (`docs: refresh reconcile and locking diagrams`) merged 2026-08-24 and is
the branch's own PR — so the branch is *stale*, not orphaned, and `main` moved
on afterward. **But the divergence is not a plain fast-forward**, so do not
assume `main` is simply newer. Read `internal/reconcile/reconcile.go`'s `Run()`
and settle which ordering matches the code before redrawing anything. Redrawing
the wrong one produces a beautiful diagram of a pipeline that does not exist.

The other three `.mmd` files were byte-identical across branch and `main` — they
carry no such ambiguity.

## Suggested sequence

1. Read `Run()` in `internal/reconcile/reconcile.go`; write down the actual step order.
2. Pick the correct `reconcile-pipeline.mmd` (or synthesize a third that matches code).
3. Run `diagram-design:doctor` to confirm the plugin's environment is ready.
4. `diagram-design:import-mermaid` per file, choosing format/size/detail.
5. Export via `diagram-design:export-diagram` (writes `.svg` + `.png` beside the source).
6. Decide whether the editorial versions *replace* the Mermaid sources or sit
   alongside them — `scripts/diagrams/render.mjs` and any docs embedding the
   rendered output need updating either way.

## Loose ends

- `docs/436-refresh-diagrams` is pushed but has no PR. Delete it once step 2
  settles which version wins — it holds nothing else.
- `README.md` also differs between that branch and `main` (165+/176-), unrelated
  to diagrams and almost certainly just drift. Ignore it; do not merge the branch.

## Context from the session that wrote this

A branch sweep on 2026-08-26 cleared 103 remote and 71 local branches, all
verified landed. Five survived; this diagram question is the only one that
turned out to be a real open design question rather than stale cruft.

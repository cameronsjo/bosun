# ADR-0014: Docs Site on Raw Astro, Diagrams in Two Layers

## Status

**Status: Draft** — TODO: set to Proposed or Accepted once reviewed.

## Context

Bosun's GitHub Pages surface was live but orphaned, still serving a retired
dashboard artifact from a deleted workflow. The four architecture diagrams
existed only as Mermaid sources rendered to ASCII art in the README, which
reads well in a terminal and poorly anywhere else.

Two questions had to be settled together: what publishes the docs, and what a
diagram *is* in this repo now that it has two audiences — humans reading a
styled page, and agents or terminal users reading plain text.

## Decision

**Raw Astro** (not Starlight) builds a static site under `site/`, deployed by
`.github/workflows/pages.yml`. Content comes from an explicit six-document
allowlist plus `docs/adr/*`, copied into the content collection at build time
by `site/scripts/sync-docs.mjs` — never a directory glob over `docs/`.

**Diagrams ship in two layers.** `docs/diagrams/*.mmd` plus the README ASCII
render stay the bot and terminal layer. `docs/diagrams/*.{html,svg}` are a
committed editorial layer, redrawn through the diagram-design skill under a
`bosun` profile derived from the webui's Maritime Command Center tokens. The
editorial layer does not replace Mermaid; both are maintained.

`make diagrams-check` binds the layers: each export embeds its source `.mmd`
SHA-256, and the check fails when a source changes without a re-export or when
an exported SVG carries active content.

## Consequences

### Pros

- One design system: the webui's tokens skin the site and the diagrams, with no
  third palette to keep in sync.
- Full theming control with no runtime dependency — fonts are self-hosted and
  the built site makes no third-party requests.
- The allowlist makes the site's content surface explicit and reviewable; a doc
  reaches the public site only by being named.
- The staleness hash makes a drifted diagram a red CI check rather than a thing
  someone notices months later.

### Cons

- Every diagram change is now two artifacts, not one: the `.mmd` and its export.
- Hand-rolled navigation and layout are ours to maintain, where Starlight would
  have supplied them.
- The editorial SVGs are inlined into the site via `set:html`, so they need the
  active-content lint that `make diagrams-check` performs.

## Alternatives Considered

| Alternative | Why not |
|-------------|---------|
| MkDocs Material / Docusaurus | Recognizable off-the-shelf theme; wanted full theming control and zero runtime |
| Starlight | Same recognizable-theme objection; the site is small enough that hand-rolled nav is cheap |
| Editorial diagrams replace Mermaid | Would strip the terminal and agent audience of a readable source |
| Icon-prompt / mascot-style-guide palettes | The webui tokens are the one already-implemented system; a third palette would drift |

## Consequences for contributors

TODO: confirm whether the dual-layer diagram contract is permanent policy or
provisional pending how the maintenance cost of two artifacts actually feels.

## References

- `docs/plans/2026-08-26-pages-and-editorial-diagrams.md` — plan and panel review
- `AGENTS.md` § Pages Site and Diagrams — the operational contract
- `docs/styles/diagram-profile.md` — the `bosun` diagram profile

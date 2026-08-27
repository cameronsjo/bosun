# README Refresh and Mascot Pipeline — Field Report

**Date:** 2026-02-21
**Type:** pipeline
**Project:** bosun

## Goal

Modernize the Bosun README with rendered diagrams and set up an image generation pipeline for a project mascot, architecture diagram, and hero banner.

## Pipeline Overview

Three pipelines were established in this session:

### 1. Mermaid Diagram Pipeline

```text
Write .mmd source → Render with mmdc (dark theme, transparent bg) → SVG in docs/diagrams/
```

Created 3 diagrams:
- **pipeline-overview** — top-level git push → deploy flow (LR)
- **architecture** — full system with yacht/bosun/crew subgraphs (TB)
- **reconcile-pipeline** — reconcile with state-skip, circuit breaker, drift verify (LR)

The README embeds raw Mermaid code blocks (GitHub renders natively), while the SVGs serve non-Mermaid contexts (slides, exports, Obsidian PDFs).

### 2. Mascot Identity Pipeline

```text
Concept → definition.md (identity + consistency anchors) → style file (bg-removal optimized) → prompt files
```

Designed a chibi-proportioned bosun officer character:
- Navy peacoat, brass buttons, silver whistle with amber glow
- Pixar-adjacent 3D render style
- 4 consistency anchors for multi-image coherence
- Style file optimized for rembg background removal (solid black bg, dual rim lighting, clean silhouette edges)

### 3. Transparent PNG Pipeline

```text
Prompt → Gemini 3 Pro Image → rembg (Docker) → Transparent PNG
```

Infrastructure created:
- `scripts/Dockerfile.rembg` — Python 3.11-slim with pre-baked U2Net model
- `scripts/remove-bg.sh` — Docker-based background removal wrapper
- 3 prompt files ready for generation (blocked on Gemini API service)

## What Worked

- **Mermaid `<br/>` for line breaks** — `\n` renders as literal text in node labels. Caught and fixed after first render.
- **Dark theme + transparent background** — `mmdc -t dark -b transparent` produces SVGs that work on both light and dark GitHub themes.
- **Mascot consistency anchors** — the 4-anchor pattern from the transparent-png-pipeline skill gives enough detail to maintain character identity across prompts without being so rigid that every image looks identical.

## What Didn't Work

- **`mmd-render` (beautiful-mermaid)** — not installed, and requires bun which isn't available. Fell back to `mmdc` via npx which worked fine.
- **`\n` in Mermaid node labels** — Mermaid uses HTML-style `<br/>` tags, not escape sequences. The `.mmd` source file had `\n` which rendered as literal backslash-n in the SVG.

## Setup / Reproduction

### Render Mermaid diagrams

> **Superseded — do not run this command.** `docs/diagrams/*.svg` are now
> committed editorial exports produced by the diagram-design skill, not by
> mmdc. Writing mmdc output to those paths clobbers them and fails
> `make diagrams-check`. Current pipeline: `make diagrams` for the README
> ASCII, `node scripts/diagrams/export-svg.mjs` for the SVGs (see
> AGENTS.md § Pages Site and Diagrams). Kept for the historical record.

```bash
npx -y @mermaid-js/mermaid-cli -i docs/diagrams/architecture.mmd \
  -o docs/diagrams/architecture.svg -t dark -b transparent
```

### Build rembg Docker image

```bash
docker build -t rembg -f scripts/Dockerfile.rembg scripts/
```

### Remove background from generated image

```bash
./scripts/remove-bg.sh output/bosun-reference.png
# Produces output/bosun-reference-nobg.png
```

### Generate images (when Gemini service is available)

```bash
/generate-image docs/prompts/bosun-reference.md
/generate-image docs/prompts/architecture-diagram.md
/generate-image docs/prompts/hero-image.md
```

## Decisions Made

1. **CodeRabbit for AI PR review** — installed as a GitHub App (free for public repos). Complements the existing Claude Code Review Action. No config file needed, auto-reviews all PRs.

2. **Skip Qodo PR-Agent** — requires an LLM API key as a GitHub secret. Will revisit when the Unraid Gemini service is running.

3. **Gemini image gen deferred to Unraid service** — no local API key, user wants to centralize image generation as a self-hosted service. 3 prompt files written and 3 beads created (blocked on `bosun-a7r`).

4. **Mascot files under `docs/`** — `docs/mascot/`, `docs/styles/`, `docs/prompts/` keep the image generation artifacts with the documentation rather than cluttering the project root.

## Key Takeaways

- README Mermaid code blocks are zero-maintenance (GitHub renders them live) — SVG exports are for everywhere else
- The mascot identity (definition.md + style + consistency anchors) is the reusable asset — individual prompt files are cheap to write once the identity is locked
- CodeRabbit + Claude Code Review gives two complementary AI reviewers at zero cost on public repos
- Image generation blocked on infrastructure, not creative direction — all 3 prompts are ready to go
- `\n` doesn't work in Mermaid node labels — always use `<br/>`

---
body_sha256: "9849aaa3dfd190035aec00dc5a677c1ea36a7202fad22bb9b0fa9e8abfd2d775"
session: "calm-quill"
session_id: "d176ea4c-c463-4251-afcb-e497a611d682"
machine: "cf6e768835c7"
approved_in: "cedar-garden"
approved_session_id: "6e388b76-ba53-4006-8fd9-cfb177538e66"
status: planned
next: "Approve plan → worktree entry (Task 1) → diagram residuals (Task 2)"
branch: plan/pages-and-editorial-diagrams
pr: "—"
updated: 2026-08-26
date: 2026-08-26
---

# Bosun Pages Site + Editorial Diagrams

## Context

Bosun's Pages surface is live but orphaned: `cameronsjo.github.io/bosun` still serves the retired beadspace dashboard (its workflow was deleted in #440; the artifact was never unpublished; `build_type: workflow` is already configured). The four architecture diagrams live as Mermaid in `docs/diagrams/*.mmd`, rendered to README ASCII by `scripts/diagrams/render.mjs` (`make diagrams`).

The 2026-08-26 diagram handoff flagged a divergence in `reconcile-pipeline.mmd` between `main` and the stale branch `docs/436-refresh-diagrams`. **Live-state review settled it: `main` is now correct** — commit `038b5dc` (2026-08-26, "docs: align reconcile diagram with deployed pipeline") superseded PR #563 with the accurate ordering (compose → health gate → hooks → local health+drift verify → staging finalize), matching this session's code-read of `Run()` (`internal/reconcile/reconcile.go:382`). The stale branch tip (`4682053`) carries zero content beyond main (`git diff origin/main...4682053` empty). The handoff doc itself is committed on `docs/diagram-handoff` with open PR #613 — not copied here.

Cameron's direction (AskUserQuestion, this session): **Astro** (raw, not Starlight), **dual-layer diagrams** ("editorial for humans, mermaid for bots"), **webui's Maritime Command Center tokens** (`webui/src/index.css`) as the one design system, site content = landing + six core docs + ADR index + diagrams page, stale Pages content left until the new site replaces it.

## Goal

Ship a nautically themed, `/design`-polished GitHub Pages site for bosun plus an editorially redrawn diagram set skinned by a shared `bosun` style profile — with the reconcile-diagram question closed and the stale `docs/436-refresh-diagrams` branch deleted.

## Alternatives declined

- **MkDocs Material / Docusaurus / hand-rolled HTML** — declined at AskUserQuestion; Astro chosen for full theming control, zero runtime.
- **Starlight** — declined at AskUserQuestion; recognizable-theme objection (same reason MkDocs lost), small site makes hand-rolled nav cheap.
- **Editorial diagrams replace Mermaid** — declined; dual-layer: `.mmd` + README ASCII stay the bot/terminal layer.
- **Icon-prompt / mascot-style-guide palettes** — declined at AskUserQuestion; webui tokens win as the one implemented system (two consumers, no third palette).

## Panel

Panel: plan-reviewer ×2, red-team-reviewer, security-posture-reviewer, cameron-review ran — 31 findings, 27 folded in, 4 declined (see Panel review — findings declined)

## Panel review — findings declined

- **[Task 5] cameron-review: consider no-build plain-HTML site (single-binary bias)** — declined: Astro settled at AskUserQuestion; the bias governs the shipped Go binary, and webui is precedent for repo-side web tooling.
- **[Task 2] plan-reviewer: README ASCII regeneration could churn other marker blocks** — declined: `render.mjs` fails loudly on marker mismatch and rewrites only between markers; the diff is reviewed at commit.
- **[Task 6] plan-reviewer: bound the /design pass** — declined: deliberately interactive; exit is Cameron calling it done, judged against the webui's rendered look as reference.
- **[Plan-wide] plan-reviewer: OpenSpec change proposal required (spec-before-code)** — declined per owner-lens ruling: the site is a docs deliverable changing no behavior covered by `openspec/specs/`; the one pipeline-shaped addition (`pages.yml`) gets a dedicated security review task (Task 8) instead. Surfaced for Cameron's veto at approval.

## Architecture

- **Design system**: `webui/src/index.css` (Maritime Command Center — Playfair Display / IBM Plex Sans / IBM Plex Mono; parchment `#f8f5f0` light, ocean `#0a1628` dark, brass/navy/ocean accents) is the single source. Two derived consumers: `site/src/styles/tokens.css` and the diagram-design profile — its body committed at `docs/styles/diagram-profile.md` (canonical, versioned), cached at `~/.diagram-design/profiles/bosun.md`, bound by a repo `.diagram-design` marker (`profile: bosun`). Mascot rim-light (cyan/amber) harmonized, not adopted.
- **Diagrams**: corrected `.mmd` (bot layer) → `diagram-design:import-mermaid` → editorial HTML → `export-diagram --svg-only` → `.svg` beside sources (human layer, committed — site build never runs the skill). No PNG layer: it had no consumer, and SVG-only drops the Playwright dependency. A staleness check (`make diagrams-check`) compares each export's embedded `.mmd` content hash against the live source in CI.
- **Site**: raw Astro under `site/`, static output, `site: 'https://cameronsjo.github.io'`, `base: '/bosun'`, all internal links via `import.meta.env.BASE_URL`. A build-time sync script copies an **explicit allowlist** (six docs + `docs/adr/*`) into the content collection, deriving `title` frontmatter from each first H1 and rewriting/pruning cross-doc links that leave the inventory. Diagrams page embeds the SVGs. Fonts self-hosted woff2 — no runtime third-party fetches.
- **Deploy**: `pages.yml` — build-only job on `pull_request` (paths: `site/**`, `docs/diagrams/**`, the allowlisted docs); build+deploy on `main` push (same paths) + `workflow_dispatch` (rollback: re-run from a known-good ref). Deploy job carries `pages: write` + `id-token: write` job-scoped, `environment: github-pages` (its existing main-only deployment branch policy preserved), `concurrency: pages` with `cancel-in-progress: false`, `upload-pages-artifact` `path: site/dist`. First deploy overwrites the stale beadspace artifact (inventoried first, per Cameron: leave until replaced).

## Tech Stack

- Astro ≥5 (exact version pinned at scaffold), pnpm via `packageManager` pin, committed `site/pnpm-lock.yaml`, Node 22 (matches `webui.yml`)
- diagram-design plugin 2.6.6 (`doctor` → `profile` → `import-mermaid` → `export-diagram --svg-only`; Playwright not required for SVG)
- `beautiful-mermaid` ASCII pipeline unchanged (`make diagrams`)
- GitHub Actions: official Pages actions pinned to **full commit SHAs** (repo CI standard), `internal/workflowcontract` coverage extended to `pages.yml`

## Global Constraints

- Branch-mode repo: all work in the worktree, pushed `-u`, draft PR at entry. Peer session live on `main` in the primary checkout — no branch switches there, explicit-path staging, `git commit -- <paths>`.
- `CLAUDE.md` is a symlink — stage `AGENTS.md`. `AGENTS.md` + `llms.txt` project-structure/workflow inventories are updated in this PR (site/, pages.yml).
- One squash subject, set as the PR title: `docs: add pages site and editorial diagrams` — a `feat:`/`fix:` prefix would mint a phantom binary release.
- No manual `CHANGELOG.md` edits (release-please-managed); no `[Unreleased]` entry — no binary-consumer-visible change (stated, not silently skipped).
- `.mmd` files stay ASCII-renderable: no styling/classDefs; `subgraph` is safe (architecture.mmd precedent).
- Content collection is an explicit allowlist in `site/src/content.config.*` — never a directory glob; `docs/security.md`'s attack-scenarios section is a deliberate inclusion rendered with its mitigations.
- Site is offline-deterministic at runtime: self-hosted fonts, no CDN CSS/JS.
- Root `.gitignore` gains `site/node_modules/`, `site/dist/`, `site/.astro/`.

## Orchestrator

Skill-driven diagram work and the interactive `/design` pass stay in-session; the site scaffold dispatches to a fresh worker; the security review dispatches Opus-tier.

**Driver:** fable — skill-driven diagram redraw and interactive /design iteration require the session model in-context

---

## Tasks

### Task 1 — Worktree entry + plan persist

**Files:** Create worktree `.claude/worktrees/pages-and-editorial-diagrams/` (branch `plan/pages-and-editorial-diagrams`); create `docs/plans/2026-08-26-pages-and-editorial-diagrams.md` (this plan).

**Interfaces:** Consumes none. Produces the branch + draft PR all later tasks commit to.

**Dispatch:** In-context · serial (first) · **Report:** —

**Steps:**
- [ ] `EnterWorktree` → push `-u`, draft PR titled `docs: add pages site and editorial diagrams`
- [ ] Persist plan; commit (producer tuple); push

### Task 2 — Reconcile-diagram residuals + retire the stale branch

**Files:** Modify `docs/diagrams/reconcile-pipeline.mmd` (residual edits only, if adopted); `README.md` (regen if `.mmd` changed); `skills/onboard/resources/gitops.md` (only if step numbering shifts — parity is a repo MUST).

**Interfaces:** Consumes Task 1 branch. Produces the settled `.mmd` set Task 4 imports.

**Dispatch:** In-context · serial (after Task 1) · **Report:** —

**Steps:**
- [ ] `git fetch origin`; confirm baseline: `origin/main`'s diagram already carries `038b5dc`'s corrected ordering — do NOT start from the stale branch
- [ ] Evaluate residual edits from the code-read against current main: (a) split `DeployGate`'s No-exit into clean-skip (breaker reset) vs breaker-trip (alert + error return), (b) explicit Health-Gate-failure → rollback edge (today encoded in the node label), (c) per-target deploy-sync invariant as a Deploy subgraph. Adopt only what improves accuracy; a no-op verdict is a valid outcome recorded in the plan
- [ ] If `.mmd` changed: `make diagrams`; verify only marker spans move in README; check onboard-skill numbering parity; commit
- [ ] Archive the stale branch's file to a temp path, then gate: `git diff origin/main...origin/docs/436-refresh-diagrams` must be **empty** (content evidence — the PR #563 record does not cover today's tip `4682053`); only then `git push origin --delete docs/436-refresh-diagrams --no-follow-tags`

### Task 3 — bosun diagram profile from webui tokens

**Files:** Create `docs/styles/diagram-profile.md` (canonical, committed), `.diagram-design` (marker: `profile: bosun`); home-dir cache `~/.diagram-design/profiles/bosun.md` (untracked by design — the committed copy is canonical).

**Interfaces:** Consumes Task 1 branch + `webui/src/index.css` token values. Produces the profile Tasks 4–5 inherit; its token table is the palette contract written into Task 5's dispatch prompt.

**Dispatch:** In-context · serial (after Task 1, parallel with Task 2) · **Report:** —

**Steps:**
- [ ] Run `diagram-design:doctor`; PASS/WARN acceptable (Playwright warn OK — SVG-only)
- [ ] Derive style guide from webui tokens (parchment/ocean papers, brass accent, WCAG AA ink/paper, ≤3 font families); `profile save bosun`; copy profile body → `docs/styles/diagram-profile.md`
- [ ] Commit profile doc + marker
- [ ] Verify: `diagram-design:profile show bosun` matches the committed doc

### Task 4 — Editorial redraw (SVG layer) + staleness guard

**Files:** Create `docs/diagrams/{pipeline-overview,architecture,reconcile-pipeline,locking-singleflight}.{html,svg}`; create `scripts/diagrams/check-exports.mjs`; modify `Makefile` (`diagrams-check`), `.github/workflows/ci.yml` (staleness step).

**Interfaces:** Consumes Task 2's settled `.mmd`, Task 3's profile. Produces the SVGs the site embeds.

**Dispatch:** In-context · serial (after Tasks 2+3) · **Report:** —

**Steps:**
- [ ] `diagram-design:import-mermaid` per file — size `doc-wide`, audience `mixed`; detail: `simplified` (pipeline-overview), `balanced` (architecture, locking-singleflight), `faithful` with zoning (reconcile-pipeline, ≤24 nodes); report each fidelity ledger; embed source `.mmd` sha256 in each HTML colophon
- [ ] `diagram-design:export-diagram --svg-only` per HTML
- [ ] `check-exports.mjs`: recompute each `.mmd` hash vs the colophon hash; wire `make diagrams-check` + CI step; verify it goes red on a mutated `.mmd`
- [ ] Commit 8 artifacts + guard

### Task 5 — Astro site scaffold

**Files:** Create `site/` (astro.config.mjs with `site`/`base`, package.json with `packageManager`, pnpm-lock.yaml, src/pages, src/layouts, src/styles/tokens.css, src/content.config.*, scripts/sync-docs.mjs, public/fonts, public mascot/icon/favicon from `assets/icon.png` + `docs/mascot/bosun-reference-nobg.png`); modify `.gitignore`, `.github/dependabot.yml` (npm/`/site` entry — lands with the scaffold, not after).

**Interfaces:** Consumes Task 4 SVGs + Task 3's token table (values written into the dispatch prompt). Produces `pnpm build` green static site.

**Dispatch:** Fresh Sonnet subagent — spec'd scaffold; dispatch prompt carries: token values, font files list, the six-doc + ADR allowlist, `base: '/bosun'` + `BASE_URL` rule, H1-title derivation, link-rewrite rules (rewrite in-inventory links, prune out-of-inventory incl. `docs/gitops.md`'s `../skills/...` link), diagrams-page embed paths, `.gitignore` + dependabot edits

**Report:** `<reports-dir>/task-5.md` — concrete `mktemp -d` path minted at dispatch; reply `done` + path

**Steps:**
- [ ] Scaffold + config (pinned Astro version, `site`/`base`, static output)
- [ ] Sync script: allowlist copy of six docs + `docs/adr/*` with derived frontmatter + link rewrite; wired into build
- [ ] Pages: landing (mascot hero, nautical glossary, install/quickstart), docs section, ADR index, diagrams page (SVG embeds + alt text)
- [ ] Token sheet from webui values (light + dark); self-hosted woff2
- [ ] `pnpm build` green; internal-link check green (no `/…` links bypassing base); commit

### Task 6 — /design polish pass (interactive)

**Files:** Modify `site/src/**`.

**Interfaces:** Consumes Task 5 scaffold. Produces Cameron-approved visuals; reference surface: webui's rendered look (comparison, not taste-from-scratch).

**Dispatch:** In-context · serial · interactive via `/design`; exit when Cameron calls it done · **Report:** —

**Steps:**
- [ ] Iterate hero, type, spacing, diagram presentation with Cameron; commit per accepted iteration
- [ ] Verify: `pnpm build` still green after final iteration

### Task 7 — Pages workflow + contract coverage + doc inventory updates

**Files:** Create `.github/workflows/pages.yml`, `internal/workflowcontract/pages_test.go`; modify `AGENTS.md`, `llms.txt`, `README.md` (site link).

**Interfaces:** Consumes Task 5 build. Produces the deploy pipeline + updated inventories.

**Dispatch:** In-context · serial (after Task 6) · **Report:** —

**Steps:**
- [ ] `pages.yml`: SHA-pinned actions; workflow-level `permissions: contents: read`; build-only job on `pull_request` (paths: `site/**`, `docs/diagrams/**`, allowlisted docs); build+deploy on `main` push (same paths) + `workflow_dispatch`; deploy job: `pages: write` + `id-token: write` job-scoped, `environment: github-pages`, `concurrency: pages` + `cancel-in-progress: false`, artifact `path: site/dist`, `pnpm install --frozen-lockfile`; header comment documents rollback (dispatch from known-good ref)
- [ ] `pages_test.go`: assert workflow-level perms read-only, write scopes only on deploy job, `on:` exactly main-push + dispatch + pull_request-build-only, deploy binds `environment: github-pages`; `make test-workflows` green
- [ ] Verify live Pages config (`build_type: workflow`, main-only deployment branch policy — already enabled, no enablement step); inventory current stale beadspace content for the record (left live until replaced, per Cameron)
- [ ] Update `AGENTS.md` + `llms.txt` (site/, pages.yml, diagrams layer) — stage `AGENTS.md`; README site link
- [ ] Verify: PR checks show the build-only job green

### Task 8 — Security review + ship

**Files:** none new (review + PR mechanics).

**Interfaces:** Consumes Tasks 4–7 artifacts. Produces the reviewed, ready PR.

**Dispatch:** Security review → `cadence-forge:security-reviewer` (Opus-tier; fallback `/security-review` or Opus reviewer with the file set — never below Opus). Rest in-context.

**Report:** `<reports-dir>/task-8-security.md` — concrete path at dispatch

**Steps:**
- [ ] Security review of the Pages control's file set: `pages.yml`, `dependabot.yml`, `internal/workflowcontract/`, `site/package.json` + lockfile + `astro.config.mjs` + `content.config.*`, the live `github-pages` environment policy (re-probe), `docs/security.md` as admitted by the allowlist; fold findings
- [ ] run `cadence-forge:polish`; fold findings (diff-based reviewers against the worktree — polish's built-in arms are cwd-blind to sub-worktrees)
- [ ] Flip PR ready; squash subject = PR title `docs: add pages site and editorial diagrams`; merge; watch first Pages deploy replace the beadspace artifact; verify live URL serves landing + diagrams under `/bosun/`

---

## Verification

- Task 2: residual-edit verdict recorded; if changed, `make diagrams` exits 0 with only marker spans moving; branch delete gated on empty `git diff origin/main...origin/docs/436-refresh-diagrams`.
- Task 3: `profile show bosun` matches `docs/styles/diagram-profile.md`.
- Task 4: fidelity ledgers reported; `make diagrams-check` red on a mutated `.mmd`, green clean; SVGs render in a browser.
- Task 5: `pnpm build` green; no base-bypassing absolute links; dependabot entry present in the same commit.
- Task 7: `make test-workflows` green including `pages_test.go`; PR build-only job green pre-merge.
- Task 8: security findings folded or explicitly surfaced; post-merge live URL check under `/bosun/`.

## Deviations

## Learnings

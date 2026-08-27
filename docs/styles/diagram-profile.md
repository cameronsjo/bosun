<!-- diagram-design-profile
name: Bosun
slug: bosun
source-url: none
created: 2026-08-26
updated: 2026-08-26
notes: Maritime Command Center — derived from webui/src/index.css tokens
-->
# Style Guide

**The single source of truth for colors, typography, and tokens.** Every diagram draws from this — not from hex values inlined in other reference files. If you want to change the visual skin of Diagram Design, change this file.

This is the **bosun** skin — Maritime Command Center, derived from `webui/src/index.css` (the one implemented design system): weathered-parchment paper with navy ink in light mode, deep-ocean paper with bone ink in dark mode, brass as the single accent. The committed canonical copy lives at `docs/styles/diagram-profile.md` in the bosun repo; the home-dir cache at `~/.diagram-design/profiles/bosun.md` is a derived copy.

---

## Tokens

### Semantic roles

Every token is referred to by **semantic role**, not by its hex value. Type references (`type-*.md`) and SKILL.md say `accent`, not a hex.

| Role | Purpose | Default (light) | Default (dark) |
|---|---|---|---|
| `paper` | Page background, default node fill | `#f8f5f0` (parchment) | `#0a1628` (deep-ocean) |
| `paper-2` | Diagram container bg, secondary fill | `#f0ebe3` | `#162440` |
| `ink` | Primary text, primary stroke | `#1a2744` (navy) | `#e8e4dc` (bone) |
| `muted` | Secondary text, default arrow stroke | `#4a5568` | `#a8b4c4` |
| `soft` | Sublabels, boundary labels | `#718096` | `#6b7a8f` |
| `rule` | Hairline borders | `rgba(26,39,68,0.12)` | `rgba(232,228,220,0.12)` |
| `rule-solid` | Stronger borders, baselines | `#d4c9b8` (weathered-sand) | `#2a3a54` |
| `accent` | Focal / 1–2 max per diagram | `#b8860b` (brass) | `#daa520` (bright-brass) |
| `accent-tint` | Fill for accent-bordered boxes | `rgba(184,134,11,0.08)` | `rgba(218,165,32,0.10)` |
| `link` | HTTP/API calls, external arrows | `#1a535c` (ocean) | `#4a90a4` (harbor-blue) |

> **Brand palette source:** `webui/src/index.css` (Maritime Command Center). Direct mappings: `paper` = `--color-bg-primary`, `paper-2` = `--color-bg-tertiary`, `ink` = `--color-text-primary`, `muted` = `--color-text-secondary`, `soft` = `--color-text-muted`, `rule-solid` = `--color-border`, `accent` = `--color-brass` (dark mode uses the dark theme's `--color-brass`), `link` = `--color-ocean` light / `--color-info` dark. `rule` and `accent-tint` are ink/brass at opacity, following the shipped skin's derivation pattern. The webui mascot's cyan/amber rim-light is harmonized (brass covers amber; ocean covers cyan), not adopted as extra hues.

### Inversion rule (light → dark)

Any `rgba(26,39,68, X)` in light becomes `rgba(232,228,220, X)` in dark. Same opacities, RGB flipped. The brass accent hue-shifts brighter (`#b8860b` → `#daa520`) to read on dark paper — the same shift the webui's dark theme makes.

### Series palette (multi-series chart types only)

Inherited unchanged from the shipped skin — onboarding does not customize series colors, and no bosun diagram is multi-series today. The "1-focal" rule still holds: `accent` (brass) is reserved for the focal series.

| Token | Light | Dark | Notes |
|---|---|---|---|
| `series-1` | `#7c8f6f` (sage) | `#9caf8f` | Non-focal series |
| `series-2` | `#5e7a9b` (dusty-blue) | `#82a0c0` | Non-focal series |
| `series-3` | `#b8915a` (mustard) | `#d3ad7a` | Non-focal series |
| `series-4` | `#9c6b50` (rust-brown) | `#b88670` | Non-focal series |
| `series-5` | `#6e6479` (slate) | `#8d8298` | Non-focal series |

Fills sit at `0.18` opacity light, `0.22` dark; strokes use the full color. **Don't backfill these tokens to non-chart types** — architecture, swimlane, etc. continue to use muted-ink variants.

### Terminal skin (opt-in alternate)

Inherited unchanged from the shipped skin (see the plugin's `primitive-terminal.md`) — a self-contained CLI-chrome register, unaffected by this profile.

| Token | Hex | Purpose |
|---|---|---|
| `terminal-page` | `#0a0a0a` | Page background behind the window |
| `terminal-paper` | `#141414` | Window body, node fill |
| `terminal-bar` | `#1b1b1b` | Titlebar strip |
| `terminal-border` | `#2b2b2b` | Window border, hairlines |
| `terminal-ink` | `#f5f5f5` | Primary text, primary stroke |
| `terminal-muted` | `#9a9a9a` | Secondary text, sublabels, ring stroke |
| `terminal-soft` | `#5c5c5c` | Tertiary — inactive dots, spokes |
| `terminal-accent` | `#ff5a36` | The one accent — focal station, prompt sign, active dot |
| `terminal-accent-tint` | `rgba(255,90,54,0.12)` | Fill for accent-bordered boxes |

**1-accent rule still holds.** Never introduce a second hue.

---

## Typography

| Role | Family | Size | Weight | Usage |
|---|---|---|---|---|
| `title` | Playfair Display | 1.75rem | 500 | Page H1 |
| `node-name` | IBM Plex Sans | 12px | 600 | Human-readable labels |
| `sublabel` | IBM Plex Mono | 9px | 400 | Port, protocol, URL, field type |
| `eyebrow` | IBM Plex Mono | 7–8px | 500, tracked 0.18em, uppercase | Type tags, axis labels |
| `arrow-label` | IBM Plex Mono | 8px | 400, tracked 0.06em | Arrow annotations |
| `callout` | Playfair Display *italic* | 14px | 400 | Editorial asides only |

### Font stack

```html
<link href="https://fonts.googleapis.com/css2?family=Playfair+Display:ital,wght@0,400;0,500;0,600;1,400&family=IBM+Plex+Sans:wght@400;500;600&family=IBM+Plex+Mono:wght@400;500;600&display=swap" rel="stylesheet">
```

**Load-bearing rule:** Mono is for *technical* content (ports, commands, URLs, field types). Names go in IBM Plex Sans. Page title is Playfair Display. Italic Playfair Display is reserved for annotation callouts. Exactly three families — the webui's own trio.

---

## Stroke, radius, spacing

| Token | Value | Use |
|---|---|---|
| `stroke-thin` | `0.8` | Tag-box outlines, leaf nodes |
| `stroke-default` | `1` | Most strokes |
| `stroke-strong` | `1.2` | Emphasis strokes |
| `radius-sm` | `4` | Small tags |
| `radius-md` | `6` | Node boxes |
| `radius-lg` | `8` | Containers, rings |
| `grid` | `4` | Every coord, size, and gap is divisible by 4 (hard rule) |

---

## Node type → treatment

Semantic role combinations — reference these by name in type specs.

| Type | Fill | Stroke |
|---|---|---|
| `focal` (1–2 max) | `accent-tint` | `accent` |
| `backend` | `#ffffff` (white) | `ink` |
| `store` | `ink @ 0.05` | `muted` |
| `external` | `ink @ 0.03` | `ink @ 0.30` |
| `input` | `muted @ 0.10` | `soft` |
| `optional` | `ink @ 0.02` | `ink @ 0.20` dashed `4,3` |
| `security` | `accent @ 0.05` | `accent @ 0.50` dashed `4,4` |

---

## Constraints (don't break these)

- **Contrast**: `ink` `#1a2744` on `paper` `#f8f5f0` and `#e8e4dc` on `#0a1628` both clear WCAG AA with wide margin; `muted` clears AA on `paper` in both modes for 11px+ text.
- **One accent**: brass is the accent. Ocean (`link`) is an arrow semantic, not a second accent — never use it for focal emphasis.
- **No rainbow palette**: the webui's status colors (healthy teal, warning gold, danger red) are UI-state colors and stay out of diagrams.
- **Serif + sans + mono**: Playfair Display + IBM Plex Sans + IBM Plex Mono, no more.
- **Paper is warm-neutral, not pure white**: `#f8f5f0` parchment is the light paper — never substitute `#ffffff` for the page.
- **Dot pattern is optional, not default**: when enabled it sits at ~10% opacity of `ink` on `paper`.
- **Container is clean by default**: the diagram sits directly on the page paper; the framed variant (`paper-2` bg + `rule` border + 8px radius) is opt-in.

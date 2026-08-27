# Maritime Command Center — build conventions

**Setup.** No provider is required — components are plain prop-driven React from `window.BosunUI`. Give every screen the parchment ground: set `background: var(--color-bg-primary); color: var(--color-text-primary); font-family: var(--font-body)` on your root element (add the `maritime-bg` class for the radial-wash + noise atmosphere). Dark mode is a `.dark` class on a root ancestor — the same tokens re-resolve to the deep-ocean palette; never hardcode dark colors.

**Styling idiom.** This system's design language lives in **component classes + CSS custom properties**, not a utility vocabulary. The stylesheet is compiled output — arbitrary Tailwind-style utility names will NOT resolve; use inline `style` with the tokens for layout glue. The classes:

| Family | Classes |
|---|---|
| Surfaces | `maritime-bg`, `maritime-card` (brass top-bar on hover), `maritime-card-accent` (brass bar always on) |
| Buttons | `btn-primary` (brass), `btn-secondary`, `btn-danger` |
| Status | `status-indicator` + `status-healthy` / `status-warning` / `status-danger` / `status-info` / `status-offline`, `status-active` (pulse) |
| Badges | `badge` + `badge-healthy` / `badge-warning` / `badge-danger` / `badge-info` / `badge-neutral` |
| Nav | `nav-item`, `nav-item-active` |
| Data | `data-table` (+ `cell-primary`, `cell-secondary`), `log-viewer`, `stat-value`, `stat-value-lg`, `stat-label`, `stat-sublabel` |
| Forms | `form-select`, `form-label` |
| Flourish | `alert-banner`, `rope-divider`, `compass-loader`, `font-display`, `font-mono`, `animate-fade-in`, `animate-fade-in-up` |

Key tokens (light/dark auto): `--color-bg-primary/-secondary/-tertiary`, `--color-text-primary/-secondary/-muted`, `--color-border/-border-subtle`, `--color-brass/-brass-light/-brass-dark`, `--color-navy`, `--color-ocean`, `--color-healthy/-warning/-danger/-info` (+ `-bg` tints), `--font-display` (Playfair Display — H1s only), `--font-body` (IBM Plex Sans), `--font-mono` (IBM Plex Mono — technical text, uppercase tracked eyebrows). Brass is the one accent; status colors are state, never decoration.

**Where the truth lives.** Read `styles.css` (imports `_ds_bundle.css` — every class and token above is defined there) and each component's `.prompt.md`. Components: `DarkModeToggle`, `ErrorBoundary`, `LoadingSpinner`, `LoadingState`, `OfflineBanner`.

**Idiomatic snippet:**

```jsx
const { LoadingState } = window.BosunUI;

<div className="maritime-bg" style={{ minHeight: '100vh', background: 'var(--color-bg-primary)', color: 'var(--color-text-primary)', fontFamily: 'var(--font-body)' }}>
  <div className="maritime-card-accent" style={{ maxWidth: 420, margin: '4rem auto', padding: '1.5rem' }}>
    <p className="stat-label">Crew Status</p>
    <p className="stat-value">12</p>
    <span className="badge badge-healthy">Healthy</span>
  </div>
  <LoadingState message="Hoisting the mainsail..." />
</div>
```

Known gap: `LoadingSpinner size="sm"` renders invisible (upstream CSS bug) — use the default size.

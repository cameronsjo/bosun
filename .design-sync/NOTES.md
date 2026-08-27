# design-sync notes — bosun (webui Maritime Command Center)

- The webui is an **app**, not a library: no `dist/` library entry, no `.d.ts` tree. The sync entry is the hand-kept barrel `webui/src/ds-entry.ts` — pass `--entry ./webui/src/ds-entry.ts` on every build/driver run (without it, PKG_DIR resolution fails on `node_modules/webui`). Keep the barrel in step with `webui/src/components/`.
- Components are pinned in `componentSrcMap` (discovery finds nothing — no `.d.ts`). Pages (`Dashboard`, `Containers`, `Logs`) and `App` are excluded as app-internal (data-fetching, not reusable).
- `cssEntry` is `dist/maritime.css` — a stable-name copy of Vite's hashed CSS output. `buildCmd` chains the copy; re-run it whenever webui source or Tailwind config changes.
- Fonts are remote (`@import` of Google Fonts in `index.css`) → `[FONT_REMOTE]`, loads at runtime. Deliberate; no `extraFonts` needed.
- **Upstream bug found 2026-08-27:** `LoadingSpinner size="sm"` renders invisible — `.compass-loader-sm` alone carries no `::before` content (only the base `.compass-loader` class defines the glyph). The `sm` cells were removed from the preview; fix belongs in `webui/src/index.css` (give `.compass-loader-sm::before` the content, or have the component emit both classes).

## Known render warns

- `[FONT_REMOTE]` "IBM Plex Mono", "IBM Plex Sans", "Playfair Display" — expected, remote font host serves them at runtime.

## Re-sync risks

- The `ds-entry.ts` barrel and `componentSrcMap` are both hand-enumerations — a new component in `webui/src/components/` ships only when added to **both**.
- `dist/maritime.css` is generated state: a re-sync that skips `buildCmd` ships stale CSS silently (the converter copies whatever file is there).
- Previews assume light mode; dark-mode rendering was never captured.
- Toolchain assumed: Node 24 local (webui `engines` says 22 — non-fatal warn), npm, Vite 8. Playwright chromium-headless-shell v1234 cached in ~/Library/Caches/ms-playwright.

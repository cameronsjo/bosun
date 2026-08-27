// Single source of truth for the diagram set. Consumed by render.mjs (README
// ASCII layer), export-svg.mjs and check-exports.mjs (editorial layer), and
// site/src/pages/diagrams/index.astro (site embeds). Adding or renaming a
// diagram happens here once; a consumer with its own copy of this list is the
// drift bug this module exists to prevent.
export const DIAGRAM_NAMES = [
  "pipeline-overview",
  "architecture",
  "reconcile-pipeline",
  "locking-singleflight",
];

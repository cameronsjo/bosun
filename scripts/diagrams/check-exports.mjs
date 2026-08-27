#!/usr/bin/env node
// Two gates over the editorial diagram layer, both run by `make diagrams-check`
// (wired into CI):
//
//  1. Staleness — every .svg/.html export embeds the sha256 of the .mmd it was
//     redrawn from. If a .mmd changes without a re-export, the embedded hash no
//     longer matches the live source and this check goes red.
//  2. Active content — the exported SVGs are inlined into the Pages site via
//     set:html, so they must carry no script, event handlers, foreignObject,
//     non-fragment href, or javascript: URL. The hash gate cannot see this: a
//     hand-edited SVG keeps its embedded hash valid.
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { DIAGRAM_NAMES as names } from "./manifest.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const diagramsDir = join(here, "..", "..", "docs", "diagrams");

// Active-content lint: these exports are inlined into the Pages site via
// set:html, so an SVG carrying script, event handlers, foreignObject, or
// external references must never pass — the staleness hash alone cannot see
// a hand-edited SVG (security review I-1, PR #616). The href check is
// quote-agnostic; url() catches CSS external references beyond @import.
const ACTIVE_CONTENT = [
  [/<script/i, "script element"],
  [/\son[a-z]+\s*=/i, "event-handler attribute"],
  [/<foreignObject/i, "foreignObject element"],
  [/(?:xlink:)?href\s*=\s*['"]?(?!#)\S/i, "non-fragment href"],
  [/javascript:/i, "javascript: URL"],
  [/url\(\s*['"]?\s*(?:https?:)?\/\//i, "external url() reference"],
];

// The one sanctioned external reference: the export contract's Google Fonts
// @import in the standalone SVG's <defs>. Stripped before linting so any
// OTHER url()/import trips the lint.
const FONT_IMPORT_RE =
  /<style[^>]*>\s*@import\s+url\((['"])https:\/\/fonts\.googleapis\.com\/[^)]*\1\);?\s*<\/style>/gi;

function lintActiveContent(name, ext, content) {
  let scope;
  if (ext === "svg") {
    // Lint the WHOLE file: content after </svg> would still reach set:html.
    scope = content;
    const svgOpens = (content.match(/<svg[\s>]/g) ?? []).length;
    if (svgOpens !== 1) {
      console.error(`FAIL ${name}.${ext}: expected exactly one <svg> root, found ${svgOpens}`);
      return true;
    }
    const afterClose = content.slice(content.lastIndexOf("</svg>") + "</svg>".length);
    if (afterClose.trim() !== "") {
      console.error(`FAIL ${name}.${ext}: non-whitespace content after </svg> (${afterClose.trim().slice(0, 40)})`);
      return true;
    }
  } else {
    // The HTML wrapper legitimately carries <style>/<link>; lint only the svg.
    const svgMatch = content.match(/<svg[\s\S]*?<\/svg>/);
    scope = svgMatch ? svgMatch[0] : content;
  }
  scope = scope.replace(FONT_IMPORT_RE, "");
  let bad = false;
  for (const [re, what] of ACTIVE_CONTENT) {
    const m = scope.match(re);
    if (m) {
      console.error(`FAIL ${name}.${ext}: active content in svg — ${what} (${m[0].slice(0, 40)})`);
      bad = true;
    }
  }
  return bad;
}

let failed = false;
for (const name of names) {
  const live = createHash("sha256")
    .update(readFileSync(join(diagramsDir, `${name}.mmd`)))
    .digest("hex");

  for (const ext of ["svg", "html"]) {
    const path = join(diagramsDir, `${name}.${ext}`);
    let content;
    try {
      content = readFileSync(path, "utf8");
    } catch {
      console.error(`FAIL ${name}.${ext}: export missing`);
      failed = true;
      continue;
    }
    if (lintActiveContent(name, ext, content)) {
      failed = true;
    }
    const m = content.match(/<!-- source-mmd: \S+ sha256:([0-9a-f]{64}) -->/);
    if (!m) {
      console.error(`FAIL ${name}.${ext}: no embedded source-mmd hash`);
      failed = true;
    } else if (m[1] !== live) {
      console.error(
        `FAIL ${name}.${ext}: stale — embedded ${m[1].slice(0, 12)}… vs live ${live.slice(0, 12)}… (re-run the editorial redraw + scripts/diagrams/export-svg.mjs)`,
      );
      failed = true;
    } else {
      console.log(`ok ${name}.${ext}`);
    }
  }
}

if (failed) {
  console.error("diagrams-check: FAIL — stale or active-content export (see lines above)");
  process.exit(1);
}
console.log("diagrams-check: all exports match their .mmd sources");

#!/usr/bin/env node
// Export the inline <svg> from each editorial diagram HTML to a standalone .svg
// beside it, per the diagram-design export contract: XML declaration, xmlns,
// fonts @import merged into <defs> (XML-escaped), title/desc preserved, and the
// source-mmd colophon comment carried over for the staleness check.
import { readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { DIAGRAM_NAMES as names } from "./manifest.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const diagramsDir = join(here, "..", "..", "docs", "diagrams");

const FONT_IMPORT =
  "@import url('https://fonts.googleapis.com/css2?family=Playfair+Display:ital,wght@0,400;0,500;0,600;1,400&amp;family=IBM+Plex+Sans:wght@400;500;600&amp;family=IBM+Plex+Mono:wght@400;500;600&amp;display=swap');";

let failed = false;
for (const name of names) {
  const htmlPath = join(diagramsDir, `${name}.html`);
  const html = readFileSync(htmlPath, "utf8");

  const svgMatch = html.match(/<svg[\s\S]*?<\/svg>/);
  if (!svgMatch) {
    console.error(`FAIL ${name}: no <svg> block found`);
    failed = true;
    continue;
  }
  let svg = svgMatch[0];

  const hashMatch = html.match(
    /<!-- source-mmd: (\S+) sha256:([0-9a-f]{64}) -->/,
  );
  if (!hashMatch) {
    console.error(`FAIL ${name}: no source-mmd colophon comment in HTML`);
    failed = true;
    continue;
  }

  if (!/xmlns=/.test(svg.slice(0, svg.indexOf(">")))) {
    svg = svg.replace("<svg", '<svg xmlns="http://www.w3.org/2000/svg"');
  }
  if (!/viewBox=/.test(svg.slice(0, svg.indexOf(">")))) {
    console.error(`FAIL ${name}: svg has no viewBox`);
    failed = true;
    continue;
  }

  const fontStyle = `<style>${FONT_IMPORT}</style>`;
  if (svg.includes("<defs>")) {
    svg = svg.replace("<defs>", `<defs>\n      ${fontStyle}`);
  } else {
    svg = svg.replace(/(<desc[\s\S]*?<\/desc>)/, `$1\n    <defs>${fontStyle}</defs>`);
  }

  const out = `<?xml version="1.0" encoding="UTF-8"?>\n<!-- source-mmd: ${hashMatch[1]} sha256:${hashMatch[2]} -->\n${svg}\n`;
  writeFileSync(join(diagramsDir, `${name}.svg`), out);
  console.log(`exported: ${name}.svg`);
}
process.exit(failed ? 1 : 0);

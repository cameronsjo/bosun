#!/usr/bin/env node
// Staleness check for the editorial diagram layer: every .svg/.html export
// embeds the sha256 of the .mmd it was redrawn from. If a .mmd changes without
// a re-export, the embedded hash no longer matches the live source and this
// check goes red. Run via `make diagrams-check` (wired into CI).
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const diagramsDir = join(here, "..", "..", "docs", "diagrams");
const names = [
  "pipeline-overview",
  "architecture",
  "reconcile-pipeline",
  "locking-singleflight",
];

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
  console.error("diagrams-check: STALE — editorial exports do not match .mmd sources");
  process.exit(1);
}
console.log("diagrams-check: all exports match their .mmd sources");

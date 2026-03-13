import { readFileSync, writeFileSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { renderMermaidAscii } from "beautiful-mermaid";

const __dirname = dirname(fileURLToPath(import.meta.url));
const ROOT = resolve(__dirname, "../..");

/** Map marker ID to .mmd source file (relative to repo root). */
const DIAGRAMS = [
  { id: "pipeline-overview", src: "docs/diagrams/pipeline-overview.mmd" },
  { id: "architecture", src: "docs/diagrams/architecture.mmd" },
  { id: "reconcile-pipeline", src: "docs/diagrams/reconcile-pipeline.mmd" },
];

/**
 * Preprocess Mermaid source for ASCII rendering.
 * `<br/>` tags become ` / ` since box-drawing boxes can't do multiline labels.
 */
function preprocess(content) {
  return content.replace(/<br\s*\/?>/gi, " / ");
}

/**
 * Replace content between marker comments in the README.
 * Markers: <!-- DIAGRAM:id --> ... <!-- /DIAGRAM:id -->
 */
function patchReadme(readme, id, asciiArt) {
  const open = `<!-- DIAGRAM:${id} -->`;
  const close = `<!-- /DIAGRAM:${id} -->`;
  const startIdx = readme.indexOf(open);
  const endIdx = readme.indexOf(close);

  if (startIdx === -1 || endIdx === -1) {
    throw new Error(`Marker pair not found for diagram "${id}"`);
  }

  if (startIdx >= endIdx) {
    throw new Error(`Markers out of order for diagram "${id}"`);
  }

  const before = readme.slice(0, startIdx + open.length);
  const after = readme.slice(endIdx);
  const block = `\n\`\`\`text\n${asciiArt}\n\`\`\`\n`;

  return before + block + after;
}

function main() {
  const readmePath = resolve(ROOT, "README.md");
  let readme = readFileSync(readmePath, "utf-8");
  let errors = 0;

  for (const { id, src } of DIAGRAMS) {
    try {
      const srcPath = resolve(ROOT, src);
      const raw = readFileSync(srcPath, "utf-8");
      const processed = preprocess(raw);
      const ascii = renderMermaidAscii(processed);
      readme = patchReadme(readme, id, ascii);
      console.log(`  rendered: ${id}`);
    } catch (err) {
      console.error(`  FAILED: ${id} — ${err.message}`);
      errors++;
    }
  }

  if (errors > 0) {
    console.error(`\n${errors} diagram(s) failed to render.`);
    process.exit(1);
  }

  writeFileSync(readmePath, readme);
  console.log("\nREADME.md updated.");
}

main();

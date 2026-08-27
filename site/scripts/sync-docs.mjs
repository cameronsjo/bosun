#!/usr/bin/env node
// Build-time content sync: copies an explicit allowlist of repo docs (never a
// directory glob over docs/) into the Astro content collections consumed by
// src/content.config.ts. Runs as the `prebuild` npm script and standalone
// (`node scripts/sync-docs.mjs`).
//
// - Derives `title` frontmatter from each file's first H1 and strips it from
//   the body (avoids a double H1 once the page template renders its own).
// - Rewrites cross-doc links: a link resolving to another allowlisted doc/ADR
//   becomes a site route (`${BASE}/docs/<slug>/`, `${BASE}/adr/<slug>/`); a
//   link leaving the inventory is unwrapped to plain text (label kept, link
//   dropped) so no dead relative link survives the sync.
// - BASE is imported from site.config.mjs, the same constant astro.config.mjs
//   feeds into the `base` build option, so the two can never drift apart.

import { fileURLToPath } from 'node:url';
import path from 'node:path';
import fs from 'node:fs/promises';
import { BASE as RAW_BASE } from '../site.config.mjs';

// Astro's own base handling tolerates a trailing slash; the route strings
// built below would emit `//` if BASE ever gained one. Normalize here so the
// invariant site/src/lib/base.ts protects in pages holds in synced links too.
const BASE = RAW_BASE.replace(/\/+$/, '');

const SCRIPT_DIR = path.dirname(fileURLToPath(import.meta.url));
const SITE_ROOT = path.resolve(SCRIPT_DIR, '..');
const REPO_ROOT = path.resolve(SITE_ROOT, '..');
const REPO_DOCS_DIR = path.join(REPO_ROOT, 'docs');
const REPO_ADR_DIR = path.join(REPO_DOCS_DIR, 'adr');

const OUT_DOCS_DIR = path.join(SITE_ROOT, 'src/content/docs');
const OUT_ADR_DIR = path.join(SITE_ROOT, 'src/content/adr');

// The one sanctioned allowlist. Do not replace with a glob over repo docs/.
const DOC_ALLOWLIST = [
  'commands.md',
  'concepts.md',
  'manifest-system.md',
  'gitops.md',
  'security.md',
  'troubleshooting.md',
];

const ADR_NUMBER_RE = /^(\d{4})-/;

async function readAdrFilenames() {
  const entries = await fs.readdir(REPO_ADR_DIR);
  // TEMPLATE.md (and anything else not matching NNNN-title.md) is not a real
  // ADR and is intentionally excluded — this is the one sanctioned glob site,
  // filtered to the ADR numbering convention.
  return entries.filter((name) => ADR_NUMBER_RE.test(name)).sort();
}

function extractTitleAndBody(raw) {
  const lines = raw.split('\n');
  let i = 0;
  while (i < lines.length && lines[i].trim() === '') i++;
  const h1Match = i < lines.length && /^#\s+(.+?)\s*$/.exec(lines[i]);
  if (!h1Match) {
    throw new Error('Expected a leading H1 heading');
  }
  const title = h1Match[1];
  let bodyStart = i + 1;
  while (bodyStart < lines.length && lines[bodyStart].trim() === '') bodyStart++;
  const body = lines.slice(bodyStart).join('\n');
  return { title, body };
}

function buildAllowlistMap(docFiles, adrFiles) {
  const map = new Map();
  for (const file of docFiles) {
    map.set(path.join(REPO_DOCS_DIR, file), {
      kind: 'doc',
      slug: file.replace(/\.md$/, ''),
    });
  }
  for (const file of adrFiles) {
    map.set(path.join(REPO_ADR_DIR, file), {
      kind: 'adr',
      slug: file.replace(/\.md$/, ''),
    });
  }
  return map;
}

// A `<!-- site-diagram: <name> -->` marker immediately before a fenced code
// block swaps that block for the editorial SVG on the site, while GitHub
// readers keep the ASCII (dual-layer: mermaid/ASCII for bots and terminals,
// editorial for humans). Fail-closed: an unknown name or a marker with no
// following fence aborts the sync rather than shipping a half-swapped page.
const DIAGRAM_MARKER_RE = /<!--\s*site-diagram:\s*([a-z0-9-]+)\s*-->\s*\n```[^\n]*\n[\s\S]*?\n```/g;
const REPO_DIAGRAMS_DIR = path.join(REPO_ROOT, 'docs/diagrams');
const SVG_FONT_IMPORT_RE = /<style[^>]*>\s*@import\s+url\((['"]).*?\1\);?\s*<\/style>/gi;

async function swapEditorialDiagrams(body, sourceFile) {
  const names = [...body.matchAll(/<!--\s*site-diagram:\s*([a-z0-9-]+)\s*-->/g)].map((m) => m[1]);
  if (names.length === 0) return body;

  const svgs = new Map();
  for (const name of names) {
    const raw = await fs.readFile(path.join(REPO_DIAGRAMS_DIR, `${name}.svg`), 'utf8').catch(() => {
      throw new Error(`${sourceFile}: site-diagram "${name}" has no docs/diagrams/${name}.svg`);
    });
    const svgMatch = raw.match(/<svg[\s\S]*?<\/svg>/);
    if (!svgMatch) throw new Error(`${sourceFile}: docs/diagrams/${name}.svg has no <svg> root`);
    const svg = svgMatch[0].replace(SVG_FONT_IMPORT_RE, '');
    if (/@import|url\(\s*['"]?\s*(?:https?:)?\/\//i.test(svg)) {
      throw new Error(`${sourceFile}: ${name}.svg carries an external reference after stripping`);
    }
    // Collapse to one line: markdown re-parses indented lines inside an HTML
    // block as a code block, which shreds a pretty-printed SVG.
    svgs.set(name, svg.replace(/\n\s*/g, ' '));
  }

  let swapped = 0;
  const out = body.replace(DIAGRAM_MARKER_RE, (whole) => {
    const name = /site-diagram:\s*([a-z0-9-]+)/.exec(whole)[1];
    swapped++;
    return `<figure class="editorial-diagram">${svgs.get(name)}</figure>`;
  });
  if (swapped !== names.length) {
    throw new Error(`${sourceFile}: ${names.length} site-diagram marker(s) but only ${swapped} followed by a fenced block`);
  }
  return out;
}

function rewriteLinks(body, sourceFilePath, allowlistMap) {
  const linkRegex = /\[([^\]]*)\]\(([^)]*)\)/g;
  return body.replace(linkRegex, (whole, label, rawUrl) => {
    const url = rawUrl.trim();

    // In-page anchors and external links are left untouched.
    if (url.startsWith('#') || /^https?:\/\//i.test(url) || url === '') {
      return whole;
    }

    const [targetPath, hash] = url.split('#');
    const resolved = path.resolve(path.dirname(sourceFilePath), targetPath);
    const target = allowlistMap.get(resolved);

    if (!target) {
      // Leaves the inventory: unwrap to plain text, never a dead link.
      return label;
    }

    const routeBase = target.kind === 'doc' ? `${BASE}/docs/${target.slug}/` : `${BASE}/adr/${target.slug}/`;
    const href = hash ? `${routeBase}#${hash}` : routeBase;
    return `[${label}](${href})`;
  });
}

async function syncDoc(file, allowlistMap) {
  const sourcePath = path.join(REPO_DOCS_DIR, file);
  const raw = await fs.readFile(sourcePath, 'utf8');
  const { title, body } = extractTitleAndBody(raw);
  const withDiagrams = await swapEditorialDiagrams(body, `docs/${file}`);
  const rewritten = rewriteLinks(withDiagrams, sourcePath, allowlistMap);
  const slug = file.replace(/\.md$/, '');
  const frontmatter = [
    '---',
    `title: ${JSON.stringify(title)}`,
    `sourcePath: ${JSON.stringify(`docs/${file}`)}`,
    '---',
    '',
  ].join('\n');
  await fs.writeFile(path.join(OUT_DOCS_DIR, `${slug}.md`), frontmatter + rewritten + '\n');
}

async function syncAdr(file, allowlistMap) {
  const sourcePath = path.join(REPO_ADR_DIR, file);
  const raw = await fs.readFile(sourcePath, 'utf8');
  const { title, body } = extractTitleAndBody(raw);
  const rewritten = rewriteLinks(body, sourcePath, allowlistMap);
  const slug = file.replace(/\.md$/, '');
  const number = ADR_NUMBER_RE.exec(file)[1];
  const frontmatter = [
    '---',
    `title: ${JSON.stringify(title)}`,
    `number: ${JSON.stringify(number)}`,
    `sourcePath: ${JSON.stringify(`docs/adr/${file}`)}`,
    '---',
    '',
  ].join('\n');
  await fs.writeFile(path.join(OUT_ADR_DIR, `${slug}.md`), frontmatter + rewritten + '\n');
}

async function clean(dir) {
  await fs.rm(dir, { recursive: true, force: true });
  await fs.mkdir(dir, { recursive: true });
}

// Repo images the site serves: copied at build time so artwork updates in the
// repo flow to the site without a hand-copy (the committed public/ copies are
// dev-server fallbacks; prebuild refreshes them).
const IMAGE_SYNC = [
  ['docs/mascot/bosun-reference-nobg.png', 'public/bosun-mascot.png'],
  ['assets/icon.png', 'public/icon.png'],
];

async function syncImages() {
  for (const [from, to] of IMAGE_SYNC) {
    await fs.copyFile(path.join(REPO_ROOT, from), path.join(SITE_ROOT, to));
  }
}

async function main() {
  const adrFiles = await readAdrFilenames();
  const allowlistMap = buildAllowlistMap(DOC_ALLOWLIST, adrFiles);

  await clean(OUT_DOCS_DIR);
  await clean(OUT_ADR_DIR);

  for (const file of DOC_ALLOWLIST) {
    await syncDoc(file, allowlistMap).catch((err) => {
      throw new Error(`sync-docs: failed on docs/${file}: ${err.message}`);
    });
  }
  for (const file of adrFiles) {
    await syncAdr(file, allowlistMap).catch((err) => {
      throw new Error(`sync-docs: failed on docs/adr/${file}: ${err.message}`);
    });
  }
  await syncImages();

  console.log(`Synced ${DOC_ALLOWLIST.length} docs, ${adrFiles.length} ADRs, ${IMAGE_SYNC.length} images.`);
}

main().catch((err) => {
  console.error(err);
  process.exitCode = 1;
});

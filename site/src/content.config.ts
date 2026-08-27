import { defineCollection, z } from 'astro:content';
import { glob } from 'astro/loaders';

// NOTE: the `glob` loaders below only see files already placed in
// src/content/{docs,adr} by scripts/sync-docs.mjs (a build-time `prebuild`
// step). The allowlist of which repo docs get synced lives entirely in that
// script — never widen it by pointing a loader at the repo's own docs/.
const docs = defineCollection({
  loader: glob({ pattern: '**/*.md', base: './src/content/docs' }),
  schema: z.object({
    title: z.string(),
    sourcePath: z.string(),
  }),
});

const adr = defineCollection({
  loader: glob({ pattern: '**/*.md', base: './src/content/adr' }),
  schema: z.object({
    title: z.string(),
    number: z.string(),
    sourcePath: z.string(),
  }),
});

export const collections = { docs, adr };

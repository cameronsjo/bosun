import { defineConfig } from 'astro/config';
import { SITE, BASE } from './site.config.mjs';

export default defineConfig({
  site: SITE,
  base: BASE,
  output: 'static',
  markdown: {
    // Deep-ocean ground for code blocks — matches the Maritime dark palette
    // (the site's own pre/.astro-code chrome adds the brass hairline).
    shikiConfig: { theme: 'material-theme-ocean' },
  },
});

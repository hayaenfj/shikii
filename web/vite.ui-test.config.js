// For the page behavior check (scripts/check-ui.mjs): builds test/ui-entry.js into one file for the browser (client). mount cannot be used with --ssr
import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
export default defineConfig({
  plugins: [svelte()],
  logLevel: 'warn',
  build: { outDir: '.ui-test', emptyOutDir: true, minify: false, lib: { entry: 'test/ui-entry.js', formats: ['es'], fileName: 'ui-entry' } },
});

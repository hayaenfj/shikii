// Build settings for shikii's page.
// - base './': it is served from the Go executable, so references from index.html are relative
// - output names are fixed, without a hash (CI checks with git diff that "the committed dist matches the source")
// - CSS is Tailwind CSS v4 (@tailwindcss/vite). Not minified (easier to check the original look and to match text in tests)
// - during development (npm run dev), /v1/ is forwarded to the local shikii serve (:8095)
import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  base: './',
  plugins: [tailwindcss(), svelte()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    cssMinify: false,
    modulePreload: { polyfill: false },
    rollupOptions: {
      output: {
        entryFileNames: 'assets/[name].js',
        chunkFileNames: 'assets/[name].js',
        assetFileNames: 'assets/[name][extname]',
      },
    },
  },
  server: {
    proxy: { '/v1': 'http://127.0.0.1:8095' },
  },
});

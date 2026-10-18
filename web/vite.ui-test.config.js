// 画面の振る舞いの検査(scripts/check-ui.mjs)用: test/ui-entry.js をブラウザ向け(client)に 1 ファイルに組む。--ssr では mount が使えない
import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
export default defineConfig({
  plugins: [svelte()],
  logLevel: 'warn',
  build: { outDir: '.ui-test', emptyOutDir: true, minify: false, lib: { entry: 'test/ui-entry.js', formats: ['es'], fileName: 'ui-entry' } },
});

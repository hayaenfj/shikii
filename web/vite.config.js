// shikii の画面のビルド設定。
// - base './': Go の実行ファイルから配るので、index.html からの参照は相対にする
// - 出力の名前はハッシュを付けず固定にする(CI が「commit した dist と元が一致する」ことを git diff で検査するため)
// - CSS は Tailwind CSS v4(@tailwindcss/vite)。圧縮しない(元の見た目の確認とテストの文字の照合がしやすい)
// - 開発中(npm run dev)は /v1/ を手元の shikii serve(:8095)に流す
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

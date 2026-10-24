#!/usr/bin/env node
// 画面の振る舞いの検査(npm run check。CI でも回す): test/ui-entry.js を vite で組み、jsdom の中で動かす。
// Node 24 では navigator / localStorage が getter なので、代入ではなく defineProperty で差し替える。
import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { JSDOM } from 'jsdom';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const web = join(dirname(fileURLToPath(import.meta.url)), '..');
execFileSync('npx', ['vite', 'build', '--config', 'vite.ui-test.config.js'], { cwd: web, stdio: ['ignore', 'ignore', 'inherit'] });

// 見た目の退行の最小の検査(監査 B、0.8.1): 配る CSS(dist/assets/index.css。CI は元から組み直した物と一致することを別に見る)に、
// 幅ごとの上書き(3 つの区切り)・暗色・自信の輪・動きの抑制・hover が残っていること。文字の有無を見る Go のテストはこれを見ていない
const css = readFileSync(join(web, 'dist', 'assets', 'index.css'), 'utf8');
for (const must of ['(min-width: 520px)', '(min-width: 800px)', '(min-width: 1100px)', 'prefers-color-scheme: dark', 'conic-gradient(', 'prefers-reduced-motion', '(hover: hover)']) {
  if (!css.includes(must)) { console.log('check-ui: FAIL (配る CSS に ' + must + ' が無い)'); process.exit(1); }
}
console.log('check-ui: ok (配る CSS に幅の区切り 3 つ・暗色・自信の輪・動きの抑制・hover がある)');
globalThis.__shikiiCSS = css; // test/ui-entry.js の checkUtilityConflicts が utility の性質を読む

const dom = new JSDOM('<!doctype html><html><body><div id="app"></div></body></html>', { url: 'http://localhost/', pretendToBeVisual: true });
for (const k of Object.getOwnPropertyNames(dom.window)) {
  if (k in globalThis) continue;
  try { globalThis[k] = dom.window[k]; } catch (e) {}
}
for (const k of ['window', 'document', 'navigator', 'localStorage']) {
  Object.defineProperty(globalThis, k, { value: dom.window[k], configurable: true, writable: true });
}
const m = await import(join(web, '.ui-test', 'ui-entry.js'));
await m.run();

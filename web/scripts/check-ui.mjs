#!/usr/bin/env node
// 画面の振る舞いの検査(npm run check。CI でも回す): test/ui-entry.js を vite で組み、jsdom の中で動かす。
// Node 24 では navigator / localStorage が getter なので、代入ではなく defineProperty で差し替える。
import { execFileSync } from 'node:child_process';
import { JSDOM } from 'jsdom';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const web = join(dirname(fileURLToPath(import.meta.url)), '..');
execFileSync('npx', ['vite', 'build', '--config', 'vite.ui-test.config.js'], { cwd: web, stdio: ['ignore', 'ignore', 'inherit'] });

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

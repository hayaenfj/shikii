#!/usr/bin/env node
// Checks the page's behavior (npm run check; CI runs it too): builds test/ui-entry.js with vite and runs it inside jsdom.
// In Node 24, navigator / localStorage are getters, so they are replaced with defineProperty instead of assignment.
import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { JSDOM } from 'jsdom';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const web = join(dirname(fileURLToPath(import.meta.url)), '..');
execFileSync('npx', ['vite', 'build', '--config', 'vite.ui-test.config.js'], { cwd: web, stdio: ['ignore', 'ignore', 'inherit'] });

// The minimal check for visual regressions (audit B, 0.8.1): the shipped CSS (dist/assets/index.css; CI separately checks it matches a rebuild from source) must still have
// the per-width overrides (3 breakpoints), dark mode, the confidence ring, reduced motion and hover. The Go tests that look for text do not check this
const css = readFileSync(join(web, 'dist', 'assets', 'index.css'), 'utf8');
for (const must of ['(min-width: 520px)', '(min-width: 800px)', '(min-width: 1100px)', 'prefers-color-scheme: dark', 'conic-gradient(', 'prefers-reduced-motion', '(hover: hover)']) {
  if (!css.includes(must)) { console.log('check-ui: FAIL (the shipped CSS has no ' + must + ')'); process.exit(1); }
}
console.log('check-ui: ok (the shipped CSS has the 3 width breakpoints, dark mode, the confidence ring, reduced motion and hover)');
globalThis.__shikiiCSS = css; // test/ui-entry.js's checkUtilityConflicts reads the utilities' properties

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

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

// The floor for text size (0.9.2 redesign P5): every font size in the shipped CSS is at least 12px. Allowed forms: px (12 or more), rem (0.75 or more, 16px root),
// em (0.9 or more, for code inside text) and var(--…) that resolves to one of these. Any other form fails, so a new way of writing a small size is not let through
const vars = new Map([...css.matchAll(/(--[\w-]+):\s*([^;}]+)/g)].map(m => [m[1], m[2].trim()]));
const small = [];
const sizeOK = v => {
  for (let i = 0; i < 5 && /^var\(/.test(v); i++) { const m = v.match(/^var\((--[\w-]+)(?:,\s*([^)]+))?\)$/); v = (m && (vars.get(m[1]) || m[2]) || '').trim(); }
  let m;
  if ((m = v.match(/^([\d.]+)px$/))) return Number(m[1]) >= 12;
  if ((m = v.match(/^([\d.]+)rem$/))) return Number(m[1]) >= 0.75;
  if ((m = v.match(/^([\d.]+)em$/))) return Number(m[1]) >= 0.9;
  return false;
};
// inherit keeps the parent's size. The preflight's own rules for small / sub, sup (80% / 75%) are allowed only because every <small> in the page sets its size with a class (checked below)
const preflightRelative = new Set(['small', 'sub, sup']);
for (const r of css.matchAll(/([^{};]+)\{([^{}]*)\}/g)) {
  const selector = r[1].trim();
  for (const m of r[2].matchAll(/(?:^|[;\s])font-size:\s*([^;]+)/g)) { const v = m[1].trim(); if (v !== 'inherit' && !(preflightRelative.has(selector) && /^\d+%$/.test(v)) && !sizeOK(v)) small.push(selector + ' font-size:' + v); }
  for (const m of r[2].matchAll(/(?:^|[;\s])font:\s*([^;]+)/g)) { const v = m[1].trim(); if (v === 'inherit') continue; const size = v.split(/\s+/).find(p => /^[\d.]+(px|rem|em)(\/|$)/.test(p) || /^var\(/.test(p)); if (!size || !sizeOK(size.split('/')[0])) small.push(selector + ' font:' + v); }
}
{
  const { readdirSync, statSync } = await import('node:fs');
  const files = []; (function walk(d) { for (const n of readdirSync(d)) { const p = join(d, n); if (statSync(p).isDirectory()) walk(p); else if (n.endsWith('.svelte')) files.push(p); } })(join(web, 'src'));
  for (const f of files) for (const m of readFileSync(f, 'utf8').matchAll(/<(small|sub|sup)\b[^>]*>/g)) { const size = m[0].match(/text-\[(\d+)px\]/); if (!size || Number(size[1]) < 12) small.push(f.slice(web.length + 1) + ' ' + m[0].slice(0, 60)); }
}
if (small.length) { console.log('check-ui: FAIL (text smaller than 12px in the shipped CSS: ' + [...new Set(small)].join(', ') + ')'); process.exit(1); }
console.log('check-ui: ok (every font size in the shipped CSS is 12px or more)');
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

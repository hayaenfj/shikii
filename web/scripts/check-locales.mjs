#!/usr/bin/env node
// Checks that the dictionaries' keys line up (npm run check; CI runs it too).
// Exits with 1 unless every language's set of keys matches en (the fallback): it fails on missing keys (in en but not here) and on extra keys (not in en).
// It also fails unless the kind of value (string / function / array) is the same as en (a different calling style would break the page).
import { locales, fallback } from '../src/lib/i18n.js';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const base = locales[fallback];
const baseKeys = Object.keys(base).sort();
let bad = 0;
const kind = v => Array.isArray(v) ? 'array' : typeof v;

for (const [code, d] of Object.entries(locales)) {
  if (code === fallback) continue;
  const keys = new Set(Object.keys(d));
  const missing = baseKeys.filter(k => !keys.has(k));
  const extra = [...keys].filter(k => !(k in base));
  const mismatched = baseKeys.filter(k => keys.has(k) && kind(d[k]) !== kind(base[k]));
  if (missing.length) { console.error(`check-locales: ${code}: keys in ${fallback} but missing here: ${missing.join(', ')}`); bad++; }
  if (extra.length) { console.error(`check-locales: ${code}: keys not in ${fallback}: ${extra.join(', ')}`); bad++; }
  if (mismatched.length) { console.error(`check-locales: ${code}: keys whose kind of value differs from ${fallback}: ${mismatched.join(', ')}`); bad++; }
  if (typeof d.langName !== 'string' || !d.langName) { console.error(`check-locales: ${code}: langName (the name shown in the language picker) is missing`); bad++; }
}

// Unused keys: fail on en keys whose 'key' string appears nowhere in src (outside the dictionaries). Keys built dynamically go in the prefix allowlist (chip_ + kind)
const srcDir = join(dirname(fileURLToPath(import.meta.url)), '..', 'src');
const files = [];
(function walk(d) { for (const n of readdirSync(d)) { const p = join(d, n); if (statSync(p).isDirectory()) { if (n !== 'locales') walk(p); } else if (/\.(svelte|js)$/.test(n)) files.push(p); } })(srcDir);
const src = files.map(f => readFileSync(f, 'utf8')).join('\n');
const dynamicPrefixes = [];
const dynamicKeys = ['collecting', 'ready', 'stale', 'calibrated'];  // t(k.state): the values of /v1/status's keys[].state
const unused = baseKeys.filter(k => k !== 'langName' && !src.includes(`'${k}'`) && !dynamicKeys.includes(k) && !dynamicPrefixes.some(pre => k.startsWith(pre)));
if (unused.length) { console.error(`check-locales: ${fallback}: keys not used on the page: ${unused.join(', ')}`); bad++; }

if (bad) process.exit(1);
console.log(`check-locales: ok (${Object.keys(locales).length} languages, ${baseKeys.length} keys)`);

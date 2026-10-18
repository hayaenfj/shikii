#!/usr/bin/env node
// 辞書の鍵の揃いを検査する(npm run check。CI でも回す)。
// 全言語の鍵の集合が en(fallback)と一致しないと 1 で終わる: 足りない鍵(en にあって無い)も、余る鍵(en に無い)も落とす。
// 値の種類(文 / 関数 / 配列)も en と同じでないと落とす(呼び方が違えば画面が壊れるため)。
import { locales, fallback } from '../src/lib/i18n.js';

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
  if (missing.length) { console.error(`check-locales: ${code}: ${fallback} にあって無い鍵: ${missing.join(', ')}`); bad++; }
  if (extra.length) { console.error(`check-locales: ${code}: ${fallback} に無い鍵: ${extra.join(', ')}`); bad++; }
  if (mismatched.length) { console.error(`check-locales: ${code}: 値の種類が ${fallback} と違う鍵: ${mismatched.join(', ')}`); bad++; }
  if (typeof d.langName !== 'string' || !d.langName) { console.error(`check-locales: ${code}: langName(言語の選択に出す名前)が無い`); bad++; }
}

if (bad) process.exit(1);
console.log(`check-locales: ok (${Object.keys(locales).length} languages, ${baseKeys.length} keys)`);

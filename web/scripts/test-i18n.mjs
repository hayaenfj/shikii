// 言語の純粋な関数の検査(npm run check。CI でも回す)。画面の文字の有無を見る Go のテストでは捕まらない振る舞い:
// 言語の決め方(保存値 → ブラウザ → en)、無い鍵の fallback、spec の表示訳の fallback(lang → en → 生)。
import test from 'node:test';
import assert from 'node:assert/strict';
import { resolveLang, translate, localized, locales, fallback } from '../src/lib/i18n.js';

test('resolveLang: saved → navigator → fallback, unknown languages fall to en', () => {
  assert.equal(resolveLang('ja', 'en-US'), 'ja');
  assert.equal(resolveLang(null, 'ja-JP'), 'ja');
  assert.equal(resolveLang(null, 'fr-FR'), fallback);
  assert.equal(resolveLang('xx', 'ja'), fallback);
  assert.equal(resolveLang(null, undefined), fallback);
});

test('translate: a key missing in one language comes from en, a key missing everywhere is returned as is', () => {
  assert.equal(translate('ja', 'gotIt'), locales.ja.gotIt);
  const probe = { ...locales.en, onlyEn: 'only in en' };
  const saved = locales.en; locales.en = probe;
  try { assert.equal(translate('ja', 'onlyEn'), 'only in en'); } finally { locales.en = saved; }
  assert.equal(translate('ja', 'nope'), 'nope');
  assert.equal(translate('xx', 'gotIt'), locales.en.gotIt);
});

test('localized: spec display text falls lang → en → raw', () => {
  assert.equal(localized('ja', { ja: 'はい', en: 'yes' }, 'raw'), 'はい');
  assert.equal(localized('ja', { en: 'yes' }, 'raw'), 'yes');
  assert.equal(localized('ja', {}, 'raw'), 'raw');
  assert.equal(localized('ja', undefined, 'raw'), 'raw');
});

test('every language formats dates with its own locale', () => {
  const d = new Date(Date.UTC(2026, 9, 18, 0, 0, 0));
  assert.notEqual(d.toLocaleString('ja'), d.toLocaleString('en'));
});

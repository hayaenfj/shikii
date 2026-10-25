// Checks the pure language functions (npm run check; CI runs it too). Behavior the Go tests that look for text on the page do not catch:
// how the language is chosen (saved value → browser → en), the fallback for missing keys, and the fallback of the spec's display translations (lang → en → raw).
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

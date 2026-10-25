// Checks the pure language functions (npm run check; CI runs it too). Behavior the Go tests that look for text on the page do not catch:
// how the language is chosen (saved value → browser → en), the fallback for missing keys, and the fallback of the spec's display translations (lang → en → raw).
import test from 'node:test';
import assert from 'node:assert/strict';
import { resolveLang, translate, localized, locales, fallback, personQuestion, personTitle } from '../src/lib/i18n.js';

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

// 0.9.2 redesign (E1): what the page asks a person. The display question in the language, then the display title; the model's prompt (question:) only when the spec has no display at all
test('personQuestion: never the model prompt when the spec has a display', () => {
  const sp = { id: 'needs-plan', question: 'Does the AI agent need the user to resolve an outstanding choice?', titles: { ja: 'ja-title' }, questions: { ja: 'ja-question' } };
  assert.equal(personQuestion('ja', sp), 'ja-question');
  assert.equal(personQuestion('en', sp), 'ja-title');
  assert.equal(personQuestion('en', { ...sp, titles: { ...sp.titles, en: 'Should it have asked first?' } }), 'Should it have asked first?');
  assert.equal(personQuestion('en', { ...sp, questions: { ...sp.questions, en: 'Should the AI have asked you first?' } }), 'Should the AI have asked you first?');
  assert.equal(personQuestion('en', { id: 'custom', question: 'Is this spam?', titles: {}, questions: {} }), 'Is this spam?');
  assert.equal(personTitle('en', sp), 'ja-title');
  assert.equal(personTitle('ja', { id: 'x', titles: {} }), '');
});

// 0.9.2: the counts behind these texts take in truths an agent sent through MCP (source mcp) as well, so they must not call them the person's answers
test('counted truths are not called the person\'s answers', () => {
  for (const [code, words] of [['ja', /あなた/], ['en', /\byou(r)?\b/i]]) {
    const d = locales[code];
    for (const text of [d.plainLearning(3, 10), d.calibrationInsufficient(3, 10), d.plainAuto]) assert.doesNotMatch(text, words, code + ': ' + text);
  }
});

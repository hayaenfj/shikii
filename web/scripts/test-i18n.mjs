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

// 0.9.3: the same holds for every other text that counts or keeps all truths (the learning count, the wording match, the held/failed setting, redo's comparison).
// Audit 0.9.3 (test hole): an allowlist, not a list of texts to look at, so a new key cannot slip through. Every text of every dictionary is rendered
// (functions called with stand-in values) and only the keys below may speak of "your answer(s)" (in Japanese too): they are about the person's own answer
// (the answer queue, "Your answer:" on a card the person answered, changing that answer). "You need not answer" is not an answer and is not matched.
export const yourAnswerKeys = { ja: ['yourAnswer', 'answerHow'], en: ['yourAnswer', 'answerHow', 'changeAnswer'] };
const yourAnswerWords = { ja: /あなた(の答え|が答えた)/, en: /\byour answers?\b|\byou answered\b/i };

// a stand-in for any argument: a number, a string, or an object whose every field is again a stand-in (summaryRituals reads r.plan.done)
const standIn = new Proxy({}, { get: (_, k) => (k === Symbol.toPrimitive ? () => 1 : standIn) });
export function rendered(v) {
  if (typeof v === 'function') {
    for (const arg of [1, standIn]) {
      try { return String(v(...Array.from({ length: Math.max(v.length, 3) }, () => arg))); } catch { /* try the next stand-in */ }
    }
    throw new Error('a dictionary function could not be rendered with stand-in values: ' + v);
  }
  if (Array.isArray(v)) return v.map(rendered).join(' | ');
  if (v && typeof v === 'object') return Object.values(v).map(rendered).join(' | ');
  return String(v);
}

export function unlistedYourAnswers(code, dict) {
  return Object.entries(dict).filter(([k, v]) => !yourAnswerKeys[code].includes(k) && yourAnswerWords[code].test(rendered(v))).map(([k]) => k);
}

test('only the allowlisted texts say "your answers"', () => {
  for (const code of ['ja', 'en']) assert.deepEqual(unlistedYourAnswers(code, locales[code]), [], code);
  // a new key that calls the counted truths the person's answers fails (the allowlist's negative example)
  assert.deepEqual(unlistedYourAnswers('ja', { ...locales.ja, newCount: n => 'あなたの答え ' + n + ' 件' }), ['newCount']);
  assert.deepEqual(unlistedYourAnswers('en', { ...locales.en, newCount: n => n + ' of your answers' }), ['newCount']);
  // "you need not answer" is not an answer
  assert.deepEqual(unlistedYourAnswers('ja', { ...locales.ja, x: 'あなたが答える必要はありません' }), []);
  // the person's own answer keeps its name, and every allowlisted key exists and really says so
  assert.match(locales.ja.yourAnswer('はい', 'x'), /あなたの答え/);
  assert.match(locales.en.yourAnswer('yes', 'x'), /Your answer/);
  for (const code of ['ja', 'en']) for (const k of yourAnswerKeys[code]) assert.match(rendered(locales[code][k]), yourAnswerWords[code], code + '.' + k + ' is on the allowlist');
});

// 0.9.3+: when the answers cannot be told apart, the AI tries other ways of asking by itself. The page says so and that the person has nothing to do,
// instead of "could not set the point … Review the question or model" (which read as a failure the person must fix)
test('a quality refusal says the AI tries other wordings and the person has nothing to do', () => {
  const ja = locales.ja.calibrationQuality(64), en = locales.en.calibrationQuality(64);
  assert.match(ja, /64/); assert.match(en, /64/);
  assert.match(ja, /\u898b\u5206\u3051\u3089\u308c\u307e\u305b\u3093/); assert.match(ja, /\u3042\u306a\u305f\u304c\u3059\u308b\u3053\u3068\u306f\u3042\u308a\u307e\u305b\u3093/); assert.match(ja, /\u5225\u306e\u805e\u304d\u65b9/);
  assert.doesNotMatch(ja, /\u898b\u76f4\u3057\u304c\u8981\u308a\u307e\u3059|\u7f6e\u3051\u307e\u305b\u3093\u3067\u3057\u305f/);
  assert.match(en, /cannot tell the answers apart/); assert.match(en, /nothing for you to do/); assert.match(en, /other wordings/);
  assert.doesNotMatch(en, /Review the question/);
});

// 0.9.4 audit: only the hook's questions get other wordings tried. For other questions the note does not promise it, and every truth on one side
// is told as one kind of answer, not as "this wording cannot tell the answers apart"
test('the note promises other wordings only where they are tried, and tells one kind of answer apart', () => {
  const ja = locales.ja.calibrationQualityOwn(64), en = locales.en.calibrationQualityOwn(64);
  assert.match(ja, /64/); assert.match(en, /64/);
  assert.match(ja, /\u898b\u5206\u3051\u3089\u308c\u307e\u305b\u3093/); assert.doesNotMatch(ja, /\u8a66\u3057\u307e\u3059|\u3042\u306a\u305f\u304c\u3059\u308b\u3053\u3068\u306f\u3042\u308a\u307e\u305b\u3093/);
  assert.match(en, /cannot tell the answers apart/); assert.doesNotMatch(en, /will try|nothing for you to do/);
  const ja1 = locales.ja.calibrationOneAnswer(64), en1 = locales.en.calibrationOneAnswer(64);
  assert.match(ja1, /64/); assert.match(ja1, /\u7247\u65b9\u3060\u3051/); assert.doesNotMatch(ja1, /\u898b\u5206\u3051\u3089\u308c\u307e\u305b\u3093|\u5225\u306e\u805e\u304d\u65b9/);
  assert.match(en1, /64/); assert.match(en1, /only one kind of answer/); assert.doesNotMatch(en1, /tell the answers apart|other wordings/);
});
